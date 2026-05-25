# kde-notify-status-monitor

KDE 桌面通知事件监控守护程序。通过 `godbus/dbus` 原生订阅 D-Bus 信号，实时捕获 KDE 通知的**创建、点击、关闭**事件，并以原子方式写入 `/run/` 目录下的 JSON 状态文件。

## 功能

- 捕获通知**创建**（`Notify` 方法调用返回的 ID）→ `/run/user/$UID/kde-notify-status/kde-dbus-notify-created.json`
- 捕获通知**点击**（`ActionInvoked` 信号）→ `/run/user/$UID/kde-notify-status/kde-dbus-notify-clicked.json`
- 捕获通知**关闭**（`NotificationClosed` 信号，含超时自动关闭）→ `/run/user/$UID/kde-notify-status/kde-dbus-notify-closed.json`
- 非 KDE 环境：等待 30 秒后以 0 退出
- 单实例锁（flock），防止重复运行
- D-Bus 断连后指数退避自动重连（1s → 2s → … → 30s）
- 每 1 小时重建一次 D-Bus 连接
- 运行 12 小时后自动退出（由 systemd 重启）
- 状态文件使用原子写入（临时文件 + rename）

## 状态文件格式

每种事件覆写一个固定文件（仅保留最新事件），使用原子写入。

**通知创建** — `kde-dbus-notify-created.json`

```json
{
  "notifyId": 42,
  "appName": "Firefox",
  "title": "下载完成",
  "message": "file.zip 已下载完成",
  "timestamp": 1779693427.381,
  "action": "created",
  "appIcon": "firefox",
  "replaceId": 0,
  "actions": [{"key": "open", "label": "打开"}, {"key": "dismiss", "label": "关闭"}],
  "expireTimeout": -1
}
```

**操作按钮点击** — `kde-dbus-notify-clicked.json`

```json
{"notifyId": 42, "timestamp": 1779693428.5, "action": "clicked", "actionKey": "open"}
```

`actionKey` 为 `"default"` 表示点击了通知正文，其他值由应用自定义。

**通知关闭** — `kde-dbus-notify-closed.json`

```json
{"notifyId": 42, "timestamp": 1779693429.1, "reason": 2, "reasonText": "dismissed"}
```

`reason` 代码：`1`=超时过期，`2`=用户关闭，`3`=程序调用关闭，`4`=未定义。

## 命令行参数

```
kde-notify-status-monitor [选项]

  --status-dir <path>   状态文件目录（默认 /run/user/$UID/kde-notify-status）
  --log-level <level>   日志级别: debug/info/warn/error（默认 info）
  --once                捕获一次信号后退出（测试用）
  --version             显示版本号并退出
```

## 架构

程序包含三个并发 goroutine：

1. **信号监听器** — 订阅 `ActionInvoked` 和 `NotificationClosed` D-Bus 信号，以 1 小时为周期运行
2. **通知监视器** — 使用独立 D-Bus 连接调用 `BecomeMonitor` 窃听 `Notify` 方法调用（`BecomeMonitor` 后连接变为只读，因此需要两个连接）
3. **主 goroutine** — 管理 12 小时进程生命周期，监听 `SIGTERM`/`SIGINT`

通知 ID 在 `Notify` 方法回复中才返回，监视器通过 D-Bus 序列号关联方法调用与回复来重构完整事件。

### D-Bus 信号语义

- **ActionInvoked** 仅在 `Notify` 调用中定义了**操作按钮**时触发，点击通知正文只会触发 `NotificationClosed(reason=2)`
- **NotificationClosed reason 代码**：`1`=超时过期，`2`=用户关闭，`3`=程序调用关闭，`4`=未定义

## 编译

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o kde-notify-status-monitor .
```

GitHub Actions 会自动构建 amd64 / arm64 / armv7 / armv6 / 386 / riscv64，推送 `v*` tag 后自动发布 Release。

## 部署（systemd user service）

```bash
# 安装二进制
sudo cp kde-notify-status-monitor /usr/local/bin/

# 安装 service 文件
cp kde-notify-status-monitor.service ~/.config/systemd/user/

# 启用并启动
systemctl --user daemon-reload
systemctl --user enable --now kde-notify-status-monitor

# 查看日志
journalctl --user -u kde-notify-status-monitor -f
```

> 默认状态目录为 `/run/user/$UID/kde-notify-status`，程序启动时自动创建。
