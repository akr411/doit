package models

import (
	"testing"
	"time"
)

func TestTodo_IsOverdue(t *testing.T) {
	tests := []struct {
		name     string
		todo     Todo
		expected bool
	}{
		{
			name: "overdue todo",
			todo: Todo{
				Deadline:  timePtr(time.Now().Add(-24 * time.Hour)),
				Completed: false,
			},
			expected: true,
		},
		{
			name: "future deadline",
			todo: Todo{
				Deadline:  timePtr(time.Now().Add(24 * time.Hour)),
				Completed: false,
			},
			expected: false,
		},
		{
			name: "completed todo with past deadline",
			todo: Todo{
				Deadline:  timePtr(time.Now().Add(-24 * time.Hour)),
				Completed: true,
			},
			expected: false,
		},
		{
			name: "no deadline",
			todo: Todo{
				Deadline:  nil,
				Completed: false,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.todo.IsOverdue(); got != tt.expected {
				t.Errorf("IsOverdue() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestTodo_DaysUntilDeadline(t *testing.T) {
	tests := []struct {
		name     string
		todo     Todo
		expected int
		delta    int
	}{
		{
			name: "deadline in 5 days",
			todo: Todo{
				Deadline: timePtr(time.Now().Add(5 * 24 * time.Hour)),
			},
			expected: 5,
			delta:    1,
		},
		{
			name: "deadline was 3 days ago",
			todo: Todo{
				Deadline: timePtr(time.Now().Add(-3 * 24 * time.Hour)),
			},
			expected: -3,
			delta:    1,
		},
		{
			name: "no deadline",
			todo: Todo{
				Deadline: nil,
			},
			expected: -1,
			delta:    0,
		},
		{
			name: "deadline today",
			todo: Todo{
				Deadline: timePtr(time.Now().Add(12 * time.Hour)),
			},
			expected: 0,
			delta:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.todo.DaysUntilDeadline()
			if abs(got-tt.expected) > tt.delta {
				t.Errorf("DaysUntilDeadline() = %v, want %v (+-%d)", got, tt.expected, tt.delta)
			}
		})
	}
}

func TestTodo_MarkComplete(t *testing.T) {
	todo := Todo{
		ID:        "test-1",
		Title:     "Test Todo",
		Completed: false,
	}
	todo.MarkComplete()

	if !todo.Completed {
		t.Error("MarkComplete() did not set Completed to true")
	}
	if todo.CompletedAt == nil {
		t.Error("MarkComplete() did not set CompletedAt")
	}
	if todo.UpdatedAt.IsZero() {
		t.Error("MarkComplete() did not update UpdatedAt")
	}
}

func TestTodo_MarkIncomplete(t *testing.T) {
	completedTime := time.Now()
	todo := Todo{
		ID:          "test-1",
		Title:       "Test Todo",
		Completed:   true,
		CompletedAt: &completedTime,
	}

	todo.MarkIncomplete()

	if todo.Completed {
		t.Error("MarkIncomplete() did not set Completed to false")
	}
	if todo.CompletedAt != nil {
		t.Error("MarkIncomplete() did not clear CompletedAt")
	}
	if todo.UpdatedAt.IsZero() {
		t.Error("MarkIncomplete() did not update UpdatedAt")
	}
}

// Helper functions
func timePtr(t time.Time) *time.Time {
	return &t
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
