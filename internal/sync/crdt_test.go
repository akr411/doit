package sync

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/akr411/doit/internal/keyring"
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
	tests := []struct {
		name string
		op1  *Operation
		op2  *Operation
		want int
	}{
		{
			name: "timestamp difference - op1 earlier",
			op1:  &Operation{Timestamp: 100, DeviceID: "device-a"},
			op2:  &Operation{Timestamp: 200, DeviceID: "device-b"},
			want: -1,
		},
		{
			name: "timestamp difference - op2 earlier",
			op1:  &Operation{Timestamp: 200, DeviceID: "device-b"},
			op2:  &Operation{Timestamp: 100, DeviceID: "device-a"},
			want: 1,
		},
		{
			name: "same timestamp - device ID tiebreaker",
			op1:  &Operation{Timestamp: 100, DeviceID: "device-a"},
			op2:  &Operation{Timestamp: 100, DeviceID: "device-b"},
			want: -1,
		},
		{
			name: "same timestamp - reverse device ID",
			op1:  &Operation{Timestamp: 100, DeviceID: "device-b"},
			op2:  &Operation{Timestamp: 100, DeviceID: "device-a"},
			want: 1,
		},
		{
			name: "identical operations",
			op1:  &Operation{Timestamp: 100, DeviceID: "device-a"},
			op2:  &Operation{Timestamp: 100, DeviceID: "device-a"},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareOperations(tt.op1, tt.op2)
			if got != tt.want {
				t.Errorf("CompareOperations() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIdempotency(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

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
	_ = db.QueryRow("SELECT COUNT(*) FROM todos").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 todo, got %d", count)
	}

	if err := op.Apply(db); err != nil {
		t.Fatalf("failed to apply operation second time: %v", err)
	}

	_ = db.QueryRow("SELECT COUNT(*) FROM todos").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 todo after second apply (idempotency), got %d", count)
	}
}

func TestCommutativity(t *testing.T) {
	db1 := setupTestDB(t)
	defer func() { _ = db1.Close() }()

	db2 := setupTestDB(t)
	defer func() { _ = db2.Close() }()

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
	_ = db1.QueryRow("SELECT task FROM todos WHERE id='test-todo-1'").Scan(&task1)
	_ = db2.QueryRow("SELECT task FROM todos WHERE id='test-todo-1'").Scan(&task2)

	if task1 != task2 {
		t.Errorf("commutativity failed: db1 has '%s', db2 has '%s'", task1, task2)
	}
}

func TestLWWConflictResolution(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

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
	_ = db.QueryRow("SELECT task FROM todos WHERE id='test-todo-1'").Scan(&task)

	if task != "Buy eggs" {
		t.Errorf("LWW failed: expected 'Buy eggs', got '%s'", task)
	}
}

func TestDeleteVsUpdateConflict(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

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

	_ = createOp.Apply(db)
	_ = updateOp.Apply(db)
	_ = deleteOp.Apply(db)

	var deleted int
	_ = db.QueryRow("SELECT deleted FROM todos WHERE id='test-todo-1'").Scan(&deleted)

	if deleted != 1 {
		t.Errorf("delete should win with higher timestamp, got deleted=%d", deleted)
	}
}

func TestOutOfOrderDelivery(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

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

	_ = op3.Apply(db)
	_ = op1.Apply(db)
	_ = op2.Apply(db)

	var completed int
	_ = db.QueryRow("SELECT completed FROM todos WHERE id='test-todo-1'").Scan(&completed)

	if completed != 1 {
		t.Errorf("out-of-order delivery failed: expected completed=1, got %d", completed)
	}
}

func TestRebuildState(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

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
	_ = db.QueryRow("SELECT COUNT(*) FROM todos WHERE deleted=0").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 todos, got %d", count)
	}

	var completed int
	_ = db.QueryRow("SELECT completed FROM todos WHERE id='test-todo-1'").Scan(&completed)
	if completed != 1 {
		t.Errorf("expected todo to be completed, got completed=%d", completed)
	}
}

