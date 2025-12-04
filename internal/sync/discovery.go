package sync

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
)

const (
	ServiceName = "_doit._tcp"
	MDNSTimeout = 3 * time.Second
)

type DiscoveryService struct {
	server  *mdns.Server
	store   StorageInterface
	stopCh  chan struct{}
	mu      sync.Mutex
	running bool
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
		return fmt.Errorf("failed to get device ID: %w", err)
	}

	deviceName := GetDeviceName()

	service, err := mdns.NewMDNSService(
		deviceName,
		ServiceName,
		"",
		"",
		port,
		nil,
		[]string{
			"v=1",
			"device_id=" + deviceID,
			"name=" + deviceName,
		},
	)
	if err != nil {
		d.running = false
		return fmt.Errorf("failed to create mDNS service: %w", err)
	}

	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		d.running = false
		return fmt.Errorf("failed to create mDNS server: %w", err)
	}

	d.server = server

	go d.discoveryLoop()

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

	close(d.stopCh)

	if d.server != nil {
		if err := d.server.Shutdown(); err != nil {
			return fmt.Errorf("failed to shutdown mDNS server: %w", err)
		}
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
	entriesCh := make(chan *mdns.ServiceEntry, 10)

	ourID, err := GetDeviceID(d.store.GetDB())
	if err != nil {
		log.Printf("Failed to get device ID: %v", err)
		return
	}

	go func() {
		for entry := range entriesCh {
			deviceID := extractDeviceID(entry.InfoFields)
			if deviceID == "" {
				continue
			}

			if deviceID == ourID {
				continue
			}

			peer := &Peer{
				ID:        deviceID,
				Name:      extractDeviceName(entry.InfoFields),
				Address:   fmt.Sprintf("%s:%d", entry.AddrV4.String(), entry.Port),
				LastSeen:  time.Now().UnixNano(),
				Status:    "discovered",
				CreatedAt: time.Now().UnixNano(),
			}

			if err := d.store.AddOrUpdatePeer(peer); err != nil {
				log.Printf("Failed to add/update peer %s: %v", peer.Name, err)
			}
		}
	}()

	params := mdns.DefaultParams(ServiceName)
	params.Timeout = MDNSTimeout
	params.Entries = entriesCh

	if err := mdns.Query(params); err != nil {
		log.Printf("mDNS query failed: %v", err)
	}

	close(entriesCh)
}

func extractDeviceID(infoFields []string) string {
	for _, field := range infoFields {
		if len(field) > 10 && field[:10] == "device_id=" {
			return field[10:]
		}
	}
	return ""
}

func extractDeviceName(infoFields []string) string {
	for _, field := range infoFields {
		if len(field) > 5 && field[:5] == "name=" {
			return field[5:]
		}
	}
	return "unknown"
}
