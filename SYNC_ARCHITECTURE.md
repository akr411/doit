# P2P Sync Architecture

Distributed todo sync with zero servers. Uses CRDT + mDNS + STUN.

---

## The Problem

```
You have:              You want:
Desktop (home)         Add/edit anywhere
Laptop (office)   →    Sync everywhere
Phone (travel)         No cloud/servers
```

**Challenges:**

- Different networks (can't always use LAN discovery)
- NAT/firewalls (can't directly connect)
- Concurrent edits (conflicts)
- Offline edits (queue until sync)

---

## The Solution Stack

```
┌─────────────────────────────────────────┐
│  CRDT (Conflict-free Replicated Data)   │ ← Handles conflicts
├─────────────────────────────────────────┤
│  Discovery: mDNS (LAN) + Pairing (WAN)  │ ← Finds devices
├─────────────────────────────────────────┤
│  NAT Traversal: STUN + WebRTC           │ ← Connects through firewalls
├─────────────────────────────────────────┤
│  Transport: P2P (HTTP/WebRTC)           │ ← Sends data
└─────────────────────────────────────────┘
```

---

## 1. CRDT: Conflict-Free Replicated Data Types

**Problem:** Two devices edit same todo offline. Who wins?

### Without CRDT (Bad):

```
Device A: title = "Buy milk and eggs"
Device B: title = "Buy milk and bread"

Sync → ??? Which one? Data loss!
```

### With CRDT (Good):

```
Device A: Operation{type: update, id: "abc", title: "Buy milk and eggs", ts: 100, device: "A"}
Device B: Operation{type: update, id: "abc", title: "Buy milk and bread", ts: 101, device: "B"}

Merge → ts: 101 wins → "Buy milk and bread" ✓
```

### How It Works:

**Operation Log** (append-only):

```
┌──────┬────────┬─────────┬──────────────────────┬─────┬────────┐
│  ID  │  Type  │ TodoID  │        Data          │ TS  │ Device │
├──────┼────────┼─────────┼──────────────────────┼─────┼────────┤
│ op1  │ create │ todo-1  │ {title: "Buy milk"}  │ 100 │   A    │
│ op2  │ update │ todo-1  │ {done: true}         │ 102 │   B    │
│ op3  │ create │ todo-2  │ {title: "Code"}      │ 105 │   A    │
│ op4  │ delete │ todo-1  │ null                 │ 110 │   B    │
└──────┴────────┴─────────┴──────────────────────┴─────┴────────┘
```

**Merge Algorithm:**

```go
func Merge(local, remote []Operation) {
    for _, op := range remote {
        if !hasOperation(op.ID) {
            apply(op)  // New operation
        }
    }
    // Rebuild state from operations sorted by timestamp
    rebuildState()
}
```

**Key Properties:**

- **Commutative:** A+B = B+A (order doesn't matter)
- **Idempotent:** Apply operation twice = apply once
- **Convergent:** All devices converge to same state

### LWW (Last-Write-Wins):

```
If ts1 > ts2 → op1 wins
If ts1 == ts2 → compare device IDs (deterministic)
```

---

## 2. mDNS: Multicast DNS

**Problem:** How do devices on same WiFi find each other?

### Traditional (Needs server):

```
Device A → Server: "I'm here!"
Device B → Server: "Who's online?"
Server → Device B: "Device A is here"
```

### mDNS (No server):

```
Device A → Multicast 224.0.0.251: "I'm doit-app at 192.168.1.10:49152"
Device B hears broadcast
Device B → connects to 192.168.1.10:49152
```

### How It Works:

**1. Announce Service:**

```go
// Device A broadcasts on port 5353
service := "_doit._tcp.local"
mdns.Announce(service, 8888)

// Broadcast packet:
{
    name: "desktop-abc._doit._tcp.local",
    port: 8888,
    addr: "192.168.1.10"
}
```

**2. Discover Services:**

```go
// Device B listens for "_doit._tcp"
mdns.Lookup("_doit._tcp", func(entry) {
    // entry = {name: "desktop-abc", addr: "192.168.1.10", port: 8888}
    connect(entry.addr, entry.port)
})
```

**Network Diagram:**

```
Router (192.168.1.1)
    │
    ├─── Desktop A (192.168.1.10)
    │    └─ Broadcasts: "I'm doit-app at :49152"
    │
    ├─── Laptop B (192.168.1.15)
    │    └─ Hears broadcast → connects to .10:49152
    │
    └─── Phone C (192.168.1.20)
         └─ Hears broadcast → connects to .10:49152

All devices discover each other automatically!
```

**Limitations:**

- ✓ Same network only (LAN/WiFi)
- ✗ Doesn't work across internet
- ✓ Zero config, instant discovery
- ✓ No servers needed

---

## 3. STUN: Session Traversal Utilities for NAT

**Problem:** Devices behind routers/firewalls can't connect directly over internet.

### NAT Problem:

```
Your device's view:
- Local IP: 192.168.1.10

Outside world's view:
- Public IP: 73.45.198.123
- Port: 54382 (random)

How does Device B know your public address?
```

### STUN Solution:

```
Device A → STUN server (stun.l.google.com:19302): "What's my public IP?"
STUN → Device A: "You're 73.45.198.123:54382"

Now Device A knows its public address!
```

### NAT Traversal Flow:

```
Step 1: Both devices query STUN
┌──────────┐                          ┌──────────┐
│ Device A │                          │ Device B │
│ (Home)   │                          │ (Office) │
└────┬─────┘                          └────┬─────┘
     │                                      │
     │ "What's my IP?"                      │ "What's my IP?"
     ├──────────►┌─────────────┐◄──────────┤
     │           │ STUN Server │            │
     │           │  (public)   │            │
     │◄──────────┤             ├───────────►│
     │ "73.45.   └─────────────┘   "52.18. │
     │  .123"                        .45"   │
     │                                      │

Step 2: Exchange addresses (via pairing code)
     │                                      │
     │  User: doit sync pair CODE           │
     ├─────────────────────────────────────►│
     │  "I'm at 73.45.198.123:54382"        │
     │                                      │

Step 3: P2P connection
     │                                      │
     │◄════════════════════════════════════►│
     │         Direct P2P sync              │
```

### Types of NAT:

**Full Cone (Easy):**

```
Router opens port 54382
Anyone can connect to Public:54382 → Local:49152
✓ Direct connection works
```

**Symmetric (Hard):**

```
Router opens different port per destination
Peer A sees you at :54382
Peer B sees you at :54383
✗ Needs TURN relay server (we'll handle this later)
```

### STUN Servers (Free & Public):

- `stun.l.google.com:19302`
- `stun.cloudflare.com:3478`
- `stun.stunprotocol.org:3478`

**What they see:** Only your public IP (no data)
**Cost:** Free
**Privacy:** High (no data passes through)

---

## 4. Pairing Flow

**Problem:** How do devices find each other on internet without server?

### Solution: Pairing Codes (One-time)

```
Device A                        Device B
────────                        ────────

1. Generate code
   doit sync init
   Code: TIGER-MOON-4782
   (encodes IP + port + pubkey)

                                2. Enter code
                                   doit sync pair TIGER-MOON-4782
                                   (decodes → gets A's address)

3. Store B's info                3. Store A's info
   PairedDevices[B] = ...          PairedDevices[A] = ...

4. Future: automatic sync        4. Future: automatic sync
```

### Pairing Code Format:

```go
type PairingData struct {
    DeviceID   string
    PublicKey  []byte
    Addresses  []string  // Multiple to try: local, public, etc.
    Timestamp  int64
}

// Encode → "TIGER-MOON-4782"
code := encodePairingCode(data)

// Decode → struct
data := decodePairingCode("TIGER-MOON-4782")
```

**After pairing:** Devices remember each other forever (stored in BBolt)

---

## 5. Complete Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                       Device A (Home)                        │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────┐       │
│  │    BBolt    │  │  Operations  │  │   Paired     │       │
│  │  (todos)    │  │     Log      │  │   Devices    │       │
│  └──────┬──────┘  └──────┬───────┘  └──────┬───────┘       │
│         │                │                  │               │
│         └────────────────┴──────────────────┘               │
│                          │                                  │
│                ┌─────────▼──────────┐                       │
│                │   Sync Engine      │                       │
│                │  - CRDT merge      │                       │
│                │  - Conflict resolve│                       │
│                └─────────┬──────────┘                       │
│                          │                                  │
│         ┌────────────────┼────────────────┐                │
│         │                │                │                │
│    ┌────▼───┐      ┌────▼────┐     ┌────▼────┐           │
│    │ mDNS   │      │ Direct  │     │  STUN   │           │
│    │Discover│      │ Connect │     │ WebRTC  │           │
│    └────┬───┘      └────┬────┘     └────┬────┘           │
│         │               │               │                 │
└─────────┼───────────────┼───────────────┼─────────────────┘
          │               │               │
          │               │               │
     ┌────▼───────────────▼───────────────▼────┐
     │       Network (LAN or Internet)          │
     └────┬───────────────┬───────────────┬────┘
          │               │               │
┌─────────┼───────────────┼───────────────┼─────────────────┐
│         │               │               │                 │
│    ┌────▼───┐      ┌────▼────┐     ┌────▼────┐           │
│    │ mDNS   │      │ Direct  │     │  STUN   │           │
│    │Discover│      │ Connect │     │ WebRTC  │           │
│    └────┬───┘      └────┬────┘     └────┬────┘           │
│         │               │               │                │
│         └────────────────┼────────────────┘                │
│                          │                                  │
│                ┌─────────▼──────────┐                       │
│                │   Sync Engine      │                       │
│                └─────────┬──────────┘                       │
│                          │                                  │
│         ┌────────────────┴──────────────────┐               │
│         │                │                  │               │
│  ┌──────▼──────┐  ┌──────▼───────┐  ┌──────▼───────┐       │
│  │    BBolt    │  │  Operations  │  │   Paired     │       │
│  │  (todos)    │  │     Log      │  │   Devices    │       │
│  └─────────────┘  └──────────────┘  └──────────────┘       │
│                                                               │
├─────────────────────────────────────────────────────────────┤
│                      Device B (Office)                       │
└─────────────────────────────────────────────────────────────┘
```

---

## 6. Sync Protocol

**Simple HTTP-based exchange:**

### Initial Sync:

```
Device B → Device A: GET /sync/state
Device A → Device B: {lastOpID: "op-1234", deviceID: "A"}

Device B → Device A: GET /sync/operations?since=op-1000
Device A → Device B: [op-1001, op-1002, ..., op-1234]

Device B applies operations → state synced!
```

### Continuous Sync:

```
Every 10 seconds:
- Check for new operations
- Send your new operations
- Merge and apply

OR

WebSocket for real-time:
Device A: create todo → immediately send operation → Device B applies
```

### API Endpoints:

```
GET  /sync/operations?since=<opID>  → Get ops after opID
POST /sync/operations                → Send operations
GET  /sync/state                     → Get current state hash
POST /sync/pair                      → Exchange pairing info
```

---

## 7. Connection Strategy

**Try everything in parallel, use first success:**

```go
func ConnectToDevice(device PairedDevice) Connection {
    results := make(chan Connection, 3)

    // Launch all in parallel
    go func() {
        if conn := tryMDNS(device); conn != nil {
            results <- conn
        }
    }()

    go func() {
        if conn := tryDirect(device.LastAddr); conn != nil {
            results <- conn
        }
    }()

    go func() {
        if conn := trySTUN(device); conn != nil {
            results <- conn
        }
    }()

    // First one to succeed wins
    select {
    case conn := <-results:
        return conn
    case <-time.After(5 * time.Second):
        return nil
    }
}
```

**Priority (speed):**

1. mDNS (same network) → ~100ms
2. Direct (cached address) → ~500ms
3. STUN (NAT traversal) → ~2s

---

## 8. Data Flow Example

**Scenario:** Edit todo on Device A while Device B offline.

```
Time  Device A (online)              Device B (offline)
────  ──────────────────              ──────────────────

T0    User: mark "Buy milk" done
      ├─ Create operation op-500
      ├─ Apply to local BBolt
      ├─ Try sync with B (fails)
      └─ Queue operation

T1    User: add "Walk dog"
      ├─ Create operation op-501
      ├─ Apply to local BBolt
      └─ Queue operation

T2                                    Device B comes online
                                      ├─ mDNS discovers A
                                      └─ Connects to A

T3    ◄─── B: "Give me ops since op-450"

T4    ───► A: [op-500, op-501]

T5                                    B receives operations
                                      ├─ Apply op-500 (mark done)
                                      ├─ Apply op-501 (add todo)
                                      └─ Rebuild state

T6                                    ✓ Device B now in sync!
```

**With conflict:**

```
Device A (offline)                    Device B (offline)
──────────────────                    ──────────────────

Edit todo-1: "Buy milk and eggs"     Edit todo-1: "Buy milk and bread"
op-500, ts: 100                       op-600, ts: 105

[Both come online]

A → B: send op-500                    B → A: send op-600
B receives op-500                     A receives op-600

Merge:                                Merge:
- op-500 (ts: 100)                    - op-500 (ts: 100)
- op-600 (ts: 105)                    - op-600 (ts: 105)
→ ts: 105 wins                        → ts: 105 wins
→ "Buy milk and bread"                → "Buy milk and bread"

✓ Both converge to same state!
```

---

## 9. User Journey

```bash
# Day 1: Setup on desktop
desktop$ doit sync init
> Sync enabled!
> Pairing code: TIGER-MOON-4782
> Active on local network.

desktop$ doit add "Buy milk"
> Added todo

# Day 1: Setup on laptop
laptop$ doit sync pair TIGER-MOON-4782
> Searching...
> Found device: desktop (192.168.1.10)
> Syncing...
> ✓ 1 todo synced

laptop$ doit list
> 1. Buy milk

# Day 2: Edit on laptop (same network)
laptop$ doit add "Walk dog"
> Added todo
> Synced with desktop [auto]

# Check desktop
desktop$ doit list
> 1. Buy milk
> 2. Walk dog ← automatically synced!

# Day 3: Laptop at coffee shop (different network)
laptop$ doit add "Call mom"
> Added todo
> Syncing with desktop... ✓ [via internet]

# Magic: Works anywhere!
```

---

## 10. Code Structure

```
internal/sync/
├── crdt.go              - Operation types, merge logic
│   ├── type Operation
│   ├── func Apply(op Operation)
│   ├── func Merge(local, remote []Operation)
│   └── func Resolve(conflict) Operation
│
├── storage.go           - Operation log in BBolt
│   ├── func SaveOperation(op)
│   ├── func GetOperations(since string) []Operation
│   └── func GetState() StateHash
│
├── discovery.go         - Find devices
│   ├── mDNS: func DiscoverLocal() []Device
│   ├── func GeneratePairingCode() string
│   └── func DecodePairingCode(code) Device
│
├── transport.go         - Connect to devices
│   ├── func TryMDNS(device) Connection
│   ├── func TryDirect(addr) Connection
│   └── func TrySTUN(device) Connection
│
├── protocol.go          - Sync protocol
│   ├── func PullOperations(device, since) []Operation
│   ├── func PushOperations(device, ops)
│   └── func InitialSync(device)
│
├── server.go            - HTTP server for sync
│   ├── GET  /sync/operations
│   ├── POST /sync/operations
│   └── POST /sync/pair
│
└── engine.go            - Main sync loop
    ├── func Start() - Background sync
    ├── func SyncWithDevice(device)
    └── func SyncAll()
```

---

## 11. Implementation Phases

**Phase 1: CRDT Foundation** (~2 days)

- [ ] Operation types
- [ ] Operation log in BBolt
- [ ] Merge algorithm
- [ ] LWW conflict resolution
- [ ] Apply operations to state

**Phase 2: Local Sync** (~2 days)

- [ ] mDNS discovery
- [ ] HTTP sync server
- [ ] Sync protocol (pull/push)
- [ ] CLI: `doit sync status`

**Phase 3: Internet Sync** (~2 days)

- [ ] Pairing code system
- [ ] STUN integration
- [ ] WebRTC connections
- [ ] Multi-method connection strategy

**Phase 4: Polish** (~1 day)

- [ ] Background sync daemon
- [ ] Sync status in UI
- [ ] Error handling
- [ ] Tests

**Total: ~1 week**

---

## 12. Libraries Needed

```go
require (
    // Already have
    go.etcd.io/bbolt v1.3.10

    // New (all compiled into binary)
    github.com/hashicorp/mdns v1.0.5              // mDNS discovery
    github.com/pion/webrtc/v3 v3.2.40             // WebRTC for NAT
    github.com/pion/stun v0.6.1                   // STUN client
    github.com/google/uuid v1.6.0                 // Operation IDs
)
```

**User installs:** Nothing (all compiled in)

---

## 13. Key Insights

**Why CRDT?**

- Makes conflicts impossible (mathematically!)
- Every device has full autonomy
- Offline-first by design

**Why mDNS?**

- Zero config for 80% of use cases (home/office)
- Instant discovery
- No servers

**Why STUN?**

- Connects through NATs/firewalls
- Free public servers
- Only sees IPs (privacy)

**Why P2P?**

- No server costs
- No single point of failure
- User owns their data
- Works offline

---

## 14. Trade-offs

**Pros:**

- ✓ Zero infrastructure costs
- ✓ Complete data ownership
- ✓ Works offline
- ✓ Fast (P2P)
- ✓ Private (encrypted)

**Cons:**

- ✗ Devices must be online simultaneously (at least once)
- ✗ No sync if all devices offline
- ✗ More complex than client-server

**Alternative for always-available sync:**

- Add optional relay server (user can self-host)
- Or offer hosted relay ($5/mo)

---

## Further Reading

**CRDT:**

- https://crdt.tech/
- https://github.com/ljwagerfield/crdt (beginner tutorial)

**mDNS:**

- RFC 6762: https://tools.ietf.org/html/rfc6762
- https://en.wikipedia.org/wiki/Multicast_DNS

**STUN/WebRTC:**

- RFC 5389: https://tools.ietf.org/html/rfc5389
- https://webrtc.org/getting-started/peer-connections

**Distributed Systems:**

- "Designing Data-Intensive Applications" by Martin Kleppmann
- https://jepsen.io/ (consistency testing)
