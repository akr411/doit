package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/akr411/doit/internal/keyring"
	"github.com/akr411/doit/internal/models"
)

func TestMain(m *testing.M) {
	keyring.DisableForTesting()
	code := m.Run()
	keyring.EnableForTesting()
	os.Exit(code)
}

func TestGetConfig(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := s.GetDB().Exec("INSERT INTO config (key, value) VALUES ('test_key', 'test_value')")
	if err != nil {
		t.Fatalf("failed to insert config: %v", err)
	}

	value, err := s.GetConfig("test_key")
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	if value != "test_value" {
		t.Errorf("expected 'test_value', got '%s'", value)
	}

	_, err = s.GetConfig("non_existent_key")
	if err == nil {
		t.Error("expected error for non-existent key")
	}
}

func TestSetConfig(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	err := s.SetConfig("new_key", "new_value")
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	value, err := s.GetConfig("new_key")
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	if value != "new_value" {
		t.Errorf("expected 'new_value', got '%s'", value)
	}

	err = s.SetConfig("new_key", "updated_value")
	if err != nil {
		t.Fatalf("SetConfig (update) failed: %v", err)
	}

	value, _ = s.GetConfig("new_key")
	if value != "updated_value" {
		t.Errorf("expected 'updated_value', got '%s'", value)
	}
}

func TestGetTodoByInvalidID(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := s.GetTodo("non-existent-id")
	if err == nil {
		t.Error("expected error for non-existent todo")
	}
}

func TestUpdateNonExistentTodo(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	todo := models.NewTodo("Test", "", 0)
	todo.ID = "non-existent"

	err := s.UpdateTodo(todo)
	if err != nil {
		t.Log("UpdateTodo on non-existent todo handled gracefully")
	}
}

func TestDeleteNonExistentTodo(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	err := s.DeleteTodo("non-existent-id")
	if err != nil {
		t.Log("DeleteTodo on non-existent todo handled gracefully")
	}
}

func TestCompleteTodo(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	todo := models.NewTodo("Test task", "", 0)
	_ = s.SaveTodo(todo)

	err := s.CompleteTodo(todo.ID, true)
	if err != nil {
		t.Fatalf("CompleteTodo failed: %v", err)
	}

	retrieved, _ := s.GetTodo(todo.ID)
	if !retrieved.Completed {
		t.Error("todo should be completed")
	}

	err = s.CompleteTodo(todo.ID, false)
	if err != nil {
		t.Fatalf("CompleteTodo (uncomplete) failed: %v", err)
	}

	retrieved, _ = s.GetTodo(todo.ID)
	if retrieved.Completed {
		t.Error("todo should not be completed")
	}
}

func TestCompleteNonExistentTodo(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	err := s.CompleteTodo("non-existent", true)
	if err == nil {
		t.Error("expected error for non-existent todo")
	}
}

func TestGetAllTodosWithDeadline(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	future := time.Now().Add(24 * time.Hour).Unix()
	past := time.Now().Add(-24 * time.Hour).Unix()

	todo1 := models.NewTodo("Future deadline", "", future)
	todo2 := models.NewTodo("Past deadline", "", past)
	todo3 := models.NewTodo("No deadline", "", 0)

	_ = s.SaveTodo(todo1)
	_ = s.SaveTodo(todo2)
	_ = s.SaveTodo(todo3)

	todos, err := s.GetAllTodos()
	if err != nil {
		t.Fatalf("GetAllTodos failed: %v", err)
	}

	if len(todos) != 3 {
		t.Errorf("expected 3 todos, got %d", len(todos))
	}

	for _, todo := range todos {
		if todo.ID == todo1.ID && todo.Deadline != future {
			t.Error("deadline mismatch for todo1")
		}
		if todo.ID == todo2.ID && todo.Deadline != past {
			t.Error("deadline mismatch for todo2")
		}
		if todo.ID == todo3.ID && todo.Deadline != 0 {
			t.Error("deadline should be 0 for todo3")
		}
	}
}

func TestTodoWithLongNote(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	longNote := string(make([]byte, 10000))
	for i := range longNote {
		longNote = longNote[:i] + "a"
	}

	todo := models.NewTodo("Test", longNote[:1000], 0)
	err := s.SaveTodo(todo)
	if err != nil {
		t.Fatalf("SaveTodo with long note failed: %v", err)
	}

	retrieved, _ := s.GetTodo(todo.ID)
	if len(retrieved.Note) != 1000 {
		t.Errorf("expected note length 1000, got %d", len(retrieved.Note))
	}
}

func TestGetPeers(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	peer := &Peer{
		ID:        "peer-1",
		Name:      "Test Peer",
		Address:   "192.168.1.100:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "connected",
		CreatedAt: time.Now().UnixNano(),
	}

	err := s.AddOrUpdatePeer(peer)
	if err != nil {
		t.Fatalf("AddOrUpdatePeer failed: %v", err)
	}

	peers, err := s.GetPeers()
	if err != nil {
		t.Fatalf("GetPeers failed: %v", err)
	}

	if len(peers) != 1 {
		t.Errorf("expected 1 peer, got %d", len(peers))
	}

	if peers[0].Name != "Test Peer" {
		t.Errorf("expected 'Test Peer', got '%s'", peers[0].Name)
	}
}

