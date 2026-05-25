package monitor

import (
"context"
"fmt"
"time"

"github.com/godbus/dbus/v5"
"github.com/loveyu/kde-dbus-notifications-events/config"
)

const (
dbusNotifyInterface = "org.freedesktop.Notifications"
dbusNotifyPath      = "/org/freedesktop/Notifications"
memberActionInvoked = "ActionInvoked"
memberClosed        = "NotificationClosed"
memberNotify        = "Notify"

maxBackoff = 30 * time.Second
)

// Run is the main monitor loop. It reconnects to D-Bus with exponential backoff
// and re-listens when the context deadline is reached (1-hour cycle).
func Run(ctx context.Context, cfg *config.Config, logger *config.Logger) error {
backoff := time.Second

for {
if ctx.Err() != nil {
return ctx.Err()
}

conn, err := connectSession(ctx, logger)
if err != nil {
return err
}

logger.Info("D-Bus Session Bus 连接成功")
backoff = time.Second // reset on successful connection

// Start optional Notify-method monitor on a separate connection.
notifyCtx, notifyCancel := context.WithCancel(ctx)
go runNotifyMonitor(notifyCtx, cfg, logger)

err = listenSignals(ctx, conn, cfg, logger)
notifyCancel()
conn.Close()

if ctx.Err() != nil {
return ctx.Err()
}

// --once: listenSignals returns nil after first event; exit cleanly.
if cfg.Once && err == nil {
return nil
}

logger.Warn(fmt.Sprintf("D-Bus监听中断: %v，%v后重连", err, backoff))
select {
case <-ctx.Done():
return ctx.Err()
case <-time.After(backoff):
backoff = backoff * 2
if backoff > maxBackoff {
backoff = maxBackoff
}
}
}
}

// connectSession connects to the session bus with exponential backoff.
func connectSession(ctx context.Context, logger *config.Logger) (*dbus.Conn, error) {
backoff := time.Second
for {
conn, err := dbus.ConnectSessionBus()
if err == nil {
return conn, nil
}
logger.Warn(fmt.Sprintf("D-Bus连接失败: %v，%v后重试", err, backoff))
select {
case <-ctx.Done():
return nil, ctx.Err()
case <-time.After(backoff):
backoff = backoff * 2
if backoff > maxBackoff {
backoff = maxBackoff
}
}
}
}

// listenSignals subscribes to ActionInvoked and NotificationClosed signals.
func listenSignals(ctx context.Context, conn *dbus.Conn, cfg *config.Config, logger *config.Logger) error {
for _, member := range []string{memberActionInvoked, memberClosed} {
call := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
fmt.Sprintf("type='signal',interface='%s',member='%s'", dbusNotifyInterface, member))
if call.Err != nil {
return fmt.Errorf("AddMatch %s 失败: %w", member, call.Err)
}
}

sigCh := make(chan *dbus.Signal, 20)
conn.Signal(sigCh)
defer conn.RemoveSignal(sigCh)

logger.Info(fmt.Sprintf("开始监听信号: %s, %s", memberActionInvoked, memberClosed))

for {
select {
case <-ctx.Done():
return ctx.Err()
case sig, ok := <-sigCh:
if !ok {
return fmt.Errorf("信号通道已关闭")
}
if sig == nil {
continue
}
dispatched := dispatchSignal(sig, cfg, logger)
if dispatched && cfg.Once {
return nil
}
}
}
}

// dispatchSignal processes a received D-Bus signal and returns true if handled.
func dispatchSignal(sig *dbus.Signal, cfg *config.Config, logger *config.Logger) bool {
ts := float64(time.Now().UnixNano()) / 1e9

switch sig.Name {
case dbusNotifyInterface + "." + memberActionInvoked:
if len(sig.Body) < 2 {
return false
}
notifyID, ok1 := sig.Body[0].(uint32)
actionKey, ok2 := sig.Body[1].(string)
if !ok1 || !ok2 {
return false
}
handleActionInvoked(notifyID, actionKey, ts, cfg, logger)
return true

case dbusNotifyInterface + "." + memberClosed:
if len(sig.Body) < 2 {
return false
}
notifyID, ok1 := sig.Body[0].(uint32)
reason, ok2 := sig.Body[1].(uint32)
if !ok1 || !ok2 {
return false
}
handleNotificationClosed(notifyID, reason, ts, cfg, logger)
return true
}
return false
}

