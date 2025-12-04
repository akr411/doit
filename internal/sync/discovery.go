package sync

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

const (
	ServiceName = "_doit._tcp"
)

type DiscoveryService struct {
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


func getLocalIPs() ([]net.IP, error) {
	ifaces, err := getActiveInterfaces()
	if err != nil {
		return nil, err
	}

	var ips []net.IP
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil {
					ips = append(ips, ipnet.IP)
				}
			}
		}
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no non-loopback IPv4 addresses found on active interfaces")
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
		if !hasValidIPv4(iface) {
			continue
		}
		log.Printf("[DEBUG] Using interface: %s", iface.Name)
		active = append(active, iface)
	}

	if len(active) == 0 {
		return nil, fmt.Errorf("no active network interfaces found")
	}

	return active, nil
}

func hasValidIPv4(iface net.Interface) bool {
	addrs, err := iface.Addrs()
	if err != nil {
		return false
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			if ipnet.IP.To4() != nil && !ipnet.IP.IsLoopback() {
				return true
			}
		}
	}
	return false
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