func TestTombstones(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

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

	_ = createOp.Apply(db)
	_ = deleteOp.Apply(db)

	var deleted int
	err := db.QueryRow("SELECT deleted FROM todos WHERE id='test-todo-1'").Scan(&deleted)
	if err != nil {
		t.Fatalf("tombstone not created: %v", err)
	}

	if deleted != 1 {
		t.Errorf("expected deleted=1, got %d", deleted)
	}

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM todos WHERE id='test-todo-1' AND deleted=0").Scan(&count)
	if count != 0 {
		t.Error("deleted todo should not be returned when filtering deleted=0")
	}
}

func TestApplyOperationInTxUnknownType(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	op := &Operation{
		ID:        "op1",
		Type:      "UNKNOWN_TYPE",
		TodoID:    "test-todo-1",
		Data:      []byte("{}"),
		Timestamp: time.Now().UnixNano(),
		DeviceID:  "device-a",
	}

	err = applyOperationInTx(tx, op)
	if err == nil {
		t.Error("expected error for unknown operation type")
	}
	if !strings.Contains(err.Error(), "unknown operation type") {
		t.Errorf("expected 'unknown operation type' error, got: %v", err)
	}
}

func TestApplyOperationInTxLWWSkipsOlder(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	createOp := &Operation{
		ID:        "op1",
		Type:      OpTypeCreate,
		TodoID:    "test-todo-1",
		Data:      createTestTodoJSON("Original", false),
		Timestamp: 200,
		DeviceID:  "device-a",
	}
	_ = createOp.Apply(db)

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	olderOp := &Operation{
		ID:        "op2",
		Type:      OpTypeUpdate,
		TodoID:    "test-todo-1",
		Data:      createTestTodoJSON("Should be skipped", false),
		Timestamp: 100,
		DeviceID:  "device-b",
	}

	err = applyOperationInTx(tx, olderOp)
	if err != nil {
		t.Fatalf("applyOperationInTx failed: %v", err)
	}

	_ = tx.Commit()

	var task string
	_ = db.QueryRow("SELECT task FROM todos WHERE id='test-todo-1'").Scan(&task)
	if task != "Original" {
		t.Errorf("expected 'Original' (older op skipped), got '%s'", task)
	}
}

func TestApplyCreateOrUpdateInTxEmptyData(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	op := &Operation{
		ID:        "op1",
		Type:      OpTypeCreate,
		TodoID:    "test-todo-1",
		Data:      []byte{},
		Timestamp: time.Now().UnixNano(),
		DeviceID:  "device-a",
	}

	err = applyCreateOrUpdateInTx(tx, op, false)
	if err == nil {
		t.Error("expected error for empty data")
	}
	if !strings.Contains(err.Error(), "requires data") {
		t.Errorf("expected 'requires data' error, got: %v", err)
	}
}

func TestApplyCreateOrUpdateInTxInvalidJSON(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	op := &Operation{
		ID:        "op1",
		Type:      OpTypeCreate,
		TodoID:    "test-todo-1",
		Data:      []byte("invalid json"),
		Timestamp: time.Now().UnixNano(),
		DeviceID:  "device-a",
	}

	err = applyCreateOrUpdateInTx(tx, op, false)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "failed to unmarshal") {
		t.Errorf("expected 'failed to unmarshal' error, got: %v", err)
	}
}

func TestApplyCompleteInTxEmptyData(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	err = applyCompleteInTx(tx, &Operation{
		ID:        "op1",
		Type:      OpTypeComplete,
		TodoID:    "test-todo-1",
		Data:      []byte{},
		Timestamp: time.Now().UnixNano(),
		DeviceID:  "device-a",
	}, false)
	if err == nil {
		t.Error("expected error for empty data")
	}
	if !strings.Contains(err.Error(), "COMPLETE operation requires data") {
		t.Errorf("expected 'COMPLETE operation requires data' error, got: %v", err)
	}
}

