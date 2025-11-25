package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show completion statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		streak, err := store.GetStreak()
		if err != nil {
			return err
		}

		fmt.Printf("Current streak: %d days\n", streak.CurrentStreak)
		fmt.Printf("Max streak: %d days\n", streak.MaxStreak)
		fmt.Printf("Total completed: %d\n", streak.TotalCompleted)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statsCmd)
}
