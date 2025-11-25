package sync

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/akr411/doit/internal/models"
)

const (
	OpTypeCreate   = "CREATE"
	OpTypeUpdate   = "UPDATE"
	OpTypeComplete = "COMPLETE"
	OpTypeDelete   = "DELETE"
)

type Operation struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	TodoID    string `json:"todo_id"`
	Data      []byte `json:"data"`
	Timestamp int64  `json:"timestamp"`
	DeviceID  string `json:"device_id"`
}

func (op *Operation) Apply(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var exists bool
	err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM operations WHERE id=?)", op.ID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check operation existence: %w", err)
	}
	if exists {
		return nil
	}

	_, err = tx.Exec(`
		INSERT INTO operations (id, type, todo_id, data, timestamp, device_id, synced)
		VALUES (?, ?, ?, ?, ?, ?, 0)
	`, op.ID, op.Type, op.TodoID, string(op.Data), op.Timestamp, op.DeviceID)
	if err != nil {
		return fmt.Errorf("failed to insert operation: %w", err)
	}

	switch op.Type {
	case OpTypeCreate:
		if err := op.applyCreate(tx); err != nil {
			return err
		}
	case OpTypeUpdate:
		if err := op.applyUpdate(tx); err != nil {
			return err
		}
	case OpTypeComplete:
		if err := op.applyComplete(tx); err != nil {
			return err
		}
	case OpTypeDelete:
		if err := op.applyDelete(tx); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}

	return tx.Commit()
}

func (op *Operation) applyCreate(tx *sql.Tx) error {
	if len(op.Data) == 0 {
		return fmt.Errorf("CREATE operation requires data")
	}

	var todo models.Todo
	if err := json.Unmarshal(op.Data, &todo); err != nil {
		return fmt.Errorf("failed to unmarshal todo data: %w", err)
	}

	var exists bool
	err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM todos WHERE id=?)", op.TodoID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check todo existence: %w", err)
	}

	if exists {
		return op.applyUpdate(tx)
	}

	completed := 0
	if todo.Completed {
		completed = 1
	}

	_, err = tx.Exec(`
		INSERT INTO todos (id, task, note, deadline, completed, created_at, updated_at, deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0)
	`, todo.ID, todo.Task, todo.Note, todo.Deadline, completed, todo.CreatedAt, op.Timestamp)

	if err != nil {
		return fmt.Errorf("failed to create todo: %w", err)
	}

	return nil
}

func (op *Operation) applyUpdate(tx *sql.Tx) error {
	if len(op.Data) == 0 {
		return fmt.Errorf("UPDATE operation requires data")
	}

	var todo models.Todo
	if err := json.Unmarshal(op.Data, &todo); err != nil {
		return fmt.Errorf("failed to unmarshal todo data: %w", err)
	}

	var lastTimestamp int64
	err := tx.QueryRow("SELECT updated_at FROM todos WHERE id=?", op.TodoID).Scan(&lastTimestamp)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to get last timestamp: %w", err)
	}

	if op.Timestamp < lastTimestamp {
		return nil
	}

	if err == sql.ErrNoRows {
		return op.applyCreate(tx)
	}

	completed := 0
	if todo.Completed {
		completed = 1
	}

	_, err = tx.Exec(`
		UPDATE todos
		SET task = ?, note = ?, deadline = ?, completed = ?, updated_at = ?
		WHERE id = ?
	`, todo.Task, todo.Note, todo.Deadline, completed, op.Timestamp, op.TodoID)

	if err != nil {
		return fmt.Errorf("failed to update todo: %w", err)
	}

	return nil
}

func (op *Operation) applyComplete(tx *sql.Tx) error {
	if len(op.Data) == 0 {
		return fmt.Errorf("COMPLETE operation requires data")
	}

	var todo models.Todo
	if err := json.Unmarshal(op.Data, &todo); err != nil {
		return fmt.Errorf("failed to unmarshal todo data: %w", err)
	}

	var lastTimestamp int64
	err := tx.QueryRow("SELECT updated_at FROM todos WHERE id=?", op.TodoID).Scan(&lastTimestamp)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to get last timestamp: %w", err)
	}

	if op.Timestamp < lastTimestamp {
		return nil
	}

	if err == sql.ErrNoRows {
		return op.applyCreate(tx)
	}

	completed := 0
	if todo.Completed {
		completed = 1
	}

	_, err = tx.Exec(`
		UPDATE todos
		SET completed = ?, updated_at = ?
		WHERE id = ?
	`, completed, op.Timestamp, op.TodoID)

	if err != nil {
		return fmt.Errorf("failed to complete todo: %w", err)
	}

	return nil
}

func (op *Operation) applyDelete(tx *sql.Tx) error {
	var lastTimestamp int64
	err := tx.QueryRow("SELECT updated_at FROM todos WHERE id=?", op.TodoID).Scan(&lastTimestamp)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to get last timestamp: %w", err)
	}

	if op.Timestamp < lastTimestamp {
		return nil
	}

	if err == sql.ErrNoRows {
		return nil
	}

	_, err = tx.Exec(`
		UPDATE todos
		SET deleted = 1, updated_at = ?
		WHERE id = ?
	`, op.Timestamp, op.TodoID)

	if err != nil {
		return fmt.Errorf("failed to delete todo: %w", err)
	}

	return nil
}
