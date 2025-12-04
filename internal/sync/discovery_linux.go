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

func (d *DiscoveryService) discover() {
	ourID, err := GetDeviceID(d.store.GetDB())
	if err != nil {
		log.Printf("Failed to get device ID: %v", err)
		return
	}

	log.Printf("[DEBUG] Discovery (avahi): ourID=%s", ourID)

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

	timeout := time.After(3 * time.Second)

	for {
		select {
		case svc := <-sb.AddChannel:
			log.Printf("[DEBUG] Service found: %s (type=%s)", svc.Name, svc.Type)

			sr, err := server.ServiceResolverNew(svc.Interface, svc.Protocol, svc.Name, svc.Type, svc.Domain, avahi.ProtoUnspec, 0)
			if err != nil {
				log.Printf("Failed to create resolver: %v", err)
				continue
			}

			select {
			case resolved := <-sr.FoundChannel:
				log.Printf("[DEBUG] Resolved: %s at %s:%d, TXT=%v", resolved.Name, resolved.Address, resolved.Port, resolved.Txt)

				txt := bytesToStrings(resolved.Txt)
				deviceID := extractDeviceIDFromTxt(txt)
				if deviceID == "" {
					log.Printf("[WARN] Empty device_id in TXT, skipping")
						continue
				}

				if deviceID == ourID {
					log.Printf("[DEBUG] Self-discovery, skipping")
						continue
				}

				deviceName := extractDeviceNameFromTxt(txt)
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
					log.Printf("[INFO] Discovered peer via avahi: %s (%s)", peer.Name, peer.Address)
				}


			case <-time.After(2 * time.Second):
				log.Printf("[WARN] Resolver timeout for %s", svc.Name)
			}

		case svc := <-sb.RemoveChannel:
			log.Printf("[INFO] Service disappeared: %s", svc.Name)

		case <-timeout:
			log.Printf("[DEBUG] Discovery complete (avahi)")
			return
		}
	}
}

func extractDeviceIDFromTxt(txt []string) string {
	for _, field := range txt {
		if strings.HasPrefix(field, "device_id=") {
			return strings.TrimPrefix(field, "device_id=")
		}
	}
	return ""
}

func bytesToStrings(b [][]byte) []string {
	result := make([]string, len(b))
	for i, bytes := range b {
		result[i] = string(bytes)
	}
	return result
}

func extractDeviceNameFromTxt(txt []string) string {
	for _, field := range txt {
		if strings.HasPrefix(field, "name=") {
			return strings.TrimPrefix(field, "name=")
		}
	}
	return ""
}