// callInfo stores all parameters from a pending Notify method call,
// keyed by D-Bus serial so we can correlate the method return (which carries
// the assigned notify ID) with the original call parameters.
//
// Notify(app_name, replaces_id, app_icon, summary, body, actions, hints, expire_timeout)
type callInfo struct {
sender        string
appName       string
replaceId     uint32
appIcon       string
summary       string   // notification title
body          string   // notification message text
actions       []string // flat pairs: [key1, label1, key2, label2, ...]
expireTimeout int32    // ms; -1 = server default, 0 = never expire
}

// runNotifyMonitor uses BecomeMonitor on a separate connection to capture
// Notify method calls and correlate their return value (the assigned notify ID).
// It is optional: if BecomeMonitor is not permitted it logs a warning and returns.
func runNotifyMonitor(ctx context.Context, cfg *config.Config, logger *config.Logger) {
conn, err := dbus.ConnectSessionBus()
if err != nil {
logger.Warn(fmt.Sprintf("Notify监控: D-Bus连接失败: %v", err))
return
}
defer conn.Close()

rules := []string{
fmt.Sprintf("type='method_call',interface='%s',member='%s'", dbusNotifyInterface, memberNotify),
"type='method_return'",
}

if err := conn.BusObject().Call(
"org.freedesktop.DBus.Monitoring.BecomeMonitor", 0,
rules, uint32(0),
).Err; err != nil {
logger.Warn(fmt.Sprintf("无法启用Notify监控(BecomeMonitor): %v，跳过通知创建捕获", err))
return
}
logger.Info("Notify方法监控已启用（捕获通知创建事件）")

monCh := make(chan *dbus.Message, 100)
conn.Eavesdrop(monCh)

pending := make(map[uint32]callInfo)

for {
select {
case <-ctx.Done():
return
case msg, ok := <-monCh:
if !ok || msg == nil {
logger.Warn("Notify监控通道已关闭")
return
}
processMonitorMessage(msg, pending, cfg, logger)
}
}
}

func processMonitorMessage(msg *dbus.Message, pending map[uint32]callInfo, cfg *config.Config, logger *config.Logger) {
switch msg.Type {
case dbus.TypeMethodCall:
iface, _ := msg.Headers[dbus.FieldInterface].Value().(string)
member, _ := msg.Headers[dbus.FieldMember].Value().(string)
if iface != dbusNotifyInterface || member != memberNotify {
return
}
sender, _ := msg.Headers[dbus.FieldSender].Value().(string)
serial := msg.Serial()

ci := callInfo{sender: sender}
// Notify(app_name string, replaces_id uint32, app_icon string,
//        summary string, body string, actions []string,
//        hints map[string]Variant, expire_timeout int32)
if len(msg.Body) >= 5 {
ci.appName, _ = msg.Body[0].(string)
ci.replaceId, _ = msg.Body[1].(uint32)
ci.appIcon, _ = msg.Body[2].(string)
ci.summary, _ = msg.Body[3].(string)
ci.body, _ = msg.Body[4].(string)
}
if len(msg.Body) >= 6 {
ci.actions = toStringSlice(msg.Body[5])
}
if len(msg.Body) >= 8 {
ci.expireTimeout, _ = msg.Body[7].(int32)
}

pending[serial] = ci
// Guard against unbounded growth (shouldn't happen in practice).
if len(pending) > 200 {
pending = make(map[uint32]callInfo)
logger.Debug("清理pending映射")
}
logger.Debug(fmt.Sprintf("Notify方法调用: serial=%d, sender=%s, app=%q, summary=%q",
serial, sender, ci.appName, ci.summary))

case dbus.TypeMethodReply:
replySerial, _ := msg.Headers[dbus.FieldReplySerial].Value().(uint32)
dest, _ := msg.Headers[dbus.FieldDestination].Value().(string)
pc, ok := pending[replySerial]
if !ok || pc.sender != dest {
return
}
delete(pending, replySerial)
if len(msg.Body) == 0 {
return
}
notifyID, ok := msg.Body[0].(uint32)
if !ok || notifyID == 0 {
return
}
ts := float64(time.Now().UnixNano()) / 1e9
handleNotifyCreated(notifyID, ts, pc, cfg, logger)
}
}

// toStringSlice converts a raw D-Bus body value ([]string or []interface{}) to []string.
func toStringSlice(v interface{}) []string {
if ss, ok := v.([]string); ok {
return ss
}
if ii, ok := v.([]interface{}); ok {
out := make([]string, 0, len(ii))
for _, x := range ii {
if s, ok := x.(string); ok {
out = append(out, s)
}
}
return out
}
return nil
}
