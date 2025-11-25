package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/akr411/doit/internal/models"
)

func setupTestDB(t *testing.T) (*Storage, func()) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	origGetDBPath := getDBPath
	getDBPath = func() (string, error) {
		return dbPath, nil
	}

	s, err := New()
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	cleanup := func() {
		s.Close()
		getDBPath = origGetDBPath
		os.RemoveAll(tmpDir)
	}

	return s, cleanup
}

func TestSaveAndGetTodo(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	todo := models.NewTodo("Buy milk", "Organic", 0)

	if err := s.SaveTodo(todo); err != nil {
		t.Fatalf("SaveTodo failed: %v", err)
	}

	if todo.ID == "" {
		t.Error("ID should be generated")
	}

	retrieved, err := s.GetTodo(todo.ID)
	if err != nil {
		t.Fatalf("GetTodo failed: %v", err)
	}

	if retrieved.Task != todo.Task {
		t.Errorf("expected task %q, got %q", todo.Task, retrieved.Task)
	}
	if retrieved.Note != todo.Note {
		t.Errorf("expected note %q, got %q", todo.Note, retrieved.Note)
	}
}

func TestGetAllTodos(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	todo1 := models.NewTodo("Task 1", "", 0)
	todo2 := models.NewTodo("Task 2", "", time.Now().Unix()+3600)
	todo3 := models.NewTodo("Task 3", "", 0)
	todo3.Completed = true

	s.SaveTodo(todo1)
	s.SaveTodo(todo2)
	s.SaveTodo(todo3)

	todos, err := s.GetAllTodos()
	if err != nil {
		t.Fatalf("GetAllTodos failed: %v", err)
	}

	if len(todos) != 3 {
		t.Errorf("expected 3 todos, got %d", len(todos))
	}
}

func TestUpdateTodo(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	todo := models.NewTodo("Original task", "", 0)
	s.SaveTodo(todo)

	todo.Task = "Updated task"
	todo.Completed = true
	todo.UpdatedAt = time.Now().Unix()

	if err := s.UpdateTodo(todo); err != nil {
		t.Fatalf("UpdateTodo failed: %v", err)
	}

	retrieved, err := s.GetTodo(todo.ID)
	if err != nil {
		t.Fatalf("GetTodo failed: %v", err)
	}

	if retrieved.Task != "Updated task" {
		t.Errorf("expected updated task, got %q", retrieved.Task)
	}
	if !retrieved.Completed {
		t.Error("todo should be completed")
	}
}

func TestDeleteTodo(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	todo := models.NewTodo("To be deleted", "", 0)
	s.SaveTodo(todo)

	if err := s.DeleteTodo(todo.ID); err != nil {
		t.Fatalf("DeleteTodo failed: %v", err)
	}

	_, err := s.GetTodo(todo.ID)
	if err == nil {
		t.Error("expected error when getting deleted todo")
	}
}

func TestStreaks(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	streak, err := s.GetStreak()
	if err != nil {
		t.Fatalf("GetStreak failed: %v", err)
	}

	if streak.CurrentStreak != 0 {
		t.Errorf("expected initial streak to be 0, got %d", streak.CurrentStreak)
	}

	streak.CurrentStreak = 5
	streak.MaxStreak = 10
	streak.TotalCompleted = 50
	streak.LastCompletedAt = time.Now().Unix()

	if err := s.UpdateStreak(streak); err != nil {
		t.Fatalf("UpdateStreak failed: %v", err)
	}

	retrieved, err := s.GetStreak()
	if err != nil {
		t.Fatalf("GetStreak failed: %v", err)
	}

	if retrieved.CurrentStreak != 5 {
		t.Errorf("expected current streak 5, got %d", retrieved.CurrentStreak)
	}
	if retrieved.MaxStreak != 10 {
		t.Errorf("expected max streak 10, got %d", retrieved.MaxStreak)
	}
}

func TestConfig(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	if err := s.SetConfig("test_key", "test_value"); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	value, err := s.GetConfig("test_key")
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	if value != "test_value" {
		t.Errorf("expected 'test_value', got %q", value)
	}

	if err := s.SetConfig("test_key", "updated_value"); err != nil {
		t.Fatalf("SetConfig update failed: %v", err)
	}

	value, err = s.GetConfig("test_key")
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	if value != "updated_value" {
		t.Errorf("expected 'updated_value', got %q", value)
	}
}
