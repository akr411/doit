package sync

import (
	"database/sql"
	"fmt"
	"os"
	"runtime"

	"github.com/google/uuid"
)

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

func GetDeviceName() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s-%s", hostname, runtime.GOOS)
}

func IsSyncEnabled(db *sql.DB) bool {
	var value string
	err := db.QueryRow("SELECT value FROM config WHERE key = 'sync_enabled'").Scan(&value)
	if err != nil {
		return false
	}
	return value == "true"
}

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

func GetSyncPort(db *sql.DB) int {
	var value string
	err := db.QueryRow("SELECT value FROM config WHERE key = 'sync_port'").Scan(&value)
	if err != nil {
		return 8888
	}

	var port int
	if _, err := fmt.Sscanf(value, "%d", &port); err != nil {
		return 8888
	}

	return port
}

func SetSyncPort(db *sql.DB, port int) error {
	_, err := db.Exec(`
		INSERT INTO config (key, value) VALUES ('sync_port', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, fmt.Sprintf("%d", port))

	return err
}