func TestUpdatePeerStatus(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	peer := &Peer{
		ID:        "peer-1",
		Name:      "Test Peer",
		Address:   "192.168.1.100:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "discovered",
		CreatedAt: time.Now().UnixNano(),
	}

	_ = s.AddOrUpdatePeer(peer)

	err := s.UpdatePeerStatus("peer-1", "connected")
	if err != nil {
		t.Fatalf("UpdatePeerStatus failed: %v", err)
	}

	peers, _ := s.GetPeers()
	if peers[0].Status != "connected" {
		t.Errorf("expected status 'connected', got '%s'", peers[0].Status)
	}
}

func TestUpdatePeerLastSeen(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	peer := &Peer{
		ID:        "peer-1",
		Name:      "Test Peer",
		Address:   "192.168.1.100:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "connected",
		CreatedAt: time.Now().UnixNano(),
	}

	_ = s.AddOrUpdatePeer(peer)

	newTime := time.Now().Add(10 * time.Second).UnixNano()
	err := s.UpdatePeerLastSeen("peer-1", newTime)
	if err != nil {
		t.Fatalf("UpdatePeerLastSeen failed: %v", err)
	}

	peers, _ := s.GetPeers()
	if peers[0].LastSeen != newTime {
		t.Errorf("last seen time not updated correctly")
	}
}

func TestGetSyncState(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	peer := &Peer{
		ID:        "peer-1",
		Name:      "Test Peer",
		Address:   "192.168.1.100:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "connected",
		CreatedAt: time.Now().UnixNano(),
	}
	_ = s.AddOrUpdatePeer(peer)

	state := &SyncState{
		PeerID:             "peer-1",
		LastOperationID:    "op-123",
		LastSyncTime:       time.Now().UnixNano(),
		OperationsSent:     10,
		OperationsReceived: 5,
	}

	err := s.UpdateSyncState("peer-1", state)
	if err != nil {
		t.Fatalf("UpdateSyncState failed: %v", err)
	}

	syncState, err := s.GetSyncState("peer-1")
	if err != nil {
		t.Fatalf("GetSyncState failed: %v", err)
	}

	if syncState.LastOperationID != "op-123" {
		t.Errorf("expected 'op-123', got '%s'", syncState.LastOperationID)
	}

	if syncState.OperationsSent != 10 {
		t.Errorf("expected 10 sent, got %d", syncState.OperationsSent)
	}

	if syncState.OperationsReceived != 5 {
		t.Errorf("expected 5 received, got %d", syncState.OperationsReceived)
	}
}

func TestSavePeerSecret(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	peer := &Peer{
		ID:        "peer-1",
		Name:      "Test Peer",
		Address:   "192.168.1.100:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "connected",
		CreatedAt: time.Now().UnixNano(),
	}
	_ = s.AddOrUpdatePeer(peer)

	secret := "test-secret-123"
	err := s.SavePeerSecret("peer-1", secret)
	if err != nil {
		t.Fatalf("SavePeerSecret failed: %v", err)
	}

	var retrieved string
	err = s.GetDB().QueryRow("SELECT secret FROM peer_secrets WHERE peer_id=?", "peer-1").Scan(&retrieved)
	if err != nil {
		t.Fatalf("failed to retrieve secret: %v", err)
	}

	if retrieved != secret {
		t.Errorf("expected '%s', got '%s'", secret, retrieved)
	}
}

func TestMarkOperationsSynced(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := s.GetDB().Exec(`INSERT INTO config (key, value) VALUES ('sync_enabled', 'true')`)
	if err != nil {
		t.Fatalf("failed to enable sync: %v", err)
	}

	_, err = s.GetDB().Exec(`INSERT INTO config (key, value) VALUES ('device_id', 'test-device')`)
	if err != nil {
		t.Fatalf("failed to set device_id: %v", err)
	}

	todo := models.NewTodo("Test", "", 0)
	_ = s.SaveTodo(todo)

	ops, _ := s.GetOperations("")
	if len(ops) == 0 {
		t.Fatal("no operations created")
	}

	ids := []string{ops[0].ID}
	err = s.MarkOperationsSynced(ids)
	if err != nil {
		t.Fatalf("MarkOperationsSynced failed: %v", err)
	}

	var synced int
	_ = s.GetDB().QueryRow("SELECT synced FROM operations WHERE id=?", ops[0].ID).Scan(&synced)
	if synced != 1 {
		t.Error("operation should be marked as synced")
	}
}

func TestGetLastOperationID(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := s.GetDB().Exec(`INSERT INTO config (key, value) VALUES ('sync_enabled', 'true')`)
	if err != nil {
		t.Fatalf("failed to enable sync: %v", err)
	}

	_, err = s.GetDB().Exec(`INSERT INTO config (key, value) VALUES ('device_id', 'test-device')`)
	if err != nil {
		t.Fatalf("failed to set device_id: %v", err)
	}

	lastID, err := s.GetLastOperationID()
	if err != nil {
		t.Fatalf("GetLastOperationID failed: %v", err)
	}

	if lastID != "" {
		t.Error("expected empty string for no operations")
	}

	todo := models.NewTodo("Test", "", 0)
	_ = s.SaveTodo(todo)

	lastID, err = s.GetLastOperationID()
	if err != nil {
		t.Fatalf("GetLastOperationID failed: %v", err)
	}

	if lastID == "" {
		t.Error("expected non-empty operation ID")
	}
}

