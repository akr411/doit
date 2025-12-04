# doit - Implementation Instructions

Personal todo CLI with P2P sync. No servers, works offline, syncs automatically.

---

## 🎯 Current Status

**Phase 0: TUI/CLI ✓ COMPLETE**
**Phase 1: CRDT Foundation ✓ COMPLETE**
**Phase 1.7: Data Retention ✓ COMPLETE**
**Phase 2: Local Network Sync (mDNS) ✓ COMPLETE**
**Phase 3: Internet Sync (STUN/WebRTC) ← START NEXT**

**IMPORTANT:** Track implementation progress in [TODO.md](TODO.md). Mark tasks complete [x] as you finish them.

See [SYNC_ARCHITECTURE.md](SYNC_ARCHITECTURE.md) for detailed sync architecture and explanations.

**Latest Updates (2025-12-04)**:
- ✓ Phase 2 (mDNS Local Network Sync) COMPLETE
- ✓ mDNS discovery fully implemented (hashicorp/mdns)
- ✓ Peer management with state tracking (peers + sync_state tables)
- ✓ HTTP sync server with shared secret authentication
- ✓ Sync protocol with exponential backoff retry (1s→60s, max 2min)
- ✓ Background sync engine (discovery: 30s, sync: 10s intervals)
- ✓ CLI commands: init, start, stop, disable, devices
- ✓ Critical fixes: sync loop prevention, port auto-increment, race conditions
- ✓ Default port: 49152 (IANA dynamic/private range, was 8888)
- ✓ 5-minute peer timeout
- ✓ All tests passing with race detector
- Ready for two-device testing on same WiFi

---

## ⚠️ MUST FOLLOW - Non-Negotiable Rules

**Before writing code:**
- Ask clarifying questions if ambiguous
- Search web for best practices if unsure
- Propose multiple approaches when uncertain

**When writing code:**
- No assumptions, no hallucinations, no guessing
- Test before marking tasks complete - run the actual code
- **UPDATE TODO.md**: Mark tasks [x] as you complete them, add new tasks as discovered
- Simple > complex, avoid over-engineering
- Follow Go idioms, prefer stdlib
- Validate all user input - block dangerous characters (null bytes, control chars, bidi overrides, zero-width chars)
- NO code comments unless absolutely necessary (no "renamed from X", no obvious explanations, no clutter)
- Keep variable names consistent across codebase regardless of libraries used
- Clean, minimal code - let the code speak for itself

**When stuck:**
- `web search: "golang <topic> best practices 2025"`
- Ask user for clarification
- Propose options, don't decide alone

---

## 🎨 UI/UX & Styling

**Color Scheme** (defined in internal/ui/styles.go):
- Primary: #5B8FF9 (blue)
- Success: #52C41A (green)
- Warning: #FAAD14 (yellow/orange)
- Error: #FF4D4F (red)
- Muted: #8C8C8C (gray)

**Helper Functions** (in internal/ui/styles.go):
```go
ui.PrintSuccess("✓ Completed %d todo(s)", count)  // Green
ui.PrintWarning("Warning: %v", err)               // Orange
ui.PrintError("Error: %v", err)                   // Red
```

**Display Rules:**
- Use Lipgloss for all styling - maintain uniform look
- Multi-line fields for long text (task/note inputs)
- Character limits: Task (200, warn at 150), Note (1000, warn at 800)
- Pagination: 10/25/50 items (configurable via `doit config pagination`)
- Retention: 10, 25, 50, 100, 200, 500 (configurable via `doit config retention`)
- No TTY vs non-TTY differences - identical output everywhere

**Note Display:**
- Truncated to 50 characters (append "..." if longer)
- Indented below task with "│" prefix in muted color
- Multi-line notes collapsed to single line (newlines → spaces)
- Not shown in --json or --quiet modes
- Full note viewable with `doit note <id>` command

