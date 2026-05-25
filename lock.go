package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/loveyu/kde-dbus-notifications-events/config"
)

// Lock represents a held flock-based single-instance lock.
type Lock struct {
	fd   int
	path string
}

// acquireLock creates/opens a lock file and acquires an exclusive non-blocking
// flock. Returns an error if another instance is already running.
func acquireLock(logger *config.Logger) (*Lock, error) {
	path := "/tmp/kde-notify-status-monitor.lock"
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("打开锁文件失败 %s: %w", path, err)
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = syscall.Close(fd)
		return nil, fmt.Errorf("另一个实例已在运行（锁文件: %s）", path)
	}
	pid := fmt.Sprintf("%d\n", os.Getpid())
	_ = syscall.Ftruncate(fd, 0)
	_, _ = syscall.Write(fd, []byte(pid))
	logger.Debug(fmt.Sprintf("获取单实例锁: %s", path))
	return &Lock{fd: fd, path: path}, nil
}

// Release unlocks and closes the lock file descriptor.
func (l *Lock) Release() {
	_ = syscall.Flock(l.fd, syscall.LOCK_UN)
	_ = syscall.Close(l.fd)
}
