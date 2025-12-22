package sync

import (
	"path/filepath"
	"testing"

	"github.com/akr411/doit/internal/storage"
	_ "modernc.org/sqlite"
)

func setupEngineTestDB(t *testing.T) (*storage.Storage, func()) {
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

func TestNewSyncEngineNoTLSCert(t *testing.T) {
	store, cleanup := setupEngineTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")

	_, err := NewSyncEngine(store)
	if err == nil {
		t.Error("expected error when no TLS cert exists")
	}
}

func TestNewSyncEngineWithTLSCert(t *testing.T) {
	store, cleanup := setupEngineTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	deviceID, _ := GetDeviceID(store.GetDB())
	certMgr := NewCertificateManager(store.GetDB())
	_ = certMgr.GenerateSelfSignedCert(deviceID)

	engine, err := NewSyncEngine(store)
	if err != nil {
		t.Fatalf("NewSyncEngine failed: %v", err)
	}

	if engine == nil {
		t.Fatal("engine should not be nil")
	}

	if engine.store != store {
		t.Error("store not set correctly")
	}

	if engine.peerMgr == nil {
		t.Error("peerMgr should be initialized")
	}

	if engine.discovery == nil {
		t.Error("discovery should be initialized")
	}

	if engine.server == nil {
		t.Error("server should be initialized")
	}

	if engine.client == nil {
		t.Error("client should be initialized")
	}
}

func TestSyncEngineIsRunning(t *testing.T) {
	store, cleanup := setupEngineTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	deviceID, _ := GetDeviceID(store.GetDB())
	certMgr := NewCertificateManager(store.GetDB())
	_ = certMgr.GenerateSelfSignedCert(deviceID)

	engine, err := NewSyncEngine(store)
	if err != nil {
		t.Fatalf("NewSyncEngine failed: %v", err)
	}

	if engine.IsRunning() {
		t.Error("engine should not be running initially")
	}
}

func TestSyncEngineStopWhenNotRunning(t *testing.T) {
	store, cleanup := setupEngineTestDB(t)
	defer cleanup()

	_ = store.SetConfig("sync_enabled", "true")
	deviceID, _ := GetDeviceID(store.GetDB())
	certMgr := NewCertificateManager(store.GetDB())
	_ = certMgr.GenerateSelfSignedCert(deviceID)

	engine, err := NewSyncEngine(store)
	if err != nil {
		t.Fatalf("NewSyncEngine failed: %v", err)
	}

	err = engine.Stop()
	if err != nil {
		t.Errorf("Stop when not running should not error: %v", err)
	}
}

func TestMaxConcurrentSyncsConstant(t *testing.T) {
	if maxConcurrentSyncs != 5 {
		t.Errorf("maxConcurrentSyncs = %d, want 5", maxConcurrentSyncs)
	}
}
