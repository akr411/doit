package sync

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akr411/doit/internal/models"
	"github.com/akr411/doit/internal/storage"
)

func setupServerTestDB(t *testing.T) (*storage.Storage, func()) {
	return setupPeerTestDB(t)
}

func TestNewSyncServer(t *testing.T) {
	store, cleanup := setupServerTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	deviceID, _ := GetDeviceID(store.GetDB())

	peerMgr := NewPeerManager(store)

	server, err := NewSyncServer(store, peerMgr)
	if err != nil {
		t.Fatalf("NewSyncServer failed: %v", err)
	}

	if server == nil {
		t.Fatal("NewSyncServer returned nil")
	}

	if server.deviceID != deviceID {
		t.Errorf("expected device ID %s, got %s", deviceID, server.deviceID)
	}

	if server.secret == "" {
		t.Error("shared secret should be generated")
	}
}

func TestAuthMiddleware(t *testing.T) {
	store, cleanup := setupServerTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	peerMgr := NewPeerManager(store)

	server, _ := NewSyncServer(store, peerMgr)

	handler := server.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("authorized"))
	})

	t.Run("no secret header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("wrong secret", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Doit-Secret", "wrong-secret")
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("correct secret", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Doit-Secret", server.secret)
		w := httptest.NewRecorder()

		handler(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		if w.Body.String() != "authorized" {
			t.Errorf("expected 'authorized', got '%s'", w.Body.String())
		}
	})
}

