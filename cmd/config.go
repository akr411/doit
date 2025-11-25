package main

import (
	"fmt"

	"github.com/akr411/doit/internal/ui"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configure doit settings",
}

var streaksCmd = &cobra.Command{
	Use:   "streaks <on|off>",
	Short: "Enable or disable streaks",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		value := args[0]
		if value != "on" && value != "off" {
			return fmt.Errorf("invalid value: must be 'on' or 'off'")
		}

		enabled := "true"
		if value == "off" {
			enabled = "false"
		}

		if err := store.SetConfig("streaks_enabled", enabled); err != nil {
			return err
		}

		ui.PrintSuccess("✓ Streaks %s", value)
		return nil
	},
}

var paginationCmd = &cobra.Command{
	Use:   "pagination <10|25|50>",
	Short: "Set pagination size",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		value := args[0]
		if value != "10" && value != "25" && value != "50" {
			return fmt.Errorf("invalid value: must be 10, 25, or 50")
		}

		if err := store.SetConfig("pagination_size", value); err != nil {
			return err
		}

		ui.PrintSuccess("✓ Pagination size set to %s", value)
		return nil
	},
}

var retentionCmd = &cobra.Command{
	Use:   "retention <10|25|50|100|200|500>",
	Short: "Set completed tasks retention limit",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		value := args[0]
		validValues := []string{"10", "25", "50", "100", "200", "500"}
		valid := false
		for _, v := range validValues {
			if value == v {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid value: must be 10, 25, 50, 100, 200, or 500")
		}

		if err := store.SetConfig("completed_limit", value); err != nil {
			return err
		}

		ui.PrintSuccess("✓ Completed tasks retention limit set to %s", value)
		return nil
	},
}

func init() {
	configCmd.AddCommand(streaksCmd)
	configCmd.AddCommand(paginationCmd)
	configCmd.AddCommand(retentionCmd)
	rootCmd.AddCommand(configCmd)
}
