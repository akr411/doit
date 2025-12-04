package sync

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/brutella/dnssd"
)

const (
	ServiceType = "_doit._tcp"
)

type DiscoveryService struct {
	responder dnssd.Responder
	handle    dnssd.ServiceHandle
	ctx       context.Context
	cancel    context.CancelFunc
	store     StorageInterface
	stopCh    chan struct{}
	mu        sync.Mutex
	running   bool
}

type StorageInterface interface {
	GetDB() *sql.DB
	AddOrUpdatePeer(peer *Peer) error
}

func NewDiscoveryService(store StorageInterface) *DiscoveryService {
	return &DiscoveryService{
		store:  store,
		stopCh: make(chan struct{}),
	}
}

func (d *DiscoveryService) Start(port int) error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return fmt.Errorf("discovery service already running")
	}
	d.running = true
	d.mu.Unlock()

	deviceID, err := GetDeviceID(d.store.GetDB())
	if err != nil {
		d.running = false
		return fmt.Errorf("failed to get device ID: %w", err)
	}

	deviceName := GetDeviceName()

	cfg := dnssd.Config{
		Name: deviceName,
		Type: ServiceType,
		Port: port,
		Text: map[string]string{
			"v":         "1",
			"device_id": deviceID,
			"name":      deviceName,
		},
	}

	sv, err := dnssd.NewService(cfg)
	if err != nil {
		d.running = false
		return fmt.Errorf("failed to create mDNS service: %w", err)
	}

	rp, err := dnssd.NewResponder()
	if err != nil {
		d.running = false
		return fmt.Errorf("failed to create mDNS responder: %w", err)
	}

	hdl, err := rp.Add(sv)
	if err != nil {
		d.running = false
		return fmt.Errorf("failed to add service: %w", err)
	}

	d.responder = rp
	d.handle = hdl
	d.ctx, d.cancel = context.WithCancel(context.Background())

	go func() {
		if err := rp.Respond(d.ctx); err != nil && err != context.Canceled {
			log.Printf("mDNS responder error: %v", err)
		}
	}()

	go d.discoveryLoop()

	log.Printf("[INFO] mDNS service announced: %s on port %d", deviceName, port)

	return nil
}

func (d *DiscoveryService) Stop() error {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return nil
	}
	d.running = false
	d.mu.Unlock()

	select {
	case <-d.stopCh:
	default:
		close(d.stopCh)
	}

	if d.cancel != nil {
		d.cancel()
	}

	return nil
}

func (d *DiscoveryService) discoveryLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	d.discover()

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
	ourID, err := GetDeviceID(d.store.GetDB())
	if err != nil {
		log.Printf("Failed to get device ID: %v", err)
		return
	}

	log.Printf("[DEBUG] Discovery starting: ourID=%s", ourID)
	entriesFound := 0

	addFunc := func(entry dnssd.BrowseEntry) {
		entriesFound++
		log.Printf("[DEBUG] Entry #%d: Name=%s, IPs=%v, Port=%d, Text=%v",
			entriesFound, entry.Name, entry.IPs, entry.Port, entry.Text)

		deviceID, ok := entry.Text["device_id"]
		if !ok || deviceID == "" {
			log.Printf("[WARN] Empty or missing device_id in TXT, skipping entry %s", entry.Name)
			return
		}

		log.Printf("[DEBUG] Extracted deviceID='%s'", deviceID)

		if deviceID == ourID {
			log.Printf("[DEBUG] Self-discovery (deviceID=%s), skipping", deviceID)
			return
		}

		if len(entry.IPs) == 0 {
			log.Printf("[WARN] No IPs for peer %s", entry.Name)
			return
		}

		addr := entry.IPs[0].String()
		deviceName, ok := entry.Text["name"]
		if !ok {
			deviceName = entry.Name
		}

		peer := &Peer{
			ID:        deviceID,
			Name:      deviceName,
			Address:   fmt.Sprintf("%s:%d", addr, entry.Port),
			LastSeen:  time.Now().UnixNano(),
			Status:    "discovered",
			CreatedAt: time.Now().UnixNano(),
		}

		log.Printf("[DEBUG] Adding peer: ID=%s, Name=%s, Address=%s", peer.ID, peer.Name, peer.Address)

		if err := d.store.AddOrUpdatePeer(peer); err != nil {
			log.Printf("Failed to add/update peer %s: %v", peer.Name, err)
		} else {
			log.Printf("[INFO] Successfully discovered peer: %s (%s)", peer.Name, peer.Address)
		}
	}

	rmvFunc := func(entry dnssd.BrowseEntry) {
		log.Printf("[INFO] Peer disappeared: %s", entry.Name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := dnssd.LookupType(ctx, ServiceType, addFunc, rmvFunc); err != nil {
		log.Printf("mDNS lookup failed: %v", err)
	}

	log.Printf("[DEBUG] Discovery complete: processed %d entries", entriesFound)
}
