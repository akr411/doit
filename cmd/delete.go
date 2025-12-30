package main

import (
	"fmt"

	"github.com/akr411/doit/internal/models"
	"github.com/akr411/doit/internal/ui"
	"github.com/akr411/doit/internal/utils"
	"github.com/spf13/cobra"
)

var (
	deleteAll       bool
	deleteCompleted bool
	deleteYes       bool
)

var deleteCmd = &cobra.Command{
	Use:     "delete [id...]",
	Short:   "Delete todos",
	Example: `  doit delete 1
  doit delete 1 2 3
  doit delete --completed
  doit delete --all -y`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if deleteAll {
			todos, err := store.GetAllTodos()
			if err != nil {
				return err
			}
			if !deleteYes {
				confirmed, err := ui.Confirm(fmt.Sprintf("Delete ALL %d todo(s)? This cannot be undone!", len(todos)))
				if err != nil {
					return err
				}
				if !confirmed {
					return nil
				}
			}
			for _, t := range todos {
				if err := store.DeleteTodo(t.ID); err != nil {
					return err
				}
			}
			ui.PrintSuccess("✓ Deleted all %d todo(s)", len(todos))
			return nil
		}

		if deleteCompleted {
			todos, err := store.GetAllTodos()
			if err != nil {
				return err
			}
			completedTodos := []*models.Todo{}
			for _, t := range todos {
				if t.Completed {
					completedTodos = append(completedTodos, t)
				}
			}
			if len(completedTodos) == 0 {
				fmt.Println("No completed todos to delete")
				return nil
			}
			if !deleteYes {
				confirmed, err := ui.Confirm(fmt.Sprintf("Delete %d completed todo(s)?", len(completedTodos)))
				if err != nil {
					return err
				}
				if !confirmed {
					return nil
				}
			}
			for _, t := range completedTodos {
				if err := store.DeleteTodo(t.ID); err != nil {
					return err
				}
			}
			ui.PrintSuccess("✓ Deleted %d completed todo(s)", len(completedTodos))
			return nil
		}

		if len(args) == 0 && isTTY && !deleteAll && !deleteCompleted {
			allTodos, err := store.GetAllTodos()
			if err != nil {
				return err
			}
			todos := []*models.Todo{}
			for _, t := range allTodos {
				if !t.Completed {
					todos = append(todos, t)
				}
			}
			pageSize := getPageSize()
			list, err := ui.RunSelectionList(todos, ui.MultiSelect, pageSize, "Select todos to delete", "↑/↓: navigate • x: select • enter: confirm • space: note • esc: cancel")
			if err != nil {
				return err
			}
			indices := list.GetSelectedIndices()
			if len(indices) == 0 {
				return nil
			}

			confirmed, err := ui.Confirm(fmt.Sprintf("Delete %d todo(s)?", len(indices)))
			if err != nil {
				return err
			}
			if !confirmed {
				return nil
			}

			for _, idx := range indices {
				todo := todos[idx]
				if err := store.DeleteTodo(todo.ID); err != nil {
					return err
				}
			}
			ui.PrintSuccess("✓ Deleted %d todo(s)", len(indices))
			return nil
		}

		if len(args) == 0 {
			return fmt.Errorf("at least one id required")
		}

		if !deleteYes {
			var message string
			if len(args) == 1 {
				message = "Delete this todo?"
			} else {
				message = fmt.Sprintf("Delete %d todo(s)?", len(args))
			}
			confirmed, err := ui.Confirm(message)
			if err != nil {
				return err
			}
			if !confirmed {
				return nil
			}
		}

		for _, arg := range args {
			id, err := utils.ResolveID(store, arg)
			if err != nil {
				return err
			}

			if _, err := store.GetTodo(id); err != nil {
				return fmt.Errorf("todo not found: %s", arg)
			}

			if err := store.DeleteTodo(id); err != nil {
				return err
			}
		}

		if len(args) == 1 {
			ui.PrintSuccess("✓ Deleted")
		} else {
			ui.PrintSuccess("✓ Deleted %d todo(s)", len(args))
		}
		return nil
	},
}

func init() {
	deleteCmd.Flags().BoolVarP(&deleteAll, "all", "a", false, "Delete all todos")
	deleteCmd.Flags().BoolVarP(&deleteCompleted, "completed", "c", false, "Delete completed todos")
	deleteCmd.Flags().BoolVarP(&deleteYes, "yes", "y", false, "Skip confirmation prompt")
	rootCmd.AddCommand(deleteCmd)
}
