package logging

import (
	"fmt"
	"io"
	"log"
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
	logger     *Logger
	once       sync.Once
	currentLog Level = INFO
)

type Logger struct {
	debugLog *log.Logger
	infoLog  *log.Logger
	warnLog  *log.Logger
	errorLog *log.Logger
	file     *os.File
}

func Init(dataDir string, level Level) error {
	var err error
	once.Do(func() {
		currentLog = level
		logPath := filepath.Join(dataDir, "sync.log")

		if fileErr := rotateLogIfNeeded(logPath); fileErr != nil {
			err = fileErr
			return
		}

		file, fileErr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if fileErr != nil {
			err = fileErr
			return
		}

		writer := io.MultiWriter(file)
		if level == DEBUG {
			writer = io.MultiWriter(os.Stdout, file)
		}

		logger = &Logger{
			debugLog: log.New(writer, "[DEBUG] ", log.LstdFlags),
			infoLog:  log.New(writer, "[INFO] ", log.LstdFlags),
			warnLog:  log.New(writer, "[WARN] ", log.LstdFlags),
			errorLog: log.New(writer, "[ERROR] ", log.LstdFlags),
			file:     file,
		}
	})
	return err
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
		os.Remove(backupPath)
		if err := os.Rename(logPath, backupPath); err != nil {
			return err
		}
	}

	return nil
}

func Close() {
	if logger != nil && logger.file != nil {
		logger.file.Close()
	}
}

func Debug(format string, v ...interface{}) {
	if logger != nil && currentLog <= DEBUG {
		logger.debugLog.Printf(format, v...)
	}
}

func Info(format string, v ...interface{}) {
	if logger != nil && currentLog <= INFO {
		logger.infoLog.Printf(format, v...)
	}
}

func Warn(format string, v ...interface{}) {
	if logger != nil && currentLog <= WARN {
		logger.warnLog.Printf(format, v...)
	}
}

func Error(format string, v ...interface{}) {
	if logger != nil && currentLog <= ERROR {
		logger.errorLog.Printf(format, v...)
	}
}

func Infof(format string, v ...interface{}) {
	Info(format, v...)
}

func Errorf(format string, v ...interface{}) {
	Error(format, v...)
}

func Printf(format string, v ...interface{}) {
	if logger != nil {
		fmt.Fprintf(logger.file, format+"\n", v...)
	}
}
