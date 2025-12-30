package sync

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/akr411/doit/internal/logging"
	"github.com/akr411/doit/internal/storage"
)

const (
	maxRequestBodySize       = 10 << 20 // 10MB
	maxOperationsPerPost     = 1000
	pairingRateLimitWindow   = time.Minute
	pairingRateLimitMax      = 5
	syncRateLimitWindow      = time.Minute
	syncRateLimitMax         = 100
	rateLimiterCleanupPeriod = 5 * time.Minute
)

type rateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	window   time.Duration
	maxReqs  int
	stopCh   chan struct{}
}

func newRateLimiter(window time.Duration, maxReqs int) *rateLimiter {
	rl := &rateLimiter{
		attempts: make(map[string][]time.Time),
		window:   window,
		maxReqs:  maxReqs,
		stopCh:   make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

func (rl *rateLimiter) stop() {
	close(rl.stopCh)
}

func (rl *rateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rateLimiterCleanupPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCh:
			return
		}
	}
}

func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	for ip, times := range rl.attempts {
		var recent []time.Time
		for _, t := range times {
			if t.After(cutoff) {
				recent = append(recent, t)
			}
		}
		if len(recent) == 0 {
			delete(rl.attempts, ip)
		} else {
			rl.attempts[ip] = recent
		}
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	var recent []time.Time
	for _, t := range rl.attempts[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= rl.maxReqs {
		rl.attempts[ip] = recent
		return false
	}

	rl.attempts[ip] = append(recent, now)
	return true
}

// SyncServer handles HTTPS requests for P2P synchronization.
// It serves the /sync/operations, /sync/state, and /sync/pair endpoints.
// All traffic is encrypted with TLS 1.3 and authenticated with mTLS.
type SyncServer struct {
	server            *http.Server
	store             *storage.Storage
	peerMgr           *PeerManager
	pairingMgr        *PairingManager
	secret            string
	deviceID          string
	deviceName        string
	errCh             chan error
	port              int
	pairingRateLimiter *rateLimiter
	syncRateLimiter    *rateLimiter
}

// OperationsResponse is the JSON response for GET /sync/operations.
type OperationsResponse struct {
	Operations []Operation `json:"operations"`
}

// OperationsRequest is the JSON request body for POST /sync/operations.
type OperationsRequest struct {
	Operations []Operation `json:"operations"`
}

// AppliedResponse confirms how many operations were successfully applied.
type AppliedResponse struct {
	Applied int `json:"applied"`
}

// StateResponse returns the sync state for GET /sync/state endpoint.
type StateResponse struct {
	LastOperationID string `json:"last_operation_id"`
	DeviceID        string `json:"device_id"`
	DeviceName      string `json:"device_name"`
}

// NewSyncServer creates an HTTPS sync server with TLS and pairing support.
// Initializes the shared secret, device ID, and pairing manager.
// Returns error if secret generation or device ID retrieval fails.
func NewSyncServer(store *storage.Storage, peerMgr *PeerManager) (*SyncServer, error) {
	secret, err := getOrGenerateSecret(store.GetDB())
	if err != nil {
		return nil, fmt.Errorf("failed to get/generate secret: %w", err)
	}

	deviceID, err := GetDeviceID(store.GetDB())
	if err != nil {
		return nil, fmt.Errorf("failed to get device ID: %w", err)
	}

	deviceName := GetDeviceName()
	pairingMgr := NewPairingManager(store.GetDB())

	return &SyncServer{
		store:              store,
		peerMgr:            peerMgr,
		pairingMgr:         pairingMgr,
		secret:             secret,
		deviceID:           deviceID,
		deviceName:         deviceName,
		pairingRateLimiter: newRateLimiter(pairingRateLimitWindow, pairingRateLimitMax),
		syncRateLimiter:    newRateLimiter(syncRateLimitWindow, syncRateLimitMax),
	}, nil
}

// Start launches the HTTPS sync server with TLS 1.3 and mTLS.
// Generates TLS certificate if needed, tries binding to port, port+1, port+2.
// Registers handlers for /sync/operations, /sync/state, and /sync/pair endpoints.
// Returns error if TLS setup fails or all ports are in use.
func (ss *SyncServer) Start(port int) error {
	certMgr := NewCertificateManager(ss.store.GetDB())
	regenerated, err := certMgr.RegenerateCertIfExpired(ss.deviceID)
	if err != nil {
		return fmt.Errorf("failed to check/generate TLS cert: %w", err)
	}
	if regenerated {
		logging.Info(" TLS certificate regenerated")
	}

	tlsConfig, err := certMgr.GetTLSConfig(true)
	if err != nil {
		return fmt.Errorf("failed to get TLS config: %w", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/sync/operations", ss.authMiddleware(ss.handleOperations))
	mux.HandleFunc("/sync/state", ss.authMiddleware(ss.handleGetState))
	mux.HandleFunc("/sync/pair", ss.handlePair)

	ports := []int{port, port + 1, port + 2}
	ss.errCh = make(chan error, 1)

	var lastErr error
	for _, p := range ports {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", p))
		if err != nil {
			lastErr = err
			continue
		}

		srv := &http.Server{
			Handler:      mux,
			TLSConfig:    tlsConfig,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		}

		go func(server *http.Server, listener net.Listener) {
			if err := server.ServeTLS(listener, "", ""); err != nil && err != http.ErrServerClosed {
				ss.errCh <- err
			}
		}(srv, ln)

		ss.server = srv
		ss.port = p
		logging.Info("HTTPS sync server started on port %d", p)
		return nil
	}

	return fmt.Errorf("failed to start server on ports %d-%d: %w. Set custom port: doit config sync_port <port>",
		ports[0], ports[len(ports)-1], lastErr)
}

// GetPort returns the port the server is listening on.
// Returns 0 if server is not started.
func (ss *SyncServer) GetPort() int {
	return ss.port
}

// Stop gracefully shuts down the HTTPS server.
// Returns error if shutdown fails. Safe to call multiple times.
func (ss *SyncServer) Stop() error {
	if ss.pairingRateLimiter != nil {
		ss.pairingRateLimiter.stop()
	}
	if ss.syncRateLimiter != nil {
		ss.syncRateLimiter.stop()
	}

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
	clientIP := ss.getClientIP(r)
	if !ss.syncRateLimiter.allow(clientIP) {
		logging.Warn(" Rate limit exceeded for sync from %s", clientIP)
		http.Error(w, "Too many sync requests, try again later", http.StatusTooManyRequests)
		return
	}

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
		logging.Warn("Failed to get operations: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	operations := OperationsFromData(ops)

	response := OperationsResponse{Operations: operations}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logging.Warn("Failed to encode operations response: %v", err)
	}
}

func (ss *SyncServer) handlePostOperations(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var req OperationsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON or request too large", http.StatusBadRequest)
		return
	}

	if len(req.Operations) > maxOperationsPerPost {
		http.Error(w, "Too many operations", http.StatusBadRequest)
		return
	}

	applied := 0
	for _, op := range req.Operations {
		if err := op.ApplyWithValidation(ss.store.GetDB()); err != nil {
			logging.Warn("Failed to apply operation %s: %v", op.ID, err)
			continue
		}
		applied++
	}

	response := AppliedResponse{Applied: applied}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logging.Warn("Failed to encode response: %v", err)
	}
}

func (ss *SyncServer) handleGetState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lastOpID, err := ss.store.GetLastOperationID()
	if err != nil {
		logging.Warn("Failed to get last operation ID: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	deviceID, err := GetDeviceID(ss.store.GetDB())
	if err != nil {
		logging.Warn("Failed to get device ID: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	deviceName := GetDeviceName()

	response := StateResponse{
		LastOperationID: lastOpID,
		DeviceID:        deviceID,
		DeviceName:      deviceName,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logging.Warn("Failed to encode state response: %v", err)
	}
}

func (ss *SyncServer) getClientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func (ss *SyncServer) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := ss.getClientIP(r)
	if !ss.pairingRateLimiter.allow(clientIP) {
		logging.Warn(" Rate limit exceeded for pairing from %s", clientIP)
		http.Error(w, "Too many pairing attempts, try again later", http.StatusTooManyRequests)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)

	var req struct {
		PairingCode     string `json:"pairing_code"`
		DeviceID        string `json:"device_id"`
		DeviceName      string `json:"device_name"`
		CertFingerprint string `json:"cert_fingerprint"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	valid, err := ss.pairingMgr.ValidateAndConsumeCode(req.PairingCode)
	if err != nil || !valid {
		logging.Warn(" Invalid pairing code from %s (%s): %v", req.DeviceName, clientIP, err)
		http.Error(w, "Invalid or expired pairing code", http.StatusUnauthorized)
		return
	}

	if req.CertFingerprint != "" {
		certMgr := NewCertificateManager(ss.store.GetDB())
		if err := certMgr.SavePeerCertificate(req.DeviceID, req.CertFingerprint); err != nil {
			logging.Warn("Failed to save peer certificate: %v", err)
		}
	}

	certMgr := NewCertificateManager(ss.store.GetDB())
	ourFingerprint, err := certMgr.GetFingerprint()
	if err != nil {
		logging.Error("Failed to get fingerprint during pairing: %v", err)
		http.Error(w, "Server error: TLS not configured", http.StatusInternalServerError)
		return
	}

	response := struct {
		SharedSecret    string `json:"shared_secret"`
		DeviceID        string `json:"device_id"`
		DeviceName      string `json:"device_name"`
		CertFingerprint string `json:"cert_fingerprint"`
	}{
		SharedSecret:    ss.secret,
		DeviceID:        ss.deviceID,
		DeviceName:      ss.deviceName,
		CertFingerprint: ourFingerprint,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logging.Warn("Failed to encode pair response: %v", err)
		return
	}

	logging.Info(" Paired with device: %s (%s)", req.DeviceName, req.DeviceID)
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
