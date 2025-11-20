package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/akr411/doit/internal/models"
)

func TestBoltStorage_TodoOperation(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewBoltStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	todo := &models.Todo{
		ID:          "test-1",
		Title:       "Test Todo",
		Description: "Test Description",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	err = storage.SaveTodo(todo)
	if err != nil {
		t.Errorf("SaveTodo failed: %v", err)
	}

	retrieved, err := storage.GetTodo("test-1")
	if err != nil {
		t.Errorf("GetTodo failed: %v", err)
	}

	retrieved.Title = "Updated Title"
	err = storage.UpdateTodo(retrieved)
	if err != nil {
		t.Errorf("UpdateTodo failed: %v", err)
	}

	updated, err := storage.GetTodo("test-1")
	if err != nil {
		t.Errorf("Updated todo title = %v, want Updated Title", updated.Title)
	}

	todo2 := &models.Todo{
		ID:          "test-2",
		Title:       "Second Todo",
		Description: "Second Description",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	err = storage.SaveTodo(todo2)
	if err != nil {
		t.Errorf("SaveTodo for Second Todo failed: %v", err)
	}

	todos, err := storage.GetAllTodos()
	if err != nil {
		t.Errorf("GetAllTodos failed: %v", err)
	}

	if len(todos) != 2 {
		t.Errorf("GetAllTodos returned %d todos, want 2", len(todos))
	}

	err = storage.DeleteTodo("test-1")
	if err != nil {
		t.Errorf("DeleteTodo failed: %v", err)
	}

	_, err = storage.GetTodo("test-1")
	if err == nil {
		t.Error("GetTodo should have failed after deletion")
	}
}

func TestBoltStorage_Sorting(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewBoltStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	now := time.Now()

	todos := []*models.Todo{
		{
			ID:        "1",
			Title:     "No deadline",
			Deadline:  nil,
			Completed: false,
		},
		{
			ID:        "2",
			Title:     "Due deadline",
			Deadline:  timePtr(now.Add(24 * time.Hour)),
			Completed: false,
		},
		{
			ID:        "3",
			Title:     "Due in 3 days",
			Deadline:  timePtr(now.Add(72 * time.Hour)),
			Completed: false,
		},
		{
			ID:        "4",
			Title:     "Completed",
			Deadline:  timePtr(now.Add(24 * time.Hour)),
			Completed: true,
		},
	}

	for _, todo := range todos {
		if err := storage.SaveTodo(todo); err != nil {
			t.Fatalf("Failed to save todo: %v", err)
		}
	}

	sorted, err := storage.GetAllTodos()
	if err != nil {
		t.Fatalf("Failed to get todos: %v", err)
	}

	// First should be incomplete with soonest deadline
	if sorted[0].ID != "2" {
		t.Errorf("First todo should be 'Due tomorrow', got %s", sorted[0].Title)
	}

	// Last should be completed
	if !sorted[len(sorted)-1].Completed {
		t.Errorf("Last todo should be completed")
	}
}

func TestBoltStorage_Streak(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewBoltStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	streak, err := storage.GetStreak()
	if err != nil {
		t.Errorf("GetStreak failed: %v", err)
	}

	if streak.CurrentStreak != 0 {
		t.Errorf("Initial current streak = %d, want 0", streak.CurrentStreak)
	}

	streak.CurrentStreak = 5
	streak.MaxStreak = 10
	streak.TotalCompleted = 50
	streak.LastCompletedAt = time.Now()

	err = storage.UpdateStreak(streak)
	if err != nil {
		t.Errorf("UpdateStreak failed: %v", err)
	}

	updated, err := storage.GetStreak()
	if err != nil {
		t.Errorf("GetStreak after update failed: %v", err)
	}

	if updated.CurrentStreak != 5 {
		t.Errorf("Updated current streak = %d, want 5", updated.CurrentStreak)
	}

	if updated.MaxStreak != 10 {
		t.Errorf("Updated max streak = %d, want 10", updated.MaxStreak)
	}
}

func TestGetTopUpcomingTodos(t *testing.T) {
	now := time.Now()

	todos := []*models.Todo{
		{ID: "3", Title: "Much Later", Deadline: timePtr(now.Add(12 * time.Hour)), Completed: false},
		{ID: "5", Title: "Completed", Deadline: timePtr(now.Add(5 * time.Hour)), Completed: true},
		{ID: "4", Title: "No deadline", Deadline: nil, Completed: false},
		{ID: "2", Title: "Later", Deadline: timePtr(now.Add(10 * time.Hour)), Completed: false},
		{ID: "1", Title: "Soon", Deadline: timePtr(now.Add(1 * time.Hour)), Completed: false},
	}

	top := GetTopUpcomingTodos(todos, 2)

	if len(top) != 2 {
		t.Errorf("GetTopUpcomingTodos returned %d todos, want 2", len(top))
	}

	if top[0].ID != "1" {
		t.Errorf("First todo should be 'Soon', got %s", top[0].Title)
	}

	if top[1].ID != "2" {
		t.Errorf("Second todo should be 'Later', got %s", top[1].Title)
	}
}

func TestGetTodosWithoutDeadline(t *testing.T) {
	now := time.Now()

	todos := []*models.Todo{
		{ID: "1", Title: "With deadline", Deadline: timePtr(now.Add(1 * time.Hour)), Completed: false},
		{ID: "2", Title: "No deadline 1", Deadline: nil, Completed: false},
		{ID: "3", Title: "No deadline 2", Deadline: nil, Completed: false},
		{ID: "4", Title: "Completed no deadline", Deadline: nil, Completed: true},
	}

	noDeadline := GetTodosWithoutDeadline(todos)

	if len(noDeadline) != 2 {
		t.Errorf("GetTodosWithoutDeadline returned %d todos, want 2", len(noDeadline))
	}

	for _, todo := range noDeadline {
		if todo.Deadline != nil {
			t.Errorf("Todo %s should not have deadline", todo.Title)
		}
		if todo.Completed {
			t.Errorf("Todo %s should not be completed", todo.Title)
		}
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
