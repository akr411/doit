# Phase 2: Local Network Sync (mDNS) - Implementation Guide

**Purpose:** Complete implementation guide for Phase 2 with research findings, decisions, and patterns.

**Quick Links:**
- [TODO.md](TODO.md) - Phase 2 implementation checklist
- [SYNC_ARCHITECTURE.md](SYNC_ARCHITECTURE.md) - High-level architecture
- [CLAUDE.md](CLAUDE.md) - Main project instructions

**Last updated:** 2025-11-26

---

## 🎯 Goal

Implement automatic peer discovery and synchronization over local network (LAN/WiFi) using mDNS.

**Success Criteria:**
- Devices on same network automatically discover each other
- Operations sync bidirectionally
- Handles network failures gracefully
- Background sync runs every 10 seconds
- Manual sync commands work

---

## 📚 Research Summary

### mDNS (Multicast DNS)

**Library:** [github.com/hashicorp/mdns](https://github.com/hashicorp/mdns) v1.0.6

**What it does:**
- Discovers services on local network without DNS server
- Broadcasts service announcements on multicast address 224.0.0.251:5353
- Other devices listen for these broadcasts
- Zero configuration needed

**Key API:**

```go
// Server: Announce service
service, _ := mdns.NewMDNSService(
    hostname,           // Instance name (e.g., "desktop-abc")
    "_doit._tcp",       // Service type
    "",                 // Domain (defaults to "local")
    "",                 // Host name (defaults to os.Hostname())
    8888,               // Port
    nil,                // IPs (defaults to all local IPs)
    []string{"v=1"},    // TXT records (metadata)
)
server, _ := mdns.NewServer(&mdns.Config{Zone: service})
defer server.Shutdown()

// Client: Discover services
entriesCh := make(chan *mdns.ServiceEntry, 10)
go func() {
    for entry := range entriesCh {
        // entry.Name, entry.Host, entry.AddrV4, entry.Port, entry.InfoFields
        fmt.Printf("Found: %s at %s:%d\n", entry.Name, entry.AddrV4, entry.Port)
    }
}()
mdns.Lookup("_doit._tcp", entriesCh)
close(entriesCh)
```

**ServiceEntry fields:**
- `Name` - Service instance name
- `Host` - DNS hostname
- `AddrV4` - IPv4 address
- `AddrV6` - IPv6 address
- `Port` - Service port
- `Info` - TXT record as string
- `InfoFields` - TXT records as []string

**Limitations:**
- Only works on local network (same WiFi/LAN)
- Many corporate networks block multicast
- Won't work in cloud environments

**Source:** [HashiCorp mDNS GitHub](https://github.com/hashicorp/mdns), [Go Package Docs](https://pkg.go.dev/github.com/hashicorp/mdns)

---

### HTTP Server Patterns

**Graceful Shutdown Best Practices:**

Based on [Go graceful shutdown patterns](https://dev.to/mokiat/proper-http-shutdown-in-go-3fji):

```go
func StartServer(port int) *http.Server {
    srv := &http.Server{
        Addr:         fmt.Sprintf(":%d", port),
        ReadTimeout:  15 * time.Second,
        WriteTimeout: 15 * time.Second,
        IdleTimeout:  60 * time.Second,
    }

    // Start in goroutine
    go func() {
        if err := srv.ListenAndServe(); err != http.ErrServerClosed {
            log.Printf("HTTP server error: %v", err)
        }
    }()

    return srv
}

func GracefulShutdown(srv *http.Server) {
    // Listen for signals
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    <-sigChan

    // Shutdown with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    if err := srv.Shutdown(ctx); err != nil {
        log.Printf("Server shutdown error: %v", err)
    }
}
```

**Key Points:**
- Set timeouts (Read, Write, Idle)
- Use `http.Server.Shutdown()` for graceful termination
- Timeout should be < 30 seconds (Kubernetes default grace period)
- Listen for SIGINT (Ctrl+C) and SIGTERM (kill)

**Sources:** [Proper HTTP Shutdown](https://dev.to/mokiat/proper-http-shutdown-in-go-3fji), [Signal Handling Tutorial](https://rafallorenz.com/go/handle-signals-to-graceful-shutdown-http-server/)

---

### HTTP Client Best Practices

**Production-Ready Configuration:**

Based on [Go HTTP client tuning guide](https://www.loginradius.com/blog/engineering/tune-the-go-http-client-for-high-performance):

```go
var syncClient = &http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:          100,
        MaxConnsPerHost:       20,
        MaxIdleConnsPerHost:   20,
        IdleConnTimeout:       90 * time.Second,
        TLSHandshakeTimeout:   10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        ResponseHeaderTimeout: 10 * time.Second,
    },
}
```

**Key Settings:**
- `Timeout`: Maximum time for entire request (30s recommended)
- `MaxIdleConns`: Total connection pool size
- `MaxConnsPerHost`: Per-host connection limit (default is only 2!)
- `IdleConnTimeout`: Keep connections alive (90s default)

**Why this matters:**
- Default `MaxIdleConnsPerHost=2` is too low for P2P
- Connection pooling reuses TCP connections
- Proper timeouts prevent hanging requests

**Sources:** [HTTP Client Performance](https://www.loginradius.com/blog/engineering/tune-the-go-http-client-for-high-performance), [Production Client Patterns](https://jsschools.com/golang/go-http-client-patterns-a-production-ready-implem/)

---

### Retry with Exponential Backoff

**Library:** [github.com/cenkalti/backoff/v4](https://github.com/cenkalti/backoff)

**Implementation:**

```go
import "github.com/cenkalti/backoff/v4"

func syncWithRetry(peer *Peer, operation func() error) error {
    b := backoff.NewExponentialBackOff()
    b.InitialInterval = 1 * time.Second
    b.MaxInterval = 60 * time.Second
    b.MaxElapsedTime = 2 * time.Minute  // Give up after 2 minutes
    b.Multiplier = 2.0                   // Double each time: 1s, 2s, 4s, 8s, 16s, 32s, 60s
    b.RandomizationFactor = 0.5          // Jitter to prevent thundering herd

    return backoff.Retry(operation, b)
}
```

**Our implementation** (matching TODO.md spec):
```
Retry intervals: 1s, 2s, 4s, 8s, 16s, 32s, 60s, 60s, 60s...
- Intervals are delays BETWEEN attempts (not cumulative)
- Max interval capped at 60s (won't go higher)
- Total retry duration: 2 minutes max
- Gives up after 2 minutes of retrying

Timeout per request: 30s (via context.WithTimeout)
- Each individual HTTP request times out after 30s
- Independent of retry logic

Example timeline:
- Attempt 1: immediate → fails
- Wait 1s
- Attempt 2: at 1s → fails
- Wait 2s
- Attempt 3: at 3s → fails
- Wait 4s
- Attempt 4: at 7s → fails
- ... continues until 2 minutes elapsed
```

**Why jitter?** Prevents all devices from retrying at exact same time (thundering herd problem).

**Sources:** [cenkalti/backoff](https://github.com/cenkalti/backoff), [Exponential Backoff in Go](https://medium.com/@nidhey60/code-smarter-exponential-backoff-in-go-made-easy-ba5224e18805)

---

### Sync Protocol Design

**Based on research** from [REST API sync design](https://stackoverflow.com/questions/56319379/rest-api-design-for-data-synchronization-service):

**Best practice:** Timestamp-based incremental sync with operation log

```
GET /sync/operations?since=<operation_id>
→ Returns all operations after the given ID
→ Client applies operations to local state
→ Efficient (only sends new data)
```

**Alternative considered:**
- Full state sync: Too large, wasteful
- Hash-based: Requires comparing all data
- **Chosen: Operation log** (matches CRDT perfectly!)

**Sources:** [REST Sync Design](https://stackoverflow.com/questions/56319379/rest-api-design-for-data-synchronization-service), [Timestamp-based Sync](https://stackoverflow.com/questions/43081770/rest-best-practice-for-sync-log-data-in-reverse-order)

---

## 🔑 Key Implementation Decisions

### Decision 1: Service Name

**Choice:** `_doit._tcp.local`

**Format:**
- `_doit` - Service name (our app)
- `_tcp` - Protocol (TCP not UDP)
- `.local` - mDNS domain (standard)

**Why:** Standard mDNS naming convention, automatically scoped to local network

---

### Decision 2: Port Selection

**Choice:** 8888 (configurable)

**Why:**
- Above 1024 (no root needed)
- Not a well-known port (avoid conflicts)
- Easy to remember
- Configurable via `sync_port` config

**Alternative considered:**
- Random ephemeral port: Harder for firewall rules
- **Chosen: Fixed default with override option**

---

### Decision 3: Peer Discovery Strategy

**Choice:** Continuous discovery with state tracking

**Pattern:**
```go
// Background goroutine
for {
    select {
    case <-ticker.C:  // Every 30 seconds
        DiscoverPeers() // Refresh peer list
        SyncWithPeers() // Sync with active peers
    case <-stopChan:
        return
    }
}
```

**Why:**
- Handles peers joining/leaving network
- Automatic reconnection if peer disappears
- No manual intervention needed

**Alternative considered:**
- One-time discovery: Misses new peers
- Event-driven only: Complex state management
- **Chosen: Polling + event-driven hybrid**

---

### Decision 4: Peer State Machine

**States:**
- `discovered` - Found via mDNS, not yet connected
- `connected` - HTTP connection established
- `syncing` - Currently transferring operations
- `disconnected` - Was connected, now unreachable
- `failed` - Connection attempts exhausted

**Transitions:**
```
discovered → connected (successful HTTP handshake)
connected → syncing (sync started)
syncing → connected (sync completed)
connected → disconnected (network error)
disconnected → connected (retry succeeded)
disconnected → failed (max retries exceeded)
```

---

### Decision 5: Sync Flow

**Initial Sync (when peer first discovered):**

```
1. GET /sync/state
   → {last_operation_id: "op-123", device_id: "abc", device_name: "desktop"}

2. GET /sync/operations?since=<our_last_op_id>
   → [op-124, op-125, ...] (operations we're missing)

3. Apply received operations (CRDT merge)

4. POST /sync/operations
   → Send our operations they're missing

5. Mark operations as synced
```

**Continuous Sync (every 10 seconds):**

```
For each connected peer:
  1. GET /sync/state (quick check)
  2. If they have new operations, pull them
  3. If we have new operations, push them
  4. Update last_sync_time
```

---

### Decision 6: Authentication

**Choice:** Shared secret in HTTP header

**Implementation:**
```go
// Generate on first sync init
secret := generateRandomBytes(32)
secretB64 := base64.StdEncoding.EncodeToString(secret)
store.SetConfig("shared_secret", secretB64)

// Include in all requests
req.Header.Set("X-Doit-Secret", secretB64)

// Validate on server
func validateSecret(r *http.Request) bool {
    provided := r.Header.Get("X-Doit-Secret")
    expected, _ := store.GetConfig("shared_secret")
    return provided == expected
}
```

**Why:**
- Simple to implement
- Prevents random devices from syncing
- Shared during pairing (Phase 3)

**Alternative considered:**
- TLS client certs: Too complex
- No auth: Security risk
- **Chosen: Shared secret** (balanced)

---

### Decision 7: Error Handling Strategy

**Principle:** Fail gracefully, continue with other peers

```go
func SyncWithPeer(peer *Peer) error {
    if err := pullOperations(peer); err != nil {
        log.Printf("Failed to pull from %s: %v", peer.Name, err)
        UpdatePeerStatus(peer.ID, "disconnected")
        return err  // Don't propagate - continue with next peer
    }

    if err := pushOperations(peer); err != nil {
        log.Printf("Failed to push to %s: %v", peer.Name, err)
        // Partial sync is OK - we pulled successfully
    }

    return nil
}
```

**Why:**
- One peer failure doesn't stop syncing with others
- Partial sync is better than no sync
- Retry will happen on next cycle

---

### Decision 8: Database Schema for Peers

**peers table:**
```sql
CREATE TABLE peers (
    id TEXT PRIMARY KEY,              -- Device ID (UUID)
    name TEXT NOT NULL,                -- Friendly name (e.g., "desktop-linux")
    address TEXT NOT NULL,             -- Single address "192.168.1.10:49152"
    last_seen INTEGER NOT NULL,        -- Last successful connection (Unix nano)
    status TEXT NOT NULL,              -- discovered|connected|syncing|disconnected|failed
    created_at INTEGER NOT NULL        -- When peer was added (Unix nano)
);

CREATE INDEX idx_peers_status ON peers(status);
CREATE INDEX idx_peers_last_seen ON peers(last_seen);
```

**Note:** Simplified from `addresses` (JSON array) to single `address`.
**Rationale:** IP addresses change via DHCP, so mDNS continuously rediscovers and updates the address. Simpler than managing JSON arrays.

**sync_state table:**
```sql
CREATE TABLE sync_state (
    peer_id TEXT PRIMARY KEY,
    last_operation_id TEXT,            -- Last op ID we received from them
    last_sync_time INTEGER,            -- Last successful sync (Unix nano)
    operations_sent INTEGER DEFAULT 0,
    operations_received INTEGER DEFAULT 0,
    FOREIGN KEY (peer_id) REFERENCES peers(id) ON DELETE CASCADE
);
```

**Why separate tables:**
- `peers` = static info (name, addresses)
- `sync_state` = dynamic sync tracking
- Easier to reset sync state without losing peer info

---

## 🏗️ Architecture Patterns

### Pattern 1: mDNS Discovery Loop

```go
type DiscoveryService struct {
    server    *mdns.Server
    entriesCh chan *mdns.ServiceEntry
    stopCh    chan struct{}
    store     *storage.Storage
}

func (d *DiscoveryService) Start() error {
    // 1. Start announcing our service
    if err := d.startServer(); err != nil {
        return err
    }

    // 2. Start discovery loop
    go d.discoveryLoop()

    return nil
}

func (d *DiscoveryService) discoveryLoop() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            d.discover()
        case <-d.stopCh:
            return
        }
    }
}

func (d *DiscoveryService) discover() {
    entriesCh := make(chan *mdns.ServiceEntry, 10)

    go func() {
        for entry := range entriesCh {
            // Extract device ID from TXT records
            deviceID := extractDeviceID(entry.InfoFields)

            // Skip ourselves
            ourID, _ := sync.GetDeviceID(d.store.GetDB())
            if deviceID == ourID {
                continue
            }

            // Add or update peer
            peer := &Peer{
                ID:      deviceID,
                Name:    entry.Name,
                Address: fmt.Sprintf("%s:%d", entry.AddrV4, entry.Port),
            }
            d.store.AddOrUpdatePeer(peer)
        }
    }()

    // Lookup with timeout
    params := mdns.DefaultParams("_doit._tcp")
    params.Timeout = 3 * time.Second
    params.Entries = entriesCh

    mdns.Query(params)
    close(entriesCh)
}
```

**Key Points:**
- Discovery runs every 30 seconds
- Announced continuously (server stays running)
- Self-filtering (skip our own device)
- Timeout prevents hanging

---

### Pattern 2: HTTP Sync Endpoints

```go
func setupSyncRoutes() *http.ServeMux {
    mux := http.NewServeMux()

    // Middleware: Auth + logging
    mux.HandleFunc("/sync/", authMiddleware(loggingMiddleware(syncHandler)))

    return mux
}

func syncHandler(w http.ResponseWriter, r *http.Request) {
    switch {
    case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/sync/operations"):
        handleGetOperations(w, r)
    case r.Method == "POST" && r.URL.Path == "/sync/operations":
        handlePostOperations(w, r)
    case r.Method == "GET" && r.URL.Path == "/sync/state":
        handleGetState(w, r)
    case r.Method == "POST" && r.URL.Path == "/sync/pair":
        handlePair(w, r)
    default:
        http.NotFound(w, r)
    }
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        secret := r.Header.Get("X-Doit-Secret")
        expected, _ := store.GetConfig("shared_secret")

        if secret != expected {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }

        next(w, r)
    }
}
```

---

### Pattern 3: Peer Connection Management

```go
type PeerManager struct {
    store     *storage.Storage
    client    *http.Client
    mu        sync.RWMutex
    activePeers map[string]*Peer
}

func (pm *PeerManager) ConnectToPeer(peer *Peer) error {
    pm.mu.Lock()
    defer pm.mu.Unlock()

    // Try HTTP connection
    url := fmt.Sprintf("http://%s/sync/state", peer.Address)
    req, _ := http.NewRequest("GET", url, nil)
    req.Header.Set("X-Doit-Secret", pm.getSecret())

    resp, err := pm.client.Do(req)
    if err != nil {
        pm.store.UpdatePeerStatus(peer.ID, "disconnected")
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return fmt.Errorf("peer returned %d", resp.StatusCode)
    }

    pm.activePeers[peer.ID] = peer
    pm.store.UpdatePeerStatus(peer.ID, "connected")
    pm.store.UpdatePeerLastSeen(peer.ID, time.Now().UnixNano())

    return nil
}
```

---

### Pattern 4: Sync Protocol Implementation

```go
func (pm *PeerManager) SyncWithPeer(peer *Peer) error {
    // 1. Get their state
    state, err := pm.getPeerState(peer)
    if err != nil {
        return fmt.Errorf("failed to get peer state: %w", err)
    }

    // 2. Pull operations we're missing
    ourLastOpID, _ := pm.store.GetLastOperationID()
    if state.LastOperationID != ourLastOpID {
        newOps, err := pm.pullOperations(peer, ourLastOpID)
        if err != nil {
            return fmt.Errorf("failed to pull operations: %w", err)
        }

        // Apply with CRDT merge
        for _, op := range newOps {
            op.Apply(pm.store.GetDB())
        }

        pm.store.UpdateSyncState(peer.ID, &SyncState{
            LastOperationID: state.LastOperationID,
            OperationsReceived: len(newOps),
        })
    }

    // 3. Push operations they're missing
    theirLastOpID := pm.getSyncState(peer.ID).LastOperationID
    ourNewOps, _ := pm.store.GetOperations(theirLastOpID)

    if len(ourNewOps) > 0 {
        if err := pm.pushOperations(peer, ourNewOps); err != nil {
            // Non-fatal - they'll get it next time
            log.Printf("Failed to push to %s: %v", peer.Name, err)
        }
    }

    pm.store.UpdatePeerLastSeen(peer.ID, time.Now().UnixNano())
    return nil
}
```

---

## ⚠️ Critical Implementation Requirements

### 1. Device ID in TXT Records

mDNS TXT records must include device_id for peer identification:

```go
deviceID, _ := sync.GetDeviceID(db)
txt := []string{
    "v=1",                           // Protocol version
    "device_id=" + deviceID,         // CRITICAL: For peer identification
    "name=" + sync.GetDeviceName(),  // Friendly name
}

service, _ := mdns.NewMDNSService(hostname, "_doit._tcp", "", "", port, nil, txt)
```

**Why:** ServiceEntry.Name may not be unique, device_id is our primary key

---

### 2. Concurrent Access Protection

**Use sync.RWMutex for peer map:**

```go
type PeerManager struct {
    mu          sync.RWMutex
    activePeers map[string]*Peer
}

func (pm *PeerManager) GetActivePeers() []*Peer {
    pm.mu.RLock()
    defer pm.mu.RUnlock()

    peers := make([]*Peer, 0, len(pm.activePeers))
    for _, p := range pm.activePeers {
        peers = append(peers, p)
    }
    return peers
}
```

**Why:** Discovery and sync happen in different goroutines - need protection

---

### 3. Avoid Sync Loops

**Problem:** Device A syncs to B, B syncs back to A, repeat forever

**Solution:** Track last_operation_id per peer

```go
// Before pulling from peer
ourLastOpID, _ := store.GetLastOperationID()
theirState, _ := getPeerState(peer)

if theirState.LastOperationID == ourLastOpID {
    // Already in sync, skip
    return nil
}

// Only pull new operations
newOps, _ := pullOperations(peer, ourLastOpID)
```

---

### 4. HTTP Request Timeouts

**All requests MUST have timeouts:**

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
resp, err := client.Do(req)
```

**Why:** Prevents hanging on network issues

---

### 5. Duplicate Operation Handling

**Already handled by CRDT!**

```go
// In operation.Apply()
var exists bool
tx.QueryRow("SELECT EXISTS(SELECT 1 FROM operations WHERE id=?)", op.ID).Scan(&exists)
if exists {
    return nil  // Idempotency - already applied
}
```

**Why:** Multiple peers may send same operation - CRDT handles this

---

## 🧪 Testing Strategy

### Unit Tests

**1. Test mDNS discovery:**
```go
func TestMDNSDiscovery(t *testing.T) {
    // Start server
    server := startTestMDNSServer(t, 8888)
    defer server.Shutdown()

    // Discover service
    entries := discoverServices(t, "_doit._tcp", 3*time.Second)

    if len(entries) == 0 {
        t.Fatal("Failed to discover service")
    }

    // Verify entry
    entry := entries[0]
    if entry.Port != 8888 {
        t.Errorf("Expected port 49152, got %d", entry.Port)
    }
}
```

**2. Test HTTP endpoints:**
```go
func TestGetOperations(t *testing.T) {
    // Setup test server
    handler := setupSyncRoutes()
    server := httptest.NewServer(handler)
    defer server.Close()

    // Request operations
    req, _ := http.NewRequest("GET", server.URL+"/sync/operations?since=op-1", nil)
    req.Header.Set("X-Doit-Secret", "test-secret")

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        t.Fatal(err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        t.Errorf("Expected 200, got %d", resp.StatusCode)
    }
}
```

**3. Test sync protocol:**
```go
func TestSyncFlow(t *testing.T) {
    // Setup two in-memory databases
    db1 := setupTestDB(t)
    db2 := setupTestDB(t)

    // Create operation in db1
    op := &Operation{...}
    op.Apply(db1)

    // Simulate sync
    ops, _ := getOperations(db1, "")
    for _, op := range ops {
        op.Apply(db2)
    }

    // Verify db2 has the operation
    todos1, _ := getAllTodos(db1)
    todos2, _ := getAllTodos(db2)

    if len(todos1) != len(todos2) {
        t.Error("Databases didn't converge")
    }
}
```

### Integration Tests

**Test with real HTTP servers:**

```go
func TestTwoDeviceSync(t *testing.T) {
    // Start server for device A
    serverA := startSyncServer(t, 8888)
    defer serverA.Shutdown()

    // Start server for device B
    serverB := startSyncServer(t, 8889)
    defer serverB.Shutdown()

    // Add todo on device A
    addTodo(dbA, "Buy milk")

    // Sync A → B
    syncDevices(dbA, dbB, "http://localhost:8889")

    // Verify B has the todo
    todos := getAllTodos(dbB)
    if len(todos) != 1 || todos[0].Task != "Buy milk" {
        t.Error("Sync failed")
    }
}
```

---

## 🚨 Common Pitfalls to Avoid

### Pitfall 1: Forgetting to Close Channels

**Wrong:**
```go
entriesCh := make(chan *mdns.ServiceEntry)
mdns.Lookup("_doit._tcp", entriesCh)
// ✗ Channel never closed - goroutine leak!
```

**Correct:**
```go
entriesCh := make(chan *mdns.ServiceEntry)
mdns.Lookup("_doit._tcp", entriesCh)
close(entriesCh)  // ✓ Always close after lookup
```

---

### Pitfall 2: Not Filtering Self-Discovery

**Wrong:**
```go
for entry := range entriesCh {
    // ✗ Will discover ourselves!
    AddPeer(entry)
}
```

**Correct:**
```go
ourDeviceID, _ := sync.GetDeviceID(db)
for entry := range entriesCh {
    deviceID := extractDeviceID(entry.InfoFields)
    if deviceID == ourDeviceID {
        continue  // ✓ Skip ourselves
    }
    AddPeer(entry)
}
```

---

### Pitfall 3: Sync Storms (Too Frequent)

**Wrong:**
```go
ticker := time.NewTicker(1 * time.Second)  // ✗ Too frequent!
```

**Correct:**
```go
ticker := time.NewTicker(10 * time.Second)  // ✓ Reasonable interval
```

**Why:** 1 second is excessive for personal todo app, wastes CPU/network

---

### Pitfall 4: Blocking Main Goroutine

**Wrong:**
```go
srv.ListenAndServe()  // ✗ Blocks forever!
```

**Correct:**
```go
go func() {
    if err := srv.ListenAndServe(); err != http.ErrServerClosed {
        log.Printf("Server error: %v", err)
    }
}()  // ✓ Non-blocking
```

---

### Pitfall 5: Not Using Request Context

**Wrong:**
```go
req, _ := http.NewRequest("GET", url, nil)
resp, _ := client.Do(req)  // ✗ No timeout!
```

**Correct:**
```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
resp, _ := client.Do(req)  // ✓ Will timeout after 30s
```

---

### Pitfall 6: Storing IP Addresses as Strings

**Issue:** IP addresses can change (DHCP), need to rediscover

**Solution:** Use mDNS continuously to refresh addresses

```go
// In AddOrUpdatePeer
existingPeer, _ := store.GetPeer(deviceID)
if existingPeer != nil {
    // Update address if changed
    if existingPeer.Address != newAddress {
        log.Printf("Peer %s address changed: %s → %s", deviceID, existingPeer.Address, newAddress)
        store.UpdatePeerAddress(deviceID, newAddress)
    }
}
```

---

### Pitfall 7: No Retry on Transient Failures

**Wrong:**
```go
if err := syncWithPeer(peer); err != nil {
    return err  // ✗ Give up immediately
}
```

**Correct:**
```go
b := backoff.NewExponentialBackOff()
b.MaxElapsedTime = 2 * time.Minute

err := backoff.Retry(func() error {
    return syncWithPeer(peer)
}, b)  // ✓ Retry with backoff
```

---

## 📋 Implementation Checklist

See [TODO.md](TODO.md) Phase 2 section for complete task breakdown.

**Key milestones:**
1. ✓ mDNS server announces service
2. ✓ mDNS client discovers peers
3. ✓ HTTP server handles sync endpoints
4. ✓ Sync protocol pulls and pushes operations
5. ✓ Background engine runs sync loop
6. ✓ CLI commands for management

---

## 🔧 Dependencies Needed

```bash
go get github.com/hashicorp/mdns@v1.0.6
go get github.com/cenkalti/backoff/v4  # Optional: For retry logic
```

**Note:** Can implement simple retry without library (just a loop with sleep)

---

## 💡 Design Principles

### Keep It Simple
- No complex peer selection algorithms needed
- Sync with ALL discovered peers (it's personal, not enterprise)
- Linear sync is fine (not 100s of peers)

### Fail Gracefully
- One peer failure doesn't stop others
- Network errors are logged, not fatal
- App works offline always

### User Experience
- Zero configuration for same-network sync
- Automatic discovery and sync
- Clear status/error messages

---

## ✅ Implementation Complete (2025-12-04)

### What Was Built

**5 New Files:**
1. `internal/sync/discovery.go` - mDNS service (HashiCorp library)
2. `internal/sync/peer.go` - Peer manager with RWMutex
3. `internal/sync/server.go` - HTTP sync endpoints
4. `internal/sync/protocol.go` - Sync client with retry
5. `internal/sync/engine.go` - Orchestrator

**3 Files Modified:**
1. `internal/storage/storage.go` - Added peers/sync_state tables + 7 methods
2. `cmd/sync.go` - Added init/start/stop/disable/devices commands
3. `cmd/root.go` - Auto-start sync engine

### Critical Fixes Applied

1. **Sync loop prevention** - Operations marked synced=1 after push (protocol.go:220-228)
2. **Peer persistence** - LoadActivePeers() on startup (engine.go:57-59)
3. **Port auto-increment** - Try 49152→49153→49154 (server.go:56-95)
4. **Query optimization** - GetOperations filters synced=0 (storage.go:623, 636)
5. **Race condition fix** - Proper Stop() ordering (engine.go:79-102)
6. **Status upgrades** - Discovered peers included in sync loop (storage.go:794)
7. **Peer timeout** - 5-minute timeout before disconnected (engine.go:133-139)
8. **Default port** - Changed to 49152 (IANA dynamic/private range)

### Lessons Learned

**What Worked Well:**
- CRDT foundation from Phase 1 made sync trivial
- HashiCorp mDNS library easy to use
- Storage abstraction clean and extensible
- Transaction-based operations prevented race conditions

**Issues Found During Implementation:**
- Need to explicitly mark operations as synced (not automatic)
- Peer manager must be loaded on restart
- Port conflicts need graceful handling
- GetActivePeers must include "discovered" status
- Type consistency important (string vs []byte)

**Performance Notes:**
- Discovery every 30s (multicast is heavy)
- Sync every 10s (unicast is light)
- Connection pooling essential (MaxConnsPerHost: 20)
- 5-minute peer timeout balances false positives vs detection speed

### Testing Status

**Single Device:**
- ✅ Commands work (init, start, stop, disable, devices, status)
- ✅ Operations created correctly
- ✅ All tests pass with -race detector
- ✅ Port auto-increment validated
- ✅ Graceful shutdown working

**Two Device (Pending):**
- Requires two devices on same WiFi
- Will validate mDNS discovery, bidirectional sync, conflict resolution

### Known Limitations

**Phase 2 Scope:**
- ✓ Same WiFi/LAN only (mDNS limitation)
- ✗ Doesn't work across internet (Phase 3 needed)
- ✓ Zero config for local network
- ✗ Corporate networks may block multicast

**Future Enhancements (Phase 3+):**
- STUN/WebRTC for internet sync
- Pairing codes for remote devices
- IPv6 support
- Adaptive sync intervals
- Sync progress indication

---

## 📖 References & Sources

**mDNS Implementation:**
- [HashiCorp mDNS GitHub](https://github.com/hashicorp/mdns)
- [mDNS Go Package Docs](https://pkg.go.dev/github.com/hashicorp/mdns)
- [mDNS Examples](https://golang.hotexamples.com/examples/github.com.hashicorp.mdns/-/NewMDNSService/golang-newmdnsservice-function-examples.html)

**HTTP Server Patterns:**
- [Proper HTTP Shutdown in Go](https://dev.to/mokiat/proper-http-shutdown-in-go-3fji)
- [Graceful Shutdown Tutorial](https://rafallorenz.com/go/handle-signals-to-graceful-shutdown-http-server/)
- [VictoriaMetrics Graceful Shutdown](https://victoriametrics.com/blog/go-graceful-shutdown/)

**HTTP Client Best Practices:**
- [HTTP Client Performance Tuning](https://www.loginradius.com/blog/engineering/tune-the-go-http-client-for-high-performance)
- [Production-Ready Client Patterns](https://jsschools.com/golang/go-http-client-patterns-a-production-ready-implem/)

**Retry/Backoff:**
- [cenkalti/backoff](https://github.com/cenkalti/backoff)
- [Exponential Backoff in Go](https://medium.com/@nidhey60/code-smarter-exponential-backoff-in-go-made-easy-ba5224e18805)

**Sync Protocol Design:**
- [REST API Sync Design](https://stackoverflow.com/questions/56319379/rest-api-design-for-data-synchronization-service)
- [Timestamp-Based Sync](https://stackoverflow.com/questions/43081770/rest-best-practice-for-sync-log-data-in-reverse-order)

---

## 🚀 Ready for Implementation

**Next:** Follow Phase 2 tasks in [TODO.md](TODO.md) in order:
1. Section 2.1: mDNS Discovery
2. Section 2.2: Peer Management
3. Section 2.3: HTTP Sync Server
4. Section 2.4: Sync Protocol
5. Section 2.5: Sync Engine
6. Section 2.6: CLI Integration

**Success:** Two devices on same network sync automatically within 10 seconds.
