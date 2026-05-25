#!/usr/bin/env bash
# test.sh — 本地测试 kde-notify-status-monitor
# 用法: bash test.sh [click|close|all]
#   click  — 仅测试点击事件（ActionInvoked，需点击通知的 action 按钮）
#   close  — 仅测试关闭事件（NotificationClosed）
#   all    — 依次测试点击和关闭（默认）

set -euo pipefail

MODE="${1:-all}"
BINARY="./kde-notify-status-monitor"
STATUS_DIR="/tmp/kde-notify-test"         # 测试用临时目录；生产默认为 /run/user/$UID/kde-notify-status
CREATED_FILE="$STATUS_DIR/kde-dbus-notify-created.json"
CLICKED_FILE="$STATUS_DIR/kde-dbus-notify-clicked.json"
CLOSED_FILE="$STATUS_DIR/kde-dbus-notify-closed.json"

# ── 颜色输出 ────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; CYAN='\033[0;36m'; NC='\033[0m'
info()    { echo -e "${CYAN}[TEST]${NC} $*"; }
success() { echo -e "${GREEN}[OK]${NC}   $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
error()   { echo -e "${RED}[ERR]${NC}  $*"; }

MONITOR_PID=""
cleanup() {
    if [[ -n "$MONITOR_PID" ]] && kill -0 "$MONITOR_PID" 2>/dev/null; then
        info "停止监听进程 PID=$MONITOR_PID"
        kill "$MONITOR_PID" 2>/dev/null || true
        wait "$MONITOR_PID" 2>/dev/null || true
    fi
}
trap cleanup EXIT

# ── 前置检查 ─────────────────────────────────────────────────────────────────
echo ""
info "═══════════════════════════════════════════════"
info " kde-notify-status-monitor 本地测试 [模式: $MODE]"
info "═══════════════════════════════════════════════"
echo ""

# 编译（源码比二进制新时重新编译）
if [[ ! -f "$BINARY" ]] || [[ main.go -nt "$BINARY" ]] || \
   [[ monitor/dbus.go -nt "$BINARY" ]] || [[ monitor/handler.go -nt "$BINARY" ]]; then
    info "编译二进制..."
    CGO_ENABLED=0 go build -o "$BINARY" . && success "编译完成: $BINARY"
else
    info "使用已有二进制: $BINARY"
fi

if ! command -v notify-send &>/dev/null; then
    error "未找到 notify-send，请安装 libnotify-bin"
    exit 1
fi

# 检查 notify-send 是否支持 --action（libnotify >= 0.8）
NOTIFY_HAS_ACTION=false
if notify-send --help 2>&1 | grep -q -- '--action'; then
    NOTIFY_HAS_ACTION=true
fi

# 检查 KDE 环境
if [[ "${XDG_CURRENT_DESKTOP:-}" != *KDE* ]]; then
    warn "XDG_CURRENT_DESKTOP=${XDG_CURRENT_DESKTOP:-<未设置>}"
    warn "非 KDE 环境下程序会等30秒后退出。"
fi

mkdir -p "$STATUS_DIR"

# ── 启动监听进程 ─────────────────────────────────────────────────────────────
start_monitor() {
    kill "$MONITOR_PID" 2>/dev/null || true
    wait "$MONITOR_PID" 2>/dev/null || true
    MONITOR_PID=""
    "$BINARY" --status-dir "$STATUS_DIR" --log-level debug --once &
    MONITOR_PID=$!
    sleep 0.8   # 等 D-Bus 订阅就绪
    if ! kill -0 "$MONITOR_PID" 2>/dev/null; then
        error "监听进程已退出（非KDE环境或D-Bus不可用？）"
        exit 1
    fi
    info "监听进程 PID=$MONITOR_PID"
}

# ── 等待状态文件 ──────────────────────────────────────────────────────────────
wait_for_file() {
    local file="$1" timeout="${2:-30}" elapsed=0
    while [[ $elapsed -lt $timeout ]]; do
        [[ -f "$file" ]] && return 0
        ! kill -0 "$MONITOR_PID" 2>/dev/null && [[ -f "$file" ]] && return 0
        sleep 1
        (( elapsed++ )) || true
        printf "\r  已等待 %2ds / %ds ..." "$elapsed" "$timeout"
    done
    echo ""
    return 1
}

# ── 格式化输出 JSON ───────────────────────────────────────────────────────────
show_file() {
    local label="$1" file="$2"
    if [[ -f "$file" ]]; then
        success "$label"
        if command -v python3 &>/dev/null; then
            python3 -m json.tool "$file" 2>/dev/null | sed 's/^/    /' || cat "$file" | sed 's/^/    /'
        else
            sed 's/^/    /' "$file"
        fi
    fi
}

PASS=0; FAIL=0

# ════════════════════════════════════════════════════════════════════════════
# 测试 1：关闭事件（NotificationClosed）
# ════════════════════════════════════════════════════════════════════════════
run_close_test() {
    echo ""
    info "────────────────────────────────────────────────"
    info "测试 1/2：关闭事件（NotificationClosed）"
    info "────────────────────────────────────────────────"
    rm -f "$CREATED_FILE" "$CLOSED_FILE"

    start_monitor

    info "发送通知（请直接点击 ✕ 或划走关闭）..."
    notify-send \
        --app-name "kde-notify-test" \
        --expire-time 30000 \
        "🔔 关闭测试" \
        "请 直接关闭 此通知（点X / 划走），不要点按钮" \
        2>/dev/null &

    echo ""
    info "等待关闭操作（最多30秒）..."
    if wait_for_file "$CLOSED_FILE" 30; then
        echo ""
        show_file "created.json:" "$CREATED_FILE"
        show_file "closed.json: " "$CLOSED_FILE"
        success "关闭事件测试 PASS ✓"
        (( PASS++ )) || true
    else
        warn "超时，未捕获到关闭事件"
        (( FAIL++ )) || true
    fi
}

# ════════════════════════════════════════════════════════════════════════════
# 测试 2：点击事件（ActionInvoked）
# ════════════════════════════════════════════════════════════════════════════
run_click_test() {
    echo ""
    info "────────────────────────────────────────────────"
    info "测试 2/2：点击事件（ActionInvoked）"
    info "────────────────────────────────────────────────"
    rm -f "$CREATED_FILE" "$CLICKED_FILE"

    start_monitor

    if [[ "$NOTIFY_HAS_ACTION" == "true" ]]; then
        info "发送带 action 按钮的通知（请点击通知底部的 [✅ 确认] 按钮）..."
        notify-send \
            --app-name "kde-notify-test" \
            --expire-time 30000 \
            --action "default:✅ 确认" \
            "🔔 点击测试" \
            "请点击底部 [✅ 确认] 按钮触发 ActionInvoked 事件" \
            2>/dev/null &
    else
        warn "当前 notify-send 不支持 --action（libnotify < 0.8）"
        warn "将发送普通通知，请直接点击通知体（KDE 中点击体也可能触发 ActionInvoked）"
        notify-send \
            --app-name "kde-notify-test" \
            --expire-time 30000 \
            "🔔 点击测试" \
            "请 点击 此通知（不要关闭）" \
            2>/dev/null &
    fi

    echo ""
    info "等待点击操作（最多30秒）..."
    if wait_for_file "$CLICKED_FILE" 30; then
        echo ""
        show_file "created.json: " "$CREATED_FILE"
        show_file "clicked.json: " "$CLICKED_FILE"
        success "点击事件测试 PASS ✓"
        (( PASS++ )) || true
    else
        echo ""
        warn "超时，未捕获到点击事件（ActionInvoked）"
        warn "提示: ActionInvoked 只在点击通知的 action 按钮时触发，"
        warn "      直接关闭通知触发 NotificationClosed(reason=1)，不触发 ActionInvoked"
        (( FAIL++ )) || true
    fi
}

# ── 执行测试 ──────────────────────────────────────────────────────────────────
case "$MODE" in
    close) run_close_test ;;
    click) run_click_test ;;
    *)     run_close_test; run_click_test ;;
esac

# ── 汇总 ─────────────────────────────────────────────────────────────────────
echo ""
info "═══════════════════════════════════════════════"
if [[ $FAIL -eq 0 ]]; then
    success "全部通过 PASS=$PASS FAIL=$FAIL ✓"
else
    warn "结果: PASS=$PASS FAIL=$FAIL"
    exit 1
fi

