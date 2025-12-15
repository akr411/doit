package sync

import (
	"database/sql"
	"fmt"
)

// Operation type constants define the four CRDT operation types.
const (
	OpTypeCreate   = "CREATE"
	OpTypeUpdate   = "UPDATE"
	OpTypeComplete = "COMPLETE"
	OpTypeDelete   = "DELETE"
)

// Operation represents a CRDT operation for todo synchronization.
// Operations are the atomic units of change in the distributed system.
// Each operation has a unique ID, timestamp for LWW resolution, and device ID for conflict tiebreaking.
type Operation struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	TodoID    string `json:"todo_id"`
	Data      []byte `json:"data"`
	Timestamp int64  `json:"timestamp"`
	DeviceID  string `json:"device_id"`
}

// Apply executes the operation against the database, recording it and applying state changes.
// Operations are idempotent - applying the same operation twice has no additional effect.
// Returns error if transaction fails or operation type is unknown.
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
		if err := applyCreateInTx(tx, op); err != nil {
			return err
		}
	case OpTypeUpdate:
		if err := applyUpdateInTx(tx, op); err != nil {
			return err
		}
	case OpTypeComplete:
		if err := applyCompleteInTx(tx, op); err != nil {
			return err
		}
	case OpTypeDelete:
		if err := applyDeleteInTx(tx, op); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}

	return tx.Commit()
}
