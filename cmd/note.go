package main

import (
	"fmt"

	"github.com/akr411/doit/internal/ui"
	"github.com/akr411/doit/internal/utils"
	"github.com/spf13/cobra"
)

var noteCmd = &cobra.Command{
	Use:     "note <id>",
	Short:   "View the full note of a todo",
	Args:    cobra.ExactArgs(1),
	Example: `  doit note 1`,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := utils.ResolveID(store, args[0])
		if err != nil {
			return err
		}

		todo, err := store.GetTodo(id)
		if err != nil {
			return fmt.Errorf("todo not found: %s", args[0])
		}

		if todo.Note == "" {
			fmt.Println(ui.MutedStyle.Render("No note for this todo"))
			return nil
		}

		fmt.Println(ui.SelectedStyle.Render(todo.Task))
		fmt.Println(todo.Note)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(noteCmd)
}