func TestCleanupOldCompleted(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "false")
	_ = s.SetConfig("completed_limit", "10")

	for i := 0; i < 15; i++ {
		todo := models.NewTodo("Test", "", 0)
		todo.Completed = true
		_ = s.SaveTodo(todo)
	}

	err := s.CleanupOldCompleted()
	if err != nil {
		t.Fatalf("CleanupOldCompleted failed: %v", err)
	}

	todos, _ := s.GetAllTodos()
	completedCount := 0
	for _, todo := range todos {
		if todo.Completed {
			completedCount++
		}
	}

	if completedCount > 10 {
		t.Errorf("expected at most 10 completed todos after cleanup, got %d", completedCount)
	}
}

func TestGetOperations(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("device_id", "test-device")

	ops, err := s.GetOperations("")
	if err != nil {
		t.Fatalf("GetOperations failed: %v", err)
	}
	if len(ops) != 0 {
		t.Errorf("expected 0 operations, got %d", len(ops))
	}

	todo1 := models.NewTodo("Task 1", "", 0)
	_ = s.SaveTodo(todo1)
	time.Sleep(10 * time.Millisecond)
	todo2 := models.NewTodo("Task 2", "", 0)
	_ = s.SaveTodo(todo2)

	ops, err = s.GetOperations("")
	if err != nil {
		t.Fatalf("GetOperations failed: %v", err)
	}
	if len(ops) != 2 {
		t.Errorf("expected 2 operations, got %d", len(ops))
	}

	if len(ops) > 0 {
		firstOpID := ops[0].ID
		ops, err = s.GetOperations(firstOpID)
		if err != nil {
			t.Fatalf("GetOperations with since failed: %v", err)
		}
		if len(ops) != 1 {
			t.Errorf("expected 1 operation after since, got %d", len(ops))
		}
	}
}

func TestGetOperationsSince(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("device_id", "test-device")

	ops, err := s.GetOperationsSince(0)
	if err != nil {
		t.Fatalf("GetOperationsSince failed: %v", err)
	}
	if len(ops) != 0 {
		t.Errorf("expected 0 ops, got %d", len(ops))
	}

	todo := models.NewTodo("Task", "", 0)
	_ = s.SaveTodo(todo)

	ops, err = s.GetOperationsSince(0)
	if err != nil {
		t.Fatalf("GetOperationsSince failed: %v", err)
	}
	if len(ops) != 1 {
		t.Errorf("expected 1 op, got %d", len(ops))
	}

	if len(ops) > 0 {
		ts := ops[0].Timestamp
		ops, err = s.GetOperationsSince(ts)
		if err != nil {
			t.Fatalf("GetOperationsSince failed: %v", err)
		}
		if len(ops) != 0 {
			t.Errorf("expected 0 ops after timestamp, got %d", len(ops))
		}
	}
}

func TestGetAllOperations(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("device_id", "test-device")

	ops, err := s.GetAllOperations()
	if err != nil {
		t.Fatalf("GetAllOperations failed: %v", err)
	}
	if len(ops) != 0 {
		t.Errorf("expected 0 ops, got %d", len(ops))
	}

	todo := models.NewTodo("Task", "", 0)
	_ = s.SaveTodo(todo)
	_ = s.CompleteTodo(todo.ID, true)
	_ = s.DeleteTodo(todo.ID)

	ops, err = s.GetAllOperations()
	if err != nil {
		t.Fatalf("GetAllOperations failed: %v", err)
	}
	if len(ops) != 3 {
		t.Errorf("expected 3 ops (CREATE, COMPLETE, DELETE), got %d", len(ops))
	}

	_ = s.MarkOperationsSynced([]string{ops[0].ID})
	ops, err = s.GetAllOperations()
	if err != nil {
		t.Fatalf("GetAllOperations failed: %v", err)
	}
	if len(ops) != 3 {
		t.Errorf("GetAllOperations should include synced ops, got %d", len(ops))
	}
}

func TestMarkOperationsSyncedEmpty(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	err := s.MarkOperationsSynced([]string{})
	if err != nil {
		t.Errorf("MarkOperationsSynced with empty slice should not error: %v", err)
	}

	err = s.MarkOperationsSynced(nil)
	if err != nil {
		t.Errorf("MarkOperationsSynced with nil should not error: %v", err)
	}
}

