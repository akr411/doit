# doit - Implementation TODO

**IMPORTANT:** Update this file as you complete tasks. Mark items complete with [x] and add new tasks as needed.

**Last updated:** 2025-12-04

---

## Phase 0: TUI/CLI ✅ COMPLETE

All tasks completed and verified. See CLAUDE.md for success criteria.

---

## Phase 1: CRDT Foundation ✅ COMPLETE

**Read First:** [docs/PHASE1_CRDT_GUIDE.md](docs/PHASE1_CRDT_GUIDE.md) for research, decisions, and implementation patterns

**Completed:** 2025-11-26

### 1.1 Operation Types

**File:** `internal/sync/operation.go`

- [x] Operation struct: ID string, Type string, TodoID string, Data []byte (JSON), Timestamp int64 (Unix nano), DeviceID string
- [x] Types: "CREATE", "UPDATE", "DELETE", "COMPLETE"
- [x] Apply(op) error - deserialize Data, insert/update todos table based on Type
- [x] Handle tombstones: DELETE operations set deleted=1, don't remove row
- [x] Note: NO VectorClock field needed (timestamp + device_id sufficient for LWW)

### 1.2 Operation Storage

**File:** `internal/storage/storage.go`

Schema changes:
- [x] Add to todos table: `deleted INTEGER DEFAULT 0`
- [x] Create index: idx_todos_deleted ON todos(deleted)
- [x] Update queries to filter: WHERE deleted=0

Operations table (exact types):
- [x] CREATE TABLE operations: id TEXT PK, type TEXT NOT NULL, todo_id TEXT NOT NULL, data TEXT NOT NULL, timestamp INTEGER NOT NULL, device_id TEXT NOT NULL, synced INTEGER DEFAULT 0
- [x] Indexes: idx_ops_timestamp(timestamp), idx_ops_synced(synced), idx_ops_device(device_id), idx_ops_todo(todo_id)
- [x] Note: NO vector_clock column

Interface:
- [x] SaveOperation(op) error
- [x] GetOperations(since string) ([]*Operation, error) - 'since' is operation ID
- [x] GetOperationsSince(timestamp int64) ([]*Operation, error) - timestamp-based query
- [x] GetLastOperationID() string - for sync protocol
- [x] MarkOperationsSynced(ids []string) error - batch update synced=1

### 1.3 CRDT Merge Logic

**File:** `internal/sync/crdt.go`

