package sync

import (
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	schema := `
	CREATE TABLE todos (
		id TEXT PRIMARY KEY,
		task TEXT NOT NULL,
		note TEXT,
		deadline INTEGER,
		completed INTEGER DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		deleted INTEGER DEFAULT 0
	);

	CREATE TABLE operations (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		todo_id TEXT NOT NULL,
		data TEXT NOT NULL,
		timestamp INTEGER NOT NULL,
		device_id TEXT NOT NULL,
		synced INTEGER DEFAULT 0
	);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create test schema: %v", err)
	}

	return db
}

func createTestTodoJSON(task string, completed bool) []byte {
	todo := map[string]interface{}{
		"id":         "test-todo-1",
		"task":       task,
		"note":       "test note",
		"deadline":   float64(0),
		"completed":  completed,
		"created_at": float64(time.Now().Unix()),
	}
	data, _ := json.Marshal(todo)
	return data
}

func TestMergeOperations(t *testing.T) {
	local := []*Operation{
		{ID: "op1", Timestamp: 100, DeviceID: "device-a"},
		{ID: "op2", Timestamp: 200, DeviceID: "device-a"},
	}

	remote := []*Operation{
		{ID: "op2", Timestamp: 200, DeviceID: "device-a"},
		{ID: "op3", Timestamp: 300, DeviceID: "device-b"},
	}

	merged := Merge(local, remote)

	if len(merged) != 3 {
		t.Errorf("expected 3 operations, got %d", len(merged))
	}

	if merged[0].ID != "op1" || merged[1].ID != "op2" || merged[2].ID != "op3" {
		t.Errorf("operations not sorted correctly")
	}
}

func TestMergeDedupesOperations(t *testing.T) {
	local := []*Operation{
		{ID: "op1", Timestamp: 100, DeviceID: "device-a"},
	}

	remote := []*Operation{
		{ID: "op1", Timestamp: 100, DeviceID: "device-a"},
	}

	merged := Merge(local, remote)

	if len(merged) != 1 {
		t.Errorf("expected 1 operation after deduplication, got %d", len(merged))
	}
}

func TestCompareOperations(t *testing.T) {
	op1 := &Operation{Timestamp: 100, DeviceID: "device-a"}
	op2 := &Operation{Timestamp: 200, DeviceID: "device-b"}

	if CompareOperations(op1, op2) != -1 {
		t.Error("op1 should be less than op2 based on timestamp")
	}

	if CompareOperations(op2, op1) != 1 {
		t.Error("op2 should be greater than op1 based on timestamp")
	}
}

func TestCompareOperationsTiebreaker(t *testing.T) {
	op1 := &Operation{Timestamp: 100, DeviceID: "device-a"}
	op2 := &Operation{Timestamp: 100, DeviceID: "device-b"}

	if CompareOperations(op1, op2) != -1 {
		t.Error("op1 should be less than op2 based on device ID")
	}

	if CompareOperations(op2, op1) != 1 {
		t.Error("op2 should be greater than op1 based on device ID")
	}
}

func TestIdempotency(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	op := &Operation{
		ID:        "op1",
		Type:      OpTypeCreate,
		TodoID:    "test-todo-1",
		Data:      createTestTodoJSON("Buy milk", false),
		Timestamp: time.Now().UnixNano(),
		DeviceID:  "device-a",
	}

	if err := op.Apply(db); err != nil {
		t.Fatalf("failed to apply operation: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM todos").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 todo, got %d", count)
	}

	if err := op.Apply(db); err != nil {
		t.Fatalf("failed to apply operation second time: %v", err)
	}

	db.QueryRow("SELECT COUNT(*) FROM todos").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 todo after second apply (idempotency), got %d", count)
	}
}

func TestCommutativity(t *testing.T) {
	db1 := setupTestDB(t)
	defer db1.Close()

	db2 := setupTestDB(t)
	defer db2.Close()

	op1 := &Operation{
		ID:        "op1",
		Type:      OpTypeCreate,
		TodoID:    "test-todo-1",
		Data:      createTestTodoJSON("Buy milk", false),
		Timestamp: 100,
		DeviceID:  "device-a",
	}

	op2 := &Operation{
		ID:        "op2",
		Type:      OpTypeUpdate,
		TodoID:    "test-todo-1",
		Data:      createTestTodoJSON("Buy milk and eggs", false),
		Timestamp: 200,
		DeviceID:  "device-a",
	}

	if err := op1.Apply(db1); err != nil {
		t.Fatalf("db1: failed to apply op1: %v", err)
	}
	if err := op2.Apply(db1); err != nil {
		t.Fatalf("db1: failed to apply op2: %v", err)
	}

	if err := op2.Apply(db2); err != nil {
		t.Fatalf("db2: failed to apply op2: %v", err)
	}
	if err := op1.Apply(db2); err != nil {
		t.Fatalf("db2: failed to apply op1: %v", err)
	}

	var task1, task2 string
	db1.QueryRow("SELECT task FROM todos WHERE id='test-todo-1'").Scan(&task1)
	db2.QueryRow("SELECT task FROM todos WHERE id='test-todo-1'").Scan(&task2)

	if task1 != task2 {
		t.Errorf("commutativity failed: db1 has '%s', db2 has '%s'", task1, task2)
	}
}

func TestLWWConflictResolution(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	op1 := &Operation{
		ID:        "op1",
		Type:      OpTypeCreate,
		TodoID:    "test-todo-1",
		Data:      createTestTodoJSON("Buy milk", false),
		Timestamp: 100,
		DeviceID:  "device-a",
	}

	op2 := &Operation{
		ID:        "op2",
		Type:      OpTypeUpdate,
		TodoID:    "test-todo-1",
		Data:      createTestTodoJSON("Buy eggs", false),
		Timestamp: 200,
		DeviceID:  "device-b",
	}

	if err := op1.Apply(db); err != nil {
		t.Fatalf("failed to apply op1: %v", err)
	}

	if err := op2.Apply(db); err != nil {
		t.Fatalf("failed to apply op2: %v", err)
	}

	var task string
	db.QueryRow("SELECT task FROM todos WHERE id='test-todo-1'").Scan(&task)

	if task != "Buy eggs" {
		t.Errorf("LWW failed: expected 'Buy eggs', got '%s'", task)
	}
}

func TestDeleteVsUpdateConflict(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	createOp := &Operation{
		ID:        "op1",
		Type:      OpTypeCreate,
		TodoID:    "test-todo-1",
		Data:      createTestTodoJSON("Buy milk", false),
		Timestamp: 100,
		DeviceID:  "device-a",
	}

	updateOp := &Operation{
		ID:        "op2",
		Type:      OpTypeUpdate,
		TodoID:    "test-todo-1",
		Data:      createTestTodoJSON("Buy eggs", false),
		Timestamp: 200,
		DeviceID:  "device-a",
	}

	deleteOp := &Operation{
		ID:        "op3",
		Type:      OpTypeDelete,
		TodoID:    "test-todo-1",
		Data:      []byte("{}"),
		Timestamp: 300,
		DeviceID:  "device-b",
	}

	createOp.Apply(db)
	updateOp.Apply(db)
	deleteOp.Apply(db)

	var deleted int
	db.QueryRow("SELECT deleted FROM todos WHERE id='test-todo-1'").Scan(&deleted)

	if deleted != 1 {
		t.Errorf("delete should win with higher timestamp, got deleted=%d", deleted)
	}
}

func TestOutOfOrderDelivery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	op1 := &Operation{
		ID:        "op1",
		Type:      OpTypeCreate,
		TodoID:    "test-todo-1",
		Data:      createTestTodoJSON("Buy milk", false),
		Timestamp: 100,
		DeviceID:  "device-a",
	}

	op2 := &Operation{
		ID:        "op2",
		Type:      OpTypeUpdate,
		TodoID:    "test-todo-1",
		Data:      createTestTodoJSON("Buy milk and eggs", false),
		Timestamp: 200,
		DeviceID:  "device-a",
	}

	op3 := &Operation{
		ID:        "op3",
		Type:      OpTypeComplete,
		TodoID:    "test-todo-1",
		Data:      createTestTodoJSON("Buy milk and eggs", true),
		Timestamp: 300,
		DeviceID:  "device-a",
	}

	op3.Apply(db)
	op1.Apply(db)
	op2.Apply(db)

	var completed int
	db.QueryRow("SELECT completed FROM todos WHERE id='test-todo-1'").Scan(&completed)

	if completed != 1 {
		t.Errorf("out-of-order delivery failed: expected completed=1, got %d", completed)
	}
}

func TestRebuildState(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	operations := []*Operation{
		{
			ID:        "op1",
			Type:      OpTypeCreate,
			TodoID:    "test-todo-1",
			Data:      createTestTodoJSON("Buy milk", false),
			Timestamp: 100,
			DeviceID:  "device-a",
		},
		{
			ID:        "op2",
			Type:      OpTypeCreate,
			TodoID:    "test-todo-2",
			Data:      createTestTodoJSON("Buy eggs", false),
			Timestamp: 200,
			DeviceID:  "device-a",
		},
		{
			ID:        "op3",
			Type:      OpTypeComplete,
			TodoID:    "test-todo-1",
			Data:      createTestTodoJSON("Buy milk", true),
			Timestamp: 300,
			DeviceID:  "device-a",
		},
	}

	if err := RebuildState(db, operations); err != nil {
		t.Fatalf("failed to rebuild state: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM todos WHERE deleted=0").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 todos, got %d", count)
	}

	var completed int
	db.QueryRow("SELECT completed FROM todos WHERE id='test-todo-1'").Scan(&completed)
	if completed != 1 {
		t.Errorf("expected todo to be completed, got completed=%d", completed)
	}
}

func TestTombstones(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	createOp := &Operation{
		ID:        "op1",
		Type:      OpTypeCreate,
		TodoID:    "test-todo-1",
		Data:      createTestTodoJSON("Buy milk", false),
		Timestamp: 100,
		DeviceID:  "device-a",
	}

	deleteOp := &Operation{
		ID:        "op2",
		Type:      OpTypeDelete,
		TodoID:    "test-todo-1",
		Data:      []byte("{}"),
		Timestamp: 200,
		DeviceID:  "device-a",
	}

	createOp.Apply(db)
	deleteOp.Apply(db)

	var deleted int
	err := db.QueryRow("SELECT deleted FROM todos WHERE id='test-todo-1'").Scan(&deleted)
	if err != nil {
		t.Fatalf("tombstone not created: %v", err)
	}

	if deleted != 1 {
		t.Errorf("expected deleted=1, got %d", deleted)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM todos WHERE id='test-todo-1' AND deleted=0").Scan(&count)
	if count != 0 {
		t.Error("deleted todo should not be returned when filtering deleted=0")
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
