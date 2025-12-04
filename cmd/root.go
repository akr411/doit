package main

import (
	"fmt"
	"net"
	"os"

	"github.com/akr411/doit/internal/storage"
	"github.com/akr411/doit/internal/sync"
	"github.com/akr411/doit/internal/ui"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var store *storage.Storage
var syncEngine *sync.SyncEngine
var isTTY bool

var rootCmd = &cobra.Command{
	Use:   "doit",
	Short: "Todo CLI with P2P sync",
	Long:  "Personal todo manager. Works offline, syncs automatically.",
	Example: `  doit add -t "Buy milk" -d "2h"
  doit complete 1
  doit list --json`,
	SilenceErrors: true,
	SilenceUsage:  false,
	RunE: func(cmd *cobra.Command, args []string) error {
		if isTTY {
			return runInteractive()
		}
		return listCmd.RunE(cmd, args)
	},
}

func init() {
	isTTY = isatty.IsTerminal(os.Stdout.Fd())

	var err error
	store, err = storage.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to initialize storage: %v\n", err)
		os.Exit(1)
	}

	if err := store.CleanupOldCompleted(); err != nil {
		ui.PrintWarning("Warning: failed to cleanup old completed tasks: %v", err)
	}

	if store.ShouldRunCleanup() {
		_, err := store.CleanupSyncData(false)
		if err != nil {
			ui.PrintWarning("Warning: failed to cleanup sync data: %v", err)
		}
	}

	if sync.IsSyncEnabled(store.GetDB()) && !isDaemonRunning() {
		syncEngine, err = sync.NewSyncEngine(store)
		if err != nil {
			ui.PrintWarning("Warning: failed to create sync engine: %v", err)
		} else {
			err = syncEngine.Start()
			if err != nil {
				ui.PrintWarning("Warning: failed to start sync engine: %v", err)
			}
		}
	}
}

func isDaemonRunning() bool {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 49151})
	if err != nil {
		return true
	}
	conn.Close()
	return false
}

func Execute() error {
	defer func() {
		if syncEngine != nil && syncEngine.IsRunning() {
			syncEngine.Stop()
		}
	}()
	return rootCmd.Execute()
}
