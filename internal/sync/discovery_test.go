package sync

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/libp2p/zeroconf/v2"
)

func TestZeroconfBrowse(t *testing.T) {
	entries := make(chan *zeroconf.ServiceEntry)

	go func() {
		for entry := range entries {
			log.Printf("Found: %s at %v:%d, TXT=%v", entry.Instance, entry.AddrIPv4, entry.Port, entry.Text)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := zeroconf.Browse(ctx, "_doit._tcp", "local.", entries)
	if err != nil {
		t.Fatalf("Failed to browse: %v", err)
	}

	<-ctx.Done()
	t.Log("Browse complete")
}
