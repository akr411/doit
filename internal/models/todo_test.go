package models

import (
	"testing"
)

func TestTodoValidation(t *testing.T) {
	tests := []struct {
		name    string
		todo    *Todo
		wantErr bool
	}{
		{
			name:    "valid todo with task",
			todo:    &Todo{Task: "Buy milk"},
			wantErr: false,
		},
		{
			name:    "empty task should error",
			todo:    &Todo{Task: ""},
			wantErr: true,
		},
		{
			name:    "task with note is valid",
			todo:    &Todo{Task: "Buy milk", Note: "Organic"},
			wantErr: false,
		},
		{
			name:    "task without note is valid",
			todo:    &Todo{Task: "Buy milk", Note: ""},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.todo.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewTodo(t *testing.T) {
	task := "Buy milk"
	note := "Organic"
	deadline := int64(1234567890)

	todo := NewTodo(task, note, deadline)

	if todo.Task != task {
		t.Errorf("expected task %q, got %q", task, todo.Task)
	}
	if todo.Note != note {
		t.Errorf("expected note %q, got %q", note, todo.Note)
	}
	if todo.Deadline != deadline {
		t.Errorf("expected deadline %d, got %d", deadline, todo.Deadline)
	}
	if todo.Completed {
		t.Error("new todo should not be completed")
	}
	if todo.CreatedAt == 0 {
		t.Error("created_at should be set")
	}
	if todo.UpdatedAt == 0 {
		t.Error("updated_at should be set")
	}
}
