package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/akr411/doit/internal/models"
	"github.com/akr411/doit/internal/storage"
	"github.com/akr411/doit/internal/utils"
)

type InteractiveComplete struct {
	store        *storage.Storage
	todos        []*models.Todo
	cursor       int
	selected     map[int]bool
	pageSize     int
	currentPage  int
	message      string
	expandedNote int
	quitting     bool
	warningMsg   string
}

func NewInteractiveComplete(store *storage.Storage, pageSize int) InteractiveComplete {
	todos, _ := store.GetAllTodos()
	return InteractiveComplete{
		store:        store,
		todos:        todos,
		cursor:       0,
		selected:     make(map[int]bool),
		pageSize:     pageSize,
		currentPage:  0,
		expandedNote: -1,
	}
}

func (m InteractiveComplete) Init() tea.Cmd {
	return nil
}

func (m InteractiveComplete) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Clear warning message on any keypress
		m.warningMsg = ""

		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.currentPage*m.pageSize {
					m.currentPage--
				}
			}
		case "down", "j":
			if m.cursor < len(m.todos)-1 {
				m.cursor++
				if m.cursor >= (m.currentPage+1)*m.pageSize {
					m.currentPage++
				}
			}
		case "left", "h", "shift+tab":
			if m.currentPage > 0 {
				m.currentPage--
				m.cursor = m.currentPage * m.pageSize
			}
		case "right", "l", "tab":
			totalPages := (len(m.todos) + m.pageSize - 1) / m.pageSize
			if m.currentPage < totalPages-1 {
				m.currentPage++
				m.cursor = m.currentPage * m.pageSize
			}
		case " ":
			if m.expandedNote == m.cursor {
				m.expandedNote = -1
			} else {
				todo := m.todos[m.cursor]
				if todo.Note != "" {
					lines := strings.Count(todo.Note, "\n") + 1
					if lines > 10 {
						_ = RunNoteViewer(todo.Note)
						m.refresh()
					} else {
						m.expandedNote = m.cursor
					}
				}
			}
		case "x":
			if m.selected[m.cursor] {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = true
			}
		case "enter":
			if len(m.selected) == 0 {
				return m, nil
			}

			completedCount := 0
			reopenedCount := 0
			for idx := range m.selected {
				todo := m.todos[idx]
				wasCompleted := todo.Completed
				_ = m.store.CompleteTodo(todo.ID, !todo.Completed)
				if !wasCompleted {
					completedCount++
					if err := m.store.RecordCompletion(); err != nil {
						m.warningMsg = fmt.Sprintf("Warning: %v", err)
					}
				} else {
					reopenedCount++
				}
			}

			if err := m.store.CleanupOldCompleted(); err != nil {
				m.warningMsg = fmt.Sprintf("Warning: failed to cleanup: %v", err)
			}
			m.selected = make(map[int]bool)
			m.refresh()
			m.expandedNote = -1

			if completedCount > 0 || reopenedCount > 0 {
				if completedCount > 0 && reopenedCount > 0 {
					m.message = fmt.Sprintf("✓ Completed %d, reopened %d", completedCount, reopenedCount)
				} else if completedCount > 0 {
					m.message = fmt.Sprintf("✓ Completed %d", completedCount)
				} else if reopenedCount > 0 {
					m.message = fmt.Sprintf("↻ Reopened %d", reopenedCount)
				}
			}
		}
	}
	return m, nil
}

func (m *InteractiveComplete) refresh() {
	m.todos, _ = m.store.GetAllTodos()
}

func (m InteractiveComplete) View() string {
	if m.quitting {
		if m.message != "" {
			return m.message + "\n"
		}
		return ""
	}

	if len(m.todos) == 0 {
		return MutedStyle.Render("No tasks found.")
	}

	var b strings.Builder

	b.WriteString(TitleStyle.Render("Toggle Completion Status") + "\n\n")

	start := m.currentPage * m.pageSize
	end := start + m.pageSize
	if end > len(m.todos) {
		end = len(m.todos)
	}

	now := time.Now().Unix()
	numWidth := len(fmt.Sprintf("%d", len(m.todos)))

	for i := start; i < end; i++ {
		todo := m.todos[i]

		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}

		status := "[ ]"
		if todo.Completed {
			status = "[x]"
		} else if m.selected[i] {
			status = "[+]"
		}

		line := fmt.Sprintf("%s %*d. %s %s", cursor, numWidth, i+1, status, todo.Task)

		if todo.Deadline > 0 {
			deadlineStr := utils.FormatDeadline(todo.Deadline)
			line += fmt.Sprintf(" (%s)", deadlineStr)
		}

		style := UnselectedStyle
		if m.cursor == i {
			style = SelectedStyle
		}
		if m.selected[i] {
			style = SelectedStyle
		} else if todo.Completed {
			style = CompletedStyle
		} else if todo.Deadline > 0 && todo.Deadline < now {
			style = OverdueStyle
		} else if todo.Deadline > 0 && todo.Deadline < now+3*24*3600 {
			style = SoonStyle
		}

		b.WriteString(style.Render(line) + "\n")

		if m.expandedNote == i && todo.Note != "" {
			noteLines := strings.Split(todo.Note, "\n")
			for _, noteLine := range noteLines {
				b.WriteString(MutedStyle.Render("  │ " + noteLine) + "\n")
			}
		}
	}

	totalPages := (len(m.todos) + m.pageSize - 1) / m.pageSize
	if totalPages > 1 {
		pagination := fmt.Sprintf("\nPage %d/%d", m.currentPage+1, totalPages)
		b.WriteString(MutedStyle.Render(pagination) + "\n")
	}

	if m.message != "" {
		b.WriteString("\n" + SuccessMessageStyle.Render(m.message) + "\n")
	}

	if m.warningMsg != "" {
		warningStyle := lipgloss.NewStyle().Foreground(Warning).Bold(true)
		b.WriteString("\n" + warningStyle.Render(m.warningMsg) + "\n")
	}

	help := "↑/↓: navigate • ←/→: page • x: toggle • enter: apply • space: note • q: quit"
	b.WriteString("\n" + HelpStyle.Render(help))

	return b.String()
}

func RunInteractiveComplete(store *storage.Storage, pageSize int) error {
	m := NewInteractiveComplete(store, pageSize)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
