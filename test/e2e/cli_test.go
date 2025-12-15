package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "doit")
	cmd := exec.Command("go", "build", "-o", binary, "github.com/akr411/doit/cmd")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\n%s", err, output)
	}
	return binary
}

func TestCLIAdd(t *testing.T) {
	tmpHome := t.TempDir()
	binary := buildBinary(t)

	cmd := exec.Command(binary, "add", "-t", "Test task")
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("doit add failed: %v\n%s", err, output)
	}

	if !strings.Contains(string(output), "✓ Added") {
		t.Errorf("Expected success message, got: %s", output)
	}

	cmd = exec.Command(binary, "list")
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(output), "Test task") {
		t.Error("Task not found in list")
	}
}

func TestCLIComplete(t *testing.T) {
	tmpHome := t.TempDir()
	binary := buildBinary(t)

	cmd := exec.Command(binary, "add", "-t", "Task to complete")
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	cmd.Run()

	cmd = exec.Command(binary, "complete", "1")
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("doit complete failed: %v\n%s", err, output)
	}

	if !strings.Contains(string(output), "✓ Completed") {
		t.Errorf("Expected success message, got: %s", output)
	}
}

func TestCLIDelete(t *testing.T) {
	tmpHome := t.TempDir()
	binary := buildBinary(t)

	cmd := exec.Command(binary, "add", "-t", "Task to delete")
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	cmd.Run()

	cmd = exec.Command(binary, "delete", "1", "-y")
	cmd.Env = append(os.Environ(), "HOME="+tmpHome)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("doit delete failed: %v\n%s", err, output)
	}

	if !strings.Contains(string(output), "✓ Deleted") {
		t.Errorf("Expected success message, got: %s", output)
	}
}

func TestCLISyncCommands(t *testing.T) {
	tmpHome := t.TempDir()
	binary := buildBinary(t)

	tests := []struct {
		name     string
		args     []string
		wantOuts []string
	}{
		{"init", []string{"sync", "init"}, []string{"Sync enabled", "already enabled"}},
		{"status after init", []string{"sync", "status"}, []string{"Sync: enabled"}},
		{"show code", []string{"sync", "show"}, []string{"Code:"}},
		{"devices", []string{"sync", "devices"}, []string{"ID"}},
		{"disable", []string{"sync", "disable"}, []string{"✓ Sync disabled"}},
		{"status after disable", []string{"sync", "status"}, []string{"Sync: disabled"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(binary, tt.args...)
			cmd.Env = append(os.Environ(), "HOME="+tmpHome)
			output, err := cmd.CombinedOutput()

			found := false
			for _, want := range tt.wantOuts {
				if strings.Contains(string(output), want) {
					found = true
					break
				}
			}

			if !found {
				if err != nil {
					t.Logf("Command failed: %v", err)
				}
				t.Errorf("Expected output to contain one of %v, got:\n%s", tt.wantOuts, output)
			}
		})
	}
}

