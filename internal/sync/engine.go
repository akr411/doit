package sync

import (
	"fmt"
	"sync"
	"time"

	"github.com/akr411/doit/internal/logging"
	"github.com/akr411/doit/internal/storage"
)

const maxConcurrentSyncs = 5

// SyncEngine coordinates all P2P synchronization components.
// It manages the sync server, client, local discovery, and peer management.
// The engine runs a background sync loop when started.
type SyncEngine struct {
	store     *storage.Storage
	discovery *LocalDiscovery
	server    *SyncServer
	client    *SyncClient
	peerMgr   *PeerManager
	stopCh    chan struct{}
	mu        sync.RWMutex
	running   bool
	syncSem   chan struct{}
	syncWg    sync.WaitGroup
}

// NewSyncEngine creates a sync engine with all required components.
// Initializes the sync server (HTTPS), client (with TLS), peer manager, and local discovery.
// Returns error if sync is not enabled or component initialization fails.
func NewSyncEngine(store *storage.Storage) (*SyncEngine, error) {
	peerMgr := NewPeerManager(store)

	discovery := NewLocalDiscovery(store, peerMgr)

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
		syncSem:   make(chan struct{}, maxConcurrentSyncs),
	}, nil
}

// Start launches the sync engine, starting the server, discovery, and sync loop.
// It loads active peers, starts the HTTPS server, begins UDP discovery, and runs
// the background sync loop. Returns error if already running or if any component fails to start.
func (se *SyncEngine) Start() error {
	se.mu.Lock()
	if se.running {
		se.mu.Unlock()
		return fmt.Errorf("sync engine already running")
	}
	se.running = true
	se.mu.Unlock()

	if err := se.peerMgr.LoadActivePeers(); err != nil {
		logging.Warn("Failed to load active peers: %v", err)
	}

	port := GetSyncPort(se.store.GetDB())

	if err := se.server.Start(port); err != nil {
		se.running = false
		return fmt.Errorf("failed to start server: %w", err)
	}

	actualPort := se.server.GetPort()
	if err := se.discovery.Start(actualPort); err != nil {
		_ = se.server.Stop()
		se.running = false
		return fmt.Errorf("failed to start discovery: %w", err)
	}

	go se.syncLoop()

	return nil
}

// Stop gracefully shuts down the sync engine.
// Stops the sync loop, waits for active syncs, then stops discovery and server.
// Safe to call multiple times - no-op if not running.
func (se *SyncEngine) Stop() error {
	se.mu.Lock()
	if !se.running {
		se.mu.Unlock()
		return nil
	}
	se.running = false
	close(se.stopCh)
	se.mu.Unlock()

	se.syncWg.Wait()

	if err := se.discovery.Stop(); err != nil {
		logging.Warn("Failed to stop discovery: %v", err)
	}

	if err := se.server.Stop(); err != nil {
		logging.Warn("Failed to stop server: %v", err)
	}

	return nil
}

// IsRunning returns true if the sync engine is currently running.
// Thread-safe check using read lock.
func (se *SyncEngine) IsRunning() bool {
	se.mu.RLock()
	defer se.mu.RUnlock()
	return se.running
}

func (se *SyncEngine) syncLoop() {
	ticker := time.NewTicker(3 * time.Second)
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
		select {
		case se.syncSem <- struct{}{}:
			se.syncWg.Add(1)
			go func(p *Peer) {
				defer func() {
					<-se.syncSem
					se.syncWg.Done()
				}()
				se.syncWithPeer(p)
			}(peer)
		case <-se.stopCh:
			return
		default:
		}
	}
}

func (se *SyncEngine) syncWithPeer(peer *Peer) {
	timeSinceLastSeen := time.Since(time.Unix(0, peer.LastSeen))
	if timeSinceLastSeen > 5*time.Minute {
		if err := se.peerMgr.UpdatePeerStatus(peer.ID, "disconnected"); err != nil {
			logging.Warn("Failed to update peer status for %s: %v", peer.Name, err)
		}
		return
	}

	if err := se.peerMgr.UpdatePeerStatus(peer.ID, "syncing"); err != nil {
		logging.Warn("Failed to update peer status for %s: %v", peer.Name, err)
		return
	}

	if err := se.client.InitialSync(peer); err != nil {
		logging.Debug("Failed to sync with %s: %v", peer.Name, err)
		_ = se.peerMgr.UpdatePeerStatus(peer.ID, "disconnected")
		return
	}

	if err := se.peerMgr.UpdatePeerStatus(peer.ID, "connected"); err != nil {
		logging.Warn("Failed to update peer status for %s: %v", peer.Name, err)
	}

	if err := se.store.UpdatePeerLastSeen(peer.ID, time.Now().UnixNano()); err != nil {
		logging.Warn("Failed to update peer last_seen for %s: %v", peer.Name, err)
	}
}
