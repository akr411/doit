package main

import (
	"fmt"
	"os"

	"github.com/akr411/doit/internal/storage"
	"github.com/akr411/doit/internal/ui"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var store *storage.Storage
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
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
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

}

func Execute() error {
	return rootCmd.Execute()
}
