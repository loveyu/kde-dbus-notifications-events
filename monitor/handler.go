package monitor

import (
"encoding/json"
"fmt"
"os"
"path/filepath"

"github.com/loveyu/kde-dbus-notifications-events/config"
)

// Action represents a single notification action button.
type Action struct {
Key   string `json:"key"`
Label string `json:"label"`
}

// createdEvent is the JSON written when a notification is first created.
// Fields are populated from the Notify() D-Bus method call parameters.
type createdEvent struct {
// --- compatibility fields (scheme C; some filled from Notify params) ---
NotifyID  uint32  `json:"notifyId"`
UniqueID  string  `json:"uniqueId"`
AppName   string  `json:"appName"`   // from Notify.app_name
Title     string  `json:"title"`     // from Notify.summary
Message   string  `json:"message"`   // from Notify.body
Device    string  `json:"device"`
Timestamp float64 `json:"timestamp"`
Action    string  `json:"action"` // always "created"

// --- enriched fields ---
AppIcon       string   `json:"appIcon,omitempty"`   // from Notify.app_icon
ReplaceId     uint32   `json:"replaceId,omitempty"` // >0 means replaces an existing notification
Actions       []Action `json:"actions,omitempty"`   // action buttons available to the user
ExpireTimeout int32    `json:"expireTimeout"`       // ms; -1=server default, 0=never
}

// clickedEvent is the JSON written when an action button is clicked (ActionInvoked signal).
type clickedEvent struct {
// --- compatibility fields ---
NotifyID  uint32  `json:"notifyId"`
UniqueID  string  `json:"uniqueId"`
AppName   string  `json:"appName"`
Title     string  `json:"title"`
Message   string  `json:"message"`
Device    string  `json:"device"`
Timestamp float64 `json:"timestamp"`
Action    string  `json:"action"` // always "clicked"

// --- enriched fields ---
// ActionKey is the identifier of the button that was activated.
// "default" conventionally means the main body of the notification was clicked.
// Other values are application-defined (e.g. "reply", "mark-as-read", "dismiss").
ActionKey string `json:"actionKey"`
}

// closedEvent is the JSON written when a notification disappears (NotificationClosed signal).
type closedEvent struct {
// --- compatibility fields ---
NotifyID  uint32  `json:"notifyId"`
UniqueID  string  `json:"uniqueId"`
AppName   string  `json:"appName"`
Title     string  `json:"title"`
Message   string  `json:"message"`
Device    string  `json:"device"`
Timestamp float64 `json:"timestamp"`

// --- enriched fields ---
// Reason codes per freedesktop.org Desktop Notifications Specification:
//   1 = expired   — notification timed out automatically
//   2 = dismissed — user closed it (clicked X, swiped away, etc.)
//   3 = closed    — application explicitly called CloseNotification()
//   4 = undefined — implementation-specific / reserved
Reason     uint32 `json:"reason"`
ReasonText string `json:"reasonText"` // human-readable English description
}

// closeReasonText maps a NotificationClosed reason code to a short description.
func closeReasonText(reason uint32) string {
switch reason {
case 1:
return "expired"
case 2:
return "dismissed"
case 3:
return "closed"
default:
return "unknown"
}
}

// parseActions converts the flat D-Bus actions array [key1, label1, key2, label2, ...]
// into typed Action structs. Unpaired trailing keys are kept with an empty label.
func parseActions(flat []string) []Action {
out := make([]Action, 0, len(flat)/2)
for i := 0; i+1 < len(flat); i += 2 {
out = append(out, Action{Key: flat[i], Label: flat[i+1]})
}
if len(flat)%2 != 0 {
out = append(out, Action{Key: flat[len(flat)-1]})
}
return out
}

func handleActionInvoked(notifyID uint32, actionKey string, ts float64, cfg *config.Config, logger *config.Logger) {
ev := clickedEvent{
NotifyID:  notifyID,
Timestamp: ts,
Action:    "clicked",
ActionKey: actionKey,
}
path := filepath.Join(cfg.StatusDir, "kde-dbus-notify-clicked.json")
logger.Info(fmt.Sprintf("信号捕获: member=ActionInvoked, notifyId=%d, actionKey=%q",
notifyID, actionKey))
writeJSON(ev, path, notifyID, logger)
}

func handleNotificationClosed(notifyID, reason uint32, ts float64, cfg *config.Config, logger *config.Logger) {
ev := closedEvent{
NotifyID:   notifyID,
Timestamp:  ts,
Reason:     reason,
ReasonText: closeReasonText(reason),
}
path := filepath.Join(cfg.StatusDir, "kde-dbus-notify-closed.json")
logger.Info(fmt.Sprintf("信号捕获: member=NotificationClosed, notifyId=%d, reason=%d(%s)",
notifyID, reason, ev.ReasonText))
writeJSON(ev, path, notifyID, logger)
}

func handleNotifyCreated(notifyID uint32, ts float64, ci callInfo, cfg *config.Config, logger *config.Logger) {
ev := createdEvent{
NotifyID:      notifyID,
AppName:       ci.appName,
Title:         ci.summary,
Message:       ci.body,
Timestamp:     ts,
Action:        "created",
AppIcon:       ci.appIcon,
ReplaceId:     ci.replaceId,
Actions:       parseActions(ci.actions),
ExpireTimeout: ci.expireTimeout,
}
path := filepath.Join(cfg.StatusDir, "kde-dbus-notify-created.json")
logger.Info(fmt.Sprintf("通知创建: notifyId=%d, app=%q, summary=%q, actions=%d个",
notifyID, ci.appName, ci.summary, len(ev.Actions)))
writeJSON(ev, path, notifyID, logger)
}

// writeJSON atomically writes v as JSON to path (tmp file + rename).
func writeJSON(v any, path string, notifyID uint32, logger *config.Logger) {
data, err := json.Marshal(v)
if err != nil {
logger.Error(fmt.Sprintf("JSON序列化失败: %v", err))
return
}

dir := filepath.Dir(path)
tmp, err := os.CreateTemp(dir, ".kde-notify-*.tmp")
if err != nil {
logger.Error(fmt.Sprintf("创建临时文件失败: %v", err))
return
}
tmpPath := tmp.Name()

_, writeErr := tmp.Write(data)
_ = tmp.Close()
if writeErr != nil {
logger.Error(fmt.Sprintf("写入临时文件失败: %v", writeErr))
_ = os.Remove(tmpPath)
return
}

if err := os.Rename(tmpPath, path); err != nil {
logger.Error(fmt.Sprintf("写入状态文件失败 %s: %v", path, err))
_ = os.Remove(tmpPath)
return
}
logger.Debug(fmt.Sprintf("写入状态文件: %s (notifyId=%d)", path, notifyID))
}
