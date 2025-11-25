package main

import (
	"fmt"
	"time"

	"github.com/akr411/doit/internal/ui"
	"github.com/akr411/doit/internal/utils"
	"github.com/spf13/cobra"
)

var completeCmd = &cobra.Command{
	Use:     "complete [id...]",
	Short:   "Mark todos as completed",
	Example: `  doit complete 1
  doit complete 1 2 3`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && isTTY {
			pageSize := getPageSize()
			return ui.RunInteractiveComplete(store, pageSize)
		}

		if len(args) == 0 {
			return fmt.Errorf("at least one id required")
		}

		for _, arg := range args {
			id, err := utils.ResolveID(store, arg)
			if err != nil {
				return err
			}

			todo, err := store.GetTodo(id)
			if err != nil {
				return fmt.Errorf("todo not found: %s", arg)
			}

			if err := store.CompleteTodo(id, !todo.Completed); err != nil {
				return err
			}

			if err := updateStreak(); err != nil {
				ui.PrintWarning("Warning: failed to update streak: %v", err)
			}
		}

		if err := store.CleanupOldCompleted(); err != nil {
			ui.PrintWarning("Warning: failed to cleanup old completed: %v", err)
		}

		if len(args) == 1 {
			ui.PrintSuccess("✓ Completed")
		} else {
			ui.PrintSuccess("✓ Completed %d todo(s)", len(args))
		}
		return nil
	},
}

func updateStreak() error {
	streaksEnabled, _ := store.GetConfig("streaks_enabled")
	if streaksEnabled == "false" {
		return nil
	}

	streak, err := store.GetStreak()
	if err != nil {
		return err
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()

	if streak.LastCompletedAt == 0 {
		streak.CurrentStreak = 1
		streak.MaxStreak = 1
	} else {
		lastDayTime := time.Unix(streak.LastCompletedAt, 0)
		lastDay := time.Date(lastDayTime.Year(), lastDayTime.Month(), lastDayTime.Day(), 0, 0, 0, 0, lastDayTime.Location()).Unix()

		daysDiff := (today - lastDay) / 86400

		if daysDiff == 0 {
		} else if daysDiff == 1 {
			streak.CurrentStreak++
			if streak.CurrentStreak > streak.MaxStreak {
				streak.MaxStreak = streak.CurrentStreak
			}
		} else {
			streak.CurrentStreak = 1
		}
	}

	streak.TotalCompleted++
	streak.LastCompletedAt = now.Unix()

	return store.UpdateStreak(streak)
}

func init() {
	rootCmd.AddCommand(completeCmd)
}