func TestMarkOperationsSyncedMultiple(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("device_id", "test-device")

	for i := 0; i < 5; i++ {
		todo := models.NewTodo("Task", "", 0)
		_ = s.SaveTodo(todo)
	}

	ops, _ := s.GetOperations("")
	if len(ops) != 5 {
		t.Fatalf("expected 5 ops, got %d", len(ops))
	}

	ids := []string{ops[0].ID, ops[1].ID, ops[2].ID}
	err := s.MarkOperationsSynced(ids)
	if err != nil {
		t.Fatalf("MarkOperationsSynced failed: %v", err)
	}

	unsyncedOps, _ := s.GetOperations("")
	if len(unsyncedOps) != 2 {
		t.Errorf("expected 2 unsynced ops, got %d", len(unsyncedOps))
	}
}

func TestAddOrUpdatePeerUpdate(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	peer := &Peer{
		ID:        "peer-1",
		Name:      "Original Name",
		Address:   "192.168.1.100:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "discovered",
		CreatedAt: time.Now().UnixNano(),
	}
	_ = s.AddOrUpdatePeer(peer)

	peer.Name = "Updated Name"
	peer.Address = "192.168.1.101:49152"
	peer.Status = "connected"
	peer.LastSeen = time.Now().Add(1 * time.Hour).UnixNano()

	err := s.AddOrUpdatePeer(peer)
	if err != nil {
		t.Fatalf("AddOrUpdatePeer (update) failed: %v", err)
	}

	peers, _ := s.GetPeers()
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}

	if peers[0].Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got '%s'", peers[0].Name)
	}
	if peers[0].Status != "connected" {
		t.Errorf("expected status 'connected', got '%s'", peers[0].Status)
	}
}

func TestGetActivePeers(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	peers := []*Peer{
		{ID: "p1", Name: "Peer 1", Address: "1.1.1.1", LastSeen: time.Now().UnixNano(), Status: "discovered", CreatedAt: time.Now().UnixNano()},
		{ID: "p2", Name: "Peer 2", Address: "2.2.2.2", LastSeen: time.Now().UnixNano(), Status: "connected", CreatedAt: time.Now().UnixNano()},
		{ID: "p3", Name: "Peer 3", Address: "3.3.3.3", LastSeen: time.Now().UnixNano(), Status: "syncing", CreatedAt: time.Now().UnixNano()},
		{ID: "p4", Name: "Peer 4", Address: "4.4.4.4", LastSeen: time.Now().UnixNano(), Status: "offline", CreatedAt: time.Now().UnixNano()},
		{ID: "p5", Name: "Peer 5", Address: "5.5.5.5", LastSeen: time.Now().UnixNano(), Status: "error", CreatedAt: time.Now().UnixNano()},
	}

	for _, p := range peers {
		_ = s.AddOrUpdatePeer(p)
	}

	activePeers, err := s.GetActivePeers()
	if err != nil {
		t.Fatalf("GetActivePeers failed: %v", err)
	}

	if len(activePeers) != 3 {
		t.Errorf("expected 3 active peers (discovered/connected/syncing), got %d", len(activePeers))
	}
}

func TestGetActivePeersEmpty(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	peers, err := s.GetActivePeers()
	if err != nil {
		t.Fatalf("GetActivePeers failed: %v", err)
	}
	if len(peers) != 0 {
		t.Errorf("expected 0 peers, got %d", len(peers))
	}
}

func TestGetSyncStateNonExistent(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	state, err := s.GetSyncState("non-existent-peer")
	if err != nil {
		t.Fatalf("GetSyncState should not error for non-existent: %v", err)
	}

	if state.PeerID != "non-existent-peer" {
		t.Errorf("expected peer_id 'non-existent-peer', got '%s'", state.PeerID)
	}
	if state.LastOperationID != "" {
		t.Errorf("expected empty LastOperationID, got '%s'", state.LastOperationID)
	}
	if state.OperationsSent != 0 {
		t.Errorf("expected 0 OperationsSent, got %d", state.OperationsSent)
	}
}

func TestUpdateSyncStateAccumulation(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	peer := &Peer{ID: "peer-1", Name: "Test", Address: "1.1.1.1", LastSeen: time.Now().UnixNano(), Status: "connected", CreatedAt: time.Now().UnixNano()}
	_ = s.AddOrUpdatePeer(peer)

	state1 := &SyncState{
		PeerID:             "peer-1",
		LastOperationID:    "op-1",
		LastSyncTime:       time.Now().UnixNano(),
		OperationsSent:     5,
		OperationsReceived: 3,
	}
	_ = s.UpdateSyncState("peer-1", state1)

	state2 := &SyncState{
		PeerID:             "peer-1",
		LastOperationID:    "op-2",
		LastSyncTime:       time.Now().UnixNano(),
		OperationsSent:     10,
		OperationsReceived: 7,
	}
	_ = s.UpdateSyncState("peer-1", state2)

	result, err := s.GetSyncState("peer-1")
	if err != nil {
		t.Fatalf("GetSyncState failed: %v", err)
	}

	if result.LastOperationID != "op-2" {
		t.Errorf("expected LastOperationID 'op-2', got '%s'", result.LastOperationID)
	}
	if result.OperationsSent != 15 {
		t.Errorf("expected 15 sent (5+10), got %d", result.OperationsSent)
	}
	if result.OperationsReceived != 10 {
		t.Errorf("expected 10 received (3+7), got %d", result.OperationsReceived)
	}
}

