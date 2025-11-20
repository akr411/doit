package models

import "time"

// Todo represents a todo item
type Todo struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Deadline    *time.Time `json:"deadline,omitempty"`
	Completed   bool       `json:"completed"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// IsOverdue checks if the todo is overdue
func (t *Todo) IsOverdue() bool {
	if t.Deadline == nil || t.Completed {
		return false
	}
	return t.Deadline.Before(time.Now())
}

// DaysUntilDeadline returns the number of days until the deadline
func (t *Todo) DaysUntilDeadline() int {
	if t.Deadline == nil {
		return -1
	}
	duration := time.Until(*t.Deadline)
	return int(duration.Hours() / 24)
}

// MarkComplete marks the todo as completed
func (t *Todo) MarkComplete() {
	t.Completed = true
	now := time.Now()
	t.CompletedAt = &now
	t.UpdatedAt = now
}

// MarkIncomplete marks the todo as incomplete
func (t *Todo) MarkIncomplete() {
	t.Completed = false
	t.CompletedAt = nil
	t.UpdatedAt = time.Now()
}
