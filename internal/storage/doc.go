// Package storage provides SQLite-backed persistence for todos, sync operations, and configuration.
//
// The storage layer uses SQLite with WAL mode for concurrent access. All sync-related
// data (operations, peers, pairing codes, certificates) is stored in a single database.
//
// # Key Features
//
// Schema: 9 tables (todos, operations, peers, sync_state, peer_secrets,
// peer_certificates, pairing_codes, config, streaks)
//
// Auto-cleanup: 30-day retention for synced operations and tombstones
//
// Transactions: All mutations wrapped in BEGIN/COMMIT
//
// Pure Go: Uses modernc.org/sqlite (no CGo)
package storage
