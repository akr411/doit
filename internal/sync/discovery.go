package sync

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/mdns"
	"github.com/libp2p/zeroconf/v2"
)

const (
	ServiceName = "_doit._tcp"
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
		d.running = false
		return fmt.Errorf("failed to get device ID: %w", err)
	}

	deviceName := GetDeviceName()
	instanceName := "doit-" + deviceID[:8]

	ips, err := getLocalIPs()
	if err != nil {
		d.running = false
		return fmt.Errorf("failed to get local IPs: %w", err)
	}

	service, err := mdns.NewMDNSService(
		instanceName,
		ServiceName,
		"",
		"",
		port,
		ips,
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

	log.Printf("[INFO] mDNS service announced: %s on port %d", instanceName, port)

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
	ourID, err := GetDeviceID(d.store.GetDB())
	if err != nil {
		log.Printf("Failed to get device ID: %v", err)
		return
	}

	log.Printf("[DEBUG] Discovery starting: ourID=%s", ourID)

	ifaces, err := getActiveInterfaces()
	if err != nil {
		log.Printf("[ERROR] Failed to get active interfaces: %v", err)
		return
	}
	log.Printf("[DEBUG] Browsing on %d interface(s)", len(ifaces))

	entries := make(chan *zeroconf.ServiceEntry)

	go func() {
		for entry := range entries {
			log.Printf("[DEBUG] Found: %s at %v:%d, TXT=%v", entry.Instance, entry.AddrIPv4, entry.Port, entry.Text)

			deviceID := extractFromText(entry.Text, "device_id")
			if deviceID == "" {
				log.Printf("[WARN] Empty device_id, skipping")
				continue
			}

			if deviceID == ourID {
				log.Printf("[DEBUG] Self-discovery, skipping")
				continue
			}

			if len(entry.AddrIPv4) == 0 {
				log.Printf("[WARN] No IPv4 address for %s", entry.Instance)
				continue
			}

			deviceName := extractFromText(entry.Text, "name")
			if deviceName == "" {
				deviceName = entry.Instance
			}

			peer := &Peer{
				ID:        deviceID,
				Name:      deviceName,
				Address:   fmt.Sprintf("%s:%d", entry.AddrIPv4[0].String(), entry.Port),
				LastSeen:  time.Now().UnixNano(),
				Status:    "discovered",
				CreatedAt: time.Now().UnixNano(),
			}

			log.Printf("[DEBUG] Adding peer: %s (%s)", peer.Name, peer.Address)

			if err := d.store.AddOrUpdatePeer(peer); err != nil {
				log.Printf("Failed to add peer: %v", err)
			} else {
				log.Printf("[INFO] Discovered peer: %s (%s)", peer.Name, peer.Address)
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := zeroconf.Browse(ctx, ServiceName, "local.", entries,
		zeroconf.SelectIfaces(ifaces),
		zeroconf.SelectIPTraffic(zeroconf.IPv4),
	); err != nil {
		log.Printf("Browse failed: %v", err)
	}

	<-ctx.Done()

	log.Printf("[DEBUG] Discovery complete")
}

func extractFromText(text []string, key string) string {
	prefix := key + "="
	for _, field := range text {
		if len(field) > len(prefix) && field[:len(prefix)] == prefix {
			return field[len(prefix):]
		}
	}
	return ""
}

func getLocalIPs() ([]net.IP, error) {
	var ips []net.IP

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ips = append(ips, ipnet.IP)
			}
		}
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no non-loopback IPv4 addresses found")
	}

	return ips, nil
}

func getActiveInterfaces() ([]net.Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	var active []net.Interface
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagRunning == 0 {
			continue
		}
		if shouldSkipInterface(iface.Name) {
			continue
		}
		active = append(active, iface)
	}

	if len(active) == 0 {
		return nil, fmt.Errorf("no active network interfaces found")
	}

	return active, nil
}

func shouldSkipInterface(name string) bool {
	skipPrefixes := []string{
		"utun",   // VPN tunnels
		"bridge", // Bridge interfaces
		"awdl",   // Apple Wireless Direct Link
		"llw",    // Low latency WLAN
		"anpi",   // Apple virtual interfaces
		"ap",     // Access point interfaces
		"gif",    // Generic tunnel
		"stf",    // 6to4 tunnel
	}

	for _, prefix := range skipPrefixes {
		if len(name) >= len(prefix) && name[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