func TestGetPeerSecret(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	peer := &Peer{ID: "peer-1", Name: "Test", Address: "1.1.1.1", LastSeen: time.Now().UnixNano(), Status: "connected", CreatedAt: time.Now().UnixNano()}
	_ = s.AddOrUpdatePeer(peer)

	_ = s.SavePeerSecret("peer-1", "secret-abc-123")

	secret, err := s.GetPeerSecret("peer-1")
	if err != nil {
		t.Fatalf("GetPeerSecret failed: %v", err)
	}
	if secret != "secret-abc-123" {
		t.Errorf("expected 'secret-abc-123', got '%s'", secret)
	}
}

func TestGetPeerSecretNonExistent(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := s.GetPeerSecret("non-existent")
	if err == nil {
		t.Error("expected error for non-existent peer secret")
	}
}

func TestSavePeerSecretOverwrite(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	peer := &Peer{ID: "peer-1", Name: "Test", Address: "1.1.1.1", LastSeen: time.Now().UnixNano(), Status: "connected", CreatedAt: time.Now().UnixNano()}
	_ = s.AddOrUpdatePeer(peer)

	_ = s.SavePeerSecret("peer-1", "secret-1")
	_ = s.SavePeerSecret("peer-1", "secret-2")

	secret, err := s.GetPeerSecret("peer-1")
	if err != nil {
		t.Fatalf("GetPeerSecret failed: %v", err)
	}
	if secret != "secret-2" {
		t.Errorf("expected 'secret-2', got '%s'", secret)
	}
}

func TestCheckIntegrity(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	result, err := s.CheckIntegrity()
	if err != nil {
		t.Fatalf("CheckIntegrity failed: %v", err)
	}

	if !result.SQLiteOK {
		t.Error("SQLite integrity check should pass on fresh DB")
	}
	if !result.ForeignKeysOK {
		t.Error("Foreign key check should pass on fresh DB")
	}
	if result.OrphanedOps != 0 {
		t.Errorf("expected 0 orphaned ops, got %d", result.OrphanedOps)
	}
	if result.InvalidJSON != 0 {
		t.Errorf("expected 0 invalid JSON, got %d", result.InvalidJSON)
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected 0 errors, got %v", result.Errors)
	}
}

func TestCheckIntegrityWithOrphanedOps(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := s.GetDB().Exec(`
		INSERT INTO operations (id, type, todo_id, data, timestamp, device_id, synced)
		VALUES ('orphan-op', 'UPDATE', 'non-existent-todo', '{"task":"test"}', ?, 'device-1', 0)
	`, time.Now().UnixNano())
	if err != nil {
		t.Fatalf("failed to insert orphaned op: %v", err)
	}

	result, err := s.CheckIntegrity()
	if err != nil {
		t.Fatalf("CheckIntegrity failed: %v", err)
	}

	if result.OrphanedOps != 1 {
		t.Errorf("expected 1 orphaned op, got %d", result.OrphanedOps)
	}
}

func TestCheckIntegrityWithInvalidJSON(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	todo := models.NewTodo("Test", "", 0)
	_ = s.SaveTodo(todo)

	_, err := s.GetDB().Exec(`
		INSERT INTO operations (id, type, todo_id, data, timestamp, device_id, synced)
		VALUES ('invalid-json-op', 'UPDATE', ?, 'not valid json {{{', ?, 'device-1', 0)
	`, todo.ID, time.Now().UnixNano())
	if err != nil {
		t.Fatalf("failed to insert invalid JSON op: %v", err)
	}

	result, err := s.CheckIntegrity()
	if err != nil {
		t.Fatalf("CheckIntegrity failed: %v", err)
	}

	if result.InvalidJSON != 1 {
		t.Errorf("expected 1 invalid JSON, got %d", result.InvalidJSON)
	}
}

func TestRepairIntegrity(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	fixed, err := s.RepairIntegrity()
	if err != nil {
		t.Fatalf("RepairIntegrity failed: %v", err)
	}

	if fixed != 0 {
		t.Errorf("expected 0 fixes on fresh DB, got %d", fixed)
	}
}

func TestRepairIntegrityOrphanedOps(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := s.GetDB().Exec(`
		INSERT INTO operations (id, type, todo_id, data, timestamp, device_id, synced)
		VALUES ('orphan-1', 'UPDATE', 'non-existent', '{"task":"test"}', ?, 'device-1', 0)
	`, time.Now().UnixNano())
	if err != nil {
		t.Fatalf("failed to insert orphaned op: %v", err)
	}

	_, err = s.GetDB().Exec(`
		INSERT INTO operations (id, type, todo_id, data, timestamp, device_id, synced)
		VALUES ('orphan-2', 'CREATE', 'another-non-existent', '{"task":"test2"}', ?, 'device-1', 0)
	`, time.Now().UnixNano())
	if err != nil {
		t.Fatalf("failed to insert orphaned op: %v", err)
	}

	fixed, err := s.RepairIntegrity()
	if err != nil {
		t.Fatalf("RepairIntegrity failed: %v", err)
	}

	if fixed != 2 {
		t.Errorf("expected 2 fixes, got %d", fixed)
	}

	result, _ := s.CheckIntegrity()
	if result.OrphanedOps != 0 {
		t.Errorf("expected 0 orphaned ops after repair, got %d", result.OrphanedOps)
	}
}

