package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/akr411/doit/internal/logging"
	"github.com/akr411/doit/internal/storage"
	"github.com/akr411/doit/internal/sync"
	"github.com/akr411/doit/internal/ui"
	"github.com/akr411/doit/internal/utils"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Manage sync and cleanup",
	Long:  "Manage P2P synchronization and data cleanup",
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Clean up old sync data",
	Long: `Clean up old tombstones and synced operations.

This removes:
- Deleted todos (tombstones) older than retention period
- Synced operations older than retention period
- Excess operations (keeps last N per todo)

Safety: Only cleans data that has been synced and is older than retention period.`,
	Example: `  doit sync cleanup
  doit sync cleanup --dry-run
  doit sync cleanup --aggressive`,
	RunE: runCleanup,
}

var (
	dryRun     bool
	aggressive bool
)

func runCleanup(cmd *cobra.Command, args []string) error {
	if !sync.IsSyncEnabled(store.GetDB()) {
		return fmt.Errorf("sync is not enabled. Enable with: doit sync init")
	}

	if aggressive && !dryRun {
		ui.PrintWarning("⚠️  AGGRESSIVE CLEANUP WARNING")
		ui.PrintWarning("This will delete ALL synced operations and tombstones,")
		ui.PrintWarning("ignoring retention periods. This may cause data loss if")
		ui.PrintWarning("peers haven't synced recently.")
		fmt.Println()

		confirmed, err := ui.Confirm("Are you sure you want to proceed?")
		if err != nil || !confirmed {
			ui.PrintError("Cleanup cancelled")
			return nil
		}
	}

	if dryRun {
		stats, err := store.GetCleanupStats()
		if err != nil {
			return fmt.Errorf("failed to get cleanup stats: %w", err)
		}

		fmt.Println("=== Dry Run - No data will be deleted ===")
		fmt.Println()
		fmt.Printf("Current state:\n")
		fmt.Printf("  Total operations: %v\n", stats.TotalOperations)
		fmt.Printf("  Synced operations: %v\n", stats.SyncedOperations)
		fmt.Printf("  Unsynced operations: %v\n", stats.UnsyncedOperations)
		fmt.Printf("  Tombstones: %v\n", stats.Tombstones)
		fmt.Printf("  Database size: %v KB\n", stats.DBSizeKB)
		fmt.Println()
		fmt.Printf("Would delete:\n")
		fmt.Printf("  Cleanable operations: %v\n", stats.CleanableOperations)
		fmt.Printf("  Cleanable tombstones: %v\n", stats.CleanableTombstones)
		fmt.Println()
		ui.PrintSuccess("Run without --dry-run to actually clean")
		return nil
	}

	stats, err := store.CleanupSyncData(aggressive)
	if err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	if stats.OperationsDeleted == 0 && stats.TombstonesDeleted == 0 {
		ui.PrintSuccess("✓ Nothing to clean up")
		return nil
	}

	ui.PrintSuccess("✓ Cleanup complete:")
	if stats.OperationsDeleted > 0 {
		fmt.Printf("  Deleted %d operations\n", stats.OperationsDeleted)
	}
	if stats.TombstonesDeleted > 0 {
		fmt.Printf("  Deleted %d tombstones\n", stats.TombstonesDeleted)
	}

	return nil
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sync and cleanup status",
	Long:  "Display sync status, database statistics, and cleanup information",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !sync.IsSyncEnabled(store.GetDB()) {
			fmt.Println("Sync: disabled")
			fmt.Println()
			fmt.Println("Enable sync with: doit sync init")
			return nil
		}

		fmt.Println("Sync: enabled")
		fmt.Println()

		stats, err := store.GetCleanupStats()
		if err != nil {
			return fmt.Errorf("failed to get stats: %w", err)
		}

		fmt.Println("Database:")
		fmt.Printf("  Size: %v KB\n", stats.DBSizeKB)
		fmt.Println()

		fmt.Println("Operations:")
		fmt.Printf("  Total: %v\n", stats.TotalOperations)
		fmt.Printf("  Synced: %v\n", stats.SyncedOperations)
		fmt.Printf("  Unsynced: %v\n", stats.UnsyncedOperations)
		fmt.Printf("  Cleanable: %v\n", stats.CleanableOperations)
		fmt.Println()

		fmt.Println("Tombstones:")
		fmt.Printf("  Total: %v\n", stats.Tombstones)
		fmt.Printf("  Cleanable: %v\n", stats.CleanableTombstones)
		fmt.Println()

		cleanupEnabled, _ := store.GetConfig("auto_cleanup_enabled")
		if cleanupEnabled == "false" {
			fmt.Println("Auto-cleanup: disabled")
		} else {
			fmt.Println("Auto-cleanup: enabled")
			if stats.HoursSinceCleanup >= 0 {
				fmt.Printf("  Last run: %d hours ago\n", stats.HoursSinceCleanup)
			} else {
				fmt.Println("  Last run: never")
			}
		}

		return nil
	},
}

