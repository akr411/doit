package sync

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func Merge(local, remote []*Operation) []*Operation {
	opMap := make(map[string]*Operation)

	for _, op := range local {
		opMap[op.ID] = op
	}

	for _, op := range remote {
		if _, exists := opMap[op.ID]; !exists {
			opMap[op.ID] = op
		}
	}

	var merged []*Operation
	for _, op := range opMap {
		merged = append(merged, op)
	}

	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Timestamp != merged[j].Timestamp {
			return merged[i].Timestamp < merged[j].Timestamp
		}
		return merged[i].DeviceID < merged[j].DeviceID
	})

	return merged
}

func CompareOperations(op1, op2 *Operation) int {
	if op1.Timestamp != op2.Timestamp {
		if op1.Timestamp > op2.Timestamp {
			return 1
		}
		return -1
	}
	return strings.Compare(op1.DeviceID, op2.DeviceID)
}

func RebuildState(db *sql.DB, operations []*Operation) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec("DELETE FROM todos")
	if err != nil {
		return fmt.Errorf("failed to clear todos table: %w", err)
	}

	sort.Slice(operations, func(i, j int) bool {
		if operations[i].Timestamp != operations[j].Timestamp {
			return operations[i].Timestamp < operations[j].Timestamp
		}
		return operations[i].DeviceID < operations[j].DeviceID
	})

	for _, op := range operations {
		if err := applyOperationInTx(tx, op); err != nil {
			return fmt.Errorf("failed to apply operation %s: %w", op.ID, err)
		}
	}

	return tx.Commit()
}

func applyOperationInTx(tx *sql.Tx, op *Operation) error {
	switch op.Type {
	case OpTypeCreate:
		return applyCreateInTx(tx, op)
	case OpTypeUpdate:
		return applyUpdateInTx(tx, op)
	case OpTypeComplete:
		return applyCompleteInTx(tx, op)
	case OpTypeDelete:
		return applyDeleteInTx(tx, op)
	default:
		return fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

func applyCreateInTx(tx *sql.Tx, op *Operation) error {
	if len(op.Data) == 0 {
		return fmt.Errorf("CREATE operation requires data")
	}

	var exists bool
	err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM todos WHERE id=?)", op.TodoID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check todo existence: %w", err)
	}

	if exists {
		return applyUpdateInTx(tx, op)
	}

	var todo map[string]interface{}
	if err := unmarshalJSON(op.Data, &todo); err != nil {
		return fmt.Errorf("failed to unmarshal todo data: %w", err)
	}

	completed := 0
	if c, ok := todo["completed"].(bool); ok && c {
		completed = 1
	}

	task, _ := todo["task"].(string)
	note, _ := todo["note"].(string)
	deadline, _ := todo["deadline"].(float64)
	createdAt, _ := todo["created_at"].(float64)

	_, err = tx.Exec(`
		INSERT INTO todos (id, task, note, deadline, completed, created_at, updated_at, deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0)
	`, op.TodoID, task, note, int64(deadline), completed, int64(createdAt), op.Timestamp)

	if err != nil {
		return fmt.Errorf("failed to create todo: %w", err)
	}

	return nil
}

func applyUpdateInTx(tx *sql.Tx, op *Operation) error {
	if len(op.Data) == 0 {
		return fmt.Errorf("UPDATE operation requires data")
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
		return applyCreateInTx(tx, op)
	}

	var todo map[string]interface{}
	if err := unmarshalJSON(op.Data, &todo); err != nil {
		return fmt.Errorf("failed to unmarshal todo data: %w", err)
	}

	completed := 0
	if c, ok := todo["completed"].(bool); ok && c {
		completed = 1
	}

	task, _ := todo["task"].(string)
	note, _ := todo["note"].(string)
	deadline, _ := todo["deadline"].(float64)

	_, err = tx.Exec(`
		UPDATE todos
		SET task = ?, note = ?, deadline = ?, completed = ?, updated_at = ?
		WHERE id = ?
	`, task, note, int64(deadline), completed, op.Timestamp, op.TodoID)

	if err != nil {
		return fmt.Errorf("failed to update todo: %w", err)
	}

	return nil
}

func applyCompleteInTx(tx *sql.Tx, op *Operation) error {
	if len(op.Data) == 0 {
		return fmt.Errorf("COMPLETE operation requires data")
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
		return applyCreateInTx(tx, op)
	}

	var todo map[string]interface{}
	if err := unmarshalJSON(op.Data, &todo); err != nil {
		return fmt.Errorf("failed to unmarshal todo data: %w", err)
	}

	completed := 0
	if c, ok := todo["completed"].(bool); ok && c {
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

func applyDeleteInTx(tx *sql.Tx, op *Operation) error {
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

func unmarshalJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