**Deadline Display:**
- Show for pending tasks only (not for completed)
- Position: After task name, e.g., "Task (due in 2h)"
- Format: "overdue"/"overdue Xd", "due in Xh", "due tomorrow", "due in Xd", "due 2025-12-31"
- Consistent across all modes: CLI list, TUI interactive, interactive complete

**Warning Messages:**
- CLI mode: Print to stdout using ui.PrintWarning()
- Interactive mode: Show inline in orange, clears on next keypress
- Examples: streak update failures, cleanup errors

---

## 🖥️ Interactive Behavior

**Bubbletea TUI** (inline, NOT fullscreen):
- Cursor: > (TUI standard)
- Checkboxes: [ ] pending, [+] selected, [x] completed
- Navigation: ↑/↓ (or j/k), ←/→ (or h/l), tab/shift+tab
- Selection: 'x' key for multi-select (not space)
- Notes: space to show/hide (fullscreen if >10 lines, q/esc to close)
- Form submission: ctrl+s from any field, or enter in deadline field
- Form quit: esc quits silently (no error if not submitted)
- UI clearing: Only clear interactive UI on quit (not entire terminal)

**Pagination:**
- Pending tasks on first pages, completed tasks on last pages (separate)
- Edit/delete interactive: Filter out completed tasks (show pending only)
- Complete interactive: Show all tasks, allow toggle both ways

---

## 📝 Commands Reference

```bash
# Interactive modes
doit                      # Main TUI: navigate, add, edit, toggle, delete
doit add                  # Form: task, note, deadline
doit complete             # Toggle completion status
doit delete               # Select and delete
doit edit                 # Select and edit

# CLI mode (multiple IDs supported)
doit add -t "Task" -n "Note" -d "2h"
doit complete 1 2 3
doit delete 1 2 3 -y      # -y skips confirmation
doit edit 1 -t "New task"

# List with pagination
doit list                 # Page 1: pending tasks with notes & deadlines
doit list --page 2        # Show page 2 (shorthand: -p)
doit list --all           # Show all, override pagination (shorthand: -a)
doit list --pending       # Filter pending
doit list --completed     # Filter completed
doit note 1               # View full note

# Configuration
doit config pagination 25    # 10, 25, 50
doit config retention 100    # 10, 25, 50, 100, 200, 500
doit config streaks on       # on|off
doit stats                   # Show streak stats

# Sync commands (Phase 1.7+)
doit sync status            # Show sync and cleanup status
doit sync cleanup           # Clean old operations and tombstones
doit sync cleanup --dry-run # Preview what would be deleted
doit sync cleanup --aggressive  # Force cleanup (with confirmation)
```

---

## 🧹 Data Retention & Cleanup

**Problem**: Without retention, operations log and tombstones grow indefinitely.

**Solution**: Automatic cleanup system (enabled by default, opt-out via config).

### What Gets Cleaned

**1. Completed Todos** (existing feature)
- Keeps last N completed (default: 50, via `completed_limit`)
- Sync disabled: Hard delete (removes row)
- Sync enabled: Soft delete (sets `deleted=1` for CRDT tombstone)

**2. Tombstones** (Phase 1.7)
- Deleted todos with `deleted=1`
- Cleaned after 30 days (configurable)
- Respects `completed_limit` (keeps last N tombstones)

**3. Operations Log** (Phase 1.7)
- Keeps operations < 30 days old (configurable)
- Keeps last 10 operations per todo (configurable)
- Only deletes synced operations (`synced=1`)

### Safety Features

✓ Never deletes unsynced operations
✓ Never deletes recent data (30 day minimum)
✓ Keeps operation history (last 10 edits per todo)
✓ Respects user's retention preferences
✓ Atomic cleanup with CTE (prevents race conditions)

### Configuration

