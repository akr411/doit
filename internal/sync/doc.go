// Package sync implements a CRDT-based P2P synchronization system with TLS encryption.
//
// The sync package provides conflict-free replicated data types (CRDTs) using
// Last-Write-Wins (LWW) conflict resolution for distributed todo synchronization.
//
// # Key Components
//
// Operations: CRDT operations (CREATE, UPDATE, COMPLETE, DELETE)
//
// Pairing: Device pairing with 6-digit codes and TLS certificate exchange
//
// Protocol: Pull/Push sync over HTTPS with mTLS authentication
//
// Discovery: UDP broadcast for local network peer discovery
//
// TLS: Ed25519 certificates with SHA256 fingerprint pinning
//
// # Security
//
// All traffic encrypted with TLS 1.3, mutual authentication via certificates,
// pairing codes expire in 15 minutes.
package sync
