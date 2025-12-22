package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogging(t *testing.T) {
	tmpDir := t.TempDir()

	err := Init(tmpDir, DEBUG)
	if err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(tmpDir, "sync.log")
	if _, err := os.Stat(logPath); err != nil {
		t.Error("Log file not created")
	}

	Debug("debug message")
	Info("info message")
	Warn("warn message")
	Error("error message")

	Close()

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}

	log := string(content)

	if !strings.Contains(log, "level=DEBUG") {
		t.Error("DEBUG log not written")
	}
	if !strings.Contains(log, "level=INFO") {
		t.Error("INFO log not written")
	}
	if !strings.Contains(log, "level=WARN") {
		t.Error("WARN log not written")
	}
	if !strings.Contains(log, "level=ERROR") {
		t.Error("ERROR log not written")
	}
}

