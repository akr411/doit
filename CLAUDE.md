# doit - Personal Todo CLI with P2P Sync

No servers, works offline, syncs automatically.

---

## Current Status

**Complete**: Phase 0 (TUI), Phase 1 (CRDT), Phase 2 (Local Sync + Pairing)
**Security**: Pairing codes ✓, TLS encryption pending
**Next**: Phase 3 - TLS + mTLS

Track: [TODO.md](TODO.md) | Architecture: [docs/SYNC_ARCHITECTURE.md](docs/SYNC_ARCHITECTURE.md)

---

## IMPORTANT - Core Principles

**Before coding**:
- Web search if unsure - no assumptions, no hallucinations
- Ask clarifying questions if ambiguous

**When coding**:
- Test before marking complete - run actual code
- Update TODO.md as you complete tasks
- Validate all input (block: null bytes, control chars, bidi overrides, zero-width chars)
- NO code comments unless absolutely necessary
- Simple > complex - avoid over-engineering
- Follow Go idioms, prefer stdlib

**When stuck**: Web search → ask user → propose options

---

## Architecture

**Stack**:
- SQLite+WAL (`modernc.org/sqlite` - pure Go, no CGo)
- Bubbletea + Lipgloss (TUI)
- Cobra (CLI)

**Sync**:
- CRDT with LWW conflict resolution
- UDP broadcast discovery (port 49151)
- HTTP sync server (port 49152) - **run via `doit sync daemon`**
- Pairing code system (6-digit, 15min expiry, single-use)
- Per-peer secrets (stored in peer_secrets table)

**Tables**: todos, operations, peers, sync_state, peer_secrets, pairing_codes, config, streaks

**Auto-cleanup**: 30-day retention (operations + tombstones), keeps last 10 ops per todo

---

## Security (2025-12-09)

**Current** (Phase 1 + 2):
- ✅ Pairing code system (no unauthenticated secret exposure)
- ✅ Per-peer secrets
- ⚠️ HTTP only (traffic visible on network)
- ⚠️ Safe on trusted networks only

**Phase 3 (Pending)**:
- TLS 1.3 + mTLS (encrypt all traffic)
- Certificate fingerprint pinning (prevent MITM)
- Safe on public WiFi

**Security docs**: [docs/PAKE_TLS_ARCHITECTURE.md](docs/PAKE_TLS_ARCHITECTURE.md)

---

## Key Commands

```bash
# Pairing (required before sync works)
doit sync show              # Generate pairing code (15min expiry)
doit sync pair <code>       # Pair with device using code

# Sync operations
doit sync daemon            # Run sync server in foreground
doit sync devices           # List discovered/paired devices
doit sync status            # Sync stats + cleanup info

# Development
doit sync init              # Enable sync
doit sync disable           # Disable sync
doit sync cleanup --dry-run # Preview cleanup

# Run `doit --help` for full command list
```

---

## Pairing Workflow

```bash
# Device A
$ doit sync show
  Code: 123-456
  Expires: 14m 59s

# Device B (on same network)
$ doit sync pair 123-456
✓ Paired with Device A

# Run daemon on both devices for continuous sync
$ doit sync daemon
✓ Sync daemon running (syncs every 10s)
```

---

## UI Guidelines

**Colors** (internal/ui/styles.go):
- Success: Green (#52C41A)
- Warning: Orange (#FAAD14)
- Error: Red (#FF4D4F)

**Helpers**:
```go
ui.PrintSuccess("✓ Done")
ui.PrintWarning("Warning: %v", err)
ui.PrintError("Error: %v", err)
```

**Pagination**: 10/25/50 (config)
**Retention**: 10-500 completed (config)

Detailed UI specs: [docs/UI_GUIDE.md](docs/UI_GUIDE.md)

---

## Key Files

**Sync/CRDT**:
- `internal/sync/operation.go` - Operation types, Apply() with LWW
- `internal/sync/crdt.go` - Merge(), RebuildState()
- `internal/sync/pairing.go` - PairingManager (code gen, validation)
- `internal/sync/server.go` - HTTP server + /pair endpoint
- `internal/sync/protocol.go` - Pull/Push with per-peer secrets
- `internal/sync/localdisco.go` - UDP broadcast discovery

**Storage**:
- `internal/storage/storage.go` - Schema, CRUD, SavePeerSecret()
- `internal/storage/cleanup.go` - Auto-cleanup (30-day retention)

**UI**:
- `internal/ui/styles.go` - Colors, print helpers
- `internal/ui/inline_list.go` - Bubbletea TUI

---

## Dependencies

```bash
go get modernc.org/sqlite                    # Storage
go get github.com/charmbracelet/bubbletea    # TUI
go get github.com/charmbracelet/lipgloss     # Styling
go get github.com/spf13/cobra                # CLI
go get github.com/google/uuid                # Device IDs

# Phase 3 (pending)
go get github.com/pion/webrtc/v3             # WebRTC P2P
go get github.com/pion/stun                  # NAT traversal
```

---

## Common Tasks

**Build**: `go build -o doit ./cmd/`
**Test**: `go test ./...`
**Test with race**: `go test ./internal/sync -race`

**Reset sync state**:
```bash
sqlite3 ~/.local/share/doit/doit.db \
  "DELETE FROM peers; DELETE FROM peer_secrets; DELETE FROM sync_state; DELETE FROM pairing_codes"
```

**View database**:
```bash
sqlite3 ~/.local/share/doit/doit.db ".tables"
sqlite3 ~/.local/share/doit/doit.db "SELECT * FROM pairing_codes"
```

---

**Last updated**: 2025-12-09
