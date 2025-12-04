# Syncthing Sync Architecture - Technical Analysis

**Purpose:** Knowledge document for understanding how production P2P sync systems work

**Date:** 2025-12-05

**Sources:**
- [Syncthing BEP Protocol](https://docs.syncthing.net/specs/bep-v1.html)
- [Syncthing Local Discovery](https://docs.syncthing.net/specs/localdisco-v4.html)

---

## Overview

Syncthing is a mature, production-grade P2P file synchronization application that successfully handles:
- Bidirectional sync across Linux, macOS, Windows
- No central server
- Conflict resolution
- Efficient delta sync
- NAT traversal

## Architecture Layers

```
┌─────────────────────────────────────┐
│ Local Discovery (UDP Broadcast)     │ ← Find devices on LAN
├─────────────────────────────────────┤
│ Global Discovery (HTTPS servers)    │ ← Find devices on internet
├─────────────────────────────────────┤
│ Connection (TLS 1.3)                │ ← Secure P2P connection
├─────────────────────────────────────┤
│ BEP Protocol (Protocol Buffers)     │ ← Sync protocol
├─────────────────────────────────────┤
│ Block Exchange                       │ ← Transfer file blocks
└─────────────────────────────────────┘
```

---

## 1. Local Discovery Protocol

### UDP Broadcast Mechanism
- **Port:** 21027/UDP
- **Timing:** Announce every 30-60 seconds
- **Addresses:**
  - IPv4: Broadcast to 255.255.255.255 or link-local broadcast
  - IPv6: Multicast to ff12::8384

### Announcement Packet
```
┌──────────────┬─────────────┬──────────────────┐
│ Magic (4B)   │ Length (2B) │ Protobuf Message │
└──────────────┴─────────────┴──────────────────┘
```

**Magic:** 0x2EA7D90B
**Content:** Device ID (SHA-256 cert hash) + List of addresses (tcp://IP:port or relay://...)

**Key design:** No request/response - just periodic announcements. Simple, predictable.

---

## 2. Block Exchange Protocol (BEP)

### Connection Establishment

**Phase 1: Hello Exchange**
```
Device A → Device B: Hello{DeviceName, ClientName, ClientVersion}
Device B → Device A: Hello{...}
```

**Phase 2: Cluster Configuration**
```
Device A → Device B: ClusterConfig{Folders[]}
Device B → Device A: ClusterConfig{Folders[]}
```

ClusterConfig includes:
- Folder IDs
- Folder labels
- Sharing modes (ReadWrite, ReadOnly, ReceiveOnly)
- Device list for each folder

**Phase 3: Index Exchange**
```
Device A → Device B: Index{Folder, Files[]}
Device B → Device A: Index{Folder, Files[]}
```

Each file entry contains:
- Name, type, permissions, timestamps
- **Version vector** (list of {DeviceID, Counter} pairs)
- Block list with hashes
- Deleted flag

### Bidirectional Sync Flow

**On subsequent changes:**
```
Device A: File modified
  ↓
Device A → All peers: IndexUpdate{Folder, Files[changed_file]}
  ↓
Device B receives IndexUpdate
  ↓
Device B: Compare version vectors
  ↓
  If Device A's version > local version:
    Device B → Device A: Request{Block offsets}
    Device A → Device B: Response{Block data}
    Device B: Write blocks, update local version
```

### Version Vectors (Conflict Resolution)

**Example:**
```
File "notes.txt" version vector:
Device A: [(A, 5), (B, 2), (C, 1)]
Device B: [(A, 5), (B, 3), (C, 1)]

Comparison:
- A's counter on B: 2 vs 3 → B has newer changes
- B wins, A pulls from B
```

**Algorithm:**
1. Compare version vectors element-wise
2. If all counters V1 ≤ V2, then V2 is newer
3. If mixed (some V1 > V2, some V1 < V2), it's a conflict
4. On conflict: Use timestamp or mark as conflicted copy

---

## 3. Sync Timing & Intervals

### Discovery
- **Announcement:** Every 30-60 seconds
- **Purpose:** Continuous device presence updates

### Index Updates
- **Trigger:** On file change (immediate)
- **Purpose:** Notify peers of changes ASAP

### Scanning
- **Full scan:** Every 60 seconds (configurable)
- **Purpose:** Detect external file changes (outside Syncthing)

### Pull/Request
- **Trigger:** On receiving IndexUpdate (immediate)
- **Purpose:** Fetch missing/outdated blocks

### Key Insight
Syncthing uses **push model for metadata** (IndexUpdate sent immediately) and **pull model for data** (receiving device requests blocks).

---

## 4. Key Design Decisions

### Why Protocol Buffers?
- Efficient binary encoding
- Schema evolution support
- Cross-language compatibility

### Why Version Vectors?
- Deterministic conflict detection
- Multi-device causality tracking
- No central authority needed

### Why Block-Level Transfer?
- Rsync-style delta sync
- Large file efficiency
- Resume capability

### Why TLS?
- Encryption and authentication in one
- Certificate-based device identity
- No password management

---

## 5. Comparison with `doit` Implementation

| Aspect | Syncthing | doit (current) |
|--------|-----------|----------------|
| **Discovery** | UDP broadcast (custom) | UDP broadcast (custom) ✓ |
| **Transport** | TLS | HTTP (no encryption) |
| **Protocol** | Protobuf | JSON |
| **Sync unit** | File blocks | Operations |
| **Conflict resolution** | Version vectors | LWW timestamps |
| **State tracking** | Sequence numbers | LastOperationID |
| **Timing** | Immediate push | 3s poll interval |

---

## 6. Lessons for `doit`

### What We Can Adopt

**1. Push model for changes:**
- Currently: Pull every 3s (polling)
- Better: Push immediately when operation created
- How: WebSocket or HTTP/2 Server-Sent Events

**2. Connection-based sync:**
- Currently: Stateless HTTP requests every 3s
- Better: Persistent connection, send updates immediately
- How: Keep HTTP connection open, stream updates

**3. Explicit handshake:**
- Currently: Jump straight to GetPeerState
- Better: Exchange capabilities, protocol version first
- How: Add /sync/hello endpoint

### What to Keep Simple

**1. No version vectors:**
- Too complex for single-value CRDT (todos)
- LWW timestamps sufficient for our use case

**2. No block-level:**
- Todos are small (bytes, not gigabytes)
- Operation-level granularity is fine

**3. No TLS (yet):**
- Phase 3 feature (WebRTC will handle encryption)
- HTTP on LAN acceptable for now

---

## 7. Immediate Action Items

### Fix Current Sync Issue

**Root cause:** Discovered peers not in activePeers map
**Fix:** Already pushed (commit 2581435)
**Action:** Rebuild Device A

### Improve Sync Responsiveness

**Current:** 3s polling interval
**Better:** Push-based (investigate options)
**Future:** Phase 3 WebRTC will enable real-time push

### Add Logging

**Problem:** No visibility into sync failures
**Fix:** Add detailed logging to sync loop
**Action:** Log when GetActivePeersList() returns empty

---

## Conclusion

Syncthing's architecture is sophisticated but serves different needs (file sync vs operation sync). Key takeaways:

✅ **Keep:** UDP broadcast discovery (proven, simple)
✅ **Keep:** Operation-based sync (appropriate for todo items)
✅ **Keep:** LWW conflict resolution (sufficient for single-value updates)

⚠️ **Consider:** Push-based updates (reduce latency)
⚠️ **Consider:** Persistent connections (reduce overhead)
⚠️ **Consider:** Better error visibility (debugging)

**Current focus:** Fix the discovered peer bug, verify sync works, then optimize if needed.

---

**For future Claude sessions:** This document provides context on production P2P sync architecture. Our implementation is simpler by design (operation-level vs block-level, HTTP vs TLS), which is appropriate for a todo app. The key bug was excluding "discovered" status peers from sync attempts - now fixed.
