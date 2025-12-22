package sync

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func setupDeviceTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	return db
}

func TestGetDeviceID(t *testing.T) {
	db := setupDeviceTestDB(t)
	defer func() { _ = db.Close() }()

	deviceID, err := GetDeviceID(db)
	if err != nil {
		t.Fatalf("GetDeviceID failed: %v", err)
	}

	if deviceID == "" {
		t.Error("device ID should not be empty")
	}

	deviceID2, err := GetDeviceID(db)
	if err != nil {
		t.Fatalf("GetDeviceID (second call) failed: %v", err)
	}

	if deviceID != deviceID2 {
		t.Errorf("device ID should be consistent: got %s and %s", deviceID, deviceID2)
	}

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM config WHERE key='device_id'").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 device_id entry, got %d", count)
	}
}

func TestGetDeviceName(t *testing.T) {
	name := GetDeviceName()

	if name == "" {
		t.Error("device name should not be empty")
	}

	hostname, _ := os.Hostname()
	if len(name) < len(hostname) {
		t.Errorf("device name should include hostname, got: %s", name)
	}
}

func TestIsSyncEnabled(t *testing.T) {
	db := setupDeviceTestDB(t)
	defer func() { _ = db.Close() }()

	if IsSyncEnabled(db) {
		t.Error("sync should be disabled by default")
	}

	_, _ = db.Exec("INSERT INTO config (key, value) VALUES ('sync_enabled', 'true')")

	if !IsSyncEnabled(db) {
		t.Error("sync should be enabled after setting config")
	}

	_, _ = db.Exec("UPDATE config SET value='false' WHERE key='sync_enabled'")

	if IsSyncEnabled(db) {
		t.Error("sync should be disabled after setting config to false")
	}
}

func TestSetSyncEnabled(t *testing.T) {
	db := setupDeviceTestDB(t)
	defer func() { _ = db.Close() }()

	err := SetSyncEnabled(db, true)
	if err != nil {
		t.Fatalf("SetSyncEnabled(true) failed: %v", err)
	}

	var value string
	_ = db.QueryRow("SELECT value FROM config WHERE key='sync_enabled'").Scan(&value)
	if value != "true" {
		t.Errorf("expected 'true', got '%s'", value)
	}

	err = SetSyncEnabled(db, false)
	if err != nil {
		t.Fatalf("SetSyncEnabled(false) failed: %v", err)
	}

	_ = db.QueryRow("SELECT value FROM config WHERE key='sync_enabled'").Scan(&value)
	if value != "false" {
		t.Errorf("expected 'false', got '%s'", value)
	}
}

func TestGetSyncPort(t *testing.T) {
	db := setupDeviceTestDB(t)
	defer func() { _ = db.Close() }()

	port := GetSyncPort(db)
	if port != 49152 {
		t.Errorf("expected default port 49152, got %d", port)
	}

	_, _ = db.Exec("INSERT INTO config (key, value) VALUES ('sync_port', '9999')")

	port = GetSyncPort(db)
	if port != 9999 {
		t.Errorf("expected custom port 9999, got %d", port)
	}

	_, _ = db.Exec("UPDATE config SET value='invalid' WHERE key='sync_port'")

	port = GetSyncPort(db)
	if port != 49152 {
		t.Errorf("expected default port 49152 for invalid value, got %d", port)
	}
}

func TestSetSyncPort(t *testing.T) {
	db := setupDeviceTestDB(t)
	defer func() { _ = db.Close() }()

	err := SetSyncPort(db, 8080)
	if err != nil {
		t.Fatalf("SetSyncPort failed: %v", err)
	}

	var value string
	_ = db.QueryRow("SELECT value FROM config WHERE key='sync_port'").Scan(&value)
	if value != "8080" {
		t.Errorf("expected '8080', got '%s'", value)
	}

	port := GetSyncPort(db)
	if port != 8080 {
		t.Errorf("expected port 8080, got %d", port)
	}
}

func TestSetSyncPortEdgeCases(t *testing.T) {
	db := setupDeviceTestDB(t)
	defer func() { _ = db.Close() }()

	if err := SetSyncPort(db, 1); err != nil {
		t.Errorf("SetSyncPort(1) should succeed: %v", err)
	}

	if err := SetSyncPort(db, 65535); err != nil {
		t.Errorf("SetSyncPort(65535) should succeed: %v", err)
	}

	port := GetSyncPort(db)
	if port != 65535 {
		t.Errorf("expected port 65535, got %d", port)
	}
}

func TestGetDeviceIDConsistency(t *testing.T) {
	db := setupDeviceTestDB(t)
	defer func() { _ = db.Close() }()

	ids := make([]string, 10)
	for i := 0; i < 10; i++ {
		id, err := GetDeviceID(db)
		if err != nil {
			t.Fatalf("GetDeviceID call %d failed: %v", i, err)
		}
		ids[i] = id
	}

	for i := 1; i < 10; i++ {
		if ids[i] != ids[0] {
			t.Errorf("device ID inconsistent: %s != %s", ids[i], ids[0])
		}
	}
}