func runSyncInit(cmd *cobra.Command, args []string) error {
	if sync.IsSyncEnabled(store.GetDB()) {
		ui.PrintWarning("Sync already enabled")
		return nil
	}

	if err := sync.SetSyncEnabled(store.GetDB(), true); err != nil {
		return fmt.Errorf("failed to enable sync: %w", err)
	}

	deviceName := sync.GetDeviceName()
	port := sync.GetSyncPort(store.GetDB())

	var secret string
	err := store.GetDB().QueryRow("SELECT value FROM config WHERE key='shared_secret'").Scan(&secret)
	if err != nil {
		return fmt.Errorf("failed to get shared secret: %w", err)
	}

	deviceID, err := sync.GetDeviceID(store.GetDB())
	if err != nil {
		return fmt.Errorf("failed to get device ID: %w", err)
	}

	certMgr := sync.NewCertificateManager(store.GetDB())
	if err := certMgr.GenerateSelfSignedCert(deviceID); err != nil {
		return fmt.Errorf("failed to generate TLS cert: %w", err)
	}

	ui.PrintSuccess("✓ Sync enabled")
	fmt.Printf("Device: %s\n", deviceName)
	fmt.Printf("Port: %d (HTTPS), %d (UDP discovery)\n", port, 49151)
	fmt.Println()
	fmt.Printf("Shared secret: %s\n", secret)
	fmt.Println()
	fmt.Println("On other devices:")
	fmt.Printf("  1. doit sync init\n")
	fmt.Printf("  2. sqlite3 ~/.local/share/doit/doit.db \"UPDATE config SET value='%s' WHERE key='shared_secret'\"\n", secret)
	fmt.Printf("  3. doit sync daemon\n")

	return nil
}


func runSyncDisable(cmd *cobra.Command, args []string) error {
	_, err := store.GetDB().Exec("INSERT OR REPLACE INTO config (key, value) VALUES ('sync_enabled', 'false')")
	if err != nil {
		return fmt.Errorf("failed to disable sync: %w", err)
	}

	ui.PrintSuccess("✓ Sync disabled")
	fmt.Println("Note: Stop any running daemon manually (pkill doit)")
	return nil
}

