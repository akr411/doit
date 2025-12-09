# PAKE + TLS Architecture for P2P Sync

**Purpose**: Explain how Password-Authenticated Key Exchange (PAKE) and TLS work together for secure P2P device pairing
**Audience**: Developers, security reviewers
**Date**: 2025-12-09

---

## Table of Contents

1. [Problem Statement](#problem-statement)
2. [PAKE Overview](#pake-overview)
3. [TLS Overview](#tls-overview)
4. [Combined Architecture](#combined-architecture)
5. [Implementation in doit](#implementation-in-doit)
6. [Security Analysis](#security-analysis)
7. [Alternatives Considered](#alternatives-considered)

---

## Problem Statement

### The Challenge

How do two devices securely establish a shared secret when:
1. They've never communicated before
2. They're on an untrusted network (public WiFi)
3. No pre-shared keys exist
4. No centralized server for authentication
5. Users want "zero-config" (no manual secret typing)

### Attack Scenarios to Prevent

```
┌─────────────────────────────────────────────────────────────┐
│ Scenario 1: Eavesdropping                                    │
│                                                               │
│ Device A ----[Secret: abc123]----> Device B                 │
│                      ↓                                        │
│                  Attacker                                     │
│                  Sees: abc123                                 │
│                  Uses: abc123 to impersonate A or B          │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│ Scenario 2: Man-in-the-Middle (MITM)                        │
│                                                               │
│ Device A ----> Attacker ----> Device B                      │
│         ↑                  ↑                                  │
│    Thinks it's          Thinks it's                          │
│    talking to B         talking to A                         │
│                                                               │
│ Attacker sees and modifies all traffic                       │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│ Scenario 3: Replay Attack                                    │
│                                                               │
│ Day 1: Device A ----[valid request]----> Device B           │
│                           ↓                                   │
│                       Attacker                                │
│                       Records                                 │
│                                                               │
│ Day 2: Attacker ----[replayed request]----> Device B        │
│        Device B accepts (thinks it's A)                      │
└─────────────────────────────────────────────────────────────┘
```

### What We Need

✅ **Authentication**: Both devices verify each other's identity
✅ **Confidentiality**: Traffic encrypted, can't be read
✅ **Integrity**: Traffic can't be modified without detection
✅ **Forward Secrecy**: Compromised session keys don't compromise past sessions
✅ **User-Friendly**: No typing long secrets

---

## PAKE Overview

### What is PAKE?

**Password-Authenticated Key Exchange** = Two parties derive a shared secret from a low-entropy password (like "123-456") without revealing the password to eavesdroppers.

### Key Properties

1. **Resistant to offline dictionary attacks**: Attacker can't capture traffic and try millions of passwords offline
2. **Symmetric**: Both parties use the same password
3. **Zero-knowledge**: Password never sent over network
4. **Derive strong key**: 6-digit PIN → 256-bit shared secret

### SPAKE2+ Protocol

**SPAKE2+** = Augmented PAKE used by WPA3, Apple iCloud, Signal

#### How It Works (Simplified)

```
┌───────────────────────────────────────────────────────────────┐
│ Setup                                                          │
├───────────────────────────────────────────────────────────────┤
│ User enters PIN on both devices: 123-456                      │
│                                                                │
│ Device A                          Device B                    │
│    P = "123456"                      P = "123456"             │
└───────────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────────────┐
│ Round 1: Exchange Masked Points                               │
├───────────────────────────────────────────────────────────────┤
│ Device A                          Device B                    │
│                                                                │
│ a = random()                      b = random()                │
│ X = g^a * M^H(P)                  Y = g^b * N^H(P)            │
│                                                                │
│         ────────── X ───────────>                             │
│         <───────── Y ────────────                             │
│                                                                │
│ (g, M, N are public elliptic curve points)                    │
│ (H is cryptographic hash)                                     │
└───────────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────────────┐
│ Round 2: Compute Shared Secret                                │
├───────────────────────────────────────────────────────────────┤
│ Device A                          Device B                    │
│                                                                │
│ K_A = (Y / N^H(P))^a              K_B = (X / M^H(P))^b        │
│                                                                │
│ K_A = K_B (if passwords match!)                               │
│                                                                │
│ Shared Secret = HKDF(K_A, X, Y, A, B)                         │
└───────────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────────────┐
│ Round 3: Mutual Confirmation                                  │
├───────────────────────────────────────────────────────────────┤
│ Device A                          Device B                    │
│                                                                │
│ MAC_A = HMAC(K, "A")              MAC_B = HMAC(K, "B")        │
│                                                                │
│         ────────── MAC_A ───────>                             │
│         <───────── MAC_B ────────                             │
│                                                                │
│ Verify MAC_B matches              Verify MAC_A matches        │
│                                                                │
│ ✅ Both authenticated              ✅ Both authenticated        │
│ ✅ Shared secret K established     ✅ Shared secret K established│
└───────────────────────────────────────────────────────────────┘
```

#### Security Analysis

**Against Eavesdropper**:
- Attacker sees: X, Y, MAC_A, MAC_B
- Attacker does NOT see: password P, secret a, secret b, shared key K
- Cannot compute K without knowing P
- X and Y are "blinded" by password hash

**Against MITM**:
- Attacker must guess password in real-time
- Wrong password → different K_A and K_B
- MAC verification fails
- Only ONE guess per attempt (can't do offline brute force)

**Against Dictionary Attack**:
- Attacker records X, Y, MAC_A, MAC_B
- Tries passwords P1, P2, P3... offline
- **DOESN'T WORK**: Need to participate in protocol actively
- Rate limiting on server prevents brute force

---

## TLS Overview

### What is TLS?

**Transport Layer Security** = Protocol for encrypted, authenticated communication

Used by: HTTPS, email (SMTP/IMAP), VPNs, basically everything

### TLS 1.3 Overview

```
┌───────────────────────────────────────────────────────────────┐
│ TLS 1.3 Handshake (Simplified)                                │
├───────────────────────────────────────────────────────────────┤
│ Client                            Server                      │
│                                                                │
│ ClientHello                                                    │
│  - Supported ciphers                                           │
│  - Key share (DH public key)                                   │
│         ─────────────────────>                                │
│                                                                │
│                                   ServerHello                  │
│                                    - Selected cipher           │
│                                    - Key share (DH public key) │
│                                    - Certificate               │
│                                    - CertificateVerify         │
│         <─────────────────────                                │
│                                                                │
│ Derive shared secret (ECDH)                                    │
│ Verify server certificate                                      │
│                                                                │
│ Certificate (optional - for mTLS)                              │
│ CertificateVerify                                              │
│ Finished (encrypted with derived key)                          │
│         ─────────────────────>                                │
│                                                                │
│                                   Finished (encrypted)         │
│         <─────────────────────                                │
│                                                                │
│ ✅ Encrypted channel established                               │
│ All future data encrypted with derived keys                    │
└───────────────────────────────────────────────────────────────┘
```

### Mutual TLS (mTLS)

In standard TLS:
- Server has certificate
- Client verifies server
- Client does NOT have certificate

In **Mutual TLS (mTLS)**:
- Server has certificate
- Client has certificate
- Both verify each other
- Used in P2P, microservices, zero-trust networks

### Self-Signed Certificates

**Traditional PKI**:
```
Root CA (trusted by OS)
  ↓
Intermediate CA
  ↓
Server Certificate (example.com)
```

**Self-Signed** (for P2P):
```
Device A Certificate (self-signed)
Device B Certificate (self-signed)
```

No Certificate Authority needed!

**Challenge**: How do you trust a self-signed cert?

**Answer**: Certificate Pinning (fingerprint verification)

```
┌───────────────────────────────────────────────────────────────┐
│ Certificate Fingerprint                                        │
├───────────────────────────────────────────────────────────────┤
│ Certificate (DER format)                                       │
│        ↓                                                       │
│    SHA-256 Hash                                                │
│        ↓                                                       │
│ 3A:F9:C2:... (fingerprint)                                     │
│                                                                │
│ Store fingerprint in database                                  │
│ On future connections: Verify cert hash matches stored        │
└───────────────────────────────────────────────────────────────┘
```

---

## Combined Architecture

### Why Use BOTH PAKE and TLS?

**PAKE alone**:
- ✅ Establishes shared secret securely
- ❌ Doesn't encrypt subsequent traffic
- ❌ No forward secrecy (same key forever)

**TLS alone**:
- ✅ Encrypts traffic
- ✅ Forward secrecy (new keys per session)
- ❌ Need to distribute certificates somehow
- ❌ Self-signed certs need out-of-band verification

**PAKE + TLS**:
- ✅ PAKE authenticates devices during pairing
- ✅ Exchange TLS certificate fingerprints over PAKE channel
- ✅ Future connections use TLS (encrypted, forward secrecy)
- ✅ mTLS verifies certificate fingerprints (prevents MITM)

### Architecture Diagram

```
┌────────────────────────────────────────────────────────────────┐
│ Phase 1: Initial Pairing (Happens Once)                        │
│                                                                 │
│ Device A                              Device B                 │
│    ↓                                     ↓                      │
│ Generate:                             Generate:                │
│  - Self-signed cert                    - Self-signed cert      │
│  - Cert fingerprint                    - Cert fingerprint      │
│    FA = SHA256(certA)                    FB = SHA256(certB)    │
│                                                                 │
│ User enters PIN: 123-456              User enters PIN: 123-456 │
│    ↓                                     ↓                      │
│ ┌─────────────────────────────────────────────────────────┐   │
│ │ PAKE PROTOCOL (SPAKE2+)                                 │   │
│ │  - Authenticate using PIN                               │   │
│ │  - Derive shared secret K                               │   │
│ └─────────────────────────────────────────────────────────┘   │
│    ↓                                     ↓                      │
│ Encrypt with K:                       Encrypt with K:          │
│  - Send FA to B                        - Send FB to A          │
│  - Send shared secret                  - Send shared secret    │
│                                                                 │
│ Store:                                Store:                   │
│  - Peer cert fingerprint FB            - Peer cert fingerprint FA│
│  - Peer shared secret                  - Peer shared secret    │
│                                                                 │
│ ✅ Pairing complete                    ✅ Pairing complete       │
└────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────┐
│ Phase 2: Regular Sync (Happens Continuously)                   │
│                                                                 │
│ Device A                              Device B                 │
│    ↓                                     ↓                      │
│ ┌─────────────────────────────────────────────────────────┐   │
│ │ TLS 1.3 HANDSHAKE                                       │   │
│ │  - ECDH key exchange                                    │   │
│ │  - Exchange certificates                                │   │
│ │  - Verify: SHA256(received cert) == stored fingerprint │   │
│ │  - Mutual authentication (mTLS)                         │   │
│ └─────────────────────────────────────────────────────────┘   │
│    ↓                                     ↓                      │
│ ✅ TLS channel established              ✅ TLS channel established│
│                                                                 │
│ ┌─────────────────────────────────────────────────────────┐   │
│ │ APPLICATION PROTOCOL                                    │   │
│ │  - Authenticate requests with shared secret (API key)   │   │
│ │  - Sync operations (encrypted by TLS)                   │   │
│ └─────────────────────────────────────────────────────────┘   │
│                                                                 │
│ All traffic encrypted and authenticated                        │
└────────────────────────────────────────────────────────────────┘
```

### Security Layering

```
┌─────────────────────────────────────────────────────────┐
│ Layer 4: Application Authentication                     │
│  - Shared secret in X-Doit-Secret header                │
│  - Prevents replay of old TLS sessions                  │
│  - Device-specific authorization                        │
├─────────────────────────────────────────────────────────┤
│ Layer 3: TLS 1.3 Encryption                             │
│  - ChaCha20-Poly1305 or AES-256-GCM                     │
│  - Forward secrecy (ephemeral keys)                     │
│  - Integrity (AEAD cipher)                              │
├─────────────────────────────────────────────────────────┤
│ Layer 2: mTLS Certificate Verification                  │
│  - Both parties present certificates                    │
│  - Fingerprint pinning (prevents MITM)                  │
│  - Device identity verification                         │
├─────────────────────────────────────────────────────────┤
│ Layer 1: Network                                        │
│  - TCP/IP                                               │
│  - Untrusted (public WiFi, internet)                    │
└─────────────────────────────────────────────────────────┘
```

---

## Implementation in doit

### Simplified Implementation (Pairing Codes)

Since PAKE libraries in Go are immature/POC-quality, we implement a simplified version:

#### Step 1: Generate Pairing Code

```go
// Device A
code := GenerateRandomCode() // "123-456"
StoreCode(code, expiresIn: 15*time.Minute)
DisplayCode(code) // Show to user
```

#### Step 2: Enter Code on Device B

```go
// Device B
userEnteredCode := GetUserInput() // "123-456"

// Send to Device A
POST /sync/pair {
    "pairing_code": "123-456",
    "cert_fingerprint": fingerprint_B
}

// Device A validates
if ValidateCode(code) && NotExpired() && NotUsed() {
    return {
        "shared_secret": secret_A,
        "cert_fingerprint": fingerprint_A
    }
}
```

#### Security Properties

Compared to full PAKE:
- ❌ Not resistant to active MITM (attacker can intercept code)
- ✅ Resistant to passive eavesdropping (code not transmitted until validated)
- ✅ Time-limited (15 minutes)
- ✅ Single-use (marked used after pairing)
- ✅ Requires physical presence (user must see screen)

**Key Limitation**: If attacker is actively MITMing during pairing, they can steal the code.

**Mitigation**: After TLS is added, even MITM during pairing doesn't help (certificate fingerprints won't match).

### Full Flow

```
┌────────────────────────────────────────────────────────────────┐
│ 1. Initialization (First Run)                                  │
├────────────────────────────────────────────────────────────────┤
│ $ doit sync init                                                │
│                                                                 │
│ - Generate random shared secret (32 bytes)                      │
│ - Generate Ed25519 self-signed certificate                      │
│ - Calculate cert fingerprint: SHA256(cert)                      │
│ - Store in database                                             │
│                                                                 │
│ ✅ Device ready for pairing                                     │
└────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────┐
│ 2. Discovery (Continuous)                                       │
├────────────────────────────────────────────────────────────────┤
│ - UDP broadcast on port 49151                                   │
│ - Announce: device_id, name, HTTP port                          │
│ - Receive announcements from peers                              │
│ - Store in peers table                                          │
│                                                                 │
│ ⚠️ No authentication at this stage                              │
│ ⚠️ Anyone can see broadcasts                                    │
└────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────┐
│ 3. Pairing (User-Initiated)                                     │
├────────────────────────────────────────────────────────────────┤
│ Device A:                                                       │
│ $ doit sync show                                                │
│                                                                 │
│   Code: 123-456                                                 │
│   Expires in: 14:32                                             │
│   Fingerprint: 3AF9C2...                                        │
│                                                                 │
│ Device B:                                                       │
│ $ doit sync pair 123-456                                        │
│                                                                 │
│ Protocol:                                                       │
│  1. B → A: POST /sync/pair {"pairing_code": "123-456",         │
│                              "cert_fingerprint": FB,            │
│                              "shared_secret": secret_B}         │
│  2. A validates code (not expired, not used)                    │
│  3. A → B: {"shared_secret": secret_A,                          │
│             "cert_fingerprint": FA}                             │
│  4. Both store peer info in database                            │
│                                                                 │
│ Database updates:                                               │
│  - peer_secrets: (peer_id, secret)                              │
│  - peer_certificates: (peer_id, fingerprint)                    │
│                                                                 │
│ ✅ Devices paired                                               │
└────────────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────────────┐
│ 4. Sync (Automatic, Every 10 Seconds)                           │
├────────────────────────────────────────────────────────────────┤
│ TLS Handshake:                                                  │
│  1. Client sends ClientHello + certificate                      │
│  2. Server sends ServerHello + certificate                      │
│  3. Both verify: SHA256(received_cert) == stored_fingerprint    │
│  4. TLS channel established ✅                                   │
│                                                                 │
│ Application Protocol:                                           │
│  1. GET /sync/state                                             │
│     → Returns: last_operation_id, device_id                     │
│  2. GET /sync/operations?since=<id>                             │
│     Header: X-Doit-Secret: <peer_secret>                        │
│     → Returns: new operations                                   │
│  3. POST /sync/operations                                       │
│     Header: X-Doit-Secret: <peer_secret>                        │
│     Body: [operations to push]                                  │
│                                                                 │
│ All encrypted by TLS ✅                                          │
│ All authenticated by secret ✅                                   │
└────────────────────────────────────────────────────────────────┘
```

---

## Security Analysis

### Threat Model

**Assumptions**:
1. Users have physical access to both devices during pairing
2. Pairing happens on local network (not internet)
3. Long-term storage is secure (attacker can't access ~/.local/share/doit/doit.db)

**Adversary Capabilities**:
- ✅ Passive eavesdropping (sees all network traffic)
- ✅ Active MITM (can intercept/modify traffic)
- ✅ On same network as devices
- ❌ No physical access to devices
- ❌ No access to database files

### Attack Scenarios & Defenses

#### 1. Passive Eavesdropping

**Attack**: Attacker captures all network traffic, tries to extract secrets

**Phase 1 (Pairing)**:
- Attacker sees: pairing code request/response
- ❌ VULNERABLE: If pairing over HTTP, attacker sees shared secret and cert fingerprints
- ✅ MITIGATED: After Phase 3 (TLS), pairing happens over HTTPS

**Phase 2 (Sync)**:
- Attacker sees: TLS encrypted traffic
- ✅ PROTECTED: Cannot decrypt (TLS 1.3 encryption)

**Verdict**: ⚠️ Vulnerable during pairing (Phase 1-2), Fixed in Phase 3

---

#### 2. Active MITM During Pairing

**Attack**: Attacker intercepts pairing request, impersonates Device A

```
Device B ──→ Attacker ──→ Device A
         (pair request)

Attacker:
 1. Receives B's code validation request
 2. Forwards to A, gets A's secret
 3. Returns own cert fingerprint to B
 4. B stores attacker's fingerprint
```

**Defense**:
- ❌ VULNERABLE: In Phase 1-2 (HTTP pairing)
- ✅ MITIGATED: In Phase 3, B also checks A's cert fingerprint matches what A displays on screen

**Better Defense** (future):
- Display cert fingerprint on Device A screen: "3AF9C2..."
- User manually verifies on Device B
- Similar to SSH fingerprint verification
- Or use QR code (out-of-band channel)

**Verdict**: ⚠️ Vulnerable (requires user verification for complete security)

---

#### 3. Active MITM During Sync

**Attack**: Attacker intercepts sync traffic after pairing

```
Device A ──→ Attacker ──→ Device B
         (sync request)
```

**Defense**:
- TLS certificate verification fails
- Device A expects cert fingerprint FA (from pairing)
- Attacker presents different cert
- SHA256(attacker_cert) ≠ FA
- Connection rejected ✅

**Verdict**: ✅ Protected (certificate pinning)

---

#### 4. Stolen Database File

**Attack**: Attacker gains access to `~/.local/share/doit/doit.db`

**What attacker gets**:
- Shared secrets (plain text)
- TLS private key (plain text)
- Certificate fingerprints
- All todo data

**Impact**:
- Can impersonate device
- Can decrypt past traffic (no forward secrecy for secrets)
- Can access all todos

**Defense**:
- Encrypt secrets with OS keychain (Phase 4)
- File system encryption (user responsibility)
- Secure deletion on unpair

**Verdict**: ⚠️ Vulnerable to physical access

---

#### 5. Replay Attacks

**Attack**: Attacker records valid request, replays later

**Defense**:
- TLS prevents replay (session IDs, sequence numbers)
- Shared secret doesn't change, BUT:
  - Each TLS session has unique keys
  - Old requests can't be replayed in new session

**Verdict**: ✅ Protected (TLS replay protection)

---

#### 6. Brute Force Pairing Code

**Attack**: Try all 1,000,000 possible codes (000-000 to 999-999)

**Defense**:
- Codes expire (15 minutes)
- Codes single-use (marked used after pairing)
- Rate limiting on /sync/pair endpoint
- Can only try ~10 codes before timeout

**Verdict**: ✅ Protected (time + rate limiting)

---

### Security Comparison

| Threat | Phase 1 (HTTP) | Phase 2 (Pairing) | Phase 3 (TLS) |
|--------|---------------|-------------------|---------------|
| Passive eavesdropping | ❌ CRITICAL | ❌ CRITICAL | ✅ PROTECTED |
| Active MITM (pairing) | ❌ CRITICAL | ⚠️ POSSIBLE | ⚠️ POSSIBLE* |
| Active MITM (sync) | ❌ CRITICAL | ⚠️ POSSIBLE | ✅ PROTECTED |
| Replay attacks | ❌ VULNERABLE | ⚠️ POSSIBLE | ✅ PROTECTED |
| Brute force | N/A | ✅ PROTECTED | ✅ PROTECTED |
| Stolen database | ⚠️ VULNERABLE | ⚠️ VULNERABLE | ⚠️ VULNERABLE |

*Requires user verification of fingerprints for complete protection

---

## Alternatives Considered

### Alternative 1: Centralized Server

**Approach**: All devices sync through central server
**Used by**: Todoist, Notion, most SaaS

**Pros**:
- ✅ Simple authentication (OAuth, API keys)
- ✅ Easy to implement
- ✅ Works from anywhere

**Cons**:
- ❌ Requires server (cost, maintenance)
- ❌ Privacy concerns (server sees all data)
- ❌ Single point of failure
- ❌ Can't work offline
- ❌ Violates "no servers" requirement

**Verdict**: ❌ Rejected (violates core requirement)

---

### Alternative 2: Full PAKE (SPAKE2+)

**Approach**: Use proper PAKE protocol for pairing

**Pros**:
- ✅ Cryptographically secure
- ✅ Resistant to all attacks (including active MITM)
- ✅ Industry standard (WPA3, iCloud)

**Cons**:
- ❌ Go libraries immature ([jtejido/spake2plus](https://github.com/jtejido/spake2plus) is POC-quality)
- ❌ More complex implementation
- ❌ Still need TLS for ongoing encryption

**Verdict**: ⚠️ Future enhancement (Phase 4+)

---

### Alternative 3: QR Code Pairing

**Approach**: Device A shows QR code, Device B scans

**Pros**:
- ✅ Out-of-band channel (camera)
- ✅ Harder to MITM (attacker needs camera access)
- ✅ User-friendly (just scan)
- ✅ Can encode cert fingerprint

**Cons**:
- ❌ Requires camera access
- ❌ Platform-specific (camera APIs)
- ❌ Doesn't work for CLI-only devices

**Verdict**: ⚠️ Future enhancement (Phase 5+)

**Comparison**:
```
Pairing Code:        QR Code:               NFC:
┌─────────┐         ┌─────────┐           ┌─────────┐
│ 123-456 │         │ █▀▀▀▀█ │           │  )))    │
│         │   vs    │ █ ▄ ▄█ │    vs     │ (Device)│
│ Type it │         │ ▀▀▀▀▀▀ │           │  Tap    │
└─────────┘         └─────────┘           └─────────┘
 Typing             Camera                Hardware
```

---

### Alternative 4: Pre-Shared Keys

**Approach**: User types long secret on both devices

**Pros**:
- ✅ Simple to implement
- ✅ No pairing protocol needed

**Cons**:
- ❌ Terrible UX (typing 32-char secret)
- ❌ Prone to errors
- ❌ Users will choose weak secrets

**Verdict**: ❌ Rejected (poor UX)

---

## References & Further Reading

### PAKE Protocols
- [Wikipedia: PAKE](https://en.wikipedia.org/wiki/Password-authenticated_key_agreement)
- [SPAKE2+ Specification](https://chris-wood.github.io/draft-bar-cfrg-spake2plus/draft-bar-cfrg-spake2plus.html)
- [TLS 1.3 PAKE Extension](https://datatracker.ietf.org/doc/html/draft-ietf-tls-pake-00)
- [Post-Quantum PAKE](https://datatracker.ietf.org/doc/draft-vos-cfrg-pqpake/)
- [PAKE Libraries in Go](https://github.com/topics/pake)

### TLS & Certificate Pinning
- [TLS 1.3 RFC](https://datatracker.ietf.org/doc/html/rfc8446)
- [Mutual TLS (mTLS) in Go](https://github.com/nicholasjackson/mtls-go-example)
- [Certificate Pinning Best Practices](https://owasp.org/www-community/controls/Certificate_and_Public_Key_Pinning)

### Device Pairing
- [Bluetooth Secure Simple Pairing](https://pmc.ncbi.nlm.nih.gov/articles/PMC6427610/)
- [Out-of-Band Authentication](https://link.springer.com/article/10.1007/s42979-019-0018-8)
- [OAuth Device Authorization](https://datatracker.ietf.org/doc/draft-ietf-oauth-cross-device-security/)

### Industry Examples
- **Syncthing**: Uses device IDs + TLS certificate verification
- **Resilio Sync**: Uses pre-shared keys + AES-256
- **Signal**: Uses safety numbers (cert fingerprints)
- **WhatsApp**: Uses QR code pairing
- **Apple AirDrop**: Uses BLE + WiFi + Apple ID verification

---

## Appendix: Cryptography Primer

### Elliptic Curve Diffie-Hellman (ECDH)

Used by TLS for key exchange:

```
Alice                         Bob
  ↓                           ↓
a = random()                b = random()
A = g^a (public)            B = g^b (public)
  ↓                           ↓
  ────────── A ────────────>
  <───────── B ──────────────
  ↓                           ↓
K = B^a                     K = A^b
  = (g^b)^a                   = (g^a)^b
  = g^(ab)                    = g^(ab)
  ↓                           ↓
Shared secret: K = K (same!)
```

**Properties**:
- Attacker sees: g, A, B
- Attacker does NOT see: a, b, K
- Computing K from (g, A, B) is hard (discrete log problem)

### Ed25519 Signatures

Used for TLS certificates:

```
Private key: d (scalar)
Public key:  P = d * G (point on curve)

Sign message m:
  r = random()
  R = r * G
  s = r + SHA512(R || P || m) * d
  Signature = (R, s)

Verify:
  s * G = R + SHA512(R || P || m) * P
  (checks math works out)
```

**Properties**:
- Small keys (32 bytes)
- Fast (faster than RSA)
- Secure (128-bit security level)

### AEAD Ciphers

Used by TLS for encryption:

**ChaCha20-Poly1305**:
- ChaCha20: Stream cipher (encrypt/decrypt)
- Poly1305: MAC (authentication)
- AEAD: Authenticated Encryption with Associated Data

```
Encrypt(key, nonce, plaintext, associated_data):
  ciphertext = ChaCha20(key, nonce, plaintext)
  tag = Poly1305(key, ciphertext || associated_data)
  return (ciphertext, tag)

Decrypt(key, nonce, ciphertext, tag, associated_data):
  check: Poly1305(key, ciphertext || associated_data) == tag
  if not: return ERROR (tampered!)
  plaintext = ChaCha20(key, nonce, ciphertext)
  return plaintext
```

**Properties**:
- ✅ Confidentiality (encryption)
- ✅ Integrity (authentication)
- ✅ Efficient (fast on all platforms)

---

## Conclusion

**Current Implementation (Phase 2)**:
- Pairing codes (simplified PAKE)
- Certificate fingerprint exchange
- Shared secret exchange
- ⚠️ Vulnerable to active MITM during pairing

**After Phase 3 (TLS)**:
- All traffic encrypted
- Mutual certificate verification
- ✅ Secure against passive eavesdropping
- ✅ Secure against MITM during sync
- ⚠️ Still vulnerable to active MITM during pairing (without user verification)

**Future Enhancements (Phase 4+)**:
- Full PAKE protocol (SPAKE2+)
- QR code pairing (out-of-band channel)
- Encrypted secrets (OS keychain)
- → Complete security against all threats

**Trade-offs**:
- Simplicity vs Security: Pairing codes are simpler but less secure than full PAKE
- UX vs Security: Pairing codes are easier than manual fingerprint verification
- Platform support: Works on all platforms (unlike QR codes/NFC)

**Recommendation**: Current approach (pairing codes + TLS) provides **good-enough security** for personal todo app. For higher security needs, implement Phase 4 enhancements.
