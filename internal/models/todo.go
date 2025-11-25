package models

import (
	"errors"
	"time"
)

type Todo struct {
	ID        string `json:"id"`
	Task      string `json:"task"`
	Note      string `json:"note"`
	Deadline  int64  `json:"deadline"`
	Completed bool   `json:"completed"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

func (t *Todo) Validate() error {
	if t.Task == "" {
		return errors.New("task must not be empty")
	}
	return nil
}

type Streak struct {
	CurrentStreak   int   `json:"current_streak"`
	MaxStreak       int   `json:"max_streak"`
	TotalCompleted  int   `json:"total_completed"`
	LastCompletedAt int64 `json:"last_completed_at"`
}

func NewTodo(task, note string, deadline int64) *Todo {
	now := time.Now().UnixNano()
	return &Todo{
		Task:      task,
		Note:      note,
		Deadline:  deadline,
		Completed: false,
		CreatedAt: now,
		UpdatedAt: now,
	}
}