func runSyncDevices(cmd *cobra.Command, args []string) error {
	if !sync.IsSyncEnabled(store.GetDB()) {
		return fmt.Errorf("sync not enabled. Run: doit sync init")
	}

	peers, err := store.GetPeers()
	if err != nil {
		return fmt.Errorf("failed to get peers: %w", err)
	}

	if len(peers) == 0 {
		fmt.Println("No devices discovered yet")
		return nil
	}

	fmt.Printf("%-36s %-20s %-25s %-15s\n", "ID", "Name", "Address", "Status")
	fmt.Println("────────────────────────────────────────────────────────────────────────────────────────────")

	for _, peer := range peers {
		lastSeen := time.Unix(0, peer.LastSeen)
		timeSince := time.Since(lastSeen)
		timeStr := fmt.Sprintf("(%s)", utils.FormatTimeSince(timeSince))

		fmt.Printf("%-36s %-20s %-25s %-15s %s\n",
			peer.ID[:8]+"...", peer.Name, peer.Address, peer.Status, timeStr)

		syncState, err := store.GetSyncState(peer.ID)
		if err == nil && syncState.LastSyncTime > 0 {
			lastSync := time.Unix(0, syncState.LastSyncTime)
			syncTimeStr := utils.FormatTimeSince(time.Since(lastSync))

			fmt.Printf("  Last sync: %s | Sent: %d ops | Received: %d ops\n",
				syncTimeStr, syncState.OperationsSent, syncState.OperationsReceived)
		}
	}

	return nil
}

