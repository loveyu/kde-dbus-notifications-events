package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/loveyu/kde-dbus-notifications-events/config"
	"github.com/loveyu/kde-dbus-notifications-events/monitor"
)

var version = "dev"

const relistenPeriod = 1 * time.Hour

func main() {
	cfg := &config.Config{}
	flag.StringVar(&cfg.StatusDir, "status-dir",
		fmt.Sprintf("/run/user/%d/kde-notify-status", os.Getuid()),
		"状态文件目录（默认 /run/user/$UID/kde-notify-status）")
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "日志级别: debug/info/warn/error（默认 info）")
	flag.BoolVar(&cfg.Once, "once", false, "捕获一次信号后退出（用于测试）")
	flag.Float64Var(&cfg.MaxHours, "max-hours", 0, "最大运行小时数，超时后自动退出（0=不限制）")

	showVersion := flag.Bool("version", false, "显示版本号并退出")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := config.NewLogger(cfg.LogLevel)

	// Non-KDE environment: sleep 30s then exit cleanly.
	if !isKDEEnvironment() {
		logger.Info(fmt.Sprintf("非KDE环境 (XDG_CURRENT_DESKTOP=%q)，等待30秒后退出",
			os.Getenv("XDG_CURRENT_DESKTOP")))
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}

	// Single instance enforcement via flock.
	lock, err := acquireLock(logger)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
	defer lock.Release()

	if err := os.MkdirAll(cfg.StatusDir, 0700); err != nil {
		logger.Error(fmt.Sprintf("创建状态目录失败: %v", err))
		os.Exit(1)
	}

	logger.Info(fmt.Sprintf("程序启动 PID=%d 状态目录=%s 日志级别=%s 最大运行=%s",
		os.Getpid(), cfg.StatusDir, cfg.LogLevel, maxHoursDisplay(cfg.MaxHours)))

	// Root context: cancelled on SIGTERM/SIGINT or max-hours timeout.
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	osSig := make(chan os.Signal, 1)
	signal.Notify(osSig, syscall.SIGTERM, syscall.SIGINT)

	// --once mode: single listen cycle, no timers.
	if cfg.Once {
		logger.Info("单次模式: 捕获一个信号后退出")
		go func() {
			if err := monitor.Run(rootCtx, cfg, logger); err != nil && rootCtx.Err() == nil {
				logger.Error(fmt.Sprintf("监听错误: %v", err))
			}
			rootCancel()
		}()
		select {
		case sig := <-osSig:
			logger.Info(fmt.Sprintf("收到信号 %v，退出", sig))
		case <-rootCtx.Done():
		}
		return
	}

	// Normal mode: 1-hour D-Bus re-listen cycles.
	var maxTimerChan <-chan time.Time
	if cfg.MaxHours > 0 {
		t := time.NewTimer(time.Duration(cfg.MaxHours * float64(time.Hour)))
		defer t.Stop()
		maxTimerChan = t.C
	}

	go func() {
		for {
			cycleCtx, cycleCancel := context.WithTimeout(rootCtx, relistenPeriod)
			logger.Info("启动D-Bus监听周期")
			if err := monitor.Run(cycleCtx, cfg, logger); err != nil && cycleCtx.Err() == nil && rootCtx.Err() == nil {
				logger.Warn(fmt.Sprintf("监听周期异常: %v", err))
			}
			cycleCancel()

			if rootCtx.Err() != nil {
				return
			}
			logger.Info("D-Bus监听周期结束，重新建立连接")
		}
	}()

	if maxTimerChan != nil {
		select {
		case sig := <-osSig:
			logger.Info(fmt.Sprintf("收到信号 %v，优雅退出", sig))
		case <-maxTimerChan:
			logger.Info(fmt.Sprintf("已运行%.1f小时，退出等待systemd重启", cfg.MaxHours))
		}
	} else {
		sig := <-osSig
		logger.Info(fmt.Sprintf("收到信号 %v，优雅退出", sig))
	}
	rootCancel()
}

// isKDEEnvironment returns true when XDG_CURRENT_DESKTOP contains "KDE"
// and DBUS_SESSION_BUS_ADDRESS is set.
func maxHoursDisplay(h float64) string {
	if h <= 0 {
		return "不限制"
	}
	return fmt.Sprintf("%.1fh", h)
}

func isKDEEnvironment() bool {
	desktop := os.Getenv("XDG_CURRENT_DESKTOP")
	dbusAddr := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if desktop == "" || dbusAddr == "" {
		return false
	}
	for _, d := range strings.Split(desktop, ":") {
		if strings.EqualFold(d, "KDE") {
			return true
		}
	}
	return false
}
