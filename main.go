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

const (
	maxUptime      = 12 * time.Hour
	relistenPeriod = 1 * time.Hour
)

func main() {
	cfg := &config.Config{}
	flag.StringVar(&cfg.StatusDir, "status-dir",
		fmt.Sprintf("/run/user/%d/kde-notify-status", os.Getuid()),
		"状态文件目录（默认 /run/user/$UID/kde-notify-status）")
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "日志级别: debug/info/warn/error（默认 info）")
	flag.BoolVar(&cfg.Once, "once", false, "捕获一次信号后退出（用于测试）")
	flag.Parse()

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

	logger.Info(fmt.Sprintf("程序启动 PID=%d 状态目录=%s 日志级别=%s",
		os.Getpid(), cfg.StatusDir, cfg.LogLevel))

	// Root context: cancelled on SIGTERM/SIGINT or 12-hour uptime limit.
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

	// Normal mode: 12-hour process lifetime, 1-hour D-Bus re-listen cycles.
	restartTimer := time.NewTimer(maxUptime)
	defer restartTimer.Stop()

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

	select {
	case sig := <-osSig:
		logger.Info(fmt.Sprintf("收到信号 %v，优雅退出", sig))
	case <-restartTimer.C:
		logger.Info("已运行12小时，退出等待systemd重启")
	}
	rootCancel()
}

// isKDEEnvironment returns true when XDG_CURRENT_DESKTOP contains "KDE"
// and DBUS_SESSION_BUS_ADDRESS is set.
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
