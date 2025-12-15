package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/akr411/doit/internal/models"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type Storage struct {
	db *sql.DB
}

type Peer struct {
	ID        string
	Name      string
	Address   string
	LastSeen  int64
	Status    string
	CreatedAt int64
}

type SyncState struct {
	PeerID             string
	LastOperationID    string
	LastSyncTime       int64
	OperationsSent     int64
	OperationsReceived int64
}

var getDBPath = func() (string, error) {
	var dataDir string
	if os.Getenv("XDG_DATA_HOME") != "" {
		dataDir = os.Getenv("XDG_DATA_HOME")
	} else if home, err := os.UserHomeDir(); err == nil {
		dataDir = filepath.Join(home, ".local", "share")
	} else {
		return "", fmt.Errorf("failed to determine home directory: %w", err)
	}
	return filepath.Join(dataDir, "doit", "doit.db"), nil
}

func New() (*Storage, error) {
	dbPath, err := getDBPath()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := enableWAL(db); err != nil {
		db.Close()
		return nil, err
	}

	s := &Storage{db: db}
	if err := s.createTables(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func enableWAL(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return fmt.Errorf("failed to enable WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return fmt.Errorf("failed to set synchronous mode: %w", err)
	}
	return nil
}

func (s *Storage) createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS todos (
		id TEXT PRIMARY KEY,
		task TEXT NOT NULL,
		note TEXT,
		deadline INTEGER,
		completed INTEGER DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		deleted INTEGER DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_todos_completed ON todos(completed);
	CREATE INDEX IF NOT EXISTS idx_todos_deadline ON todos(deadline);
	CREATE INDEX IF NOT EXISTS idx_todos_deleted ON todos(deleted);
	CREATE INDEX IF NOT EXISTS idx_todos_cleanup ON todos(deleted, updated_at);
	CREATE INDEX IF NOT EXISTS idx_todos_completed_cleanup ON todos(completed, updated_at);

	CREATE TABLE IF NOT EXISTS streaks (
		id INTEGER PRIMARY KEY CHECK(id=1),
		current_streak INTEGER DEFAULT 0,
		max_streak INTEGER DEFAULT 0,
		total_completed INTEGER DEFAULT 0,
		last_completed_at INTEGER
	);

	CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS operations (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		todo_id TEXT NOT NULL,
		data TEXT NOT NULL,
		timestamp INTEGER NOT NULL,
		device_id TEXT NOT NULL,
		synced INTEGER DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_ops_timestamp ON operations(timestamp);
	CREATE INDEX IF NOT EXISTS idx_ops_synced ON operations(synced);
	CREATE INDEX IF NOT EXISTS idx_ops_device ON operations(device_id);
	CREATE INDEX IF NOT EXISTS idx_ops_todo ON operations(todo_id);

	CREATE TABLE IF NOT EXISTS peers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		address TEXT NOT NULL,
		last_seen INTEGER NOT NULL,
		status TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_peers_status ON peers(status);
	CREATE INDEX IF NOT EXISTS idx_peers_last_seen ON peers(last_seen);

	CREATE TABLE IF NOT EXISTS sync_state (
		peer_id TEXT PRIMARY KEY,
		last_operation_id TEXT,
		last_sync_time INTEGER,
		operations_sent INTEGER DEFAULT 0,
		operations_received INTEGER DEFAULT 0,
		FOREIGN KEY (peer_id) REFERENCES peers(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS peer_secrets (
		peer_id TEXT PRIMARY KEY,
		secret TEXT NOT NULL,
		FOREIGN KEY (peer_id) REFERENCES peers(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS pairing_codes (
		code TEXT PRIMARY KEY,
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL,
		used INTEGER DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_pairing_expires ON pairing_codes(expires_at);

	CREATE TABLE IF NOT EXISTS peer_certificates (
		device_id TEXT PRIMARY KEY,
		fingerprint TEXT NOT NULL,
		FOREIGN KEY (device_id) REFERENCES peers(id) ON DELETE CASCADE
	);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	if err := s.runMigrations(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	_, err := s.db.Exec("INSERT OR IGNORE INTO streaks (id) VALUES (1)")
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT OR IGNORE INTO config (key, value) VALUES ('auto_cleanup_enabled', 'true')
	`)
	return err
}

func (s *Storage) runMigrations() error {
	var hasDeletedColumn bool
	err := s.db.QueryRow(`
		SELECT COUNT(*) > 0
		FROM pragma_table_info('todos')
		WHERE name='deleted'
	`).Scan(&hasDeletedColumn)

	if err != nil {
		return fmt.Errorf("failed to check for deleted column: %w", err)
	}

	if !hasDeletedColumn {
		_, err = s.db.Exec("ALTER TABLE todos ADD COLUMN deleted INTEGER DEFAULT 0")
		if err != nil {
			return fmt.Errorf("failed to add deleted column: %w", err)
		}

		_, err = s.db.Exec("CREATE INDEX IF NOT EXISTS idx_todos_deleted ON todos(deleted)")
		if err != nil {
			return fmt.Errorf("failed to create deleted index: %w", err)
		}
	}

	return nil
}

func (s *Storage) SaveTodo(todo *models.Todo) error {
	if todo.ID == "" {
		todo.ID = uuid.New().String()
	}

	if err := todo.Validate(); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var syncEnabled string
	err = tx.QueryRow("SELECT value FROM config WHERE key = 'sync_enabled'").Scan(&syncEnabled)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to check sync_enabled: %w", err)
	}

	timestamp := time.Now().UnixNano()
	todo.UpdatedAt = timestamp

	if syncEnabled == "true" {
		var deviceID string
		err = tx.QueryRow("SELECT value FROM config WHERE key = 'device_id'").Scan(&deviceID)
		if err == sql.ErrNoRows {
			deviceID = uuid.New().String()
			_, err = tx.Exec("INSERT INTO config (key, value) VALUES ('device_id', ?)", deviceID)
			if err != nil {
				return fmt.Errorf("failed to create device_id: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("failed to get device_id: %w", err)
		}

		data, err := json.Marshal(todo)
		if err != nil {
			return fmt.Errorf("failed to marshal todo: %w", err)
		}

		opID := uuid.New().String()

		_, err = tx.Exec(`
			INSERT INTO operations (id, type, todo_id, data, timestamp, device_id, synced)
			VALUES (?, ?, ?, ?, ?, ?, 0)
		`, opID, "CREATE", todo.ID, string(data), timestamp, deviceID)
		if err != nil {
			return fmt.Errorf("failed to save operation: %w", err)
		}
	}

	completed := 0
	if todo.Completed {
		completed = 1
	}

	_, err = tx.Exec(`
		INSERT INTO todos (id, task, note, deadline, completed, created_at, updated_at, deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0)
	`, todo.ID, todo.Task, todo.Note, todo.Deadline, completed, todo.CreatedAt, timestamp)

	if err != nil {
		return fmt.Errorf("failed to save todo: %w", err)
	}

	return tx.Commit()
}

func (s *Storage) GetTodo(id string) (*models.Todo, error) {
	var todo models.Todo
	var completed int

	err := s.db.QueryRow(`
		SELECT id, task, note, COALESCE(deadline, 0), completed, created_at, updated_at
		FROM todos WHERE id = ? AND deleted = 0
	`, id).Scan(&todo.ID, &todo.Task, &todo.Note, &todo.Deadline, &completed, &todo.CreatedAt, &todo.UpdatedAt)

	if err != nil {
		return nil, err
	}

	todo.Completed = completed == 1
	return &todo, nil
}

func (s *Storage) GetAllTodos() ([]*models.Todo, error) {
	rows, err := s.db.Query(`
		SELECT id, task, note, COALESCE(deadline, 0), completed, created_at, updated_at
		FROM todos
		WHERE deleted = 0
		ORDER BY completed ASC,
		         CASE WHEN COALESCE(deadline, 0) > 0 THEN deadline ELSE 9999999999 END ASC,
		         created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var todos []*models.Todo
	for rows.Next() {
		var todo models.Todo
		var completed int

		if err := rows.Scan(&todo.ID, &todo.Task, &todo.Note, &todo.Deadline, &completed, &todo.CreatedAt, &todo.UpdatedAt); err != nil {
			return nil, err
		}

		todo.Completed = completed == 1
		todos = append(todos, &todo)
	}

	return todos, rows.Err()
}

func (s *Storage) UpdateTodo(todo *models.Todo) error {
	if err := todo.Validate(); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	timestamp := time.Now().UnixNano()
	todo.UpdatedAt = timestamp

	var syncEnabled string
	err = tx.QueryRow("SELECT value FROM config WHERE key = 'sync_enabled'").Scan(&syncEnabled)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to check sync_enabled: %w", err)
	}

	if syncEnabled == "true" {
		var deviceID string
		err = tx.QueryRow("SELECT value FROM config WHERE key = 'device_id'").Scan(&deviceID)
		if err != nil {
			return fmt.Errorf("failed to get device_id: %w", err)
		}

		data, err := json.Marshal(todo)
		if err != nil {
			return fmt.Errorf("failed to marshal todo: %w", err)
		}

		opID := uuid.New().String()

		_, err = tx.Exec(`
			INSERT INTO operations (id, type, todo_id, data, timestamp, device_id, synced)
			VALUES (?, ?, ?, ?, ?, ?, 0)
		`, opID, "UPDATE", todo.ID, string(data), timestamp, deviceID)
		if err != nil {
			return fmt.Errorf("failed to save operation: %w", err)
		}
	}

	completed := 0
	if todo.Completed {
		completed = 1
	}

	_, err = tx.Exec(`
		UPDATE todos
		SET task = ?, note = ?, deadline = ?, completed = ?, updated_at = ?
		WHERE id = ?
	`, todo.Task, todo.Note, todo.Deadline, completed, timestamp, todo.ID)

	if err != nil {
		return fmt.Errorf("failed to update todo: %w", err)
	}

	return tx.Commit()
}

func (s *Storage) CompleteTodo(id string, completed bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	var todo models.Todo
	var completedInt int
	err = tx.QueryRow(`
		SELECT id, task, note, COALESCE(deadline, 0), completed, created_at, updated_at
		FROM todos WHERE id = ? AND deleted = 0
	`, id).Scan(&todo.ID, &todo.Task, &todo.Note, &todo.Deadline, &completedInt, &todo.CreatedAt, &todo.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to get todo: %w", err)
	}

	todo.Completed = completed
	timestamp := time.Now().UnixNano()
	todo.UpdatedAt = timestamp

	var syncEnabled string
	err = tx.QueryRow("SELECT value FROM config WHERE key = 'sync_enabled'").Scan(&syncEnabled)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to check sync_enabled: %w", err)
	}

	if syncEnabled == "true" {
		var deviceID string
		err = tx.QueryRow("SELECT value FROM config WHERE key = 'device_id'").Scan(&deviceID)
		if err != nil {
			return fmt.Errorf("failed to get device_id: %w", err)
		}

		data, err := json.Marshal(todo)
		if err != nil {
			return fmt.Errorf("failed to marshal todo: %w", err)
		}

		opID := uuid.New().String()

		_, err = tx.Exec(`
			INSERT INTO operations (id, type, todo_id, data, timestamp, device_id, synced)
			VALUES (?, ?, ?, ?, ?, ?, 0)
		`, opID, "COMPLETE", todo.ID, string(data), timestamp, deviceID)
		if err != nil {
			return fmt.Errorf("failed to save operation: %w", err)
		}
	}

	completedVal := 0
	if completed {
		completedVal = 1
	}

	_, err = tx.Exec(`
		UPDATE todos
		SET completed = ?, updated_at = ?
		WHERE id = ?
	`, completedVal, timestamp, id)

	if err != nil {
		return fmt.Errorf("failed to update todo: %w", err)
	}

	return tx.Commit()
}

func (s *Storage) DeleteTodo(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	timestamp := time.Now().UnixNano()

	var syncEnabled string
	err = tx.QueryRow("SELECT value FROM config WHERE key = 'sync_enabled'").Scan(&syncEnabled)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to check sync_enabled: %w", err)
	}

	if syncEnabled == "true" {
		var deviceID string
		err = tx.QueryRow("SELECT value FROM config WHERE key = 'device_id'").Scan(&deviceID)
		if err != nil {
			return fmt.Errorf("failed to get device_id: %w", err)
		}

		opID := uuid.New().String()

		_, err = tx.Exec(`
			INSERT INTO operations (id, type, todo_id, data, timestamp, device_id, synced)
			VALUES (?, ?, ?, ?, ?, ?, 0)
		`, opID, "DELETE", id, "{}", timestamp, deviceID)
		if err != nil {
			return fmt.Errorf("failed to save operation: %w", err)
		}
	}

	_, err = tx.Exec(`
		UPDATE todos
		SET deleted = 1, updated_at = ?
		WHERE id = ?
	`, timestamp, id)

	if err != nil {
		return fmt.Errorf("failed to delete todo: %w", err)
	}

	return tx.Commit()
}

func (s *Storage) HardDeleteTodo(id string) error {
	_, err := s.db.Exec("DELETE FROM todos WHERE id = ?", id)
	return err
}

func (s *Storage) GetStreak() (*models.Streak, error) {
	var streak models.Streak
	err := s.db.QueryRow(`
		SELECT current_streak, max_streak, total_completed, COALESCE(last_completed_at, 0)
		FROM streaks WHERE id = 1
	`).Scan(&streak.CurrentStreak, &streak.MaxStreak, &streak.TotalCompleted, &streak.LastCompletedAt)

	if err != nil {
		return nil, err
	}

	return &streak, nil
}

func (s *Storage) UpdateStreak(streak *models.Streak) error {
	_, err := s.db.Exec(`
		UPDATE streaks
		SET current_streak = ?, max_streak = ?, total_completed = ?, last_completed_at = ?
		WHERE id = 1
	`, streak.CurrentStreak, streak.MaxStreak, streak.TotalCompleted, streak.LastCompletedAt)

	return err
}

func (s *Storage) GetConfig(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&value)
	return value, err
}

func (s *Storage) SetConfig(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO config (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, key, value)
	return err
}

func (s *Storage) CleanupOldCompleted() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	limitStr, err := s.GetConfig("completed_limit")
	if err != nil {
		limitStr = "50"
	}

	var limit int
	fmt.Sscanf(limitStr, "%d", &limit)
	if limit <= 0 {
		limit = 50
	}

	var syncEnabled string
	err = tx.QueryRow("SELECT value FROM config WHERE key='sync_enabled'").Scan(&syncEnabled)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to check sync status: %w", err)
	}

	rows, err := tx.Query(`
		SELECT id FROM todos
		WHERE completed = 1
		ORDER BY updated_at DESC
		LIMIT -1 OFFSET ?
	`, limit)
	if err != nil {
		return err
	}
	defer rows.Close()

	var idsToDelete []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		idsToDelete = append(idsToDelete, id)
	}
	rows.Close()

	timestamp := time.Now().UnixNano()

	if syncEnabled == "true" {
		for _, id := range idsToDelete {
			_, err := tx.Exec(`
				UPDATE todos SET deleted = 1, updated_at = ?
				WHERE id = ?
			`, timestamp, id)
			if err != nil {
				return fmt.Errorf("failed to soft delete todo: %w", err)
			}
		}
	} else {
		for _, id := range idsToDelete {
			_, err := tx.Exec("DELETE FROM todos WHERE id = ?", id)
			if err != nil {
				return fmt.Errorf("failed to hard delete todo: %w", err)
			}
		}
	}

	return tx.Commit()
}

func (s *Storage) SaveOperation(id, opType, todoID string, data []byte, timestamp int64, deviceID string) error {
	_, err := s.db.Exec(`
		INSERT INTO operations (id, type, todo_id, data, timestamp, device_id, synced)
		VALUES (?, ?, ?, ?, ?, ?, 0)
	`, id, opType, todoID, string(data), timestamp, deviceID)
	return err
}

func (s *Storage) GetOperations(since string) ([]OperationData, error) {
	var rows *sql.Rows
	var err error

	if since == "" {
		rows, err = s.db.Query(`
			SELECT id, type, todo_id, data, timestamp, device_id, synced
			FROM operations
			WHERE synced = 0
			ORDER BY timestamp ASC, id ASC
		`)
	} else {
		var sinceTimestamp int64
		err = s.db.QueryRow("SELECT timestamp FROM operations WHERE id = ?", since).Scan(&sinceTimestamp)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("failed to get since timestamp: %w", err)
		}

		rows, err = s.db.Query(`
			SELECT id, type, todo_id, data, timestamp, device_id, synced
			FROM operations
			WHERE timestamp > ? AND synced = 0
			ORDER BY timestamp ASC, id ASC
		`, sinceTimestamp)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var operations []OperationData
	for rows.Next() {
		var op OperationData
		if err := rows.Scan(&op.ID, &op.Type, &op.TodoID, &op.Data, &op.Timestamp, &op.DeviceID, &op.Synced); err != nil {
			return nil, err
		}
		operations = append(operations, op)
	}

	return operations, rows.Err()
}

func (s *Storage) GetOperationsSince(timestamp int64) ([]OperationData, error) {
	rows, err := s.db.Query(`
		SELECT id, type, todo_id, data, timestamp, device_id, synced
		FROM operations
		WHERE timestamp > ?
		ORDER BY timestamp ASC, id ASC
	`, timestamp)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var operations []OperationData
	for rows.Next() {
		var op OperationData
		if err := rows.Scan(&op.ID, &op.Type, &op.TodoID, &op.Data, &op.Timestamp, &op.DeviceID, &op.Synced); err != nil {
			return nil, err
		}
		operations = append(operations, op)
	}

	return operations, rows.Err()
}

func (s *Storage) GetLastOperationID() (string, error) {
	var id string
	err := s.db.QueryRow(`
		SELECT id FROM operations
		ORDER BY timestamp DESC, id DESC
		LIMIT 1
	`).Scan(&id)

	if err == sql.ErrNoRows {
		return "", nil
	}

	return id, err
}

func (s *Storage) MarkOperationsSynced(ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, id := range ids {
		_, err := tx.Exec("UPDATE operations SET synced = 1 WHERE id = ?", id)
		if err != nil {
			return fmt.Errorf("failed to mark operation synced: %w", err)
		}
	}

	return tx.Commit()
}

func (s *Storage) GetDB() *sql.DB {
	return s.db
}

func (s *Storage) AddOrUpdatePeer(peer *Peer) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO peers (id, name, address, last_seen, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			address = excluded.address,
			last_seen = excluded.last_seen,
			status = excluded.status
	`, peer.ID, peer.Name, peer.Address, peer.LastSeen, peer.Status, peer.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to add/update peer: %w", err)
	}

	return tx.Commit()
}

func (s *Storage) GetPeers() ([]*Peer, error) {
	rows, err := s.db.Query(`
		SELECT id, name, address, last_seen, status, created_at
		FROM peers
		ORDER BY last_seen DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query peers: %w", err)
	}
	defer rows.Close()

	var peers []*Peer
	for rows.Next() {
		peer := &Peer{}
		err := rows.Scan(&peer.ID, &peer.Name, &peer.Address, &peer.LastSeen, &peer.Status, &peer.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan peer: %w", err)
		}
		peers = append(peers, peer)
	}

	return peers, rows.Err()
}

func (s *Storage) GetActivePeers() ([]*Peer, error) {
	rows, err := s.db.Query(`
		SELECT id, name, address, last_seen, status, created_at
		FROM peers
		WHERE status IN ('discovered', 'connected', 'syncing')
		ORDER BY last_seen DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query active peers: %w", err)
	}
	defer rows.Close()

	var peers []*Peer
	for rows.Next() {
		peer := &Peer{}
		err := rows.Scan(&peer.ID, &peer.Name, &peer.Address, &peer.LastSeen, &peer.Status, &peer.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan peer: %w", err)
		}
		peers = append(peers, peer)
	}

	return peers, rows.Err()
}

func (s *Storage) UpdatePeerStatus(id string, status string) error {
	_, err := s.db.Exec("UPDATE peers SET status = ? WHERE id = ?", status, id)
	if err != nil {
		return fmt.Errorf("failed to update peer status: %w", err)
	}
	return nil
}

func (s *Storage) UpdatePeerLastSeen(id string, timestamp int64) error {
	_, err := s.db.Exec("UPDATE peers SET last_seen = ? WHERE id = ?", timestamp, id)
	if err != nil {
		return fmt.Errorf("failed to update peer last_seen: %w", err)
	}
	return nil
}

func (s *Storage) GetSyncState(peerID string) (*SyncState, error) {
	state := &SyncState{PeerID: peerID}
	err := s.db.QueryRow(`
		SELECT last_operation_id, last_sync_time, operations_sent, operations_received
		FROM sync_state
		WHERE peer_id = ?
	`, peerID).Scan(&state.LastOperationID, &state.LastSyncTime, &state.OperationsSent, &state.OperationsReceived)

	if err == sql.ErrNoRows {
		return state, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get sync state: %w", err)
	}

	return state, nil
}

func (s *Storage) UpdateSyncState(peerID string, state *SyncState) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO sync_state (peer_id, last_operation_id, last_sync_time, operations_sent, operations_received)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(peer_id) DO UPDATE SET
			last_operation_id = excluded.last_operation_id,
			last_sync_time = excluded.last_sync_time,
			operations_sent = sync_state.operations_sent + excluded.operations_sent,
			operations_received = sync_state.operations_received + excluded.operations_received
	`, peerID, state.LastOperationID, state.LastSyncTime, state.OperationsSent, state.OperationsReceived)

	if err != nil {
		return fmt.Errorf("failed to update sync state: %w", err)
	}

	return tx.Commit()
}

func (s *Storage) SavePeerSecret(peerID, secret string) error {
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO peer_secrets (peer_id, secret)
		VALUES (?, ?)
	`, peerID, secret)
	return err
}

func (s *Storage) GetPeerSecret(peerID string) (string, error) {
	var secret string
	err := s.db.QueryRow(`
		SELECT secret FROM peer_secrets WHERE peer_id = ?
	`, peerID).Scan(&secret)
	return secret, err
}

func (s *Storage) Close() error {
	return s.db.Close()
}
