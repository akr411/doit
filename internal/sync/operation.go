package sync

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/akr411/doit/internal/storage"
	"github.com/akr411/doit/internal/utils"
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
	defer func() { _ = tx.Rollback() }()

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

	if err := applyOperationInTx(tx, op); err != nil {
		return err
	}

	return tx.Commit()
}

// ApplyWithValidation applies the operation after validating the data.
// This should be used when receiving operations from peers to prevent
// injection of malicious data through sync.
func (op *Operation) ApplyWithValidation(db *sql.DB) error {
	if op.Type == OpTypeCreate || op.Type == OpTypeUpdate || op.Type == OpTypeComplete {
		if err := op.validateData(); err != nil {
			return fmt.Errorf("invalid operation data: %w", err)
		}
	}
	return op.Apply(db)
}

func (op *Operation) validateData() error {
	if len(op.Data) == 0 {
		return nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal(op.Data, &data); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	if task, ok := data["task"].(string); ok {
		if err := utils.ValidateTask(task); err != nil {
			return fmt.Errorf("invalid task: %w", err)
		}
	}

	if note, ok := data["note"].(string); ok && note != "" {
		if err := utils.ValidateNote(note); err != nil {
			return fmt.Errorf("invalid note: %w", err)
		}
	}

	return nil
}

// OperationFromData converts storage.OperationData to sync.Operation.
func OperationFromData(opData storage.OperationData) Operation {
	return Operation{
		ID:        opData.ID,
		Type:      opData.Type,
		TodoID:    opData.TodoID,
		Data:      []byte(opData.Data),
		Timestamp: opData.Timestamp,
		DeviceID:  opData.DeviceID,
	}
}

// OperationsFromData converts a slice of storage.OperationData to []Operation.
func OperationsFromData(ops []storage.OperationData) []Operation {
	operations := make([]Operation, len(ops))
	for i, opData := range ops {
		operations[i] = OperationFromData(opData)
	}
	return operations
}
