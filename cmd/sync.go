package main

import (
	"fmt"

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

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.AddCommand(cleanupCmd)
	syncCmd.AddCommand(statusCmd)

	cleanupCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be deleted without deleting")
	cleanupCmd.Flags().BoolVar(&aggressive, "aggressive", false, "Delete all synced data ignoring retention periods (DANGEROUS)")
}