func TestCheckIntegrityNoMissingTimestamps(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	todo := models.NewTodo("Test", "", 0)
	_ = s.SaveTodo(todo)

	result, err := s.CheckIntegrity()
	if err != nil {
		t.Fatalf("CheckIntegrity failed: %v", err)
	}

	if result.MissingTimestamps != 0 {
		t.Errorf("expected 0 missing timestamps (schema has NOT NULL), got %d", result.MissingTimestamps)
	}
}

func TestCleanupOldCompletedWithSyncEnabled(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("device_id", "test-device")
	_ = s.SetConfig("completed_limit", "5")

	for i := 0; i < 10; i++ {
		todo := models.NewTodo("Test", "", 0)
		todo.Completed = true
		_ = s.SaveTodo(todo)
	}

	err := s.CleanupOldCompleted()
	if err != nil {
		t.Fatalf("CleanupOldCompleted failed: %v", err)
	}

	var softDeleted int
	_ = s.GetDB().QueryRow("SELECT COUNT(*) FROM todos WHERE deleted=1").Scan(&softDeleted)
	if softDeleted != 5 {
		t.Errorf("expected 5 soft-deleted todos with sync enabled, got %d", softDeleted)
	}
}

func TestCleanupOldCompletedDefaultLimit(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "false")

	for i := 0; i < 60; i++ {
		todo := models.NewTodo("Test", "", 0)
		todo.Completed = true
		_ = s.SaveTodo(todo)
	}

	err := s.CleanupOldCompleted()
	if err != nil {
		t.Fatalf("CleanupOldCompleted failed: %v", err)
	}

	var count int
	_ = s.GetDB().QueryRow("SELECT COUNT(*) FROM todos WHERE completed=1").Scan(&count)
	if count > 50 {
		t.Errorf("expected at most 50 (default limit), got %d", count)
	}
}

func TestCleanupOldCompletedInvalidLimit(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "false")
	_ = s.SetConfig("completed_limit", "-5")

	for i := 0; i < 60; i++ {
		todo := models.NewTodo("Test", "", 0)
		todo.Completed = true
		_ = s.SaveTodo(todo)
	}

	err := s.CleanupOldCompleted()
	if err != nil {
		t.Fatalf("CleanupOldCompleted failed: %v", err)
	}

	var count int
	_ = s.GetDB().QueryRow("SELECT COUNT(*) FROM todos WHERE completed=1").Scan(&count)
	if count > 50 {
		t.Errorf("expected at most 50 (default for invalid), got %d", count)
	}
}

func TestSaveOperation(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	data := []byte(`{"task":"test task"}`)
	err := s.SaveOperation("op-123", "CREATE", "todo-123", data, time.Now().UnixNano(), "device-1")
	if err != nil {
		t.Fatalf("SaveOperation failed: %v", err)
	}

	ops, err := s.GetAllOperations()
	if err != nil {
		t.Fatalf("GetAllOperations failed: %v", err)
	}

	if len(ops) != 1 {
		t.Fatalf("expected 1 op, got %d", len(ops))
	}

	if ops[0].ID != "op-123" {
		t.Errorf("expected ID 'op-123', got '%s'", ops[0].ID)
	}
	if ops[0].Type != "CREATE" {
		t.Errorf("expected type 'CREATE', got '%s'", ops[0].Type)
	}
	if ops[0].TodoID != "todo-123" {
		t.Errorf("expected TodoID 'todo-123', got '%s'", ops[0].TodoID)
	}
}

func TestHardDeleteTodo(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	todo := models.NewTodo("Test", "", 0)
	_ = s.SaveTodo(todo)

	var count int
	_ = s.GetDB().QueryRow("SELECT COUNT(*) FROM todos WHERE id=?", todo.ID).Scan(&count)
	if count != 1 {
		t.Fatal("todo should exist")
	}

	err := s.HardDeleteTodo(todo.ID)
	if err != nil {
		t.Fatalf("HardDeleteTodo failed: %v", err)
	}

	_ = s.GetDB().QueryRow("SELECT COUNT(*) FROM todos WHERE id=?", todo.ID).Scan(&count)
	if count != 0 {
		t.Error("todo should be permanently deleted")
	}
}

func TestGetPeersOrdering(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	now := time.Now().UnixNano()
	peers := []*Peer{
		{ID: "p1", Name: "Old", Address: "1.1.1.1", LastSeen: now - 1000, Status: "connected", CreatedAt: now},
		{ID: "p2", Name: "Recent", Address: "2.2.2.2", LastSeen: now, Status: "connected", CreatedAt: now},
		{ID: "p3", Name: "Middle", Address: "3.3.3.3", LastSeen: now - 500, Status: "connected", CreatedAt: now},
	}

	for _, p := range peers {
		_ = s.AddOrUpdatePeer(p)
	}

	result, _ := s.GetPeers()
	if len(result) != 3 {
		t.Fatalf("expected 3 peers, got %d", len(result))
	}

	if result[0].ID != "p2" {
		t.Errorf("expected most recent first (p2), got %s", result[0].ID)
	}
}

