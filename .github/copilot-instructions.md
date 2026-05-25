# Copilot Instructions

## Build & Run

```bash
# Build
go build -o kde-notify-status-monitor .

# Cross-compile (all CI targets use CGO_ENABLED=0)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o kde-notify-status-monitor-linux-arm64 .

# Test locally (requires KDE session + notify-send)
bash test.sh [close|click|all]

# Run in test/single-shot mode
./kde-notify-status-monitor --once --status-dir /tmp/test-notify --log-level debug
```

There are no unit tests; the only test harness is `test.sh`.

## Architecture

The program has three concurrent goroutines in normal operation:

1. **Signal listener** (`monitor.listenSignals`) — subscribes to `ActionInvoked` and `NotificationClosed` D-Bus signals on the session bus. Runs in 1-hour cycles, restarted by `monitor.Run`.
2. **Notify monitor** (`monitor.runNotifyMonitor`) — a **separate** D-Bus connection that calls `BecomeMonitor` to eavesdrop on `Notify` method calls. This connection becomes read-only after `BecomeMonitor`. It correlates method calls with their replies via a `pending map[uint32]callInfo` keyed by D-Bus serial, then emits `created` events.
3. **Main goroutine** (`main.go`) — owns the 12-hour process lifetime timer and listens for `SIGTERM`/`SIGINT`.

### Why two D-Bus connections?

`BecomeMonitor` turns the connection into a read-only monitor — it can no longer call methods or subscribe to signals. The signal listener needs a normal read-write connection. Hence `runNotifyMonitor` always opens its own `dbus.ConnectSessionBus()`.

### Event correlation (created events)

`Notify` method calls arrive **before** the notification server assigns an ID. The ID arrives in the method reply. `processMonitorMessage` stores `callInfo` in `pending[serial]` on `TypeMethodCall`, then on `TypeMethodReply` matches by `replySerial` and the original sender to reconstruct the full event.

### Output files

Each event type overwrites a single fixed file (last-event semantics, not a log):

| Event | File |
|---|---|
| Notification created | `$STATUS_DIR/kde-dbus-notify-created.json` |
| Action button clicked | `$STATUS_DIR/kde-dbus-notify-clicked.json` |
| Notification closed | `$STATUS_DIR/kde-dbus-notify-closed.json` |

Default `$STATUS_DIR` is `/run/user/$UID`. Files are written atomically (temp file + `os.Rename`).

### D-Bus signal semantics

- **ActionInvoked** only fires for explicit **action buttons** defined in the `Notify` call. Clicking the notification body fires `NotificationClosed(reason=2)` instead.
- **NotificationClosed reason codes**: `1`=expired, `2`=dismissed (user), `3`=closed (app called `CloseNotification`), `4`=undefined.

## Key Conventions

- **No Co-authored-by trailers** in git commits.
- **All log output goes to stderr** via `config.Logger` (RFC3339 timestamps, `[DEBUG/INFO/WARN/ERROR]` prefix). Never use `log` or `fmt.Print` directly.
- **Atomic writes only**: always `os.CreateTemp` → write → `os.Rename`. Never write state files directly.
- **Single instance** via `syscall.Flock` on `/run/user/$UID/kde-notify-status-monitor.lock`.
- **Non-KDE guard**: check `XDG_CURRENT_DESKTOP` contains `"KDE"` (case-insensitive) and `DBUS_SESSION_BUS_ADDRESS` is set; if not, sleep 30s then `os.Exit(0)` (allows systemd `Restart=on-failure` without thrash).
- **`--once` flag** is for testing: `listenSignals` returns `nil` after the first dispatched event; `Run()` checks `cfg.Once && err == nil` and exits cleanly instead of reconnecting.
- **`toStringSlice`** is required when reading D-Bus `as` (array of strings) values from raw messages — godbus may return `[]string` or `[]interface{}` depending on context.
- CGO is disabled in all builds (`CGO_ENABLED=0`).

## Release

Push a `v*` tag. GitHub Actions builds all six Linux architectures and publishes a release automatically via `softprops/action-gh-release`.
