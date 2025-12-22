package main

import (
	"database/sql"
	"fmt"

	"github.com/akr411/doit/internal/sync"
	"github.com/akr411/doit/internal/ui"
	"github.com/spf13/cobra"
)

var repairCmd = &cobra.Command{
	Use:   "repair",
	Short: "Database repair and recovery",
	Long:  "Tools for checking database integrity and recovering from corruption",
}

var repairCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check database integrity",
	Long: `Verify database integrity and CRDT consistency.

This performs:
- SQLite integrity check (PRAGMA integrity_check)
- Schema validation (all required tables exist)
- CRDT consistency check (todos match operation log)
- Foreign key validation
- Index validation

Read-only operation - safe to run anytime.`,
	RunE: runRepairCheck,
}

var repairRebuildCmd = &cobra.Command{
	Use:   "rebuild",
	Short: "Rebuild state from operations",
	Long: `Rebuild todos table from operation log using CRDT.

WARNING: This will:
1. Delete ALL todos
2. Replay all operations in timestamp order
3. Reconstruct state from scratch

Use this if:
- CRDT state diverged from operations
- Suspected data corruption
- After manual database edits

Backup your database first!`,
	RunE: runRepairRebuild,
}

var repairResetSyncCmd = &cobra.Command{
	Use:   "reset-sync",
	Short: "Reset sync state",
	Long: `Clear all sync-related data (peers, operations, pairing).

This removes:
- All paired devices
- Shared secrets and certificates
- Sync operations
- Pairing codes
- Sync state

Keeps:
- Todos (your actual data)
- Streaks and statistics
- Configuration (except sync settings)

Useful for:
- Starting sync from scratch
- Resolving sync conflicts
- Testing sync setup`,
	RunE: runRepairResetSync,
}

var repairFixCmd = &cobra.Command{
	Use:   "fix",
	Short: "Auto-fix common issues",
	Long: `Automatically repair common database issues.

This fixes:
- Orphaned operations (operations without corresponding todos)
- Missing timestamps on todos

Does NOT fix:
- SQLite corruption (requires database rebuild)
- Foreign key violations (may require manual intervention)
- Invalid JSON in operations (requires manual review)

Safe to run - only fixes clearly recoverable issues.`,
	RunE: runRepairFix,
}

func runRepairCheck(cmd *cobra.Command, args []string) error {
	fmt.Println("=== Database Integrity Check ===")
	fmt.Println()

	result, err := store.CheckIntegrity()
	if err != nil {
		ui.PrintError("Check failed: %v", err)
		return err
	}

	db := store.GetDB()

	fmt.Print("1. SQLite integrity... ")
	if result.SQLiteOK {
		ui.PrintSuccess("OK")
	} else {
		ui.PrintError("FAILED")
	}

	fmt.Print("2. Schema validation... ")
	requiredTables := []string{
		"todos", "operations", "peers", "sync_state",
		"peer_secrets", "peer_certificates", "pairing_codes",
		"config", "streaks",
	}
	schemaOK := true
	for _, table := range requiredTables {
		var exists int
		err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists)
		if err != nil || exists == 0 {
			schemaOK = false
			break
		}
	}
	if schemaOK {
		ui.PrintSuccess("OK")
	} else {
		ui.PrintError("FAILED: missing tables")
	}

	fmt.Print("3. Foreign keys... ")
	if result.ForeignKeysOK {
		ui.PrintSuccess("OK")
	} else {
		ui.PrintError("FAILED")
	}

	fmt.Print("4. Data consistency... ")
	dataIssues := result.OrphanedOps + result.InvalidJSON + result.DuplicateTodos + result.MissingTimestamps
	if dataIssues == 0 {
		ui.PrintSuccess("OK")
	} else {
		ui.PrintWarning("%d issues found", dataIssues)
	}

	if sync.IsSyncEnabled(db) {
		fmt.Print("5. CRDT consistency... ")
		if err := checkCRDTConsistency(db); err != nil {
			ui.PrintWarning("WARNING: %v", err)
		} else {
			ui.PrintSuccess("OK")
		}
	}

	fmt.Println()

	if len(result.Errors) > 0 {
		ui.PrintWarning("Issues found:")
		for _, e := range result.Errors {
			fmt.Printf("  - %s\n", e)
		}
		fmt.Println()
		fmt.Println("Run 'doit repair fix' to auto-fix recoverable issues")
		return nil
	}

	ui.PrintSuccess("✓ Database healthy")
	return nil
}

