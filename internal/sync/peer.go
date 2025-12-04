package sync

import (
	"sync"

	"github.com/akr411/doit/internal/storage"
)

type Peer = storage.Peer
type SyncState = storage.SyncState

type PeerManager struct {
	store       *storage.Storage
	mu          sync.RWMutex
	activePeers map[string]*Peer
}

func NewPeerManager(store *storage.Storage) *PeerManager {
	return &PeerManager{
		store:       store,
		activePeers: make(map[string]*Peer),
	}
}

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

func (pm *PeerManager) GetPeer(id string) (*Peer, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	peer, ok := pm.activePeers[id]
	return peer, ok
}

func (pm *PeerManager) AddOrUpdatePeer(peer *Peer) error {
	if err := pm.store.AddOrUpdatePeer(peer); err != nil {
		return err
	}

	if peer.Status == "connected" || peer.Status == "syncing" {
		pm.mu.Lock()
		pm.activePeers[peer.ID] = peer
		pm.mu.Unlock()
	}

	return nil
}

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

func (pm *PeerManager) GetActivePeersList() []*Peer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	peers := make([]*Peer, 0, len(pm.activePeers))
	for _, peer := range pm.activePeers {
		peers = append(peers, peer)
	}

	return peers
}
