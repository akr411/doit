package sync

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/akr411/doit/internal/storage"
)

type SyncEngine struct {
	store     *storage.Storage
	discovery *LocalDiscovery
	server    *SyncServer
	client    *SyncClient
	peerMgr   *PeerManager
	stopCh    chan struct{}
	mu        sync.RWMutex
	running   bool
}

func NewSyncEngine(store *storage.Storage) (*SyncEngine, error) {
	peerMgr := NewPeerManager(store)

	discovery := NewLocalDiscovery(store)

	server, err := NewSyncServer(store, peerMgr)
	if err != nil {
		return nil, fmt.Errorf("failed to create sync server: %w", err)
	}

	client, err := NewSyncClient(store)
	if err != nil {
		return nil, fmt.Errorf("failed to create sync client: %w", err)
	}

	return &SyncEngine{
		store:     store,
		discovery: discovery,
		server:    server,
		client:    client,
		peerMgr:   peerMgr,
		stopCh:    make(chan struct{}),
	}, nil
}

func (se *SyncEngine) Start() error {
	se.mu.Lock()
	if se.running {
		se.mu.Unlock()
		return fmt.Errorf("sync engine already running")
	}
	se.running = true
	se.mu.Unlock()

	if err := se.peerMgr.LoadActivePeers(); err != nil {
		log.Printf("Failed to load active peers: %v", err)
	}

	port := GetSyncPort(se.store.GetDB())

	if err := se.server.Start(port); err != nil {
		se.running = false
		return fmt.Errorf("failed to start server: %w", err)
	}

	actualPort := se.server.GetPort()
	if err := se.discovery.Start(actualPort); err != nil {
		se.server.Stop()
		se.running = false
		return fmt.Errorf("failed to start discovery: %w", err)
	}

	go se.syncLoop()

	return nil
}

func (se *SyncEngine) Stop() error {
	se.mu.Lock()
	if !se.running {
		se.mu.Unlock()
		return nil
	}
	se.mu.Unlock()

	close(se.stopCh)

	se.mu.Lock()
	se.running = false
	se.mu.Unlock()

	if err := se.discovery.Stop(); err != nil {
		log.Printf("Failed to stop discovery: %v", err)
	}

	if err := se.server.Stop(); err != nil {
		log.Printf("Failed to stop server: %v", err)
	}

	return nil
}

func (se *SyncEngine) IsRunning() bool {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.running
}

func (se *SyncEngine) syncLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			se.syncWithAllPeers()
		case <-se.stopCh:
			return
		}
	}
}

func (se *SyncEngine) syncWithAllPeers() {
	peers := se.peerMgr.GetActivePeersList()

	for _, peer := range peers {
		go se.syncWithPeer(peer)
	}
}

func (se *SyncEngine) syncWithPeer(peer *Peer) {
	timeSinceLastSeen := time.Since(time.Unix(0, peer.LastSeen))
	if timeSinceLastSeen > 5*time.Minute {
		if err := se.peerMgr.UpdatePeerStatus(peer.ID, "disconnected"); err != nil {
			log.Printf("Failed to update peer status for %s: %v", peer.Name, err)
		}
		return
	}

	if err := se.peerMgr.UpdatePeerStatus(peer.ID, "syncing"); err != nil {
		log.Printf("Failed to update peer status for %s: %v", peer.Name, err)
		return
	}

	if err := se.client.InitialSync(peer); err != nil {
		log.Printf("Failed to sync with %s: %v", peer.Name, err)
		se.peerMgr.UpdatePeerStatus(peer.ID, "disconnected")
		return
	}

	if err := se.peerMgr.UpdatePeerStatus(peer.ID, "connected"); err != nil {
		log.Printf("Failed to update peer status for %s: %v", peer.Name, err)
	}

	if err := se.store.UpdatePeerLastSeen(peer.ID, time.Now().UnixNano()); err != nil {
		log.Printf("Failed to update peer last_seen for %s: %v", peer.Name, err)
	}
}