func runSyncDaemon(cmd *cobra.Command, args []string) error {
	if !sync.IsSyncEnabled(store.GetDB()) {
		return fmt.Errorf("sync not enabled. Run: doit sync init")
	}

	dataDir, err := storage.GetDataDir()
	if err != nil {
		return fmt.Errorf("failed to get data directory: %w", err)
	}

	if err := logging.Init(dataDir, logging.INFO); err != nil {
		return fmt.Errorf("failed to initialize logging: %w", err)
	}
	defer logging.Close()

	pidFile := filepath.Join(dataDir, "doit-sync.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer func() { _ = os.Remove(pidFile) }()

	engine, err := sync.NewSyncEngine(store)
	if err != nil {
		return fmt.Errorf("failed to create sync engine: %w", err)
	}

	if err := engine.Start(); err != nil {
		return fmt.Errorf("failed to start sync engine: %w", err)
	}

	ui.PrintSuccess("✓ Sync daemon running (Ctrl+C to stop)")
	logging.Info("Sync daemon started")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	<-sigCh
	logging.Info("Shutdown signal received, stopping gracefully")
	ui.PrintSuccess("\n✓ Sync daemon stopped")

	return nil
}

func runSyncShow(cmd *cobra.Command, args []string) error {
	if !sync.IsSyncEnabled(store.GetDB()) {
		return fmt.Errorf("sync not enabled. Run: doit sync init")
	}

	var secret string
	err := store.GetDB().QueryRow("SELECT value FROM config WHERE key='shared_secret'").Scan(&secret)
	if err != nil {
		return fmt.Errorf("failed to get secret: %w", err)
	}

	pairingMgr := sync.NewPairingManager(store.GetDB(), secret)
	code, err := pairingMgr.GenerateCode()
	if err != nil {
		return fmt.Errorf("failed to generate code: %w", err)
	}

	deviceName := sync.GetDeviceName()

	certMgr := sync.NewCertificateManager(store.GetDB())
	fingerprint, err := certMgr.GetFingerprint()
	if err != nil {
		return fmt.Errorf("failed to get fingerprint: %w", err)
	}

	shortFingerprint := formatFingerprint(fingerprint[:32])

	fmt.Println()
	fmt.Printf("═══════════════════════════════════════════════════\n")
	fmt.Printf("  Pairing: %s\n", deviceName)
	fmt.Printf("═══════════════════════════════════════════════════\n")
	fmt.Println()
	fmt.Printf("  Code: %s\n", code.Code)
	fmt.Println()
	expiresIn := time.Until(time.Unix(0, code.ExpiresAt))
	fmt.Printf("  Expires: %dm %ds\n", int(expiresIn.Minutes()), int(expiresIn.Seconds())%60)
	fmt.Println()
	ui.PrintWarning("  SECURITY: Verify this fingerprint on pairing device:")
	fmt.Printf("  Fingerprint: %s\n", shortFingerprint)
	fmt.Println()
	fmt.Printf("On other device run:\n")
	fmt.Printf("  $ doit sync pair %s\n", code.Code)
	fmt.Println()
	fmt.Printf("═══════════════════════════════════════════════════\n")

	return nil
}

func formatFingerprint(fp string) string {
	var parts []string
	for i := 0; i < len(fp); i += 4 {
		end := i + 4
		if end > len(fp) {
			end = len(fp)
		}
		parts = append(parts, fp[i:end])
	}
	result := ""
	for i, p := range parts {
		if i > 0 && i%4 == 0 {
			result += "\n               "
		} else if i > 0 {
			result += " "
		}
		result += p
	}
	return result
}

func runSyncPair(cmd *cobra.Command, args []string) error {
	code := args[0]

	if !sync.IsSyncEnabled(store.GetDB()) {
		return fmt.Errorf("sync not enabled. Run: doit sync init")
	}

	peers, err := store.GetPeers()
	if err != nil {
		return fmt.Errorf("failed to get peers: %w", err)
	}

	if len(peers) == 0 {
		return fmt.Errorf("no devices discovered. Ensure both devices are on same network")
	}

	deviceID, _ := sync.GetDeviceID(store.GetDB())
	deviceName := sync.GetDeviceName()

	certMgr := sync.NewCertificateManager(store.GetDB())
	ourFingerprint, err := certMgr.GetFingerprint()
	if err != nil {
		return fmt.Errorf("failed to get fingerprint: %w", err)
	}

	var paired bool
	for _, peer := range peers {
		if pairErr := pairWithPeer(peer, code, deviceID, deviceName, ourFingerprint, certMgr); pairErr != nil {
			ui.PrintWarning("Failed to pair with %s: %v", peer.Name, pairErr)
			continue
		}
		ui.PrintSuccess("✓ Paired with %s", peer.Name)
		paired = true
	}

	if !paired {
		return fmt.Errorf("failed to pair. Check code and try again")
	}

	return nil
}

func pairWithPeer(peer *storage.Peer, code, deviceID, deviceName, ourFingerprint string, certMgr *sync.CertificateManager) error {
	fmt.Printf("Pairing with %s...\n", peer.Name)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	reqData := map[string]string{
		"pairing_code":     code,
		"device_id":        deviceID,
		"device_name":      deviceName,
		"cert_fingerprint": ourFingerprint,
	}

	body, _ := json.Marshal(reqData)
	url := fmt.Sprintf("https://%s/sync/pair", peer.Address)

	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("rate limited, try again later")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rejected (status %d)", resp.StatusCode)
	}

	var pairResp struct {
		SharedSecret    string `json:"shared_secret"`
		DeviceID        string `json:"device_id"`
		DeviceName      string `json:"device_name"`
		CertFingerprint string `json:"cert_fingerprint"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&pairResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if pairResp.CertFingerprint != "" && resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		tlsFingerprint := sync.ComputeCertFingerprint(resp.TLS.PeerCertificates[0])
		if tlsFingerprint != pairResp.CertFingerprint {
			return fmt.Errorf("certificate fingerprint mismatch (possible MITM attack)")
		}
	}

	if pairResp.CertFingerprint == "" {
		return fmt.Errorf("peer did not provide certificate fingerprint")
	}

	shortFingerprint := formatFingerprint(pairResp.CertFingerprint[:32])
	fmt.Println()
	ui.PrintWarning("SECURITY VERIFICATION REQUIRED")
	fmt.Printf("Peer fingerprint from %s:\n", pairResp.DeviceName)
	fmt.Printf("  %s\n", shortFingerprint)
	fmt.Println()
	fmt.Println("Compare this with the fingerprint shown on the other device.")
	fmt.Println("If they match, the connection is secure.")
	fmt.Println()

	confirmed, err := ui.Confirm("Does this fingerprint match the other device?")
	if err != nil {
		return fmt.Errorf("failed to get confirmation: %w", err)
	}
	if !confirmed {
		ui.PrintError("Pairing cancelled - fingerprint not verified")
		ui.PrintError("This may indicate a man-in-the-middle attack!")
		return fmt.Errorf("fingerprint verification failed")
	}

	if err := store.SavePeerSecret(peer.ID, pairResp.SharedSecret); err != nil {
		return fmt.Errorf("failed to save secret: %w", err)
	}

	if err := certMgr.SavePeerCertificate(peer.ID, pairResp.CertFingerprint); err != nil {
		return fmt.Errorf("failed to save cert: %w", err)
	}

	return nil
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Enable sync",
	Long:  "Enable P2P synchronization (runs automatically during any doit command)",
	RunE:  runSyncInit,
}

var disableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable sync",
	Long:  "Disable P2P synchronization",
	RunE:  runSyncDisable,
}

var devicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List paired devices",
	Long:  "Show all discovered and paired devices",
	RunE:  runSyncDevices,
}

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run sync engine in foreground",
	Long:  "Start sync engine and keep it running (for testing/development)",
	RunE:  runSyncDaemon,
}

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show pairing code",
	Long:  "Generate and display pairing code for this device",
	RunE:  runSyncShow,
}

var pairCmd = &cobra.Command{
	Use:   "pair <code>",
	Short: "Pair with device",
	Long:  "Pair with another device using pairing code",
	Args:  cobra.ExactArgs(1),
	RunE:  runSyncPair,
}

var unpairCmd = &cobra.Command{
	Use:   "unpair <device-id>",
	Short: "Unpair a device",
	Long:  "Remove pairing with a device (removes shared secrets and certificates)",
	Args:  cobra.ExactArgs(1),
	RunE:  runSyncUnpair,
}

func runSyncUnpair(cmd *cobra.Command, args []string) error {
	if !sync.IsSyncEnabled(store.GetDB()) {
		return fmt.Errorf("sync not enabled")
	}

	deviceID := args[0]

	peers, err := store.GetPeers()
	if err != nil {
		return fmt.Errorf("failed to get peers: %w", err)
	}

	var matchedPeer *storage.Peer
	for _, peer := range peers {
		if peer.ID == deviceID || peer.ID[:8] == deviceID {
			matchedPeer = peer
			break
		}
	}

	if matchedPeer == nil {
		return fmt.Errorf("device not found: %s", deviceID)
	}

	confirmed, err := ui.Confirm(fmt.Sprintf("Unpair device '%s'? This will remove sync data.", matchedPeer.Name))
	if err != nil {
		return err
	}
	if !confirmed {
		ui.PrintError("Unpair cancelled")
		return nil
	}

	tx, err := store.GetDB().Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("DELETE FROM peer_secrets WHERE peer_id=?", matchedPeer.ID); err != nil {
		return fmt.Errorf("failed to delete peer secret: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM peer_certificates WHERE device_id=?", matchedPeer.ID); err != nil {
		return fmt.Errorf("failed to delete peer certificate: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM sync_state WHERE peer_id=?", matchedPeer.ID); err != nil {
		return fmt.Errorf("failed to delete sync state: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM peers WHERE id=?", matchedPeer.ID); err != nil {
		return fmt.Errorf("failed to delete peer: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	ui.PrintSuccess("✓ Unpaired device '%s'", matchedPeer.Name)
	return nil
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.AddCommand(cleanupCmd)
	syncCmd.AddCommand(statusCmd)
	syncCmd.AddCommand(initCmd)
	syncCmd.AddCommand(disableCmd)
	syncCmd.AddCommand(devicesCmd)
	syncCmd.AddCommand(daemonCmd)
	syncCmd.AddCommand(showCmd)
	syncCmd.AddCommand(pairCmd)
	syncCmd.AddCommand(unpairCmd)

	cleanupCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be deleted without deleting")
	cleanupCmd.Flags().BoolVar(&aggressive, "aggressive", false, "Delete all synced data ignoring retention periods (DANGEROUS)")
}
