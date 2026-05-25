package monitor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/loveyu/kde-dbus-notifications-events/config"
)

// actionEvent is the JSON format for clicked and created notification files.
type actionEvent struct {
	NotifyID  uint32  `json:"notifyId"`
	Timestamp float64 `json:"timestamp"`
	Action    string  `json:"action"`
}

// closedEvent is the JSON format for the notification closed file.
type closedEvent struct {
	NotifyID  uint32  `json:"notifyId"`
	Timestamp float64 `json:"timestamp"`
	Reason    uint32  `json:"reason"`
}

func handleActionInvoked(notifyID uint32, ts float64, cfg *config.Config, logger *config.Logger) {
	ev := actionEvent{
		NotifyID:  notifyID,
		Timestamp: ts,
		Action:    "clicked",
	}
	path := filepath.Join(cfg.StatusDir, "kde-dbus-notify-clicked.json")
	logger.Info(fmt.Sprintf("信号捕获: member=ActionInvoked, notifyId=%d", notifyID))
	writeJSON(ev, path, notifyID, logger)
}

func handleNotificationClosed(notifyID, reason uint32, ts float64, cfg *config.Config, logger *config.Logger) {
	ev := closedEvent{
		NotifyID:  notifyID,
		Timestamp: ts,
		Reason:    reason,
	}
	path := filepath.Join(cfg.StatusDir, "kde-dbus-notify-closed.json")
	logger.Info(fmt.Sprintf("信号捕获: member=NotificationClosed, notifyId=%d, reason=%d", notifyID, reason))
	writeJSON(ev, path, notifyID, logger)
}

func handleNotifyCreated(notifyID uint32, ts float64, cfg *config.Config, logger *config.Logger) {
	ev := actionEvent{
		NotifyID:  notifyID,
		Timestamp: ts,
		Action:    "created",
	}
	path := filepath.Join(cfg.StatusDir, "kde-dbus-notify-created.json")
	logger.Info(fmt.Sprintf("通知创建: notifyId=%d", notifyID))
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
