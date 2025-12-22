package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

var (
	mu           sync.Mutex
	slogger      *slog.Logger
	logFile      *os.File
	currentLevel Level = INFO
)

func Init(dataDir string, level Level) error {
	mu.Lock()
	defer mu.Unlock()

	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
		slogger = nil
	}

	currentLevel = level
	logPath := filepath.Join(dataDir, "sync.log")

	if err := rotateLogIfNeeded(logPath); err != nil {
		return err
	}

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	logFile = file

	var writer io.Writer = file
	if level == DEBUG {
		writer = io.MultiWriter(os.Stdout, file)
	}

	var slogLevel slog.Level
	switch level {
	case DEBUG:
		slogLevel = slog.LevelDebug
	case INFO:
		slogLevel = slog.LevelInfo
	case WARN:
		slogLevel = slog.LevelWarn
	case ERROR:
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: slogLevel,
	}
	handler := slog.NewTextHandler(writer, opts)
	slogger = slog.New(handler)

	return nil
}

func rotateLogIfNeeded(logPath string) error {
	const maxSize = 10 * 1024 * 1024

	info, err := os.Stat(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if info.Size() > maxSize {
		backupPath := logPath + ".old"
		_ = os.Remove(backupPath)
		if err := os.Rename(logPath, backupPath); err != nil {
			return err
		}
	}

	return nil
}

func Close() {
	mu.Lock()
	defer mu.Unlock()

	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
		slogger = nil
	}
}

func Debug(format string, v ...interface{}) {
	mu.Lock()
	l := slogger
	level := currentLevel
	mu.Unlock()
	if l != nil && level <= DEBUG {
		l.Debug(fmt.Sprintf(format, v...))
	}
}

func Info(format string, v ...interface{}) {
	mu.Lock()
	l := slogger
	level := currentLevel
	mu.Unlock()
	if l != nil && level <= INFO {
		l.Info(fmt.Sprintf(format, v...))
	}
}

func Warn(format string, v ...interface{}) {
	mu.Lock()
	l := slogger
	level := currentLevel
	mu.Unlock()
	if l != nil && level <= WARN {
		l.Warn(fmt.Sprintf(format, v...))
	}
}

func Error(format string, v ...interface{}) {
	mu.Lock()
	l := slogger
	level := currentLevel
	mu.Unlock()
	if l != nil && level <= ERROR {
		l.Error(fmt.Sprintf(format, v...))
	}
}

func Infof(format string, v ...interface{}) {
	Info(format, v...)
}

func Errorf(format string, v ...interface{}) {
	Error(format, v...)
}

func Printf(format string, v ...interface{}) {
	mu.Lock()
	f := logFile
	mu.Unlock()
	if f != nil {
		_, _ = f.WriteString(fmt.Sprintf(format, v...) + "\n")
	}
}