```bash
completed_limit: 50                    # Max completed+deleted to keep
operation_retention_days: 30           # Keep ops for N days
tombstone_retention_days: 30           # Keep tombstones for N days
max_operations_per_todo: 10            # Keep last N ops per todo
auto_cleanup_enabled: true             # Auto-cleanup (default: on)
cleanup_interval_hours: 24             # Run every N hours
```

### Commands

```bash
doit sync status              # View cleanup stats
doit sync cleanup             # Run cleanup now
doit sync cleanup --dry-run   # Preview without deleting
doit sync cleanup --aggressive # Nuclear option (asks for confirmation)
```

---

## 🗄️ Technical Stack

**Storage:** SQLite+WAL (modernc.org/sqlite - pure Go, no CGo)
- Location: `~/.local/share/doit/doit.db` (Linux/Mac)
- Tables: todos, operations, peers, sync_state, config, streaks
- Why: ACID, crash-resistant, battle-tested, single file

**UI/TUI:**
- Bubbletea (inline interactive mode)
- Lipgloss (styling)
- Cobra (CLI framework)

**Sync (Phase 1+):**
- CRDT (conflict resolution)
- mDNS (LAN discovery)
- STUN/WebRTC (internet sync)

---

## ✅ Phase 0 Complete - Verification

All features implemented and tested:
- ✓ Works in pipes and without TTY: `doit list | grep milk`
- ✓ Interactive Bubbletea forms with validation
- ✓ Pagination and retention configurable
- ✓ Separate pages for completed tasks (CLI and TUI)
- ✓ Index-based (1,2,3) + UUID compatibility
- ✓ Multiple ID operations: `complete/delete 1 2 3`
- ✓ Auto-cleanup on startup and after completion
- ✓ List shows truncated notes and deadlines
- ✓ `doit note <id>` command for full notes
- ✓ Colored output: green (success), orange (warnings), red (errors)
- ✓ No TTY vs non-TTY differences

---

## ✅ Phase 1 Complete - CRDT Foundation

<success_criteria>
All Phase 1 success criteria met:
- ✓ Operations are commutative, idempotent, associative
- ✓ Concurrent edits converge to same state
- ✓ Operation log correctly rebuilds state
- ✓ All Phase 1 tasks in TODO.md marked [x]
- ✓ All CRDT tests passing (11/11)
- ✓ Timestamp consistency verified (nanosecond precision)
- ✓ Data retention prevents unbounded growth
</success_criteria>

**Implemented Features:**
- Operation types: CREATE, UPDATE, COMPLETE, DELETE
- LWW conflict resolution (timestamp + device_id tiebreaker)
- Tombstone support (soft delete with `deleted=1`)
- Device identity (UUID stored in config)
- Transactional integrity (operation saved first, then state)
- Data retention (auto-cleanup of old operations and tombstones)

**Key Files:**
- `internal/sync/operation.go` - Operation struct, Apply() logic with LWW
- `internal/sync/crdt.go` - Merge(), RebuildState(), comparison logic
- `internal/sync/device.go` - GetDeviceID(), GetDeviceName(), config helpers
- `internal/storage/storage.go` - Hooked SaveTodo/UpdateTodo/CompleteTodo/DeleteTodo to create operations
- `internal/storage/cleanup.go` - CleanupSyncData(), retention policy enforcement
- `cmd/sync.go` - CLI commands for status and cleanup

**Architecture:** See [SYNC_ARCHITECTURE.md](SYNC_ARCHITECTURE.md) and [PHASE1_CRDT_GUIDE.md](PHASE1_CRDT_GUIDE.md)

---

## 🚀 Next Steps: Phase 2 - Local Network Sync (mDNS)

<task_definition>
**Objective**: Implement automatic peer discovery and synchronization over local network (LAN/WiFi)

**Core Goal**: Two devices on same WiFi automatically discover and sync within 10 seconds, zero configuration

