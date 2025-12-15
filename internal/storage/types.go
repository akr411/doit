package storage

// OperationData represents a CRDT operation as stored in the database.
// Used for type-safe operation retrieval instead of map[string]interface{}.
type OperationData struct {
	ID        string
	Type      string
	TodoID    string
	Data      string
	Timestamp int64
	DeviceID  string
	Synced    int
}

// SyncStats contains synchronization statistics and cleanup information.
// Returned by GetCleanupStats for display in sync status command.
type SyncStats struct {
	TotalOperations      int
	SyncedOperations     int
	UnsyncedOperations   int
	Tombstones           int
	CleanableOperations  int
	CleanableTombstones  int
	DBSizeKB             int64
	LastCleanupTime      int64
	HoursSinceCleanup    int
}
