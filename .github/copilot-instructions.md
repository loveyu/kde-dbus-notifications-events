# Copilot 指令

## 构建与运行

```bash
# 构建
go build -o kde-notify-status-monitor .

# 交叉编译（所有 CI 目标均使用 CGO_ENABLED=0）
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o kde-notify-status-monitor-linux-arm64 .

# 本地测试（需要 KDE 桌面会话 + notify-send）
bash test.sh [close|click|all]

# 以测试/单次捕获模式运行
./kde-notify-status-monitor --once --status-dir /tmp/test-notify --log-level debug
```

没有单元测试，唯一的测试脚本是 `test.sh`。

## 架构

程序正常运行时包含三个并发 goroutine：

1. **信号监听器**（`monitor.listenSignals`）— 订阅会话总线上的 `ActionInvoked` 和 `NotificationClosed` D-Bus 信号。以 1 小时为周期运行，由 `monitor.Run` 负责重���。
2. **通知监视器**（`monitor.runNotifyMonitor`）— 使用一个**独立的** D-Bus 连接，调用 `BecomeMonitor` 窃听 `Notify` 方法调用。该连接在 `BecomeMonitor` 之后变为只读。它通过 `pending map[uint32]callInfo`（以 D-Bus 序列号为键）将方法调用与其回复进行关联，然后发出 `created` 事件。
3. **主 goroutine**（`main.go`）— 拥有 12 小时进程生命周期计时器，并监听 `SIGTERM`/`SIGINT` 信号。

### 为什么需要两个 D-Bus 连接？

`BecomeMonitor` 会将连接变为只读监视器——此后它不能再调用方法或订阅信号。信号监听器需要一个普通的读写连接。因此 `runNotifyMonitor` 始终打开自己的 `dbus.ConnectSessionBus()`。

### 事件关联（created 事件）

`Notify` 方法调用在通知服务器分配 ID **之前**到达。ID 在方法回复中返回。`processMonitorMessage` 在 `TypeMethodCall` 时将 `callInfo` 存入 `pending[serial]`，然后在 `TypeMethodReply` 时通过 `replySerial` 和原始发送者进行匹配，重构完整事件。

### 输出文件

每种事件类型覆写一个固定的文件（仅保留最新事件，非日志追加）：

| 事件 | 文件 |
|---|---|
| 通知创建 | `$STATUS_DIR/kde-dbus-notify-created.json` |
| 操作按钮点击 | `$STATUS_DIR/kde-dbus-notify-clicked.json` |
| 通知关闭 | `$STATUS_DIR/kde-dbus-notify-closed.json` |

默认 `$STATUS_DIR` 为 `/run/user/$UID/kde-notify-status`，程序启动时自动创建。文件使用原子写入（临时文件 + `os.Rename`）。

### D-Bus 信号语义

- **ActionInvoked** 仅在 `Notify` 调用中定义了明确的**操作按钮**时触发。点击通知正文会触发 `NotificationClosed(reason=2)`。
- **NotificationClosed reason 代码**：`1`=超时过期，`2`=用户关闭，`3`=应用调用 `CloseNotification` 关闭，`4`=未定义。

## 关键约定

- Git 提交中**不使用 Co-authored-by 尾注**。
- **所有日志输出到 stderr**，通过 `config.Logger`（RFC3339 时间戳，`[DEBUG/INFO/WARN/ERROR]` 前缀）。禁止直接使用 `log` 或 `fmt.Print`。
- **仅使用原子写入**：始终 `os.CreateTemp` → 写入 → `os.Rename`。禁止直接写入状态文件。
- **单实例**通过 `/run/user/$UID/kde-notify-status-monitor.lock` 上的 `syscall.Flock` 实现。
- **非 KDE 环境守卫**：检查 `XDG_CURRENT_DESKTOP` 是否包含 `"KDE"`（不区分大小写）且 `DBUS_SESSION_BUS_ADDRESS` 已设置；若不满足则休眠 30 秒后 `os.Exit(0)`（允许 systemd `Restart=on-failure` 而不会反复重启）。
- **`--once` 标志**用于测试：`listenSignals` 在首次分发事件后返回 `nil`；`Run()` 检查 `cfg.Once && err == nil` 后干净退出而非重连。
- **`toStringSlice`** 在从原始消息中读取 D-Bus `as`（字符串数组）值时必须使用——godbus 根据上下文可能返回 `[]string` 或 `[]interface{}`。
- 所有构建均禁用 CGO（`CGO_ENABLED=0`）。

## 发布

推送 `v*` 标签。GitHub Actions 自动构建全部六种 Linux 架构，并通过 `softprops/action-gh-release` 自动发布 Release。
