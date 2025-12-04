//go:build !linux

package sync

import (
	"fmt"
	"log"
	"strings"

	"github.com/hashicorp/mdns"
)

func (d *DiscoveryService) discover() {
	ourID, err := GetDeviceID(d.store.GetDB())
	if err != nil {
		log.Printf("Failed to get device ID: %v", err)
		return
	}

	log.Printf("[DEBUG] Discovery (mdns) starting: ourID=%s", ourID)

	entriesCh := make(chan *mdns.ServiceEntry, 10)

	go func() {
		for entry := range entriesCh {
			log.Printf("[DEBUG] Found: %s at %v:%d, TXT=%v",
				entry.Name, entry.AddrV4, entry.Port, entry.InfoFields)

			deviceID := extractFromTxt(entry.InfoFields, "device_id")
			if deviceID == "" {
				log.Printf("[WARN] Empty device_id, skipping")
				continue
			}

			if deviceID == ourID {
				log.Printf("[DEBUG] Self-discovery, skipping")
				continue
			}

			if entry.AddrV4 == nil {
				log.Printf("[WARN] No IPv4 address for %s", entry.Name)
				continue
			}

			deviceName := extractFromTxt(entry.InfoFields, "name")
			if deviceName == "" {
				deviceName = entry.Name
			}

			peer := &Peer{
				ID:        deviceID,
				Name:      deviceName,
				Address:   fmt.Sprintf("%s:%d", entry.AddrV4.String(), entry.Port),
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

	if err := mdns.Lookup(ServiceName, entriesCh); err != nil {
		log.Printf("Lookup failed: %v", err)
	}

	close(entriesCh)

	log.Printf("[DEBUG] Discovery complete")
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
