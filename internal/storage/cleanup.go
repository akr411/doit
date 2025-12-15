package storage

import (
	"database/sql"
	"fmt"
	"time"
)

type CleanupStats struct {
	TombstonesDeleted int
	OperationsDeleted int
	BytesFreed        int64
}

func (s *Storage) CleanupSyncData(aggressive bool) (*CleanupStats, error) {
	var syncEnabled string
	err := s.db.QueryRow("SELECT value FROM config WHERE key='sync_enabled'").Scan(&syncEnabled)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to check sync status: %w", err)
	}

	if syncEnabled != "true" {
		return &CleanupStats{}, nil
	}

	cleanupEnabled, _ := s.GetConfig("auto_cleanup_enabled")
	if cleanupEnabled == "false" && !aggressive {
		return &CleanupStats{}, nil
	}

	stats := &CleanupStats{}

	tombstoneCount, err := s.cleanupTombstones(aggressive)
	if err != nil {
		return stats, fmt.Errorf("failed to cleanup tombstones: %w", err)
	}
	stats.TombstonesDeleted = tombstoneCount

	opCount, err := s.cleanupOperations(aggressive)
	if err != nil {
		return stats, fmt.Errorf("failed to cleanup operations: %w", err)
	}
	stats.OperationsDeleted = opCount

	_, err = s.db.Exec(`
		INSERT INTO config (key, value) VALUES ('last_cleanup_time', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, time.Now().Unix())

	return stats, err
}

func (s *Storage) cleanupTombstones(aggressive bool) (int, error) {
	retentionDays := s.getTombstoneRetentionDays()
	completedLimit := s.getCompletedLimit()

	minRetentionNano := int64(retentionDays * 24 * 60 * 60) * 1e9
	if aggressive {
		minRetentionNano = 0
	}

	cutoffTime := time.Now().UnixNano() - minRetentionNano

	result, err := s.db.Exec(`
		WITH keep_ids AS (
			SELECT id FROM todos
			WHERE deleted = 1 OR completed = 1
			ORDER BY updated_at DESC
			LIMIT ?
		)
		DELETE FROM todos
		WHERE deleted = 1
		AND updated_at < ?
		AND id NOT IN (SELECT id FROM keep_ids)
	`, completedLimit, cutoffTime)

	if err != nil {
		return 0, fmt.Errorf("failed to delete tombstones: %w", err)
	}

	rows, _ := result.RowsAffected()
	return int(rows), nil
}

func (s *Storage) cleanupOperations(aggressive bool) (int, error) {
	retentionDays := s.getOperationRetentionDays()
	maxOpsPerTodo := s.getMaxOperationsPerTodo()

	minRetentionNano := time.Now().UnixNano() - int64(retentionDays*24*60*60)*1e9
	if aggressive {
		minRetentionNano = time.Now().UnixNano()
	}

	result, err := s.db.Exec(`
		DELETE FROM operations
		WHERE synced = 1
		AND timestamp < ?
		AND id NOT IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY todo_id ORDER BY timestamp DESC) as rn
				FROM operations
			)
			WHERE rn <= ?
		)
	`, minRetentionNano, maxOpsPerTodo)

	if err != nil {
		return 0, fmt.Errorf("failed to delete operations: %w", err)
	}

	rows, _ := result.RowsAffected()
	return int(rows), nil
}

func (s *Storage) ShouldRunCleanup() bool {
	var syncEnabled string
	err := s.db.QueryRow("SELECT value FROM config WHERE key='sync_enabled'").Scan(&syncEnabled)
	if err != nil || syncEnabled != "true" {
		return false
	}

	cleanupEnabled, _ := s.GetConfig("auto_cleanup_enabled")
	if cleanupEnabled == "false" {
		return false
	}

	var lastCleanupStr string
	err = s.db.QueryRow("SELECT value FROM config WHERE key='last_cleanup_time'").Scan(&lastCleanupStr)
	if err == sql.ErrNoRows {
		return true
	}

	var lastCleanup int64
	fmt.Sscanf(lastCleanupStr, "%d", &lastCleanup)

	intervalHours := s.getCleanupIntervalHours()
	nextCleanup := lastCleanup + int64(intervalHours*3600)

	return time.Now().Unix() >= nextCleanup
}

func (s *Storage) GetCleanupStats() (SyncStats, error) {
	var stats SyncStats

	s.db.QueryRow("SELECT COUNT(*) FROM operations").Scan(&stats.TotalOperations)
	s.db.QueryRow("SELECT COUNT(*) FROM operations WHERE synced=1").Scan(&stats.SyncedOperations)
	s.db.QueryRow("SELECT COUNT(*) FROM operations WHERE synced=0").Scan(&stats.UnsyncedOperations)
	s.db.QueryRow("SELECT COUNT(*) FROM todos WHERE deleted=1").Scan(&stats.Tombstones)

	retentionDays := s.getOperationRetentionDays()
	cutoffNano := time.Now().UnixNano() - int64(retentionDays*24*60*60)*1e9

	tombRetention := s.getTombstoneRetentionDays()
	tombCutoff := time.Now().UnixNano() - int64(tombRetention*24*60*60)*1e9
	completedLimit := s.getCompletedLimit()
	s.db.QueryRow(`
		WITH keep_ids AS (
			SELECT id FROM todos
			WHERE deleted = 1 OR completed = 1
			ORDER BY updated_at DESC
			LIMIT ?
		)
		SELECT COUNT(*) FROM todos
		WHERE deleted=1
		AND updated_at < ?
		AND id NOT IN (SELECT id FROM keep_ids)
	`, completedLimit, tombCutoff).Scan(&stats.CleanableTombstones)

	s.db.QueryRow(`
		SELECT COUNT(*) FROM operations
		WHERE synced=1
		AND timestamp < ?
		AND id NOT IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY todo_id ORDER BY timestamp DESC) as rn
				FROM operations
			)
			WHERE rn <= ?
		)
	`, cutoffNano, s.getMaxOperationsPerTodo()).Scan(&stats.CleanableOperations)

	var pageCount, pageSize int64
	s.db.QueryRow("PRAGMA page_count").Scan(&pageCount)
	s.db.QueryRow("PRAGMA page_size").Scan(&pageSize)
	stats.DBSizeKB = (pageCount * pageSize) / 1024

	var lastCleanupStr string
	err := s.db.QueryRow("SELECT value FROM config WHERE key='last_cleanup_time'").Scan(&lastCleanupStr)
	if err == nil {
		fmt.Sscanf(lastCleanupStr, "%d", &stats.LastCleanupTime)
		stats.HoursSinceCleanup = int((time.Now().Unix() - stats.LastCleanupTime) / 3600)
	} else {
		stats.LastCleanupTime = 0
		stats.HoursSinceCleanup = -1
	}

	return stats, nil
}

func (s *Storage) getTombstoneRetentionDays() int {
	var days string
	err := s.db.QueryRow("SELECT value FROM config WHERE key='tombstone_retention_days'").Scan(&days)
	if err != nil {
		return 30
	}

	var d int
	fmt.Sscanf(days, "%d", &d)
	if d <= 0 {
		return 30
	}
	return d
}

func (s *Storage) getOperationRetentionDays() int {
	var days string
	err := s.db.QueryRow("SELECT value FROM config WHERE key='operation_retention_days'").Scan(&days)
	if err != nil {
		return 30
	}

	var d int
	fmt.Sscanf(days, "%d", &d)
	if d <= 0 {
		return 30
	}
	return d
}

func (s *Storage) getMaxOperationsPerTodo() int {
	var max string
	err := s.db.QueryRow("SELECT value FROM config WHERE key='max_operations_per_todo'").Scan(&max)
	if err != nil {
		return 10
	}

	var m int
	fmt.Sscanf(max, "%d", &m)
	if m <= 0 {
		return 10
	}
	return m
}

func (s *Storage) getCompletedLimit() int {
	limitStr, err := s.GetConfig("completed_limit")
	if err != nil {
		return 50
	}

	var limit int
	fmt.Sscanf(limitStr, "%d", &limit)
	if limit <= 0 {
		return 50
	}
	return limit
}

func (s *Storage) getCleanupIntervalHours() int {
	var hours string
	err := s.db.QueryRow("SELECT value FROM config WHERE key='cleanup_interval_hours'").Scan(&hours)
	if err != nil {
		return 24
	}

	var h int
	fmt.Sscanf(hours, "%d", &h)
	if h <= 0 {
		return 24
	}
	return h
}

func (s *Storage) SetAutoCleanupEnabled(enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}

	return s.SetConfig("auto_cleanup_enabled", value)
}
