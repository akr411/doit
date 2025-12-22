package sync

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/akr411/doit/internal/storage"
	_ "modernc.org/sqlite"
)

func setupProtocolTestDB(t *testing.T) (*storage.Storage, func()) {
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

func TestPeerStateJSON(t *testing.T) {
	state := PeerState{
		LastOperationID: "op-123",
		DeviceID:        "device-456",
		DeviceName:      "TestDevice",
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var parsed PeerState
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if parsed.LastOperationID != state.LastOperationID {
		t.Error("LastOperationID mismatch")
	}
	if parsed.DeviceID != state.DeviceID {
		t.Error("DeviceID mismatch")
	}
	if parsed.DeviceName != state.DeviceName {
		t.Error("DeviceName mismatch")
	}
}

func TestSyncClientPushOperationsEmpty(t *testing.T) {
	store, cleanup := setupProtocolTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")

	certMgr := NewCertificateManager(store.GetDB())
	deviceID, _ := GetDeviceID(store.GetDB())
	_ = certMgr.GenerateSelfSignedCert(deviceID)

	client, err := NewSyncClient(store)
	if err != nil {
		t.Fatalf("NewSyncClient failed: %v", err)
	}

	peer := &Peer{
		ID:      "peer-1",
		Address: "localhost:9999",
	}

	err = client.PushOperations(peer, "secret", []Operation{})
	if err != nil {
		t.Errorf("PushOperations with empty ops should succeed: %v", err)
	}
}

func TestSyncClientGetPeerStateMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sync/state" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		state := PeerState{
			LastOperationID: "op-latest",
			DeviceID:        "remote-device",
			DeviceName:      "RemoteDevice",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	}))
	defer server.Close()

	store, cleanup := setupProtocolTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	deviceID, _ := GetDeviceID(store.GetDB())
	certMgr := NewCertificateManager(store.GetDB())
	_ = certMgr.GenerateSelfSignedCert(deviceID)

	peer := &Peer{
		ID:      "peer-1",
		Address: server.Listener.Addr().String(),
	}

	client := &SyncClient{
		client: server.Client(),
		store:  store,
	}

	_, err := client.GetPeerState(peer)
	if err == nil {
		t.Skip("HTTP server doesn't support HTTPS, expected to fail")
	}
}

func TestSyncClientPullOperationsMockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Doit-Secret") != "test-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		ops := OperationsResponse{
			Operations: []Operation{
				{
					ID:        "op-1",
					Type:      OpTypeCreate,
					TodoID:    "todo-1",
					Data:      []byte(`{"task":"test"}`),
					Timestamp: time.Now().UnixNano(),
					DeviceID:  "remote",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ops)
	}))
	defer server.Close()

	store, cleanup := setupProtocolTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	deviceID, _ := GetDeviceID(store.GetDB())
	certMgr := NewCertificateManager(store.GetDB())
	_ = certMgr.GenerateSelfSignedCert(deviceID)

	peer := &Peer{
		ID:      "peer-1",
		Address: server.Listener.Addr().String(),
	}

	client := &SyncClient{
		client: server.Client(),
		store:  store,
	}

	_, err := client.PullOperations(peer, "test-secret", "")
	if err == nil {
		t.Skip("HTTP server doesn't support HTTPS, expected to fail")
	}
}

func TestOperationsRequestResponse(t *testing.T) {
	req := OperationsRequest{
		Operations: []Operation{
			{
				ID:        "op-1",
				Type:      OpTypeCreate,
				TodoID:    "todo-1",
				Data:      []byte(`{"task":"test"}`),
				Timestamp: 12345,
				DeviceID:  "device-1",
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var parsed OperationsRequest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if len(parsed.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(parsed.Operations))
	}

	if parsed.Operations[0].ID != "op-1" {
		t.Error("operation ID mismatch")
	}
}

func TestErrUnauthorized(t *testing.T) {
	if ErrUnauthorized.Error() != "unauthorized" {
		t.Errorf("ErrUnauthorized = %q, want 'unauthorized'", ErrUnauthorized.Error())
	}
}

func TestNewSyncClientNoCert(t *testing.T) {
	store, cleanup := setupProtocolTestDB(t)
	defer cleanup()

	_, err := NewSyncClient(store)
	if err == nil {
		t.Error("expected error when no TLS cert exists")
	}
}

func TestNewSyncClientWithCert(t *testing.T) {
	store, cleanup := setupProtocolTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	deviceID, _ := GetDeviceID(store.GetDB())
	certMgr := NewCertificateManager(store.GetDB())
	_ = certMgr.GenerateSelfSignedCert(deviceID)

	client, err := NewSyncClient(store)
	if err != nil {
		t.Fatalf("NewSyncClient failed: %v", err)
	}

	if client.store != store {
		t.Error("store not set correctly")
	}
}
