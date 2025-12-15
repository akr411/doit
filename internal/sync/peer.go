package sync

import (
	"sync"

	"github.com/akr411/doit/internal/storage"
)

// Peer is an alias for storage.Peer representing a discovered or paired device.
type Peer = storage.Peer

// SyncState is an alias for storage.SyncState tracking sync progress with a peer.
type SyncState = storage.SyncState

// PeerManager maintains an in-memory cache of active peers for sync operations.
// It synchronizes with the database and provides thread-safe access to peer information.
type PeerManager struct {
	store       *storage.Storage
	mu          sync.RWMutex
	activePeers map[string]*Peer
}

// NewPeerManager creates a peer manager with an empty active peers cache.
func NewPeerManager(store *storage.Storage) *PeerManager {
	return &PeerManager{
		store:       store,
		activePeers: make(map[string]*Peer),
	}
}

// LoadActivePeers loads all active peers from database into memory cache.
// Active peers are those with status "discovered", "connected", or "syncing".
// Returns error if database query fails.
func (pm *PeerManager) LoadActivePeers() error {
	peers, err := pm.store.GetActivePeers()
	if err != nil {
		return err
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.activePeers = make(map[string]*Peer)
	for _, peer := range peers {
		pm.activePeers[peer.ID] = peer
	}

	return nil
}

// GetPeer retrieves a peer from the active peers cache by ID.
// Returns the peer and true if found, nil and false otherwise.
// Thread-safe via read lock.
func (pm *PeerManager) GetPeer(id string) (*Peer, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	peer, ok := pm.activePeers[id]
	return peer, ok
}

// AddOrUpdatePeer adds or updates a peer in both database and memory cache.
// Peers with status "discovered", "connected", or "syncing" are cached in memory.
// Returns error if database update fails.
func (pm *PeerManager) AddOrUpdatePeer(peer *Peer) error {
	if err := pm.store.AddOrUpdatePeer(peer); err != nil {
		return err
	}

	if peer.Status == "discovered" || peer.Status == "connected" || peer.Status == "syncing" {
		pm.mu.Lock()
		pm.activePeers[peer.ID] = peer
		pm.mu.Unlock()
	}

	return nil
}

// UpdatePeerStatus changes a peer's status in database and updates the memory cache.
// Active statuses ("connected", "syncing") are kept in cache, others are removed.
// Returns error if database update fails. Thread-safe via write lock.
func (pm *PeerManager) UpdatePeerStatus(id string, status string) error {
	if err := pm.store.UpdatePeerStatus(id, status); err != nil {
		return err
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if status == "connected" || status == "syncing" {
		if peer, ok := pm.activePeers[id]; ok {
			peer.Status = status
		}
	} else {
		delete(pm.activePeers, id)
	}

	return nil
}

// GetActivePeersList returns a snapshot of all active peers from memory cache.
// Thread-safe via read lock. Order is not guaranteed.
func (pm *PeerManager) GetActivePeersList() []*Peer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	peers := make([]*Peer, 0, len(pm.activePeers))
	for _, peer := range pm.activePeers {
		peers = append(peers, peer)
	}

	return peers
}