func TestApplyCompleteInTxCreatesIfNotExists(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}

	todoJSON := map[string]interface{}{
		"id":         "test-todo-1",
		"task":       "Complete creates todo",
		"note":       "",
		"deadline":   float64(0),
		"completed":  true,
		"created_at": float64(time.Now().Unix()),
	}
	data, _ := json.Marshal(todoJSON)

	err = applyCompleteInTx(tx, &Operation{
		ID:        "op1",
		Type:      OpTypeComplete,
		TodoID:    "test-todo-1",
		Data:      data,
		Timestamp: time.Now().UnixNano(),
		DeviceID:  "device-a",
	}, false)
	if err != nil {
		t.Fatalf("applyCompleteInTx failed: %v", err)
	}

	_ = tx.Commit()

	var task string
	var completed int
	_ = db.QueryRow("SELECT task, completed FROM todos WHERE id='test-todo-1'").Scan(&task, &completed)
	if task != "Complete creates todo" {
		t.Errorf("expected 'Complete creates todo', got '%s'", task)
	}
	if completed != 1 {
		t.Error("expected completed=1")
	}
}

func TestApplyDeleteInTxNoOpIfNotExists(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}

	err = applyDeleteInTx(tx, &Operation{
		ID:        "op1",
		Type:      OpTypeDelete,
		TodoID:    "nonexistent-todo",
		Data:      []byte("{}"),
		Timestamp: time.Now().UnixNano(),
		DeviceID:  "device-a",
	}, false)

	if err != nil {
		t.Fatalf("applyDeleteInTx should be no-op for non-existent, got: %v", err)
	}

	_ = tx.Commit()

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM todos WHERE id='nonexistent-todo'").Scan(&count)
	if count != 0 {
		t.Error("no todo should be created for delete on non-existent")
	}
}

func TestRebuildStateEmpty(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	err := RebuildState(db, []*Operation{})
	if err != nil {
		t.Fatalf("RebuildState with empty operations should succeed: %v", err)
	}

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM todos").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 todos, got %d", count)
	}
}

func TestRebuildStateClearsExisting(t *testing.T) {
	db := setupTestDB(t)
	defer func() { _ = db.Close() }()

	_, _ = db.Exec(`
		INSERT INTO todos (id, task, note, deadline, completed, created_at, updated_at, deleted)
		VALUES ('existing', 'Existing Todo', '', 0, 0, 1, 1, 0)
	`)

	operations := []*Operation{
		{
			ID:        "op1",
			Type:      OpTypeCreate,
			TodoID:    "new-todo",
			Data:      createTestTodoJSON("New Todo", false),
			Timestamp: 100,
			DeviceID:  "device-a",
		},
	}

	err := RebuildState(db, operations)
	if err != nil {
		t.Fatalf("RebuildState failed: %v", err)
	}

	var existingCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM todos WHERE id='existing'").Scan(&existingCount)
	if existingCount != 0 {
		t.Error("existing todo should be cleared")
	}

	var newCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM todos WHERE id='new-todo'").Scan(&newCount)
	if newCount != 1 {
		t.Error("new todo should be created")
	}
}

func TestMergeEmpty(t *testing.T) {
	result := Merge([]*Operation{}, []*Operation{})
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d operations", len(result))
	}
}

func TestMergeTimestampTiebreaker(t *testing.T) {
	op1 := &Operation{ID: "op1", Timestamp: 100, DeviceID: "device-b"}
	op2 := &Operation{ID: "op2", Timestamp: 100, DeviceID: "device-a"}

	merged := Merge([]*Operation{op1}, []*Operation{op2})

	if len(merged) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(merged))
	}

	if merged[0].DeviceID != "device-a" {
		t.Errorf("expected device-a first (tiebreaker), got %s", merged[0].DeviceID)
	}
}

func TestMain(m *testing.M) {
	keyring.DisableForTesting()
	code := m.Run()
	keyring.EnableForTesting()
	os.Exit(code)
}