**Key Components**:
1. **mDNS Discovery** - Broadcast/discover `_doit._tcp` service on LAN
2. **Peer Management** - Track discovered devices (status, last_seen, sync_state)
3. **HTTP Sync Server** - Endpoints: GET/POST /sync/operations, GET /sync/state
4. **Sync Protocol** - Pull/push operations with exponential backoff retry
5. **Sync Engine** - Background loop: discover (30s) + sync (10s)
6. **CLI Integration** - Commands: init, start, stop, disable, devices
</task_definition>

**READ FIRST:** [PHASE2_MDNS_GUIDE.md](PHASE2_MDNS_GUIDE.md)
- Complete API documentation for hashicorp/mdns (ServiceEntry fields, Config options, etc.)
- HTTP server/client best practices with production-ready code examples
- Peer management patterns with complete state machine (discovered → connected → syncing → disconnected/failed)
- Sync protocol flow with detailed request/response examples
- 7 common pitfalls with wrong/correct code comparisons
- Testing strategy with test skeletons

**Key Sections Referenced:**
- Decision 6: Authentication (shared secret pattern)
- Decision 8: Database Schema (peers + sync_state tables)
- Pattern 4: Sync Protocol Implementation (complete flow)
- Pitfall 3: Avoid Sync Loops (operation ID tracking)

**Implementation Tasks:** See [TODO.md](TODO.md) - Phase 2 section (6 subsections, ~40 tasks)

**Implementation Order:** 2.1 → 2.2 → 2.3 → 2.4 → 2.5 → 2.6 (follow sequentially)

**Prerequisites Met:**
- ✓ Operation log working (Phase 1)
- ✓ CRDT merge logic verified (Phase 1)
- ✓ Device identity established (Phase 1)
- ✓ Data retention prevents unbounded growth (Phase 1.7)
- ✓ Timestamp consistency (nanosecond precision)
- ✓ All tests passing with race detector

<critical_implementation_notes>
**MUST DO:**
- Include device_id in mDNS TXT records for peer identification
- Filter out self-discovery (skip our own device_id)
- Use context.WithTimeout for all HTTP requests (30s max)
- Auth: Shared secret in X-Doit-Secret header (see PHASE2_MDNS_GUIDE.md "Decision 6: Authentication")
- Wrap all endpoints with auth middleware (except GET /sync/state which is used for handshake)
- Use sync.RWMutex for concurrent peer map access
- Transaction-wrap all database operations
- Never block main goroutine (run server/discovery in goroutines)
- Implement exponential backoff retry: 1s, 2s, 4s, 8s, 16s, 32s, 60s (max), 2min total duration
- One peer failure must not stop syncing with others

**MUST AVOID:**
- Sync loops - track last_operation_id per peer (see PHASE2_MDNS_GUIDE.md "Pitfall 3")
- Hanging requests - always use context.WithTimeout (30s max)
- Channel leaks - always close(entriesCh) after mdns.Lookup
- Sync storms - minimum 10s interval for sync loop
- Race conditions - use mutexes for all shared state (peers map, running flag)

**Operation Tracking:**
- Use GetOperations(since_op_id) for sync protocol (primary method)
- Use GetOperationsSince(timestamp) for cleanup/recovery only
</critical_implementation_notes>

---

## 📚 Key Dependencies

```bash
# Current (Phase 0-1)
go get modernc.org/sqlite                    # Storage (Phase 0)
go get github.com/charmbracelet/bubbletea    # TUI (Phase 0)
go get github.com/charmbracelet/lipgloss     # Styling (Phase 0)
go get github.com/spf13/cobra                # CLI (Phase 0)
go get github.com/google/uuid                # Device IDs (Phase 1)

# Phase 2 (LAN Sync)
go get github.com/hashicorp/mdns@v1.0.6      # mDNS discovery

# Phase 3 (Internet Sync)
go get github.com/pion/webrtc/v3             # WebRTC P2P
go get github.com/pion/stun                  # NAT traversal
```

---

**Last updated:** 2025-11-26