func runRepairFix(cmd *cobra.Command, args []string) error {
	fmt.Println("=== Auto-Fix Database Issues ===")
	fmt.Println()

	result, err := store.CheckIntegrity()
	if err != nil {
		return fmt.Errorf("failed to check integrity: %w", err)
	}

	if !result.SQLiteOK {
		ui.PrintError("SQLite corruption detected - cannot auto-fix")
		fmt.Println("Try: sqlite3 ~/.local/share/doit/doit.db \".recover\" | sqlite3 doit-recovered.db")
		return fmt.Errorf("sqlite corruption")
	}

	fixable := result.OrphanedOps + result.MissingTimestamps
	if fixable == 0 {
		ui.PrintSuccess("No auto-fixable issues found")
		return nil
	}

	fmt.Printf("Found %d fixable issues:\n", fixable)
	if result.OrphanedOps > 0 {
		fmt.Printf("  - %d orphaned operations\n", result.OrphanedOps)
	}
	if result.MissingTimestamps > 0 {
		fmt.Printf("  - %d todos with missing timestamps\n", result.MissingTimestamps)
	}
	fmt.Println()

	confirmed, err := ui.Confirm("Fix these issues?")
	if err != nil {
		return err
	}
	if !confirmed {
		ui.PrintError("Fix cancelled")
		return nil
	}

	fixed, err := store.RepairIntegrity()
	if err != nil {
		return fmt.Errorf("repair failed: %w", err)
	}

	ui.PrintSuccess("✓ Fixed %d issues", fixed)
	return nil
}

func checkCRDTConsistency(db *sql.DB) error {
	var todoCount int
	err := db.QueryRow("SELECT COUNT(*) FROM todos WHERE deleted=0").Scan(&todoCount)
	if err != nil {
		return fmt.Errorf("failed to count todos: %w", err)
	}

	var opCount int
	err = db.QueryRow(`
		SELECT COUNT(DISTINCT todo_id)
		FROM operations
		WHERE type IN ('CREATE', 'UPDATE', 'COMPLETE')
		AND todo_id NOT IN (
			SELECT todo_id FROM operations WHERE type='DELETE'
		)
	`).Scan(&opCount)
	if err != nil {
		return fmt.Errorf("failed to count operations: %w", err)
	}

	if todoCount != opCount {
		return fmt.Errorf("mismatch: %d todos vs %d operation todos", todoCount, opCount)
	}

	return nil
}

func runRepairRebuild(cmd *cobra.Command, args []string) error {
	if !sync.IsSyncEnabled(store.GetDB()) {
		return fmt.Errorf("sync not enabled (repair rebuild only works with sync enabled)")
	}

	ui.PrintWarning("⚠️  WARNING: This will delete all todos and rebuild from operations")
	ui.PrintWarning("⚠️  Backup your database first: cp ~/.local/share/doit/doit.db ~/doit-backup.db")
	fmt.Println()

	confirmed, err := ui.Confirm("Proceed with rebuild?")
	if err != nil {
		return err
	}
	if !confirmed {
		ui.PrintError("Rebuild cancelled")
		return nil
	}

	fmt.Println()
	fmt.Println("Rebuilding state from operations...")

	operationsData, err := store.GetAllOperations()
	if err != nil {
		return fmt.Errorf("failed to get operations: %w", err)
	}

	if len(operationsData) == 0 {
		return fmt.Errorf("no operations found (nothing to rebuild)")
	}

	fmt.Printf("Found %d operations to replay...\n", len(operationsData))

	operations := make([]*sync.Operation, len(operationsData))
	for i, opData := range operationsData {
		operations[i] = &sync.Operation{
			ID:        opData.ID,
			Type:      opData.Type,
			TodoID:    opData.TodoID,
			Data:      []byte(opData.Data),
			Timestamp: opData.Timestamp,
			DeviceID:  opData.DeviceID,
		}
	}

	if err := sync.RebuildState(store.GetDB(), operations); err != nil {
		return fmt.Errorf("rebuild failed: %w", err)
	}

	var todoCount int
	_ = store.GetDB().QueryRow("SELECT COUNT(*) FROM todos WHERE deleted=0").Scan(&todoCount)

	ui.PrintSuccess("✓ Rebuild complete: %d todos", todoCount)
	fmt.Println()
	fmt.Println("Verify with: doit list")
	return nil
}

func runRepairResetSync(cmd *cobra.Command, args []string) error {
	if !sync.IsSyncEnabled(store.GetDB()) {
		return fmt.Errorf("sync not enabled")
	}

	ui.PrintWarning("⚠️  WARNING: This will remove all sync data")
	fmt.Println()
	fmt.Println("This removes:")
	fmt.Println("  - All paired devices")
	fmt.Println("  - Shared secrets and certificates")
	fmt.Println("  - Sync operations")
	fmt.Println("  - Pairing codes")
	fmt.Println()
	fmt.Println("This keeps:")
	fmt.Println("  - Your todos")
	fmt.Println("  - Streaks and statistics")
	fmt.Println()

	confirmed, err := ui.Confirm("Reset sync state?")
	if err != nil {
		return err
	}
	if !confirmed {
		ui.PrintError("Reset cancelled")
		return nil
	}

	tx, err := store.GetDB().Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	tables := []string{
		"peer_secrets",
		"peer_certificates",
		"sync_state",
		"peers",
		"pairing_codes",
		"operations",
	}

	for _, table := range tables {
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s", table)); err != nil {
			return fmt.Errorf("failed to clear %s: %w", table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	ui.PrintSuccess("✓ Sync state reset")
	fmt.Println()
	fmt.Println("To resume sync:")
	fmt.Println("  doit sync init")
	return nil
}

func init() {
	rootCmd.AddCommand(repairCmd)
	repairCmd.AddCommand(repairCheckCmd)
	repairCmd.AddCommand(repairRebuildCmd)
	repairCmd.AddCommand(repairResetSyncCmd)
	repairCmd.AddCommand(repairFixCmd)
}
