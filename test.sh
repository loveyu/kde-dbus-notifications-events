#!/usr/bin/env bash
# test.sh — 本地测试 kde-notify-status-monitor
# 用法: bash test.sh

set -euo pipefail

BINARY="./kde-notify-status-monitor"
STATUS_DIR="/tmp/kde-notify-test"         # 测试用临时目录；生产默认为 /run/user/$UID
CREATED_FILE="$STATUS_DIR/kde-dbus-notify-created.json"
CLICKED_FILE="$STATUS_DIR/kde-dbus-notify-clicked.json"
CLOSED_FILE="$STATUS_DIR/kde-dbus-notify-closed.json"

# ── 颜色输出 ────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()    { echo -e "${CYAN}[TEST]${NC} $*"; }
success() { echo -e "${GREEN}[OK]${NC}   $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
error()   { echo -e "${RED}[ERR]${NC}  $*"; }

cleanup() {
    if [[ -n "${MONITOR_PID:-}" ]] && kill -0 "$MONITOR_PID" 2>/dev/null; then
        info "停止监听进程 PID=$MONITOR_PID"
        kill "$MONITOR_PID" 2>/dev/null || true
        wait "$MONITOR_PID" 2>/dev/null || true
    fi
}
trap cleanup EXIT

# ── 前置检查 ─────────────────────────────────────────────────────────────────
echo ""
info "═══════════════════════════════════════════════"
info " kde-notify-status-monitor 本地测试"
info "═══════════════════════════════════════════════"
echo ""

# 编译（如果二进制不存在或源码更新）
if [[ ! -f "$BINARY" ]] || [[ main.go -nt "$BINARY" ]] || \
   [[ monitor/dbus.go -nt "$BINARY" ]] || [[ monitor/handler.go -nt "$BINARY" ]]; then
    info "编译二进制..."
    CGO_ENABLED=0 go build -o "$BINARY" . && success "编译完成: $BINARY"
else
    info "使用已有二进制: $BINARY"
fi

if ! command -v notify-send &>/dev/null; then
    error "未找到 notify-send，请安装 libnotify-bin 或 libnotify"
    exit 1
fi

# 检查 KDE 环境
if [[ "${XDG_CURRENT_DESKTOP:-}" != *KDE* ]]; then
    warn "XDG_CURRENT_DESKTOP=${XDG_CURRENT_DESKTOP:-<未设置>}"
    warn "非 KDE 环境，程序会等30秒后退出。"
    warn "如需强制测试，按 Ctrl+C 跳过，或手动设置:"
    warn "  export XDG_CURRENT_DESKTOP=KDE"
    warn "  export DBUS_SESSION_BUS_ADDRESS=\$DBUS_SESSION_BUS_ADDRESS"
fi

mkdir -p "$STATUS_DIR"
rm -f "$CREATED_FILE" "$CLICKED_FILE" "$CLOSED_FILE"

# ── 启动监听（--once：收到第一个信号后退出，方便测试）───────────────────────
info "启动监听进程（--once 模式，日志级别=debug）..."
"$BINARY" \
    --status-dir "$STATUS_DIR" \
    --log-level debug \
    --once \
    &
MONITOR_PID=$!
success "监听进程 PID=$MONITOR_PID"

# 等待进程就绪（给 D-Bus 订阅一点时间）
sleep 1
if ! kill -0 "$MONITOR_PID" 2>/dev/null; then
    error "监听进程已退出（非KDE环境或D-Bus不可用？）"
    exit 1
fi

# ── 发送测试通知 ─────────────────────────────────────────────────────────────
echo ""
info "发送测试通知..."
NOTIFY_ID=$(notify-send \
    --print-id \
    --app-name "kde-notify-test" \
    --expire-time 30000 \
    --urgency normal \
    "🔔 测试通知" \
    "请在30秒内 点击 或 关闭 此通知" \
    2>/dev/null || echo "0")

if [[ "$NOTIFY_ID" -gt 0 ]]; then
    success "通知已发送，notify-send 返回 ID=$NOTIFY_ID"
else
    warn "notify-send 未返回 ID（部分版本不支持 --print-id）"
fi

# ── 等待状态文件出现 ──────────────────────────────────────────────────────────
echo ""
info "等待你 点击 或 关闭 通知（最多30秒）..."
echo ""

TIMEOUT=30
ELAPSED=0
EVENT_FILE=""
while [[ $ELAPSED -lt $TIMEOUT ]]; do
    if [[ -f "$CLICKED_FILE" ]]; then EVENT_FILE="$CLICKED_FILE"; break; fi
    if [[ -f "$CLOSED_FILE"  ]]; then EVENT_FILE="$CLOSED_FILE";  break; fi
    if ! kill -0 "$MONITOR_PID" 2>/dev/null; then
        # --once 模式下进程正常退出
        if [[ -f "$CLICKED_FILE" ]]; then EVENT_FILE="$CLICKED_FILE"; break; fi
        if [[ -f "$CLOSED_FILE"  ]]; then EVENT_FILE="$CLOSED_FILE";  break; fi
    fi
    sleep 1
    (( ELAPSED++ )) || true
    printf "\r  已等待 %2ds / %ds..." "$ELAPSED" "$TIMEOUT"
done
echo ""

# ── 结果展示 ─────────────────────────────────────────────────────────────────
echo ""
info "═══════════════════════════════════════════════"
info " 测试结果"
info "═══════════════════════════════════════════════"

show_file() {
    local label="$1" file="$2"
    if [[ -f "$file" ]]; then
        success "$label → $file"
        if command -v python3 &>/dev/null; then
            python3 -m json.tool "$file" | sed 's/^/    /'
        else
            cat "$file" | sed 's/^/    /'
        fi
    fi
}

show_file "created（通知创建）" "$CREATED_FILE"
show_file "clicked （点击）"    "$CLICKED_FILE"
show_file "closed  （关闭）"    "$CLOSED_FILE"

if [[ -z "$EVENT_FILE" ]]; then
    warn "超时，未捕获到事件（通知可能未显示或未操作）"
    exit 1
else
    echo ""
    success "测试通过 ✓"
fi