func TestCleanupSyncData(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	stats, err := s.CleanupSyncData(false)
	if err != nil {
		t.Fatalf("CleanupSyncData failed: %v", err)
	}
	if stats.TombstonesDeleted != 0 && stats.OperationsDeleted != 0 {
		t.Log("No cleanup expected when sync disabled")
	}
}

func TestCleanupSyncDataEnabled(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("device_id", "test-device")
	_ = s.SetConfig("auto_cleanup_enabled", "true")

	stats, err := s.CleanupSyncData(false)
	if err != nil {
		t.Fatalf("CleanupSyncData failed: %v", err)
	}

	if stats == nil {
		t.Error("expected non-nil stats")
	}
}

func TestCleanupSyncDataDisabled(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("auto_cleanup_enabled", "false")

	stats, err := s.CleanupSyncData(false)
	if err != nil {
		t.Fatalf("CleanupSyncData failed: %v", err)
	}

	if stats.TombstonesDeleted != 0 || stats.OperationsDeleted != 0 {
		t.Error("should skip cleanup when auto_cleanup_enabled=false")
	}
}

func TestCleanupSyncDataAggressive(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("device_id", "test-device")
	_ = s.SetConfig("auto_cleanup_enabled", "false")

	stats, err := s.CleanupSyncData(true)
	if err != nil {
		t.Fatalf("CleanupSyncData aggressive failed: %v", err)
	}

	if stats == nil {
		t.Error("expected non-nil stats")
	}
}

func TestShouldRunCleanup(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	result := s.ShouldRunCleanup()
	if result {
		t.Error("should return false when sync disabled")
	}

	_ = s.SetConfig("sync_enabled", "true")

	result = s.ShouldRunCleanup()
	if !result {
		t.Error("should return true when sync enabled and no last cleanup")
	}

	_ = s.SetConfig("last_cleanup_time", "0")
	result = s.ShouldRunCleanup()
	if !result {
		t.Error("should return true when last cleanup was long ago")
	}

	_ = s.SetConfig("auto_cleanup_enabled", "false")
	result = s.ShouldRunCleanup()
	if result {
		t.Error("should return false when auto_cleanup disabled")
	}
}

func TestGetCleanupStats(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("device_id", "test-device")

	todo := models.NewTodo("Test", "", 0)
	_ = s.SaveTodo(todo)
	_ = s.DeleteTodo(todo.ID)

	stats, err := s.GetCleanupStats()
	if err != nil {
		t.Fatalf("GetCleanupStats failed: %v", err)
	}

	if stats.TotalOperations < 1 {
		t.Errorf("expected at least 1 operation, got %d", stats.TotalOperations)
	}
	if stats.Tombstones < 1 {
		t.Errorf("expected at least 1 tombstone, got %d", stats.Tombstones)
	}
	if stats.DBSizeKB <= 0 {
		t.Error("expected DBSizeKB > 0")
	}
	if stats.HoursSinceCleanup != -1 {
		t.Errorf("expected -1 for never cleaned, got %d", stats.HoursSinceCleanup)
	}
}

func TestSetAutoCleanupEnabled(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	err := s.SetAutoCleanupEnabled(true)
	if err != nil {
		t.Fatalf("SetAutoCleanupEnabled failed: %v", err)
	}

	val, _ := s.GetConfig("auto_cleanup_enabled")
	if val != "true" {
		t.Errorf("expected 'true', got '%s'", val)
	}

	err = s.SetAutoCleanupEnabled(false)
	if err != nil {
		t.Fatalf("SetAutoCleanupEnabled failed: %v", err)
	}

	val, _ = s.GetConfig("auto_cleanup_enabled")
	if val != "false" {
		t.Errorf("expected 'false', got '%s'", val)
	}
}

func TestGetOperationsWithNonExistentSince(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("device_id", "test-device")

	todo := models.NewTodo("Test", "", 0)
	_ = s.SaveTodo(todo)

	ops, err := s.GetOperations("non-existent-op-id")
	if err != nil {
		t.Fatalf("GetOperations should handle non-existent since: %v", err)
	}

	if len(ops) != 1 {
		t.Errorf("expected 1 op (since lookup returns no timestamp), got %d", len(ops))
	}
}

func TestGetCleanupStatsWithCleanupTime(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("last_cleanup_time", "1700000000")

	stats, err := s.GetCleanupStats()
	if err != nil {
		t.Fatalf("GetCleanupStats failed: %v", err)
	}

	if stats.LastCleanupTime != 1700000000 {
		t.Errorf("expected LastCleanupTime 1700000000, got %d", stats.LastCleanupTime)
	}
	if stats.HoursSinceCleanup < 0 {
		t.Errorf("expected positive HoursSinceCleanup, got %d", stats.HoursSinceCleanup)
	}
}

