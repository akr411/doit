package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/akr411/doit/internal/models"
	"github.com/akr411/doit/internal/ui"
	"github.com/akr411/doit/internal/utils"
	"github.com/spf13/cobra"
)

var (
	listJSON      bool
	listCompleted bool
	listPending   bool
	listQuiet     bool
	listAll       bool
	listPage      int
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all todos",
	RunE: func(cmd *cobra.Command, args []string) error {
		todos, err := store.GetAllTodos()
		if err != nil {
			return err
		}

		if listCompleted {
			filtered := make([]*models.Todo, 0)
			for _, t := range todos {
				if t.Completed {
					filtered = append(filtered, t)
				}
			}
			todos = filtered
		} else if listPending {
			filtered := make([]*models.Todo, 0)
			for _, t := range todos {
				if !t.Completed {
					filtered = append(filtered, t)
				}
			}
			todos = filtered
		}

		if listJSON {
			data, err := json.MarshalIndent(todos, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		}

		if listQuiet {
			for _, t := range todos {
				fmt.Println(t.ID)
			}
			return nil
		}

		// Validate page number
		if listPage < 0 {
			return fmt.Errorf("page number must be positive")
		}

		// Get page size from config
		pageSize := getPageSize()

		// If filters are applied, use simple pagination (no separation)
		if listCompleted || listPending {
			if listAll {
				for i := 0; i < len(todos); i++ {
					displayTodoAtIndex(todos[i], i+1)
				}
			} else {
				page := listPage
				if page == 0 {
					page = 1
				}

				totalPages := (len(todos) + pageSize - 1) / pageSize
				if totalPages == 0 {
					totalPages = 1
				}

				if page > totalPages {
					return fmt.Errorf("page %d out of range (1-%d)", page, totalPages)
				}

				startIdx := (page - 1) * pageSize
				endIdx := startIdx + pageSize
				if endIdx > len(todos) {
					endIdx = len(todos)
				}

				displayTodosRange(todos, startIdx, endIdx, 0)

				if listPage == 0 && totalPages > 1 {
					fmt.Printf("\n%s\n", ui.MutedStyle.Render(fmt.Sprintf("Page 1/%d (use --page/-p to see more or --all/-a to show all)", totalPages)))
				} else if listPage > 0 {
					fmt.Printf("\n%s\n", ui.MutedStyle.Render(fmt.Sprintf("Page %d/%d", page, totalPages)))
				}
			}
			return nil
		}

		// Separate pending and completed todos (like interactive mode)
		pendingTodos := []*models.Todo{}
		completedTodos := []*models.Todo{}
		for _, t := range todos {
			if t.Completed {
				completedTodos = append(completedTodos, t)
			} else {
				pendingTodos = append(pendingTodos, t)
			}
		}

		// Calculate pagination with separate pages for pending and completed
		pendingPages := (len(pendingTodos) + pageSize - 1) / pageSize
		if pendingPages == 0 {
			pendingPages = 1
		}
		completedPages := (len(completedTodos) + pageSize - 1) / pageSize
		totalPages := pendingPages + completedPages

		// Default to page 1 if not specified
		page := listPage
		if page == 0 {
			page = 1
		}

		if listAll {
			// Show all todos (pending first, then completed)
			for i := 0; i < len(pendingTodos); i++ {
				displayTodoAtIndex(pendingTodos[i], i+1)
			}
			for i := 0; i < len(completedTodos); i++ {
				displayTodoAtIndex(completedTodos[i], len(pendingTodos)+i+1)
			}
		} else {
			// Validate page range
			if page > totalPages {
				return fmt.Errorf("page %d out of range (1-%d)", page, totalPages)
			}

			// Determine if showing pending or completed page
			if page <= pendingPages {
				// Show pending page
				startIdx := (page - 1) * pageSize
				endIdx := startIdx + pageSize
				if endIdx > len(pendingTodos) {
					endIdx = len(pendingTodos)
				}
				displayTodosRange(pendingTodos, startIdx, endIdx, 0)
			} else {
				// Show completed page
				completedPageIdx := page - pendingPages - 1
				startIdx := completedPageIdx * pageSize
				endIdx := startIdx + pageSize
				if endIdx > len(completedTodos) {
					endIdx = len(completedTodos)
				}
				// Use global index offset
				displayTodosRange(completedTodos, startIdx, endIdx, len(pendingTodos))
			}

			// Show pagination info
			if listPage == 0 && totalPages > 1 {
				fmt.Printf("\n%s\n", ui.MutedStyle.Render(fmt.Sprintf("Page 1/%d (use --page/-p to see more or --all/-a to show all)", totalPages)))
			} else if listPage > 0 {
				fmt.Printf("\n%s\n", ui.MutedStyle.Render(fmt.Sprintf("Page %d/%d", page, totalPages)))
			}
		}

		return nil
	},
}

func displayTodoAtIndex(t *models.Todo, displayIndex int) {
	status := "[ ]"
	if t.Completed {
		status = "[x]"
	}

	line := fmt.Sprintf("%d. %s %s", displayIndex, status, t.Task)

	// Show deadline for pending tasks only
	if !t.Completed && t.Deadline > 0 {
		deadlineStr := utils.FormatDeadline(t.Deadline)
		line += fmt.Sprintf(" (%s)", deadlineStr)
	}

	fmt.Println(line)

	// Show truncated note if present
	if t.Note != "" {
		truncated := t.Note
		if len(truncated) > 50 {
			truncated = truncated[:47] + "..."
		}
		// Replace newlines with space for truncated display
		truncated = strings.ReplaceAll(truncated, "\n", " ")
		fmt.Printf("  %s %s\n", ui.MutedStyle.Render("│"), ui.MutedStyle.Render(truncated))
	}
}

func displayTodosRange(todos []*models.Todo, startIdx, endIdx, globalOffset int) {
	for i := startIdx; i < endIdx; i++ {
		displayTodoAtIndex(todos[i], globalOffset+i+1)
	}
}

func init() {
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output as JSON")
	listCmd.Flags().BoolVar(&listCompleted, "completed", false, "Show only completed todos")
	listCmd.Flags().BoolVar(&listPending, "pending", false, "Show only pending todos")
	listCmd.Flags().BoolVar(&listQuiet, "quiet", false, "Show only IDs")
	listCmd.Flags().BoolVarP(&listAll, "all", "a", false, "Show all todos (override pagination)")
	listCmd.Flags().IntVarP(&listPage, "page", "p", 0, "Page number to display (default: 1)")
	rootCmd.AddCommand(listCmd)
}
