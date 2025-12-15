package storage

type OperationData struct {
	ID        string
	Type      string
	TodoID    string
	Data      string
	Timestamp int64
	DeviceID  string
	Synced    int
}

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