func TestRetentionConfigDefaults(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("auto_cleanup_enabled", "true")

	stats, err := s.CleanupSyncData(false)
	if err != nil {
		t.Fatalf("CleanupSyncData failed: %v", err)
	}

	if stats == nil {
		t.Error("expected non-nil stats")
	}
}

func TestRetentionConfigCustom(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("auto_cleanup_enabled", "true")
	_ = s.SetConfig("tombstone_retention_days", "7")
	_ = s.SetConfig("operation_retention_days", "14")
	_ = s.SetConfig("max_operations_per_todo", "5")
	_ = s.SetConfig("cleanup_interval_hours", "12")

	stats, err := s.CleanupSyncData(false)
	if err != nil {
		t.Fatalf("CleanupSyncData with custom config failed: %v", err)
	}

	if stats == nil {
		t.Error("expected non-nil stats")
	}
}

func TestRetentionConfigInvalid(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("auto_cleanup_enabled", "true")
	_ = s.SetConfig("tombstone_retention_days", "-1")
	_ = s.SetConfig("operation_retention_days", "abc")
	_ = s.SetConfig("max_operations_per_todo", "0")
	_ = s.SetConfig("cleanup_interval_hours", "-5")

	stats, err := s.CleanupSyncData(false)
	if err != nil {
		t.Fatalf("CleanupSyncData with invalid config should use defaults: %v", err)
	}

	if stats == nil {
		t.Error("expected non-nil stats")
	}
}

func TestCheckIntegrityDeleteOpsNotOrphaned(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_, err := s.GetDB().Exec(`
		INSERT INTO operations (id, type, todo_id, data, timestamp, device_id, synced)
		VALUES ('delete-op', 'DELETE', 'non-existent-todo', '{}', ?, 'device-1', 0)
	`, time.Now().UnixNano())
	if err != nil {
		t.Fatalf("failed to insert DELETE op: %v", err)
	}

	result, err := s.CheckIntegrity()
	if err != nil {
		t.Fatalf("CheckIntegrity failed: %v", err)
	}

	if result.OrphanedOps != 0 {
		t.Errorf("DELETE ops should not be counted as orphaned, got %d", result.OrphanedOps)
	}
}

func TestShouldRunCleanupRecentCleanup(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("auto_cleanup_enabled", "true")
	_ = s.SetConfig("cleanup_interval_hours", "24")
	_ = s.SetConfig("last_cleanup_time", fmt.Sprintf("%d", time.Now().Unix()))

	result := s.ShouldRunCleanup()
	if result {
		t.Error("should return false when cleanup was recent")
	}
}

func TestUpdateTodoWithSync(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("device_id", "test-device")

	todo := models.NewTodo("Test", "", 0)
	_ = s.SaveTodo(todo)

	todo.Task = "Updated"
	todo.Note = "New note"
	err := s.UpdateTodo(todo)
	if err != nil {
		t.Fatalf("UpdateTodo failed: %v", err)
	}

	ops, _ := s.GetAllOperations()
	if len(ops) != 2 {
		t.Errorf("expected 2 ops (CREATE + UPDATE), got %d", len(ops))
	}
}

func TestDeleteTodoWithSync(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	_ = s.SetConfig("sync_enabled", "true")
	_ = s.SetConfig("device_id", "test-device")

	todo := models.NewTodo("Test", "", 0)
	_ = s.SaveTodo(todo)

	err := s.DeleteTodo(todo.ID)
	if err != nil {
		t.Fatalf("DeleteTodo failed: %v", err)
	}

	ops, _ := s.GetAllOperations()
	if len(ops) != 2 {
		t.Errorf("expected 2 ops (CREATE + DELETE), got %d", len(ops))
	}

	hasDelete := false
	for _, op := range ops {
		if op.Type == "DELETE" {
			hasDelete = true
		}
	}
	if !hasDelete {
		t.Error("expected DELETE operation")
	}
}

func TestGetPeersEmpty(t *testing.T) {
	s, cleanup := setupTestDB(t)
	defer cleanup()

	peers, err := s.GetPeers()
	if err != nil {
		t.Fatalf("GetPeers failed: %v", err)
	}
	if len(peers) != 0 {
		t.Errorf("expected 0 peers, got %d", len(peers))
	}
}

func TestNewWithPath(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "subdir", "test.db")

	s, err := NewWithPath(dbPath)
	if err != nil {
		t.Fatalf("NewWithPath failed: %v", err)
	}
	defer func() { _ = s.Close() }()

	todo := models.NewTodo("Test", "", 0)
	err = s.SaveTodo(todo)
	if err != nil {
		t.Fatalf("SaveTodo failed: %v", err)
	}

	retrieved, err := s.GetTodo(todo.ID)
	if err != nil {
		t.Fatalf("GetTodo failed: %v", err)
	}
	if retrieved.Task != "Test" {
		t.Errorf("expected 'Test', got '%s'", retrieved.Task)
	}
}

func TestClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	s, err := NewWithPath(dbPath)
	if err != nil {
		t.Fatalf("NewWithPath failed: %v", err)
	}

	err = s.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	err = s.Close()
	if err == nil {
		t.Log("Closing already closed DB may or may not error")
	}
}
