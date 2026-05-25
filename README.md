# kde-notify-status-monitor

KDE 桌面通知事件监控守护程序。通过 `godbus/dbus` 原生订阅 D-Bus 信号，实时捕获 KDE 通知的**创建、点击、关闭**事件，并以原子方式写入 `/run/` 目录下的 JSON 状态文件。

## 功能

- 捕获通知**创建**（`Notify` 方法调用返回的 ID）→ `kde-dbus-notify-created.json`
- 捕获通知**点击**（`ActionInvoked` 信号）→ `kde-dbus-notify-clicked.json`
- 捕获通知**关闭**（`NotificationClosed` 信号，含超时自动关闭）→ `kde-dbus-notify-closed.json`
- 非 KDE 环境：等待 30 秒后以 0 退出
- 单实例锁（flock），防止重复运行
- D-Bus 断连后指数退避自动重连（1s → 2s → … → 30s）
- 每 1 小时重建一次 D-Bus 连接
- 运行 12 小时后自动退出（由 systemd 重启）
- 状态文件使用原子写入（临时文件 + rename）

## 状态文件格式（方案 C，仅传递 ID）

```json
{"notifyId":42,"uniqueId":"","appName":"","title":"","message":"","device":"","timestamp":1779693427.381,"action":"clicked"}
```

关闭事件额外含 `reason` 字段（1=用户主动关闭 2=超时 3=程序调用关闭）。

## 命令行参数

```
kde-notify-status-monitor [选项]

  --status-dir <path>   状态文件目录（默认 /run）
  --log-level <level>   日志级别: debug/info/warn/error（默认 info）
  --once                捕获一次信号后退出（测试用）
```

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

> `/run/` 目录默认只有 root 可写。若以普通用户运行，可将 `--status-dir` 改为 `/run/user/$UID` 或其他有写权限的目录，并相应修改 PHP 侧的读取路径。
