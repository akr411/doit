# Phase 1: CRDT Foundation - Implementation Guide

**Purpose:** This document contains research, decisions, and implementation details for Phase 1 CRDT implementation.

**Quick Links:**
- [TODO.md](TODO.md) - Phase 1 implementation checklist (mark tasks as you complete)
- [SYNC_ARCHITECTURE.md](SYNC_ARCHITECTURE.md) - High-level architecture and concepts
- [CLAUDE.md](CLAUDE.md) - Main project instructions

**Last updated:** 2025-11-26

---

## 🎯 Goal

Build a conflict-free data sync foundation using operation-based CRDT with Last-Write-Wins (LWW) resolution.

**Success:** Operations are commutative, idempotent, associative. Concurrent edits converge to same state.

---

## 📚 Research Summary

### What is a CRDT?

[Conflict-free Replicated Data Types](https://crdt.tech/) are data structures that:
- Replicate across multiple devices without coordination
- Automatically resolve conflicts
- Guarantee eventual consistency (all replicas converge to same state)

### Operation-Based vs State-Based

**Operation-based CRDTs** ([source](https://www.bartoszsypytkowski.com/operation-based-crdts-protocol/)):
- Send operations (e.g., "mark todo-1 complete") between devices
- Requires Reliable Causal Broadcast (operations not lost, delivered in causal order)
- Smaller messages than state-based
- What we're implementing ✓

**State-based CRDTs:**
- Send entire state, merge states
- No delivery guarantees needed
- Larger messages
- Not using for this project

### Last-Write-Wins (LWW)

[LWW resolution](https://en.wikipedia.org/wiki/Conflict-free_replicated_data_type) picks the latest update based on timestamps:
- If timestamp1 > timestamp2 → operation1 wins
- If timestamps equal → use device_id lexicographically (deterministic tiebreaker)

**Simple and effective for todo apps** where losing some concurrent edits is acceptable.

---

## 🔑 Key Decisions

### 1. Ordering: Timestamp + Device ID (NO Vector Clocks)

**Decision:** Use Unix nanosecond timestamp + device_id for ordering

**Why:**
- **Simpler**: O(1) space per operation vs O(n) for vector clocks
- **Sufficient**: LWW doesn't need causality detection (vector clocks do)
- **Performant**: Small operations, fast comparisons
- **Good enough**: [Research shows](https://cs.stackexchange.com/questions/101496/difference-between-lamport-timestamps-and-vector-clocks) Lamport-style timestamps work for LWW CRDTs

**Tradeoff:**
- ✗ Can't detect concurrent operations (but we don't need to - LWW chooses one)
- ✓ Much simpler implementation
- ✓ Smaller operation size

### 2. Operation Data: Full Todo Snapshot

**Decision:** Store complete todo object in every operation

**Why:**
- **Simple to apply**: Just deserialize JSON and insert/update
- **Easy rebuild**: RebuildState() is trivial - apply operations in order
- **Tiny data**: Todos are ~200 bytes, not documents with thousands of lines
- **Atomic**: One operation = one complete state

**Tradeoff:**
- ✗ Slightly larger operations (~200 bytes vs ~50 for delta)
- ✓ Much simpler code
- ✓ No field-level merge complexity
- ✓ [Delta-CRDTs](https://www.sciencedirect.com/science/article/abs/pii/S0743731517302332) are for large objects; todos are already deltas

**Operation Data Format:**
```json
{
  "task": "Buy milk",
  "note": "Organic 2%",
  "deadline": 1234567890,
  "completed": false
}
```

### 3. COMPLETE as Separate Operation Type

**Decision:** COMPLETE is distinct from UPDATE

**Why:**
- Semantic clarity: User action is "complete task" not "update task"
- Streak tracking: Easy to filter COMPLETE operations
- Conflict resolution: COMPLETE vs UPDATE have different intents
- Better audit trail

**Types:**
- `CREATE` - New todo created
- `UPDATE` - Task/note/deadline changed
- `COMPLETE` - Completion status toggled
- `DELETE` - Todo deleted (tombstone)

### 4. Tombstones: Add 'deleted' Column

**Decision:** Add `deleted INTEGER DEFAULT 0` to todos table NOW (Phase 1)

**Why:**
- [Standard CRDT pattern](https://xi-editor.io/docs/crdt.html): Deletion marks, doesn't remove
- Sync safety: DELETE operations can sync without data loss
- Rebuild support: RebuildState() preserves deletions
- [Prevents resurrection](https://github.com/yorkie-team/yorkie/blob/main/design/garbage-collection.md): Deleted todos don't reappear

**Implementation:**
```sql
ALTER TABLE todos ADD COLUMN deleted INTEGER DEFAULT 0;

-- Queries
SELECT * FROM todos WHERE deleted=0;

-- Delete operation
UPDATE todos SET deleted=1, updated_at=? WHERE id=?;
```

**Future optimization** (Phase 4+): Garbage collect tombstones after all devices sync

### 5. RebuildState: Full Clear and Rebuild

**Decision:** DELETE all todos, then apply all operations in timestamp order

**Why:**
- **Safest**: Handles any corruption or inconsistency
- **Simple**: No state tracking needed
- **Correct**: Guaranteed to produce consistent state from operations
- Used for recovery, not normal operation

**Implementation:**
```go
func RebuildState(ops []*Operation) error {
    tx.Exec("DELETE FROM todos")

    // Sort by timestamp, then device_id
    sort.Slice(ops, func(i, j int) bool {
        if ops[i].Timestamp != ops[j].Timestamp {
            return ops[i].Timestamp < ops[j].Timestamp
        }
        return ops[i].DeviceID < ops[j].DeviceID
    })

    for _, op := range ops {
        Apply(op)
    }
}
```

### 6. GetOperations(since): Takes Operation ID

**Decision:** `since` parameter is an operation ID (e.g., "op-1234")

**Why:**
- Sync protocol: "Give me all operations after the last one I have"
- Simpler than timestamp (no clock skew issues)
- Operation IDs are UUIDs (unique, sortable by creation time via UUID v7)

**Also provide:** `GetOperationsSince(timestamp int64)` for timestamp-based queries

---

## 🏗️ Architecture Decisions

### Operation Structure

```go
type Operation struct {
    ID        string  // UUID v7 (time-sortable)
    Type      string  // "CREATE", "UPDATE", "COMPLETE", "DELETE"
    TodoID    string  // Which todo this operation affects
    Data      []byte  // Full todo JSON snapshot (or null for DELETE)
    Timestamp int64   // Unix nanoseconds (for LWW ordering)
    DeviceID  string  // Device UUID (for deterministic tiebreaker)
}
```

**No VectorClock needed** - timestamp + device_id is sufficient for LWW

### Operation Types and Data Content

| Type | When Created | Data Content | Effect on todos Table |
|------|-------------|--------------|----------------------|
| CREATE | `doit add` | Full todo JSON | INSERT new row |
| UPDATE | `doit edit` | Full todo JSON with changes | UPDATE row |
| COMPLETE | `doit complete` | Full todo JSON with completed toggled | UPDATE completed field |
| DELETE | `doit delete` | null or {} | UPDATE deleted=1 |

### Storage Schema Changes

**Add to todos table:**
```sql
ALTER TABLE todos ADD COLUMN deleted INTEGER DEFAULT 0;
CREATE INDEX idx_todos_deleted ON todos(deleted);
```

**New operations table:**
```sql
CREATE TABLE operations (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    todo_id TEXT NOT NULL,
    data TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    device_id TEXT NOT NULL,
    synced INTEGER DEFAULT 0
);

CREATE INDEX idx_ops_timestamp ON operations(timestamp);
CREATE INDEX idx_ops_synced ON operations(synced);
CREATE INDEX idx_ops_device ON operations(device_id);
CREATE INDEX idx_ops_todo ON operations(todo_id);
```

**Note:** No vector_clock column needed

### Transactional Integrity

Every storage operation must be atomic:

```go
func SaveTodo(todo *Todo) error {
    tx.Begin()

    // 1. Create operation FIRST (source of truth)
    op := Operation{
        ID: uuid.New(),
        Type: "CREATE",
        TodoID: todo.ID,
        Data: json.Marshal(todo),
        Timestamp: time.Now().UnixNano(),
        DeviceID: getDeviceID(),
    }
    tx.Exec("INSERT INTO operations ...")

    // 2. Then update state
    tx.Exec("INSERT INTO todos ...")

    tx.Commit()
}
```

**Critical:** Operation must be saved first. If state update fails, we can rebuild from operations.

---

## ⚠️ Critical Implementation Requirements

### 1. Idempotency

Applying same operation twice = applying once

```go
func Apply(op *Operation) error {
    // Check if already applied
    var exists bool
    db.QueryRow("SELECT EXISTS(SELECT 1 FROM operations WHERE id=?)", op.ID).Scan(&exists)
    if exists {
        return nil  // Already applied, skip
    }

    // Apply operation...
}
```

### 2. Commutativity

Order of applying concurrent operations doesn't matter (they have different timestamps)

**Guaranteed by:** LWW resolution always picks same winner regardless of application order

### 3. Deterministic Conflict Resolution

Same inputs always produce same output

```go
func Compare(op1, op2 *Operation) int {
    if op1.Timestamp != op2.Timestamp {
        return op1.Timestamp - op2.Timestamp  // Newer wins
    }
    return strings.Compare(op1.DeviceID, op2.DeviceID)  // Deterministic tiebreaker
}
```

### 4. Tombstone Handling

DELETE operations don't remove rows:

```go
// DELETE operation
func Apply(op *Operation) error {
    if op.Type == "DELETE" {
        db.Exec("UPDATE todos SET deleted=1, updated_at=? WHERE id=?",
                op.Timestamp, op.TodoID)
        return nil
    }
    // ...
}
```

**Important:** [Tombstones prevent resurrection](https://xi-editor.io/docs/crdt.html) - deleted items stay deleted even when syncing with device that created them

### 5. Sync-Disabled Mode

If user hasn't enabled sync, skip operation logging:

```go
func SaveTodo(todo *Todo) error {
    syncEnabled, _ := store.GetConfig("sync_enabled")

    tx.Begin()

    if syncEnabled == "true" {
        // Save operation
        tx.Exec("INSERT INTO operations ...")
    }

    // Always save todo
    tx.Exec("INSERT INTO todos ...")

    tx.Commit()
}
```

---

## 🔍 Edge Cases & Pitfalls

### 1. Clock Skew

**Problem:** Device A has clock set to 2099, Device B has 2025

**Solution:** Operations still work because:
- All devices apply operations in timestamp order
- Same order on all devices = same final state
- LWW means Device A's operations always "win" (but that's deterministic)

**Better solution (Phase 4):** Detect large clock skew, warn user

### 2. Duplicate Operations

**Problem:** Network retry sends same operation twice

**Solution:** Idempotency - check operation ID before applying

```go
if db.Exists("SELECT 1 FROM operations WHERE id=?", op.ID) {
    return nil  // Already have it
}
```

### 3. Out-of-Order Delivery

**Problem:** Receive op-3 before op-2

**Solution:** CRDT handles this! Just apply when received, RebuildState() will sort correctly

### 4. Concurrent Edits to Same Field

**Problem:** Both devices edit task title simultaneously

**Solution:** LWW - higher timestamp wins (or device_id if equal)

**Consequence:** One edit is lost (acceptable for todo app)

### 5. DELETE vs UPDATE Conflict

**Problem:** Device A deletes todo while Device B updates it

**Solution:** Whichever operation has higher timestamp wins

**Tradeoff:** If DELETE wins, UPDATE is lost. If UPDATE wins, todo is resurrected.

**Mitigation (Phase 4+):** Show conflict UI, let user decide

---

## 📋 Implementation Checklist

### Phase 1.1: Operation Types
- [ ] Create `internal/sync/operation.go`
- [ ] Define Operation struct (no VectorClock field)
- [ ] Implement Apply(op) for each type: CREATE, UPDATE, COMPLETE, DELETE
- [ ] Handle tombstones (DELETE sets deleted=1)

### Phase 1.2: Operation Storage
- [ ] Add `deleted` column to todos table (migration)
- [ ] Create operations table (no vector_clock column)
- [ ] Create indexes on timestamp, synced, device_id, todo_id
- [ ] Implement SaveOperation(op)
- [ ] Implement GetOperations(since string) - since is operation ID
- [ ] Implement GetOperationsSince(timestamp int64)
- [ ] Implement GetLastOperationID()
- [ ] Implement MarkOperationsSynced(ids []string)

### Phase 1.3: CRDT Merge Logic
- [ ] Create `internal/sync/crdt.go`
- [ ] Implement Merge(local, remote) - combine and dedupe by ID
- [ ] Implement LWW comparison (timestamp, then device_id)
- [ ] Implement RebuildState(ops) - DELETE FROM todos, then apply all
- [ ] Sort operations: by timestamp ASC, then device_id ASC

### Phase 1.4: Device Identity
- [ ] Create `internal/sync/device.go`
- [ ] Generate UUID on first run, store in config.device_id
- [ ] Implement GetDeviceID()
- [ ] Implement GetDeviceName() - hostname + OS
- [ ] Store sync config: sync_enabled (default: false), sync_port (default: 8888)

### Phase 1.5: Hook Storage → Operations
- [ ] Modify SaveTodo() to create CREATE operation
- [ ] Modify UpdateTodo() to create UPDATE operation
- [ ] Add CompleteTodo() wrapper to create COMPLETE operation
- [ ] Modify DeleteTodo() to create DELETE operation + set deleted=1
- [ ] Ensure operations created FIRST, then state changes
- [ ] Check sync_enabled before creating operations
- [ ] Use transactions for atomicity

---

## 🧪 Testing Requirements

### Unit Tests

Test each operation type:
```go
func TestCreateOperation(t *testing.T) {
    op := Operation{Type: "CREATE", Data: todoJSON, ...}
    Apply(op)
    // Verify todo exists in DB
}

func TestIdempotency(t *testing.T) {
    Apply(op)
    Apply(op)  // Apply twice
    // Verify only one row in DB
}
```

### CRDT Property Tests

**Commutative:**
```go
func TestCommutative(t *testing.T) {
    Apply(op1); Apply(op2)
    state1 := GetAllTodos()

    ClearDB()

    Apply(op2); Apply(op1)  // Reverse order
    state2 := GetAllTodos()

    assert.Equal(state1, state2)  // Same result
}
```

**Idempotent:**
```go
func TestIdempotent(t *testing.T) {
    Apply(op)
    state1 := GetAllTodos()

    Apply(op)  // Again
    state2 := GetAllTodos()

    assert.Equal(state1, state2)  // No change
}
```

**Convergent:**
```go
func TestConvergence(t *testing.T) {
    // Simulate two devices with different operation orders
    deviceA := []Operation{op1, op2, op3}
    deviceB := []Operation{op3, op1, op2}  // Different order

    ApplyAll(deviceA)
    stateA := GetAllTodos()

    ClearDB()

    ApplyAll(deviceB)
    stateB := GetAllTodos()

    assert.Equal(stateA, stateB)  // Converged
}
```

### Edge Case Tests

- [ ] Clock skew: Device A (ts: 999999999), Device B (ts: 1000)
- [ ] Duplicate operations: Apply same op_id twice
- [ ] Out-of-order: Receive op-3, op-1, op-2
- [ ] DELETE vs UPDATE conflict: Simultaneous delete and edit
- [ ] Empty operation log: RebuildState() on fresh DB

---

## 🚨 Common Pitfalls to Avoid

### 1. DON'T Remove Deleted Todos from Table

**Wrong:**
```go
db.Exec("DELETE FROM todos WHERE id=?", todoID)  // ✗ Data loss!
```

**Correct:**
```go
db.Exec("UPDATE todos SET deleted=1 WHERE id=?", todoID)  // ✓ Tombstone
```

**Why:** [Tombstones are required](https://xi-editor.io/docs/crdt.html) to prevent resurrection when syncing

### 2. DON'T Skip Operation on Sync Disabled

**Wrong:**
```go
if syncEnabled {
    saveOperation()
    saveTodo()
} else {
    saveTodo()  // ✗ Inconsistent state if sync enabled later!
}
```

**Correct:**
```go
tx.Begin()
if syncEnabled {
    saveOperation()  // Operation first
}
saveTodo()  // State always saved
tx.Commit()
```

**Why:** Operations are source of truth. State can be rebuilt from operations.

### 3. DON'T Use Wall Clock Timestamps Alone

**Wrong:**
```go
Timestamp: time.Now().Unix()  // ✗ Seconds granularity, collisions likely!
```

**Correct:**
```go
Timestamp: time.Now().UnixNano()  // ✓ Nanosecond precision
```

**Why:** Multiple operations in same second will have same timestamp, need nano precision

### 4. DON'T Apply Operations Without Deduplication

**Wrong:**
```go
for _, op := range remoteOps {
    Apply(op)  // ✗ May apply duplicates!
}
```

**Correct:**
```go
for _, op := range remoteOps {
    if !exists(op.ID) {
        Apply(op)  // ✓ Idempotent
    }
}
```

### 5. DON'T Forget Deterministic Tiebreaker

**Wrong:**
```go
if op1.Timestamp > op2.Timestamp {
    return op1  // ✗ What if timestamps equal?
}
return op2
```

**Correct:**
```go
if op1.Timestamp != op2.Timestamp {
    return op1.Timestamp > op2.Timestamp
}
return op1.DeviceID > op2.DeviceID  // ✓ Deterministic
```

---

## 📦 Data Flow

### Creating a Todo

```
User: doit add -t "Buy milk"
    ↓
SaveTodo()
    ↓
tx.Begin()
    ↓
1. INSERT INTO operations (
    id: uuid-1,
    type: "CREATE",
    todo_id: todo-1,
    data: '{"task":"Buy milk",...}',
    timestamp: 1234567890000000,
    device_id: device-A
   )
    ↓
2. INSERT INTO todos (
    id: todo-1,
    task: "Buy milk",
    ...
   )
    ↓
tx.Commit()
```

### Syncing (Future Phase 2)

```
Device A                    Device B
────────                    ────────

1. GetLastOperationID()
   → "op-100"
                            2. Send to B:
                               GET /sync/operations?since=op-100

                            3. B responds with:
                               [op-101, op-102, op-103]

4. Receive operations
   ↓
5. For each op:
   - Check if exists (by ID)
   - If not, Apply(op)
   ↓
6. State updated!
```

### Rebuilding from Corruption

```
Detected corruption
    ↓
RebuildState()
    ↓
1. ops = GetAllOperations()
2. Sort by timestamp ASC, device_id ASC
3. DELETE FROM todos
4. For each op in ops:
     Apply(op)
    ↓
5. State rebuilt from source of truth!
```

---

## 🎓 Key Learnings from Research

### Reliable Causal Broadcast

[Operation-based CRDTs require](https://www.bartoszsypytkowski.com/operation-based-crdts-protocol/):
- **Reliable delivery**: Operations not lost
- **Causal order**: If op1 caused op2, deliver op1 first

**Our approach (Phase 2):**
- Use HTTP with retry for reliability
- Track last synced operation ID per peer for ordering
- Operations have timestamps for global ordering

### Eventual Consistency

[All CRDTs guarantee](https://en.wikipedia.org/wiki/Conflict-free_replicated_data_type) **strong eventual consistency**:
- All replicas that have received same operations have same state
- Eventually all replicas receive all operations
- Eventually all replicas converge

**Our guarantee:** If all devices sync, they'll have identical todo lists

### Garbage Collection

[Tombstones can be pruned](https://github.com/yorkie-team/yorkie/blob/main/design/garbage-collection.md) when:
- All devices have synced the DELETE operation
- No device will send operations referencing deleted todo

**Our approach:**
- Phase 1-3: Keep all tombstones (safe)
- Phase 4+: Implement retention policy (e.g., delete tombstones older than 30 days if all devices synced)

---

## 🔧 Implementation Notes

### UUID v7 for Operation IDs

Use UUID v7 (time-ordered) instead of v4 (random):
- Sortable by creation time
- Better database index performance
- Preserves some causality information

```go
import "github.com/google/uuid"

opID := uuid.NewV7().String()  // Time-ordered
```

### JSON Serialization

Store todos as JSON in operations:
```go
data, _ := json.Marshal(todo)
op.Data = data

// Later:
var todo Todo
json.Unmarshal(op.Data, &todo)
```

### Query Patterns

**Get active todos:**
```sql
SELECT * FROM todos WHERE deleted=0 ORDER BY created_at DESC
```

**Get all operations for rebuild:**
```sql
SELECT * FROM operations ORDER BY timestamp ASC, device_id ASC
```

**Get operations after specific ID:**
```sql
SELECT * FROM operations
WHERE timestamp > (SELECT timestamp FROM operations WHERE id=?)
ORDER BY timestamp ASC
```

---

## 📖 References & Sources

**CRDT Fundamentals:**
- [CRDT.tech - About CRDTs](https://crdt.tech/)
- [Wikipedia: Conflict-free Replicated Data Types](https://en.wikipedia.org/wiki/Conflict-free_replicated_data_type)
- [CRDT Survey - Algorithmic Techniques](https://mattweidner.com/2023/09/26/crdt-survey-3.html)

**Operation-Based CRDTs:**
- [Operation-based CRDTs: Protocol](https://www.bartoszsypytkowski.com/operation-based-crdts-protocol/)
- [Operation-based CRDTs: Registers and Sets](https://www.bartoszsypytkowski.com/operation-based-crdts-registers-and-sets/)
- [Pure Operation-based CRDTs](https://www.bartoszsypytkowski.com/pure-operation-based-crdts/)

**Clocks and Ordering:**
- [Distributed Clocks and CRDTs](https://adamwulf.me/2021/05/distributed-clocks-and-crdts/)
- [Lamport vs Vector Clocks](https://cs.stackexchange.com/questions/101496/difference-between-lamport-timestamps-and-vector-clocks)
- [Clocks and Causality - Ordering Events](https://www.exhypothesi.com/clocks-and-causality/)

**Tombstones and Deletion:**
- [Xi Editor: CRDT Approach](https://xi-editor.io/docs/crdt.html)
- [Yorkie: Garbage Collection Design](https://github.com/yorkie-team/yorkie/blob/main/design/garbage-collection.md)
- [Data Laced with History: Causal Trees](http://archagon.net/blog/2018/03/24/data-laced-with-history/)

**Delta-CRDTs:**
- [Delta State Replicated Data Types](https://www.sciencedirect.com/science/article/abs/pii/S0743731517302332)
- [Efficient Synchronization of State-based CRDTs](https://vitorenes.org/post/2019/04/efficient-sync/)

**Go Implementations:**
- [Building a Collaborative Text Editor in Go](https://databases.systems/posts/collaborative-editor)
- [GitHub: neurodrone/crdt - Go CRDT Implementation](https://github.com/neurodrone/crdt)
- [GitHub: go-pluto CRDT Package](https://pkg.go.dev/github.com/go-pluto/pluto/crdt)

---

## 🚀 Ready to Implement

**Next Steps:**
1. Review [TODO.md](TODO.md) Phase 1 checklist (sections 1.1-1.6)
2. Follow implementation order: 1.1 → 1.2 → 1.3 → 1.4 → 1.5 → 1.6
3. Mark tasks [x] as you complete them
4. Run tests from section 1.6 before marking Phase 1 complete
5. Refer back to this guide for decisions, patterns, and pitfall avoidance

**For architecture context:** See [SYNC_ARCHITECTURE.md](SYNC_ARCHITECTURE.md) for diagrams and explanations
