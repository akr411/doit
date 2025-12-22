package sync

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/akr411/doit/internal/storage"
	_ "modernc.org/sqlite"
)

func setupPeerTestDB(t *testing.T) (*storage.Storage, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	s, err := storage.NewWithPath(dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	cleanup := func() {
		_ = s.Close()
	}

	return s, cleanup
}

func TestNewPeerManager(t *testing.T) {
	store, cleanup := setupPeerTestDB(t)
	defer cleanup()

	pm := NewPeerManager(store)
	if pm == nil {
		t.Fatal("NewPeerManager returned nil")
	}
}

func TestLoadActivePeers(t *testing.T) {
	store, cleanup := setupPeerTestDB(t)
	defer cleanup()

	pm := NewPeerManager(store)

	peer := &Peer{
		ID:        "peer-1",
		Name:      "Test Peer",
		Address:   "192.168.1.100:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "connected",
		CreatedAt: time.Now().UnixNano(),
	}

	_ = store.AddOrUpdatePeer(peer)

	err := pm.LoadActivePeers()
	if err != nil {
		t.Fatalf("LoadActivePeers failed: %v", err)
	}

	_, ok := pm.GetPeer("peer-1")
	if !ok {
		t.Error("peer should be loaded")
	}
}

func TestGetPeer(t *testing.T) {
	store, cleanup := setupPeerTestDB(t)
	defer cleanup()

	pm := NewPeerManager(store)

	peer := &Peer{
		ID:        "peer-1",
		Name:      "Test Peer",
		Address:   "192.168.1.100:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "connected",
		CreatedAt: time.Now().UnixNano(),
	}

	_ = store.AddOrUpdatePeer(peer)
	_ = pm.LoadActivePeers()

	retrieved, ok := pm.GetPeer("peer-1")
	if !ok {
		t.Fatal("GetPeer should find peer")
	}

	if retrieved.Name != "Test Peer" {
		t.Errorf("expected 'Test Peer', got '%s'", retrieved.Name)
	}

	_, ok = pm.GetPeer("non-existent")
	if ok {
		t.Error("GetPeer should return false for non-existent peer")
	}
}

func TestAddOrUpdatePeer(t *testing.T) {
	store, cleanup := setupPeerTestDB(t)
	defer cleanup()

	pm := NewPeerManager(store)

	peer := &Peer{
		ID:        "peer-1",
		Name:      "Test Peer",
		Address:   "192.168.1.100:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "discovered",
		CreatedAt: time.Now().UnixNano(),
	}

	err := pm.AddOrUpdatePeer(peer)
	if err != nil {
		t.Fatalf("AddOrUpdatePeer failed: %v", err)
	}

	retrieved, ok := pm.GetPeer("peer-1")
	if !ok {
		t.Fatal("peer should be in memory")
	}

	if retrieved.Status != "discovered" {
		t.Errorf("expected 'discovered', got '%s'", retrieved.Status)
	}

	peer.Status = "connected"
	err = pm.AddOrUpdatePeer(peer)
	if err != nil {
		t.Fatalf("AddOrUpdatePeer (update) failed: %v", err)
	}

	retrieved, _ = pm.GetPeer("peer-1")
	if retrieved.Status != "connected" {
		t.Errorf("expected 'connected' after update, got '%s'", retrieved.Status)
	}
}

func TestGetActivePeersList(t *testing.T) {
	store, cleanup := setupPeerTestDB(t)
	defer cleanup()

	pm := NewPeerManager(store)

	peer1 := &Peer{
		ID:        "peer-1",
		Name:      "Peer 1",
		Address:   "192.168.1.100:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "connected",
		CreatedAt: time.Now().UnixNano(),
	}

	peer2 := &Peer{
		ID:        "peer-2",
		Name:      "Peer 2",
		Address:   "192.168.1.101:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "disconnected",
		CreatedAt: time.Now().UnixNano(),
	}

	peer3 := &Peer{
		ID:        "peer-3",
		Name:      "Peer 3",
		Address:   "192.168.1.102:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "syncing",
		CreatedAt: time.Now().UnixNano(),
	}

	_ = pm.AddOrUpdatePeer(peer1)
	_ = pm.AddOrUpdatePeer(peer2)
	_ = pm.AddOrUpdatePeer(peer3)

	activePeers := pm.GetActivePeersList()

	if len(activePeers) != 2 {
		t.Errorf("expected 2 active peers, got %d", len(activePeers))
	}

	hasConnected := false
	hasSyncing := false
	for _, p := range activePeers {
		if p.Status == "connected" {
			hasConnected = true
		}
		if p.Status == "syncing" {
			hasSyncing = true
		}
		if p.Status == "disconnected" {
			t.Error("disconnected peer should not be in active list")
		}
	}

	if !hasConnected || !hasSyncing {
		t.Error("active peers should include connected and syncing")
	}
}

func TestUpdatePeerStatus(t *testing.T) {
	store, cleanup := setupPeerTestDB(t)
	defer cleanup()

	pm := NewPeerManager(store)

	peer := &Peer{
		ID:        "peer-1",
		Name:      "Test Peer",
		Address:   "192.168.1.100:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "discovered",
		CreatedAt: time.Now().UnixNano(),
	}

	_ = store.AddOrUpdatePeer(peer)
	_ = pm.LoadActivePeers()

	err := pm.UpdatePeerStatus("peer-1", "connected")
	if err != nil {
		t.Fatalf("UpdatePeerStatus failed: %v", err)
	}

	retrieved, ok := pm.GetPeer("peer-1")
	if !ok {
		t.Fatal("peer should still be in cache")
	}
	if retrieved.Status != "connected" {
		t.Errorf("expected 'connected', got '%s'", retrieved.Status)
	}

	peers, _ := store.GetPeers()
	if peers[0].Status != "connected" {
		t.Error("status should be persisted in database")
	}
}

func TestUpdateNonExistentPeerStatus(t *testing.T) {
	store, cleanup := setupPeerTestDB(t)
	defer cleanup()

	pm := NewPeerManager(store)

	err := pm.UpdatePeerStatus("non-existent", "connected")
	if err != nil {
		t.Fatalf("UpdatePeerStatus should not error for non-existent peer: %v", err)
	}

	_, ok := pm.GetPeer("non-existent")
	if ok {
		t.Error("non-existent peer should not be in cache")
	}
}

func TestGetActivePeersCount(t *testing.T) {
	store, cleanup := setupPeerTestDB(t)
	defer cleanup()

	pm := NewPeerManager(store)

	activePeers := pm.GetActivePeersList()
	if len(activePeers) != 0 {
		t.Error("should have 0 active peers initially")
	}

	peer1 := &Peer{
		ID:        "peer-1",
		Name:      "Peer 1",
		Address:   "192.168.1.100:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "connected",
		CreatedAt: time.Now().UnixNano(),
	}

	peer2 := &Peer{
		ID:        "peer-2",
		Name:      "Peer 2",
		Address:   "192.168.1.101:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "disconnected",
		CreatedAt: time.Now().UnixNano(),
	}

	_ = pm.AddOrUpdatePeer(peer1)
	_ = pm.AddOrUpdatePeer(peer2)

	activePeers = pm.GetActivePeersList()
	if len(activePeers) != 1 {
		t.Errorf("expected 1 active peer (only connected), got %d", len(activePeers))
	}
}

func TestConcurrentPeerAccess(t *testing.T) {
	store, cleanup := setupPeerTestDB(t)
	defer cleanup()

	pm := NewPeerManager(store)

	peer := &Peer{
		ID:        "peer-1",
		Name:      "Test Peer",
		Address:   "192.168.1.100:49152",
		LastSeen:  time.Now().UnixNano(),
		Status:    "connected",
		CreatedAt: time.Now().UnixNano(),
	}

	_ = pm.AddOrUpdatePeer(peer)

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			pm.GetPeer("peer-1")
			pm.GetActivePeersList()
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
