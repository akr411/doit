package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/akr411/doit/internal/retry"
	"github.com/akr411/doit/internal/storage"
)

var ErrUnauthorized = errors.New("unauthorized")

var syncHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:          100,
		MaxConnsPerHost:       20,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	},
}

type SyncClient struct {
	client *http.Client
	store  *storage.Storage
}

type PeerState struct {
	LastOperationID string `json:"last_operation_id"`
	DeviceID        string `json:"device_id"`
	DeviceName      string `json:"device_name"`
}

func NewSyncClient(store *storage.Storage) (*SyncClient, error) {
	certMgr := NewCertificateManager(store.GetDB())
	tlsConfig, err := certMgr.GetTLSConfig(false)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:       tlsConfig,
			MaxIdleConns:          100,
			MaxConnsPerHost:       20,
			MaxIdleConnsPerHost:   20,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
		},
	}

	return &SyncClient{
		client: client,
		store:  store,
	}, nil
}

func (sc *SyncClient) GetPeerState(peer *Peer) (*PeerState, error) {
	url := fmt.Sprintf("https://%s/sync/state", peer.Address)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := sc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get peer state: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("peer returned status %d", resp.StatusCode)
	}

	var state PeerState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return nil, fmt.Errorf("failed to decode peer state: %w", err)
	}

	return &state, nil
}

func (sc *SyncClient) PullOperations(peer *Peer, secret string, since string) ([]Operation, error) {
	url := fmt.Sprintf("https://%s/sync/operations?since=%s", peer.Address, since)

	var operations []Operation
	ctx := context.Background()
	err := sc.executeWithRetry(ctx, func() error {
		reqCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(reqCtx, "GET", url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("X-Doit-Secret", secret)

		resp, err := sc.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("pairing rejected: %w", ErrUnauthorized)
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("peer returned status %d", resp.StatusCode)
		}

		var response OperationsResponse
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return fmt.Errorf("failed to decode operations: %w", err)
		}

		operations = response.Operations
		return nil
	})

	if err != nil {
		return nil, err
	}

	return operations, nil
}

func (sc *SyncClient) PushOperations(peer *Peer, secret string, ops []Operation) error {
	if len(ops) == 0 {
		return nil
	}

	url := fmt.Sprintf("https://%s/sync/operations", peer.Address)

	request := OperationsRequest{Operations: ops}
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal operations: %w", err)
	}

	ctx := context.Background()
	return sc.executeWithRetry(ctx, func() error {
		reqCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(reqCtx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Doit-Secret", secret)

		resp, err := sc.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("pairing rejected: %w", ErrUnauthorized)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("peer returned status %d: %s", resp.StatusCode, string(body))
		}

		return nil
	})
}

func (sc *SyncClient) InitialSync(peer *Peer) error {
	// Get peer secret from database (must be paired first via `doit sync pair`)
	secret, err := sc.store.GetPeerSecret(peer.ID)
	if err != nil {
		return fmt.Errorf("not paired with %s. Pair devices using 'doit sync show' and 'doit sync pair'", peer.Name)
	}

	peerState, err := sc.GetPeerState(peer)
	if err != nil {
		return fmt.Errorf("failed to get peer state: %w", err)
	}

	syncState, err := sc.store.GetSyncState(peer.ID)
	if err != nil {
		return fmt.Errorf("failed to get sync state: %w", err)
	}

	ourLastOpID := syncState.LastOperationID

	newOps, err := sc.PullOperations(peer, secret, ourLastOpID)
	if err != nil {
		return fmt.Errorf("failed to pull operations: %w", err)
	}

	for _, op := range newOps {
		if err := op.Apply(sc.store.GetDB()); err != nil {
			log.Printf("Failed to apply operation %s: %v", op.ID, err)
		}
	}

	theirLastOpID := peerState.LastOperationID
	ops, err := sc.store.GetOperations(syncState.LastOperationID)
	if err != nil {
		return fmt.Errorf("failed to get operations to push: %w", err)
	}

	operations := make([]Operation, 0, len(ops))
	for _, opData := range ops {
		op := Operation{
			ID:        opData.ID,
			Type:      opData.Type,
			TodoID:    opData.TodoID,
			Data:      []byte(opData.Data),
			Timestamp: opData.Timestamp,
			DeviceID:  opData.DeviceID,
		}
		operations = append(operations, op)
	}

	if len(operations) > 0 {
		if err := sc.PushOperations(peer, secret, operations); err != nil {
			log.Printf("Failed to push operations to %s: %v", peer.Name, err)
		} else {
			opIDs := make([]string, len(operations))
			for i, op := range operations {
				opIDs[i] = op.ID
			}
			if err := sc.store.MarkOperationsSynced(opIDs); err != nil {
				log.Printf("Failed to mark operations synced: %v", err)
			}
		}
	}

	newSyncState := &SyncState{
		PeerID:             peer.ID,
		LastOperationID:    theirLastOpID,
		LastSyncTime:       time.Now().UnixNano(),
		OperationsSent:     int64(len(operations)),
		OperationsReceived: int64(len(newOps)),
	}

	if err := sc.store.UpdateSyncState(peer.ID, newSyncState); err != nil {
		return fmt.Errorf("failed to update sync state: %w", err)
	}

	return nil
}

func (sc *SyncClient) executeWithRetry(ctx context.Context, fn func() error) error {
	cfg := retry.Config{
		MaxAttempts: 7,
		InitialWait: 1 * time.Second,
		MaxWait:     60 * time.Second,
		Multiplier:  2.0,
	}

	err := retry.Do(ctx, cfg, fn)
	if errors.Is(err, ErrUnauthorized) {
		return err
	}
	return err
}
