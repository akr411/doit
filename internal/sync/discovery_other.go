//go:build !linux

package sync

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

const MDNSTimeout = 3 * time.Second

func (d *DiscoveryService) discover() {
	ourID, err := GetDeviceID(d.store.GetDB())
	if err != nil {
		log.Printf("Failed to get device ID: %v", err)
		return
	}

	log.Printf("[DEBUG] Discovery (mdns): ourID=%s", ourID)

	entriesCh := make(chan *mdns.ServiceEntry, 10)

	go func() {
		for entry := range entriesCh {
			log.Printf("[DEBUG] Found: %s at %v:%d, TXT=%v", entry.Name, entry.AddrV4, entry.Port, entry.InfoFields)

			deviceID := extractDeviceID(entry.InfoFields)
			if deviceID == "" {
				log.Printf("[WARN] Empty device_id, skipping")
				continue
			}

			if deviceID == ourID {
				log.Printf("[DEBUG] Self-discovery, skipping")
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

			log.Printf("[DEBUG] Adding peer: %s (%s)", peer.Name, peer.Address)

			if err := d.store.AddOrUpdatePeer(peer); err != nil {
				log.Printf("Failed to add peer: %v", err)
			} else {
				log.Printf("[INFO] Discovered peer via mdns: %s (%s)", peer.Name, peer.Address)
			}
		}
	}()

	params := mdns.DefaultParams("_doit._tcp")
	params.Timeout = MDNSTimeout
	params.Entries = entriesCh

	if err := mdns.Query(params); err != nil {
		log.Printf("mDNS query failed: %v", err)
	}

	close(entriesCh)

	log.Printf("[DEBUG] Discovery complete (mdns)")
}

func extractDeviceID(infoFields []string) string {
	for _, field := range infoFields {
		if strings.HasPrefix(field, "device_id=") {
			return strings.TrimPrefix(field, "device_id=")
		}
	}
	return ""
}

func extractDeviceName(infoFields []string) string {
	for _, field := range infoFields {
		if strings.HasPrefix(field, "name=") {
			return strings.TrimPrefix(field, "name=")
		}
	}
	return "unknown"
}
