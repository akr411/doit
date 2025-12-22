package sync

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOperationApply(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	todoJSON := map[string]interface{}{
		"id":         "todo-1",
		"task":       "Test task",
		"note":       "Test note",
		"deadline":   0,
		"completed":  false,
		"created_at": time.Now().Unix(),
	}
	data, _ := json.Marshal(todoJSON)

	op := &Operation{
		ID:        "op-1",
		Type:      OpTypeCreate,
		TodoID:    "todo-1",
		Data:      data,
		Timestamp: time.Now().UnixNano(),
		DeviceID:  "device-1",
	}

	err := op.Apply(db)
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM todos WHERE id='todo-1'").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 todo, got %d", count)
	}

	var opCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM operations WHERE id='op-1'").Scan(&opCount)
	if opCount != 1 {
		t.Errorf("expected 1 operation recorded, got %d", opCount)
	}
}

func TestOperationApplyIdempotent(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	todoJSON := map[string]interface{}{
		"id":         "todo-1",
		"task":       "Test task",
		"note":       "",
		"deadline":   0,
		"completed":  false,
		"created_at": time.Now().Unix(),
	}
	data, _ := json.Marshal(todoJSON)

	op := &Operation{
		ID:        "op-1",
		Type:      OpTypeCreate,
		TodoID:    "todo-1",
		Data:      data,
		Timestamp: time.Now().UnixNano(),
		DeviceID:  "device-1",
	}

	_ = op.Apply(db)
	_ = op.Apply(db)
	_ = op.Apply(db)

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM todos WHERE id='todo-1'").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 todo after 3 applies (idempotent), got %d", count)
	}

	var opCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM operations WHERE id='op-1'").Scan(&opCount)
	if opCount != 1 {
		t.Errorf("expected 1 operation (idempotent), got %d", opCount)
	}
}

func TestOperationApplyUpdate(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	createJSON := map[string]interface{}{
		"id":         "todo-1",
		"task":       "Original",
		"note":       "",
		"deadline":   0,
		"completed":  false,
		"created_at": time.Now().Unix(),
	}
	createData, _ := json.Marshal(createJSON)

	createOp := &Operation{
		ID:        "op-1",
		Type:      OpTypeCreate,
		TodoID:    "todo-1",
		Data:      createData,
		Timestamp: time.Now().UnixNano(),
		DeviceID:  "device-1",
	}
	_ = createOp.Apply(db)

	updateJSON := map[string]interface{}{
		"id":         "todo-1",
		"task":       "Updated",
		"note":       "",
		"deadline":   0,
		"completed":  false,
		"created_at": time.Now().Unix(),
	}
	updateData, _ := json.Marshal(updateJSON)

	updateOp := &Operation{
		ID:        "op-2",
		Type:      OpTypeUpdate,
		TodoID:    "todo-1",
		Data:      updateData,
		Timestamp: time.Now().UnixNano() + 1000000,
		DeviceID:  "device-1",
	}

	err := updateOp.Apply(db)
	if err != nil {
		t.Fatalf("Apply UPDATE failed: %v", err)
	}

	var task string
	_ = db.QueryRow("SELECT task FROM todos WHERE id='todo-1'").Scan(&task)
	if task != "Updated" {
		t.Errorf("expected 'Updated', got '%s'", task)
	}
}

func TestOperationApplyComplete(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	createJSON := map[string]interface{}{
		"id":         "todo-1",
		"task":       "Test",
		"note":       "",
		"deadline":   0,
		"completed":  false,
		"created_at": time.Now().Unix(),
	}
	createData, _ := json.Marshal(createJSON)

	createOp := &Operation{
		ID:        "op-1",
		Type:      OpTypeCreate,
		TodoID:    "todo-1",
		Data:      createData,
		Timestamp: time.Now().UnixNano(),
		DeviceID:  "device-1",
	}
	_ = createOp.Apply(db)

	completeJSON := map[string]interface{}{
		"id":         "todo-1",
		"task":       "Test",
		"note":       "",
		"deadline":   0,
		"completed":  true,
		"created_at": time.Now().Unix(),
	}
	completeData, _ := json.Marshal(completeJSON)

	completeOp := &Operation{
		ID:        "op-2",
		Type:      OpTypeComplete,
		TodoID:    "todo-1",
		Data:      completeData,
		Timestamp: time.Now().UnixNano() + 1000000,
		DeviceID:  "device-1",
	}

	err := completeOp.Apply(db)
	if err != nil {
		t.Fatalf("Apply COMPLETE failed: %v", err)
	}

	var completed int
	_ = db.QueryRow("SELECT completed FROM todos WHERE id='todo-1'").Scan(&completed)
	if completed != 1 {
		t.Error("todo should be completed")
	}
}

func TestOperationApplyDelete(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	createJSON := map[string]interface{}{
		"id":         "todo-1",
		"task":       "Test",
		"note":       "",
		"deadline":   0,
		"completed":  false,
		"created_at": time.Now().Unix(),
	}
	createData, _ := json.Marshal(createJSON)

	createOp := &Operation{
		ID:        "op-1",
		Type:      OpTypeCreate,
		TodoID:    "todo-1",
		Data:      createData,
		Timestamp: time.Now().UnixNano(),
		DeviceID:  "device-1",
	}
	_ = createOp.Apply(db)

	deleteOp := &Operation{
		ID:        "op-2",
		Type:      OpTypeDelete,
		TodoID:    "todo-1",
		Data:      []byte("{}"),
		Timestamp: time.Now().UnixNano() + 1000000,
		DeviceID:  "device-1",
	}

	err := deleteOp.Apply(db)
	if err != nil {
		t.Fatalf("Apply DELETE failed: %v", err)
	}

	var deleted int
	_ = db.QueryRow("SELECT deleted FROM todos WHERE id='todo-1'").Scan(&deleted)
	if deleted != 1 {
		t.Error("todo should be marked deleted")
	}
}

func TestOperationApplyInvalidType(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	op := &Operation{
		ID:        "op-1",
		Type:      "INVALID_TYPE",
		TodoID:    "todo-1",
		Data:      []byte("{}"),
		Timestamp: time.Now().UnixNano(),
		DeviceID:  "device-1",
	}

	err := op.Apply(db)
	if err == nil {
		t.Error("expected error for invalid operation type")
	}
}
