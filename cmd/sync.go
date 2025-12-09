package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/akr411/doit/internal/sync"
	"github.com/akr411/doit/internal/ui"
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
	var syncEnabled string
	err := store.GetDB().QueryRow("SELECT value FROM config WHERE key='sync_enabled'").Scan(&syncEnabled)
	if err != nil || syncEnabled != "true" {
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
		fmt.Printf("  Total operations: %v\n", stats["total_operations"])
		fmt.Printf("  Synced operations: %v\n", stats["synced_operations"])
		fmt.Printf("  Unsynced operations: %v\n", stats["unsynced_operations"])
		fmt.Printf("  Tombstones: %v\n", stats["tombstones"])
		fmt.Printf("  Database size: %v KB\n", stats["db_size_kb"])
		fmt.Println()
		fmt.Printf("Would delete:\n")
		fmt.Printf("  Cleanable operations: %v\n", stats["cleanable_operations"])
		fmt.Printf("  Cleanable tombstones: %v\n", stats["cleanable_tombstones"])
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
		var syncEnabled string
		err := store.GetDB().QueryRow("SELECT value FROM config WHERE key='sync_enabled'").Scan(&syncEnabled)
		if err != nil || syncEnabled != "true" {
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
		fmt.Printf("  Size: %v KB\n", stats["db_size_kb"])
		fmt.Println()

		fmt.Println("Operations:")
		fmt.Printf("  Total: %v\n", stats["total_operations"])
		fmt.Printf("  Synced: %v\n", stats["synced_operations"])
		fmt.Printf("  Unsynced: %v\n", stats["unsynced_operations"])
		fmt.Printf("  Cleanable: %v\n", stats["cleanable_operations"])
		fmt.Println()

		fmt.Println("Tombstones:")
		fmt.Printf("  Total: %v\n", stats["tombstones"])
		fmt.Printf("  Cleanable: %v\n", stats["cleanable_tombstones"])
		fmt.Println()

		cleanupEnabled, _ := store.GetConfig("auto_cleanup_enabled")
		if cleanupEnabled == "false" {
			fmt.Println("Auto-cleanup: disabled")
		} else {
			fmt.Println("Auto-cleanup: enabled")
			if hoursSince, ok := stats["hours_since_cleanup"].(int64); ok && hoursSince >= 0 {
				fmt.Printf("  Last run: %d hours ago\n", hoursSince)
			} else {
				fmt.Println("  Last run: never")
			}
		}

		return nil
	},
}

func runSyncInit(cmd *cobra.Command, args []string) error {
	var syncEnabled string
	err := store.GetDB().QueryRow("SELECT value FROM config WHERE key='sync_enabled'").Scan(&syncEnabled)
	if err == nil && syncEnabled == "true" {
		ui.PrintWarning("Sync already enabled")
		return nil
	}

	_, err = store.GetDB().Exec("INSERT OR REPLACE INTO config (key, value) VALUES ('sync_enabled', 'true')")
	if err != nil {
		return fmt.Errorf("failed to enable sync: %w", err)
	}

	deviceName := sync.GetDeviceName()
	port := sync.GetSyncPort(store.GetDB())

	var secret string
	err = store.GetDB().QueryRow("SELECT value FROM config WHERE key='shared_secret'").Scan(&secret)
	if err != nil {
		return fmt.Errorf("failed to get shared secret: %w", err)
	}

	ui.PrintSuccess("✓ Sync enabled")
	fmt.Printf("Device: %s\n", deviceName)
	fmt.Printf("Port: %d (HTTP), %d (UDP discovery)\n", port, 49151)
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
	var syncEnabled string
	err := store.GetDB().QueryRow("SELECT value FROM config WHERE key='sync_enabled'").Scan(&syncEnabled)
	if err != nil || syncEnabled != "true" {
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
		timeStr := ""

		if timeSince < time.Minute {
			timeStr = fmt.Sprintf("(%ds ago)", int(timeSince.Seconds()))
		} else if timeSince < time.Hour {
			timeStr = fmt.Sprintf("(%dm ago)", int(timeSince.Minutes()))
		} else if timeSince < 24*time.Hour {
			timeStr = fmt.Sprintf("(%dh ago)", int(timeSince.Hours()))
		} else {
			timeStr = fmt.Sprintf("(%dd ago)", int(timeSince.Hours()/24))
		}

		fmt.Printf("%-36s %-20s %-25s %-15s %s\n",
			peer.ID[:8]+"...", peer.Name, peer.Address, peer.Status, timeStr)

		syncState, err := store.GetSyncState(peer.ID)
		if err == nil && syncState.LastSyncTime > 0 {
			lastSync := time.Unix(0, syncState.LastSyncTime)
			syncTime := time.Since(lastSync)
			syncTimeStr := ""
			if syncTime < time.Minute {
				syncTimeStr = fmt.Sprintf("%ds ago", int(syncTime.Seconds()))
			} else if syncTime < time.Hour {
				syncTimeStr = fmt.Sprintf("%dm ago", int(syncTime.Minutes()))
			} else {
				syncTimeStr = fmt.Sprintf("%dh ago", int(syncTime.Hours()))
			}

			fmt.Printf("  Last sync: %s | Sent: %d ops | Received: %d ops\n",
				syncTimeStr, syncState.OperationsSent, syncState.OperationsReceived)
		}
	}

	return nil
}

