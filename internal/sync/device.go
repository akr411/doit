package sync

import (
	"database/sql"
	"fmt"
	"os"
	"runtime"

	"github.com/google/uuid"
)

// GetDeviceID retrieves or generates the unique device identifier.
// On first call, generates a new UUID and stores it in config.
// Returns the device ID string or error if database access fails.
func GetDeviceID(db *sql.DB) (string, error) {
	var deviceID string
	err := db.QueryRow("SELECT value FROM config WHERE key = 'device_id'").Scan(&deviceID)

	if err == sql.ErrNoRows {
		deviceID = uuid.New().String()
		_, err = db.Exec(`
			INSERT INTO config (key, value) VALUES ('device_id', ?)
		`, deviceID)
		if err != nil {
			return "", fmt.Errorf("failed to save device_id: %w", err)
		}
		return deviceID, nil
	}

	if err != nil {
		return "", fmt.Errorf("failed to get device_id: %w", err)
	}

	return deviceID, nil
}

// GetDeviceName returns a human-readable device name combining hostname and OS.
// Format: "hostname-os" (e.g., "macbook-darwin", "desktop-linux").
// Returns "unknown-os" if hostname cannot be determined.
func GetDeviceName() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s-%s", hostname, runtime.GOOS)
}

// IsSyncEnabled checks if P2P synchronization is enabled in config.
// Returns false if config key doesn't exist or value is not "true".
func IsSyncEnabled(db *sql.DB) bool {
	var value string
	err := db.QueryRow("SELECT value FROM config WHERE key = 'sync_enabled'").Scan(&value)
	if err != nil {
		return false
	}
	return value == "true"
}

// SetSyncEnabled updates the sync enabled status in config.
// Stores "true" or "false" string in the database.
func SetSyncEnabled(db *sql.DB, enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}

	_, err := db.Exec(`
		INSERT INTO config (key, value) VALUES ('sync_enabled', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, value)

	return err
}

// GetSyncPort retrieves the configured sync server port.
// Returns 49152 as default if not configured or on error.
func GetSyncPort(db *sql.DB) int {
	var value string
	err := db.QueryRow("SELECT value FROM config WHERE key = 'sync_port'").Scan(&value)
	if err != nil {
		return 49152
	}

	var port int
	if _, err := fmt.Sscanf(value, "%d", &port); err != nil {
		return 49152
	}

	return port
}

// SetSyncPort updates the sync server port in config.
// The port is stored as a string representation of the integer.
func SetSyncPort(db *sql.DB, port int) error {
	_, err := db.Exec(`
		INSERT INTO config (key, value) VALUES ('sync_port', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, fmt.Sprintf("%d", port))

	return err
}
