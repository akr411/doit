package sync

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/akr411/doit/internal/storage"
)

type SyncServer struct {
	server  *http.Server
	store   *storage.Storage
	peerMgr *PeerManager
	secret  string
	errCh   chan error
	port    int
}

type OperationsResponse struct {
	Operations []Operation `json:"operations"`
}

type OperationsRequest struct {
	Operations []Operation `json:"operations"`
}

type AppliedResponse struct {
	Applied int `json:"applied"`
}

type StateResponse struct {
	LastOperationID string `json:"last_operation_id"`
	DeviceID        string `json:"device_id"`
	DeviceName      string `json:"device_name"`
	SharedSecret    string `json:"shared_secret"`
}

func NewSyncServer(store *storage.Storage, peerMgr *PeerManager) (*SyncServer, error) {
	secret, err := getOrGenerateSecret(store.GetDB())
	if err != nil {
		return nil, fmt.Errorf("failed to get/generate secret: %w", err)
	}

	return &SyncServer{
		store:   store,
		peerMgr: peerMgr,
		secret:  secret,
	}, nil
}

func (ss *SyncServer) Start(port int) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/sync/operations", ss.authMiddleware(ss.handleOperations))
	mux.HandleFunc("/sync/state", ss.handleGetState)

	ports := []int{port, port + 1, port + 2}
	ss.errCh = make(chan error, 1)

	var lastErr error
	for _, p := range ports {
		srv := &http.Server{
			Addr:         fmt.Sprintf(":%d", p),
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		}

		go func(server *http.Server) {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				ss.errCh <- err
			}
		}(srv)

		select {
		case err := <-ss.errCh:
			lastErr = err
			srv.Close()
			continue
		case <-time.After(100 * time.Millisecond):
			ss.server = srv
			ss.port = p
			log.Printf("HTTP sync server started on port %d", p)
			return nil
		}
	}

	return fmt.Errorf("failed to start server on ports %d-%d: %w. Set custom port: doit config sync_port <port>",
		ports[0], ports[len(ports)-1], lastErr)
}

func (ss *SyncServer) GetPort() int {
	return ss.port
}

func (ss *SyncServer) Stop() error {
	if ss.server == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return ss.server.Shutdown(ctx)
}

func (ss *SyncServer) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("X-Doit-Secret")
		if provided != ss.secret {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (ss *SyncServer) handleOperations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ss.handleGetOperations(w, r)
	case http.MethodPost:
		ss.handlePostOperations(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (ss *SyncServer) handleGetOperations(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")

	ops, err := ss.store.GetOperations(since)
	if err != nil {
		log.Printf("Failed to get operations: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	operations := make([]Operation, 0, len(ops))
	for _, opData := range ops {
		op := Operation{
			ID:        opData["id"].(string),
			Type:      opData["type"].(string),
			TodoID:    opData["todo_id"].(string),
			Data:      []byte(opData["data"].(string)),
			Timestamp: opData["timestamp"].(int64),
			DeviceID:  opData["device_id"].(string),
		}
		operations = append(operations, op)
	}

	response := OperationsResponse{Operations: operations}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (ss *SyncServer) handlePostOperations(w http.ResponseWriter, r *http.Request) {
	var req OperationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	applied := 0
	for _, op := range req.Operations {
		if err := op.Apply(ss.store.GetDB()); err != nil {
			log.Printf("Failed to apply operation %s: %v", op.ID, err)
			continue
		}
		applied++
	}

	response := AppliedResponse{Applied: applied}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (ss *SyncServer) handleGetState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lastOpID, err := ss.store.GetLastOperationID()
	if err != nil {
		log.Printf("Failed to get last operation ID: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	deviceID, err := GetDeviceID(ss.store.GetDB())
	if err != nil {
		log.Printf("Failed to get device ID: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	deviceName := GetDeviceName()

	response := StateResponse{
		LastOperationID: lastOpID,
		DeviceID:        deviceID,
		DeviceName:      deviceName,
		SharedSecret:    ss.secret,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func getOrGenerateSecret(db *sql.DB) (string, error) {
	var secret string
	err := db.QueryRow("SELECT value FROM config WHERE key='shared_secret'").Scan(&secret)

	if err == sql.ErrNoRows {
		secretBytes := make([]byte, 32)
		if _, err := rand.Read(secretBytes); err != nil {
			return "", fmt.Errorf("failed to generate secret: %w", err)
		}
		secret = base64.StdEncoding.EncodeToString(secretBytes)

		_, err = db.Exec("INSERT INTO config (key, value) VALUES ('shared_secret', ?)", secret)
		if err != nil {
			return "", fmt.Errorf("failed to store secret: %w", err)
		}

		return secret, nil
	}

	if err != nil {
		return "", fmt.Errorf("failed to get secret: %w", err)
	}

	return secret, nil
}
