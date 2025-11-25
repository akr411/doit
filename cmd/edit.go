package main

import (
	"fmt"

	"github.com/akr411/doit/internal/models"
	"github.com/akr411/doit/internal/ui"
	"github.com/akr411/doit/internal/utils"
	"github.com/spf13/cobra"
)

var (
	editTask     string
	editNote     string
	editDeadline string
)

var editCmd = &cobra.Command{
	Use:   "edit [id]",
	Short: "Edit a todo",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var todo *models.Todo

		if len(args) == 0 && isTTY {
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
			list, err := ui.RunSelectionList(todos, ui.SingleSelect, pageSize, "Select a todo to edit", "↑/↓: navigate • enter: select • space: note • esc: cancel")
			if err != nil {
				return err
			}
			todo = list.GetSelectedTodo()
			if todo == nil {
				return nil
			}
		} else if len(args) == 1 {
			id, err := utils.ResolveID(store, args[0])
			if err != nil {
				return err
			}
			todo, err = store.GetTodo(id)
			if err != nil {
				return fmt.Errorf("todo not found: %w", err)
			}
		} else {
			return fmt.Errorf("id required")
		}

		if !cmd.Flags().Changed("task") && !cmd.Flags().Changed("note") && !cmd.Flags().Changed("deadline") && isTTY {
			editedTodo, err := ui.RunTodoForm(todo)
			if err != nil {
				return err
			}
			if editedTodo == nil {
				return nil
			}
			if err := store.UpdateTodo(editedTodo); err != nil {
				return err
			}
			ui.PrintSuccess("✓ Updated")
			return nil
		}

		if todo == nil {
			return fmt.Errorf("todo not found")
		}

		if editTask != "" {
			if err := utils.ValidateTask(editTask); err != nil {
				return err
			}
			todo.Task = editTask
		}
		if cmd.Flags().Changed("note") {
			note := utils.ProcessNoteInput(editNote)
			if err := utils.ValidateNote(note); err != nil {
				return err
			}
			todo.Note = note
		}
		if editDeadline != "" {
			if err := utils.ValidateDeadline(editDeadline); err != nil {
				return err
			}
			deadline, err := utils.ParseDeadline(editDeadline)
			if err != nil {
				return err
			}
			todo.Deadline = deadline
		}

		if err := store.UpdateTodo(todo); err != nil {
			return err
		}

		ui.PrintSuccess("✓ Updated")
		return nil
	},
}

func init() {
	editCmd.Flags().StringVarP(&editTask, "task", "t", "", "Update task description")
	editCmd.Flags().StringVarP(&editNote, "note", "n", "", "Update note")
	editCmd.Flags().StringVarP(&editDeadline, "deadline", "d", "", "Update deadline")
	rootCmd.AddCommand(editCmd)
}
