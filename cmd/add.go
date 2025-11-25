package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/akr411/doit/internal/models"
	"github.com/akr411/doit/internal/ui"
	"github.com/akr411/doit/internal/utils"
	"github.com/spf13/cobra"
)

var (
	addTask     string
	addNote     string
	addDeadline string
)

var addCmd = &cobra.Command{
	Use:     "add",
	Short:   "Add a new todo",
	Example: `  doit add -t "Buy milk" -d "2h"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isTTY && addTask == "" {
			return fmt.Errorf("--task is required")
		}

		if isTTY && addTask == "" {
			todo, err := ui.RunTodoForm(nil)
			if err != nil {
				return err
			}
			if todo == nil {
				return nil
			}
			if err := store.SaveTodo(todo); err != nil {
				return err
			}
			ui.PrintSuccess("✓ Added")
			return nil
		}

		task := addTask
		if task == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read from stdin: %w", err)
			}
			task = strings.TrimSpace(string(data))
		}

		if err := utils.ValidateTask(task); err != nil {
			return err
		}

		note := utils.ProcessNoteInput(addNote)
		if err := utils.ValidateNote(note); err != nil {
			return err
		}

		if err := utils.ValidateDeadline(addDeadline); err != nil {
			return err
		}

		deadline, err := utils.ParseDeadline(addDeadline)
		if err != nil {
			return err
		}

		todo := models.NewTodo(task, note, deadline)
		if err := store.SaveTodo(todo); err != nil {
			return err
		}

		ui.PrintSuccess("✓ Added")
		return nil
	},
}

func init() {
	addCmd.Flags().StringVarP(&addTask, "task", "t", "", "Task description")
	addCmd.Flags().StringVarP(&addNote, "note", "n", "", "Optional note")
	addCmd.Flags().StringVarP(&addDeadline, "deadline", "d", "", "Deadline (e.g., 2h, 1d, 2025-12-01)")
	rootCmd.AddCommand(addCmd)
}