func TestHandleGetState(t *testing.T) {
	store, cleanup := setupServerTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	deviceID, _ := GetDeviceID(store.GetDB())

	todo := models.NewTodo("Test", "", 0)
	_ = store.SaveTodo(todo)

	peerMgr := NewPeerManager(store)
	server, _ := NewSyncServer(store, peerMgr)

	req := httptest.NewRequest("GET", "/sync/state", nil)
	w := httptest.NewRecorder()

	server.handleGetState(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp StateResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.DeviceID != deviceID {
		t.Errorf("expected device ID %s, got %s", deviceID, resp.DeviceID)
	}

	if resp.DeviceName == "" {
		t.Error("device name should not be empty")
	}
}

func TestHandleGetOperations(t *testing.T) {
	store, cleanup := setupServerTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	peerMgr := NewPeerManager(store)
	server, _ := NewSyncServer(store, peerMgr)

	todo := models.NewTodo("Test", "", 0)
	_ = store.SaveTodo(todo)

	req := httptest.NewRequest("GET", "/sync/operations", nil)
	req.Header.Set("X-Doit-Secret", server.secret)
	w := httptest.NewRecorder()

	server.handleOperations(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp OperationsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Operations) == 0 {
		t.Error("expected operations from SaveTodo")
	}
}

func TestHandlePostOperations(t *testing.T) {
	store, cleanup := setupServerTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	peerMgr := NewPeerManager(store)
	server, _ := NewSyncServer(store, peerMgr)

	todoJSON := map[string]interface{}{
		"id":         "todo-1",
		"task":       "Remote task",
		"note":       "",
		"deadline":   0,
		"completed":  false,
		"created_at": time.Now().Unix(),
	}
	data, _ := json.Marshal(todoJSON)

	ops := []Operation{
		{
			ID:        "op-1",
			Type:      OpTypeCreate,
			TodoID:    "todo-1",
			Data:      data,
			Timestamp: time.Now().UnixNano(),
			DeviceID:  "remote-device",
		},
	}

	reqBody := OperationsRequest{Operations: ops}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/sync/operations", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Doit-Secret", server.secret)
	w := httptest.NewRecorder()

	server.handleOperations(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AppliedResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp.Applied != 1 {
		t.Errorf("expected 1 applied, got %d", resp.Applied)
	}

	todo, err := store.GetTodo("todo-1")
	if err != nil {
		t.Fatal("todo should be created")
	}

	if todo.Task != "Remote task" {
		t.Errorf("expected 'Remote task', got '%s'", todo.Task)
	}
}

func TestGetPort(t *testing.T) {
	store, cleanup := setupServerTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	peerMgr := NewPeerManager(store)
	server, _ := NewSyncServer(store, peerMgr)

	if server.GetPort() != 0 {
		t.Error("port should be 0 before Start()")
	}
}

func TestHandlePairNoCode(t *testing.T) {
	store, cleanup := setupServerTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	peerMgr := NewPeerManager(store)
	server, _ := NewSyncServer(store, peerMgr)

	reqBody := `{"device_id":"remote","device_name":"Remote","cert_fingerprint":"abc123"}`
	req := httptest.NewRequest("POST", "/pair", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handlePair(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (no pairing code), got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlePairInvalidCode(t *testing.T) {
	store, cleanup := setupServerTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	peerMgr := NewPeerManager(store)
	server, _ := NewSyncServer(store, peerMgr)

	reqBody := `{"pairing_code":"0000-0000","device_id":"remote","device_name":"Remote","cert_fingerprint":"abc123"}`
	req := httptest.NewRequest("POST", "/pair", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handlePair(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlePairValidCode(t *testing.T) {
	store, cleanup := setupServerTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	peerMgr := NewPeerManager(store)
	server, _ := NewSyncServer(store, peerMgr)

	code, err := server.pairingMgr.GenerateCode()
	if err != nil {
		t.Fatal(err)
	}

	reqBody := `{"pairing_code":"` + code.Code + `","device_id":"remote-device-id","device_name":"Remote","cert_fingerprint":"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"}`
	req := httptest.NewRequest("POST", "/pair", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handlePair(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandlePairInvalidJSON(t *testing.T) {
	store, cleanup := setupServerTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	peerMgr := NewPeerManager(store)
	server, _ := NewSyncServer(store, peerMgr)

	req := httptest.NewRequest("POST", "/pair", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.handlePair(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleGetOperationsWithSince(t *testing.T) {
	store, cleanup := setupServerTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	peerMgr := NewPeerManager(store)
	server, _ := NewSyncServer(store, peerMgr)

	todo := models.NewTodo("Test", "", 0)
	_ = store.SaveTodo(todo)

	req := httptest.NewRequest("GET", "/sync/operations?since=some-op-id", nil)
	req.Header.Set("X-Doit-Secret", server.secret)
	w := httptest.NewRecorder()

	server.handleOperations(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(time.Minute, 10)
	if rl == nil {
		t.Fatal("newRateLimiter returned nil")
	}

	ip := "192.168.1.1"

	for i := 0; i < 10; i++ {
		if !rl.allow(ip) {
			t.Errorf("request %d should be allowed", i)
		}
	}

	if rl.allow(ip) {
		t.Error("11th request should be rate limited")
	}
}

func TestGetClientIP(t *testing.T) {
	store, cleanup := setupServerTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	peerMgr := NewPeerManager(store)
	server, _ := NewSyncServer(store, peerMgr)

	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{
			name:       "standard remote addr with port",
			remoteAddr: "192.168.1.1:12345",
			want:       "192.168.1.1",
		},
		{
			name:       "another IP",
			remoteAddr: "10.0.0.1:8080",
			want:       "10.0.0.1",
		},
		{
			name:       "invalid format fallback",
			remoteAddr: "invalid",
			want:       "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr

			got := server.getClientIP(req)
			if got != tt.want {
				t.Errorf("getClientIP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandleOperationsUnsupportedMethod(t *testing.T) {
	store, cleanup := setupServerTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	peerMgr := NewPeerManager(store)
	server, _ := NewSyncServer(store, peerMgr)

	req := httptest.NewRequest("DELETE", "/sync/operations", nil)
	req.Header.Set("X-Doit-Secret", server.secret)
	w := httptest.NewRecorder()

	server.handleOperations(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}
