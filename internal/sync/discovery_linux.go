//go:build linux

package sync

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/holoplot/go-avahi"
)

type avahiState struct {
	conn  *dbus.Conn
	server *avahi.Server
	group *avahi.EntryGroup
}

var avahiSvc *avahiState

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

	conn, err := dbus.SystemBus()
	if err != nil {
		d.running = false
		return fmt.Errorf("failed to connect to system bus: %w", err)
	}

	server, err := avahi.ServerNew(conn)
	if err != nil {
		d.running = false
		return fmt.Errorf("failed to create avahi server: %w", err)
	}

	group, err := server.EntryGroupNew()
	if err != nil {
		server.Close()
		d.running = false
		return fmt.Errorf("failed to create entry group: %w", err)
	}

	hostname, err := server.GetHostNameFqdn()
	if err != nil {
		server.Close()
		d.running = false
		return fmt.Errorf("failed to get hostname: %w", err)
	}

	txt := [][]byte{
		[]byte("v=1"),
		[]byte("device_id=" + deviceID),
		[]byte("name=" + deviceName),
	}

	err = group.AddService(avahi.InterfaceUnspec, avahi.ProtoUnspec, 0,
		instanceName, ServiceName, "local", hostname, uint16(port), txt)
	if err != nil {
		server.Close()
		d.running = false
		return fmt.Errorf("failed to add service: %w", err)
	}

	err = group.Commit()
	if err != nil {
		server.Close()
		d.running = false
		return fmt.Errorf("failed to commit entry group: %w", err)
	}

	state, err := group.GetState()
	if err != nil {
		log.Printf("[WARN] Failed to get entry group state: %v", err)
	} else {
		log.Printf("[DEBUG] EntryGroup state after commit: %d", state)
	}

	avahiSvc = &avahiState{
		conn:  conn,
		server: server,
		group: group,
	}

	go d.discoveryLoop()

	log.Printf("[INFO] Avahi service announced: %s on port %d (host=%s)", instanceName, port, hostname)

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

	if avahiSvc != nil {
		if avahiSvc.group != nil {
			avahiSvc.group.Reset()
		}
		if avahiSvc.server != nil {
			avahiSvc.server.Close()
		}
		avahiSvc = nil
	}

	return nil
}

func (d *DiscoveryService) discover() {
	ourID, err := GetDeviceID(d.store.GetDB())
	if err != nil {
		log.Printf("Failed to get device ID: %v", err)
		return
	}

	log.Printf("[DEBUG] Discovery (avahi) starting: ourID=%s", ourID)

	conn, err := dbus.SystemBus()
	if err != nil {
		log.Printf("Failed to connect to system bus: %v", err)
		return
	}

	server, err := avahi.ServerNew(conn)
	if err != nil {
		log.Printf("Failed to create avahi server: %v", err)
		return
	}
	defer server.Close()

	sb, err := server.ServiceBrowserNew(avahi.InterfaceUnspec, avahi.ProtoUnspec, "_doit._tcp", "local", 0)
	if err != nil {
		log.Printf("Failed to create service browser: %v", err)
		return
	}

	timeout := time.After(5 * time.Second)

	for {
		select {
		case svc := <-sb.AddChannel:
			log.Printf("[DEBUG] Service found: %s (type=%s)", svc.Name, svc.Type)

			go func(s avahi.Service) {
				sr, err := server.ServiceResolverNew(s.Interface, s.Protocol, s.Name, s.Type, s.Domain, avahi.ProtoUnspec, 0)
				if err != nil {
					log.Printf("Failed to create resolver: %v", err)
					return
				}

				select {
				case resolved := <-sr.FoundChannel:
					log.Printf("[DEBUG] Resolved: %s at %s:%d, TXT=%v", resolved.Name, resolved.Address, resolved.Port, resolved.Txt)

					txt := bytesToStrings(resolved.Txt)
					deviceID := extractFromTxt(txt, "device_id")
					if deviceID == "" {
						log.Printf("[WARN] Empty device_id in TXT, skipping")
						return
					}

					if deviceID == ourID {
						log.Printf("[DEBUG] Self-discovery, skipping")
						return
					}

					deviceName := extractFromTxt(txt, "name")
					if deviceName == "" {
						deviceName = resolved.Name
					}

					peer := &Peer{
						ID:        deviceID,
						Name:      deviceName,
						Address:   fmt.Sprintf("%s:%d", resolved.Address, resolved.Port),
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

				case <-time.After(2 * time.Second):
					log.Printf("[WARN] Resolver timeout for %s", s.Name)
				}
			}(svc)

		case svc := <-sb.RemoveChannel:
			log.Printf("[INFO] Service disappeared: %s", svc.Name)

		case <-timeout:
			log.Printf("[DEBUG] Discovery complete")
			return
		}
	}
}

func bytesToStrings(b [][]byte) []string {
	strs := make([]string, len(b))
	for i, bb := range b {
		strs[i] = string(bb)
	}
	return strs
}

func extractFromTxt(txt []string, key string) string {
	prefix := key + "="
	for _, field := range txt {
		if strings.HasPrefix(field, prefix) {
			return field[len(prefix):]
		}
	}
	return ""
}