func runSyncDaemon(cmd *cobra.Command, args []string) error {
	var syncEnabled string
	err := store.GetDB().QueryRow("SELECT value FROM config WHERE key='sync_enabled'").Scan(&syncEnabled)
	if err != nil || syncEnabled != "true" {
		return fmt.Errorf("sync not enabled. Run: doit sync init")
	}

	engine, err := sync.NewSyncEngine(store)
	if err != nil {
		return fmt.Errorf("failed to create sync engine: %w", err)
	}

	if err := engine.Start(); err != nil {
		return fmt.Errorf("failed to start sync engine: %w", err)
	}

	ui.PrintSuccess("✓ Sync daemon running (Ctrl+C to stop)")

	select {}
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

	fmt.Println()
	fmt.Printf("═══════════════════════════════════\n")
	fmt.Printf("  Pairing Code: %s\n", deviceName)
	fmt.Printf("═══════════════════════════════════\n")
	fmt.Println()
	fmt.Printf("  Code: %s\n", code.Code)
	fmt.Println()
	expiresIn := time.Unix(0, code.ExpiresAt).Sub(time.Now())
	fmt.Printf("  Expires: %dm %ds\n", int(expiresIn.Minutes()), int(expiresIn.Seconds())%60)
	fmt.Println()
	fmt.Printf("On other device run:\n")
	fmt.Printf("  $ doit sync pair %s\n", code.Code)
	fmt.Println()
	fmt.Printf("═══════════════════════════════════\n")

	return nil
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

	var paired bool
	for _, peer := range peers {
		fmt.Printf("Pairing with %s...\n", peer.Name)

		reqData := map[string]string{
			"pairing_code": code,
			"device_id":    deviceID,
			"device_name":  deviceName,
		}

		body, _ := json.Marshal(reqData)
		url := fmt.Sprintf("http://%s/sync/pair", peer.Address)

		resp, err := http.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			ui.PrintWarning("Failed: %v", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			ui.PrintWarning("Rejected (status %d)", resp.StatusCode)
			continue
		}

		var pairResp struct {
			SharedSecret string `json:"shared_secret"`
			DeviceID     string `json:"device_id"`
			DeviceName   string `json:"device_name"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&pairResp); err != nil {
			ui.PrintWarning("Failed to parse response: %v", err)
			continue
		}

		if err := store.SavePeerSecret(peer.ID, pairResp.SharedSecret); err != nil {
			ui.PrintWarning("Failed to save secret: %v", err)
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

	cleanupCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be deleted without deleting")
	cleanupCmd.Flags().BoolVar(&aggressive, "aggressive", false, "Delete all synced data ignoring retention periods (DANGEROUS)")
}
