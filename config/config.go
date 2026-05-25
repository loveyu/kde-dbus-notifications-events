package config

import (
	"fmt"
	"os"
	"time"
)

// Config holds all program configuration.
type Config struct {
	StatusDir string
	LogLevel  string
	Once      bool
	MaxHours  float64
}

// Logger writes structured log lines to stderr.
type Logger struct {
	level int
}

const (
	levelDebug = iota
	levelInfo
	levelWarn
	levelError
)

// NewLogger creates a Logger from a level string (debug/info/warn/error).
func NewLogger(level string) *Logger {
	l := &Logger{}
	switch level {
	case "debug":
		l.level = levelDebug
	case "warn":
		l.level = levelWarn
	case "error":
		l.level = levelError
	default:
		l.level = levelInfo
	}
	return l
}

func (l *Logger) log(level, msg string) {
	fmt.Fprintf(os.Stderr, "%s [%s] %s\n", time.Now().Format(time.RFC3339), level, msg)
}

func (l *Logger) Debug(msg string) {
	if l.level <= levelDebug {
		l.log("DEBUG", msg)
	}
}

func (l *Logger) Info(msg string) {
	if l.level <= levelInfo {
		l.log("INFO", msg)
	}
}

func (l *Logger) Warn(msg string) {
	if l.level <= levelWarn {
		l.log("WARN", msg)
	}
}

func (l *Logger) Error(msg string) {
	if l.level <= levelError {
		l.log("ERROR", msg)
	}
}