- [x] Merge(local, remote) - combine ops, dedupe by ID (use map for O(1) lookup), return union
- [x] LWW comparison: if ts1>ts2 → op1 wins; if ts1==ts2 → compare device_id lexicographically (deterministic)
- [x] RebuildState(ops) - DELETE FROM todos → sort ops by timestamp ASC, device_id ASC → Apply each in order
- [x] Verify CRDT properties: commutative (order doesn't matter), idempotent (apply twice = apply once), convergent (same final state)
- [x] Test with concurrent operations, out-of-order delivery, duplicates

### 1.4 Device Identity

**File:** `internal/sync/device.go`

- [x] Generate UUID on first run, store in config table as key="device_id"
- [x] GetDeviceID() - read from config table
- [x] GetDeviceName() - os.Hostname() + runtime.GOOS (e.g., "desktop-arch-linux")
- [x] Store sync config in config table: sync_enabled (bool), sync_port (int)

### 1.5 Hook Storage → Operations

**File:** `internal/storage/storage.go`

- [x] Modify SaveTodo: tx.Begin() → INSERT CREATE operation (full todo JSON) → INSERT todo → tx.Commit()
- [x] Modify UpdateTodo: tx.Begin() → INSERT UPDATE operation (full todo JSON) → UPDATE todo → tx.Commit()
- [x] Add CompleteTodo wrapper: tx.Begin() → INSERT COMPLETE operation (full todo JSON with toggled completed) → UPDATE todo → tx.Commit()
- [x] Modify DeleteTodo: tx.Begin() → INSERT DELETE operation (Data=null) → UPDATE todo SET deleted=1 → tx.Commit()
- [x] CRITICAL: Operation comes FIRST (source of truth), then state change
- [x] Check sync_enabled before creating operations (if disabled, skip operation insert)
- [x] Use timestamp: time.Now().UnixNano() (nanosecond precision to avoid collisions)
- [x] Use GetDeviceID() for device_id field

### 1.6 Testing & Verification

**File:** `internal/sync/crdt_test.go`

CRDT Properties:
- [x] Test commutative: Apply(op1, op2) == Apply(op2, op1)
- [x] Test idempotent: Apply(op) twice == Apply(op) once
- [x] Test convergent: Different operation orders → same final state
- [x] Test LWW resolution: Higher timestamp wins, device_id tiebreaker

Edge Cases:
- [x] Clock skew: Device with ts=999999, device with ts=1000
- [x] Duplicate operations: Same op_id applied multiple times
- [x] Out-of-order: Receive op-3, op-1, op-2 → correct final state
- [x] DELETE vs UPDATE conflict: Simultaneous delete and edit
- [x] Empty DB: RebuildState() on fresh database
- [x] Tombstone queries: deleted=1 todos not returned by GetAllTodos()

Integration:
- [x] Add todo → verify operation created
- [x] Edit todo → verify UPDATE operation
- [x] Complete todo → verify COMPLETE operation (not UPDATE)
- [x] Delete todo → verify DELETE operation + deleted=1
- [x] RebuildState() → verify todos table matches operation log

---

## Phase 1.7: Data Retention ✅ COMPLETE

**Objective:** Prevent unbounded growth of operations log and tombstones

**Completed:** 2025-11-26

### Retention System

**File:** `internal/storage/cleanup.go`

- [x] CleanupSyncData(aggressive) - unified cleanup function
- [x] cleanupTombstones() - remove old deleted todos (30 days + completed_limit)
- [x] cleanupOperations() - remove old synced ops (30 days + keep last 10 per todo)
- [x] ShouldRunCleanup() - check if cleanup needed (24h interval)
- [x] GetCleanupStats() - show cleanup stats and database size
- [x] Configuration helpers (retention days, max ops per todo, interval)

### Storage Updates

**File:** `internal/storage/storage.go`

- [x] Modified CleanupOldCompleted() - soft delete when sync enabled, hard delete otherwise
- [x] Added transaction wrapper for atomic cleanup
- [x] Fixed timestamp consistency (nanosecond throughout)
- [x] Added composite indexes for cleanup queries

### CLI Integration

**File:** `cmd/sync.go`

- [x] doit sync status - show sync state, operations, tombstones, DB size
- [x] doit sync cleanup - manual cleanup
- [x] doit sync cleanup --dry-run - preview without deleting
- [x] doit sync cleanup --aggressive - nuclear option with confirmation

### Startup Integration

**File:** `cmd/root.go`

- [x] Auto-cleanup on startup (if interval passed)
- [x] Respects auto_cleanup_enabled config (default: true)
- [x] Silent operation with warnings only on errors

### Configuration Defaults

- [x] auto_cleanup_enabled: true (opt-out design)
- [x] cleanup_interval_hours: 24
- [x] operation_retention_days: 30
- [x] tombstone_retention_days: 30
- [x] max_operations_per_todo: 10

### Testing

- [x] Cleanup actually removes old data
- [x] Dry-run shows accurate preview
- [x] Aggressive mode requires confirmation
- [x] CTE queries prevent race conditions
- [x] Transactional cleanup prevents partial deletes
- [x] All unit tests pass with race detector

---

## Phase 2: Local Network Sync (mDNS) ✅ COMPLETE

**Completed:** 2025-12-04

**Read First:** [docs/PHASE2_MDNS_GUIDE.md](docs/PHASE2_MDNS_GUIDE.md) for research, API docs, patterns, and pitfalls

### 2.1 mDNS Discovery

**File:** `internal/sync/discovery.go`

Dependencies:
- [x] `go get github.com/hashicorp/mdns@v1.0.6`

Server (Announcement):
- [x] StartMDNSServer(port) - mdns.NewMDNSService() with service="_doit._tcp", port from config
- [x] TXT records MUST include: "v=1", "device_id=<uuid>", "name=<hostname-os>"
- [x] Return *mdns.Server for later Shutdown()

Client (Discovery):
- [x] DiscoverPeers() - mdns.Lookup("_doit._tcp", entriesCh) with 3s timeout
- [x] Parse ServiceEntry: extract device_id from InfoFields, get AddrV4 + Port
- [x] Filter self: skip if device_id == our device_id
- [x] Return []Peer or send to channel

Background Loop:
- [x] discoveryLoop() - runs every 30s, calls DiscoverPeers(), updates peer addresses
- [x] Handle events: new peer → AddOrUpdatePeer(), existing peer → update last_seen

### 2.2 Peer Management

**File:** `internal/sync/peer.go` + `internal/storage/storage.go` (schema)

Schema (exact types - add to storage.go createTables()):
- [x] `peers`: id TEXT PK, name TEXT NOT NULL, address TEXT NOT NULL, last_seen INTEGER, status TEXT, created_at INTEGER NOT NULL
- [x] `sync_state`: peer_id TEXT PK, last_operation_id TEXT, last_sync_time INTEGER, operations_sent INTEGER DEFAULT 0, operations_received INTEGER DEFAULT 0, FOREIGN KEY(peer_id) REFERENCES peers(id) ON DELETE CASCADE
- [x] Indexes: idx_peers_status ON peers(status), idx_peers_last_seen ON peers(last_seen)
- [x] Note: Simplified from addresses (JSON array) to single address (re-discovered via mDNS if IP changes)

Peer struct:
- [x] type Peer struct with ID, Name, Address, LastSeen, Status, CreatedAt

Interface (in storage.go):
- [x] AddOrUpdatePeer(peer) error - INSERT OR UPDATE peer, upsert address if exists
- [x] GetPeers() ([]*Peer, error) - all peers ordered by last_seen DESC
- [x] GetActivePeers() ([]*Peer, error) - WHERE status IN ('connected', 'syncing')
- [x] UpdatePeerStatus(id, status) error - status: "discovered"|"connected"|"syncing"|"disconnected"|"failed"
- [x] UpdatePeerLastSeen(id, timestamp) error
- [x] GetSyncState(peerID) (*SyncState, error)
- [x] UpdateSyncState(peerID, lastOpID, lastSyncTime, sent, received) error
- [x] CRITICAL: Use transactions, handle concurrent updates with sync.RWMutex in peer manager

### 2.3 HTTP Sync Server

**File:** `internal/sync/server.go`

Server Setup:
- [x] Create http.Server with timeouts: ReadTimeout=15s, WriteTimeout=15s, IdleTimeout=60s
- [x] Start on port from GetSyncPort() (default: 8888)
- [x] Run in goroutine (don't block main)
- [x] Graceful shutdown: listen for os.Interrupt + syscall.SIGTERM
- [x] Shutdown with 30s timeout: srv.Shutdown(ctx)
- [x] Return server reference for later shutdown

Endpoints (see docs/PHASE2_MDNS_GUIDE.md for detailed examples):
- [x] GET /sync/operations?since=opID
  - Call store.GetOperations(since)
  - Convert to []*sync.Operation (not map[string]interface{})
  - Return JSON array
  - Auth required

- [x] POST /sync/operations
  - Parse JSON array of operations
  - For each op: op.Apply(db) (idempotency handled in Apply)
  - Return count of applied operations
  - Auth required

- [x] GET /sync/state
  - Return {last_op_id, device_id, device_name}
  - No auth needed (used for initial handshake)

- [x] POST /sync/pair (for Phase 3, stub for now)
  - Validate shared_secret
  - Add peer to database
  - Return our info
  - Auth required

Auth Middleware:
- [x] Generate shared_secret on first sync init (crypto/rand 32 bytes, base64)
- [x] Store in config.shared_secret
- [x] authMiddleware() - check X-Doit-Secret header, return 401 if wrong/missing
- [x] Wrap all endpoints except GET /sync/state

### 2.4 Sync Protocol

**File:** `internal/sync/protocol.go`

HTTP Client Setup:
- [x] Create shared http.Client with: Timeout=30s, MaxIdleConns=100, MaxConnsPerHost=20, IdleConnTimeout=90s
- [x] Reuse client for all requests (connection pooling)

Core Functions:
- [x] PullOperations(peer, since) error
  - HTTP GET http://{peer.Address}/sync/operations?since={since}
  - Set X-Doit-Secret header
  - Parse JSON response into []*Operation
  - Return operations or error

- [x] PushOperations(peer, ops) error
  - HTTP POST http://{peer.Address}/sync/operations
  - Set X-Doit-Secret header
  - Send JSON array of operations
  - Return error if failed

- [x] GetPeerState(peer) (*PeerState, error)
  - HTTP GET http://{peer.Address}/sync/state
  - NO auth needed (initial handshake)
  - Parse {last_op_id, device_id, device_name}
  - Return state

- [x] InitialSync(peer) error
  - GET /sync/state from peer
  - Pull operations we're missing
  - Push operations they're missing
  - Update sync_state table

Retry Logic:
- [x] Implement exponential backoff: 1s, 2s, 4s, 8s, 16s, 32s, 60s (max)
- [x] Max retry time: 2 minutes total
- [x] Per-request timeout: 30s (via context)
- [x] Return error only after max retries exhausted
- [x] Use context.WithTimeout for each request

### 2.5 Sync Engine

**File:** `internal/sync/engine.go`

Sync Engine struct:
- [x] type SyncEngine with store, peerManager, discovery, server, stopCh, running flag
- [x] Use sync.RWMutex to protect running state

Core Functions:
- [x] Start(ctx context.Context) error
  - Start mDNS server (announcement)
  - Start mDNS discovery loop (30s interval)
  - Start HTTP sync server
  - Start sync loop (10s interval)
  - Return immediately (all run in goroutines)

- [x] Stop() error
  - Set running = false
  - Close stopCh (stops all goroutines)
  - Shutdown mDNS server
  - Shutdown HTTP server (30s timeout)
  - Wait for goroutines to finish

- [x] syncLoop()
  - time.Ticker(10s)
  - For each active peer: SyncWithPeer(peer)
  - On error: log, update peer status, continue to next
  - Select on ticker and stopCh

- [x] SyncWithPeer(peer) error
  - UpdatePeerStatus(peer.ID, "syncing")
  - Call protocol.InitialSync(peer)
  - On success: UpdatePeerStatus("connected"), update last_seen
  - On error: UpdatePeerStatus("disconnected"), return error
  - CRITICAL: One peer failure doesn't stop others

Error Handling:
- [x] Log errors with context (peer name, error type)
- [x] Update peer status appropriately
- [x] Never panic - continue with next peer
- [x] Track consecutive failures (for failed state)

### 2.6 CLI Integration

**File:** `cmd/sync.go` (expand existing file)

Subcommands to add:

- [x] `doit sync init`
  - Check if already enabled (warn if yes)
  - SetConfig("sync_enabled", "true")
  - Generate shared_secret if not exists (32 random bytes, base64)
  - Generate device_id if not exists
  - Start sync engine (in background, survives app exit via daemon in Phase 4)
  - Print success message with device name
  - Note: Pairing code printing moved to Phase 3

- [x] `doit sync start` (for manual start)
  - Check sync_enabled = true
  - Start sync engine
  - Print "Sync started"

- [x] `doit sync stop` (for manual stop)
  - Stop sync engine gracefully
  - Print "Sync stopped"

- [x] `doit sync disable`
  - Stop sync engine
  - SetConfig("sync_enabled", "false")
  - Offer to clean up sync data (operations + peers)
  - Print confirmation

- [x] Update existing `doit sync status` command
  - Add: connected peers list
  - Add: last sync time per peer
  - Add: sync engine running status

- [x] `doit sync devices`
  - List all peers from database
  - Show: name, address, status, last_seen
  - Format: table or JSON (--json flag)

Global sync engine:
- [x] var syncEngine *sync.SyncEngine (package-level)
- [x] Start on app init if sync_enabled=true
- [x] Stop on app exit (graceful)

Notes:
- [x] `doit sync pair` - Phase 3 (internet sync)
- [x] `doit sync show` - Phase 3 (pairing codes)
- [ ] `doit sync unpair` - Phase 3

---

## Phase 2.7: Critical Fixes ✅ COMPLETE

**Completed:** 2025-12-04

### Fixes Applied

- [x] Issue #1: Operations marked synced after push (protocol.go:220-228)
- [x] Issue #2: Peer manager loads on startup (engine.go:57-59)
- [x] Issue #3: Data type consistency fixed (storage.go:658)
- [x] Issue #5: Race condition in Stop() eliminated (engine.go:79-102)
- [x] Issue #6: Port auto-increment 49152→49153→49154 (server.go:56-95)
- [x] Issue #7: GetOperations filters synced=0 (storage.go:623, 636)
- [x] Issue #8: Discovered peers included in sync loop (storage.go:794)
- [x] Issue #9: 5-minute peer timeout added (engine.go:133-139)
- [x] Default port changed: 8888 → 49152 (device.go:69, 74)

### Validation

- [x] Build succeeds
- [x] All tests pass with -race detector
- [x] Basic commands validated
- [x] Operations created and tracked correctly
- [x] Ready for two-device testing

---

## Phase 3: Internet Sync (STUN/WebRTC)

### 3.1 Pairing System

**File:** `internal/sync/device.go`

- [ ] Generate Ed25519 keypair on first sync init, store in config table
- [ ] GeneratePairingCode() - encode: device_id + public_key + local_addrs + public_addr(STUN) + timestamp + shared_secret
- [ ] Format: base32 encoding, uppercase, no padding
- [ ] DecodePairingCode(code) - validate timestamp (<15min from now) → return PairingInfo
- [ ] ExchangePairingInfo(code) - decode → Connect(peer) → POST /sync/pair → AddPeer(response)

### 3.2 STUN Integration

**File:** `internal/sync/stun.go`

- [ ] `go get github.com/pion/stun`
- [ ] DiscoverPublicAddress() (string, error) - returns "ip:port"
- [ ] STUN servers: stun.l.google.com:19302, stun1.l.google.com:19302, stun.cloudflare.com:3478
- [ ] Query: send binding request, parse XOR-MAPPED-ADDRESS
- [ ] Cache in memory (publicAddr string, lastCheck time.Time), refresh if age > 5min
- [ ] Timeout: 5s per server, try next on timeout/error

### 3.3 WebRTC Connections

**File:** `internal/sync/webrtc.go`

- [ ] `go get github.com/pion/webrtc/v3`
- [ ] CreateWebRTCConnection(peer) - create connection, data channel, SDP offer/answer
- [ ] Wait for connection state == "connected" (timeout 10s)
- [ ] Keep-alive: send ping on data channel every 30s, close if no pong in 60s

### 3.4 Multi-Method Connection

**File:** `internal/sync/transport.go`

- [ ] Connect(peer) - 3 parallel attempts: tryMDNS, tryDirect, trySTUN
- [ ] Use context.WithCancel: first success cancels others, timeout 10s total
- [ ] Connection interface: Read/Write/Close
- [ ] Wrap HTTP conn OR WebRTC data channel
- [ ] On failure: retry with exponential backoff

### 3.5 Address Exchange

**File:** `internal/sync/protocol.go`

- [ ] POST /sync/pair endpoint - validate shared_secret, INSERT INTO peers, return our info
- [ ] Address updates: when public IP changes, UPDATE peers SET addresses
- [ ] Addresses stored as JSON array: ["192.168.1.10:49152", "73.45.198.123:49152"]

### 3.6 CLI Commands

**File:** `cmd/sync.go`

- [ ] `doit sync pair <code>` - pair with device
- [ ] `doit sync show` - show my pairing code
- [ ] `doit sync devices` - list paired devices
- [ ] `doit sync unpair <device>` - remove pairing

---

## Phase 4: Polish & Testing

### 4.1 Background Daemon

**File:** `internal/sync/daemon.go`

- [ ] Run sync engine in background on app start
- [ ] `doit sync daemon` - foreground service
- [ ] Signal handling, PID file, logging

### 4.2 UI Integration

**File:** `internal/ui/inline_list.go`

- [ ] Sync indicator in header: "⟳ Synced with 2 devices"
- [ ] Show last sync time
- [ ] `s` key → sync settings (if interactive mode)

### 4.3 Configuration

- [ ] Store in SQLite config: sync_enabled, sync_port (8888), sync_interval (10s)
- [ ] STUN servers list, max_peers
- [ ] GetConfig(key), SetConfig(key, val)

### 4.4 Error Handling

- [ ] Network errors: timeout, unreachable, dropped connection
- [ ] Storage errors: disk full, corruption
- [ ] Invalid operations: malformed data
- [ ] Retry with exponential backoff
- [ ] Graceful degradation: work offline always

### 4.5 Logging

- [ ] Structured logs: sync events, errors with context
- [ ] Debug mode: verbose operation log
- [ ] File: `~/.local/share/doit/sync.log`

### 4.6 Basic Testing

Unit tests:
- [ ] CRDT: merge, LWW resolution, apply/rebuild
- [ ] Pairing: encode/decode codes

Integration:
- [ ] 2-device sync, concurrent edits, offline→online, network partition
- [ ] Test: LAN, internet, 3+ devices, NAT traversal

---

## Phase 4.5: Battle Testing (CRITICAL)

See docs/SYNC_ARCHITECTURE.md for detailed battle testing scenarios.

### 4.5.1 Database Corruption & Recovery
- [ ] Power loss recovery test
- [ ] Disk full handling
- [ ] Corrupted DB detection and rebuild
- [ ] Missing WAL file recovery
- [ ] Concurrent access test
- [ ] Backup during write test

### 4.5.2 CRDT Conflict & Consistency
- [ ] Concurrent edit same todo (timestamp tie)
- [ ] Delete vs update conflict
- [ ] 3-way merge test
- [ ] Clock skew handling
- [ ] Duplicate operation idempotency
- [ ] Out-of-order operation handling
- [ ] Network partition recovery
- [ ] Cascading sync test

### 4.5.3 Network Failure & Resilience
- [ ] Connection drop mid-sync
- [ ] Timeout on slow network
- [ ] Peer offline handling
- [ ] Flaky connection test
- [ ] NAT traversal failure
- [ ] Firewall block handling
- [ ] mDNS blocked fallback
- [ ] DNS failure handling

### 4.5.4 Race Conditions & Concurrency
- [ ] Concurrent SaveTodo test
- [ ] Read-write race test
- [ ] Sync while editing test
- [ ] 3 devices sync simultaneously
- [ ] 1000 rapid operations test
- [ ] `go test -race` must pass

### 4.5.5 Edge Cases & Boundaries
- [ ] Empty DB (fresh install)
- [ ] 10K todos performance
- [ ] Very long strings (10K chars)
- [ ] Special chars: Unicode, emoji, SQL injection, path traversal
- [ ] Invalid data validation
- [ ] Max values: int64 max timestamp, max UUID
- [ ] Zero/null handling

### 4.5.6 Stress & Load Testing
- [ ] 1000 ops/sec test
- [ ] 7-day continuous run
- [ ] 10 paired devices scaling
- [ ] 100K operation log sync
- [ ] Sync storm handling

### 4.5.7 Security & Malicious Input
- [ ] Invalid operations rejection
- [ ] Replay attack prevention
- [ ] Tampered data validation
- [ ] Pairing brute force protection
- [ ] Resource exhaustion defense

### 4.5.8 Recovery & Repair

**File:** `cmd/repair.go`

- [ ] `doit repair check` - integrity check
- [ ] `doit repair rebuild` - rebuild from operations
- [ ] `doit repair reset-sync` - clear sync state

### 4.5.9 Chaos Engineering

**File:** `test/chaos.sh`

- [ ] 1h chaos test with random failures
- [ ] Verify recovery from all failure modes

### 4.5.10 Automated Battle Test Suite

**File:** `test/battle/battle_test.go`

- [ ] Multi-device simulator
- [ ] Random failure injection
- [ ] Invariant verification: no data loss, no corruption, eventual consistency
- [ ] CI/CD integration

### 4.5.11 User Acceptance Testing
- [ ] 1 week daily driver use
- [ ] Multi-device (3+) testing
- [ ] Offline mode testing
- [ ] Crash and recover testing
- [ ] 100 todos sync verification

### 4.5.12 Failure Mode Documentation
- [ ] Document all failure modes in README
- [ ] Recovery procedures for each mode
- [ ] Add troubleshooting section to help

---

## Phase 5: Advanced Features (Future - On Request)

- [ ] E2E encryption (encrypt ops with peer's pubkey)
- [ ] Optional relay server (for symmetric NAT)
- [ ] Conflict UI (show both versions, let user choose)
- [ ] Selective sync (incomplete only, by tag/category)
- [ ] Sync history (timeline, per-device log, rollback)

---

## Documentation Tasks

- [ ] Update README with sync usage
- [ ] Create FAQ: how sync works, privacy, offline scenarios
- [ ] Architecture diagram
- [ ] CONTRIBUTING.md
- [ ] Troubleshooting guide

---

## Notes

**Before calling Phase 1 complete:**
- All 1.1-1.5 tasks must be ✓
- CRDT properties verified (commutative, idempotent, associative)
- Operation log correctly rebuilds state
- No data loss on conflicts

**Before calling project production-ready:**
- All battle testing scenarios pass
- 1 week real-world use with zero crashes
- All devices converge to same state
- Sync within 10s
- Works offline always
