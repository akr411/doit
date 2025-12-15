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

type mainMode int

const (
	modeList mainMode = iota
	modeAdd
	modeEdit
)

type MainInteractive struct {
	store              *storage.Storage
	pendingTodos       []*models.Todo
	completedTodos     []*models.Todo
	cursor             int
	pageSize           int
	currentPage        int
	expandedNote       int
	inCompletedSection bool
	quitting           bool
	confirmingDelete   bool
	mode               mainMode
	form               TodoForm
	editingTodo        *models.Todo
	warningMsg         string
}

func NewMainInteractive(store *storage.Storage, pageSize int) MainInteractive {
	todos, _ := store.GetAllTodos()
	pending := []*models.Todo{}
	completed := []*models.Todo{}
	for _, t := range todos {
		if t.Completed {
			completed = append(completed, t)
		} else {
			pending = append(pending, t)
		}
	}
	return MainInteractive{
		store:          store,
		pendingTodos:   pending,
		completedTodos: completed,
		cursor:         0,
		pageSize:       pageSize,
		currentPage:    0,
		expandedNote:   -1,
	}
}

func (m MainInteractive) Init() tea.Cmd {
	return nil
}

func (m MainInteractive) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle form mode
	if m.mode == modeAdd || m.mode == modeEdit {
		updatedForm, cmd := m.form.Update(msg)
		m.form = updatedForm.(TodoForm)

		if m.form.quitting {
			if m.form.submitted {
				todo, err := m.form.GetTodo()
				if err == nil && todo != nil {
					if m.mode == modeAdd {
						m.store.SaveTodo(todo)
					} else {
						todo.UpdatedAt = time.Now().Unix()
						m.store.UpdateTodo(todo)
					}
					m.refresh()
				}
			}
			m.mode = modeList
			m.editingTodo = nil
			return m, nil
		}
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle delete confirmation mode
		if m.confirmingDelete {
			switch msg.String() {
			case "y", "Y":
				todo := m.getTodoAtCursor()
				if todo != nil {
					m.store.DeleteTodo(todo.ID)
					m.refresh()
					totalItems := len(m.pendingTodos) + len(m.completedTodos)
					if m.cursor >= totalItems && m.cursor > 0 {
						m.cursor--
						m.updateSection()
					}
					m.expandedNote = -1
				}
				m.confirmingDelete = false
			case "n", "N", "esc":
				m.confirmingDelete = false
			}
			return m, nil
		}

		// Clear warning message on any keypress
		m.warningMsg = ""

		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			pendingPages := (len(m.pendingTodos) + m.pageSize - 1) / m.pageSize
			if pendingPages == 0 {
				pendingPages = 1
			}

			if m.currentPage < pendingPages {
				// On pending page
				pageStart := m.currentPage * m.pageSize
				if m.cursor > pageStart {
					m.cursor--
				}
			} else {
				// On completed page
				completedPageIdx := m.currentPage - pendingPages
				pageStart := len(m.pendingTodos) + completedPageIdx*m.pageSize
				if m.cursor > pageStart {
					m.cursor--
				}
			}
			m.updateSection()
		case "down", "j":
			pendingPages := (len(m.pendingTodos) + m.pageSize - 1) / m.pageSize
			if pendingPages == 0 {
				pendingPages = 1
			}

			if m.currentPage < pendingPages {
				// On pending page
				pageEnd := (m.currentPage+1)*m.pageSize - 1
				if pageEnd >= len(m.pendingTodos) {
					pageEnd = len(m.pendingTodos) - 1
				}
				if m.cursor < pageEnd {
					m.cursor++
				}
			} else {
				// On completed page
				completedPageIdx := m.currentPage - pendingPages
				pageEnd := len(m.pendingTodos) + (completedPageIdx+1)*m.pageSize - 1
				if pageEnd >= len(m.pendingTodos)+len(m.completedTodos) {
					pageEnd = len(m.pendingTodos) + len(m.completedTodos) - 1
				}
				if m.cursor < pageEnd {
					m.cursor++
				}
			}
			m.updateSection()
		case "left", "h", "shift+tab":
			pendingPages := (len(m.pendingTodos) + m.pageSize - 1) / m.pageSize
			if pendingPages == 0 {
				pendingPages = 1
			}

			if m.currentPage > 0 {
				m.currentPage--
				if m.currentPage < pendingPages {
					m.cursor = m.currentPage * m.pageSize
					if m.cursor >= len(m.pendingTodos) {
						m.cursor = len(m.pendingTodos) - 1
					}
				} else {
					m.cursor = len(m.pendingTodos) + (m.currentPage-pendingPages)*m.pageSize
				}
				m.updateSection()
			}
		case "right", "l", "tab":
			pendingPages := (len(m.pendingTodos) + m.pageSize - 1) / m.pageSize
			if pendingPages == 0 {
				pendingPages = 1
			}
			completedPages := (len(m.completedTodos) + m.pageSize - 1) / m.pageSize
			totalPages := pendingPages + completedPages

			if m.currentPage < totalPages-1 {
				m.currentPage++
				if m.currentPage < pendingPages {
					m.cursor = m.currentPage * m.pageSize
					if m.cursor >= len(m.pendingTodos) {
						m.cursor = len(m.pendingTodos) - 1
					}
				} else {
					m.cursor = len(m.pendingTodos) + (m.currentPage-pendingPages)*m.pageSize
				}
				m.updateSection()
			}
		case " ":
			if m.expandedNote == m.cursor {
				m.expandedNote = -1
			} else {
				todo := m.getTodoAtCursor()
				if todo != nil && todo.Note != "" {
					lines := strings.Count(todo.Note, "\n") + 1
					if lines > 10 {
						RunNoteViewer(todo.Note)
						m.refresh()
					} else {
						m.expandedNote = m.cursor
					}
				}
			}
		case "c":
			todo := m.getTodoAtCursor()
			if todo != nil {
				wasCompleted := todo.Completed
				m.store.CompleteTodo(todo.ID, !todo.Completed)
				if !wasCompleted {
					if err := updateStreakForStore(m.store); err != nil {
						m.warningMsg = fmt.Sprintf("Warning: %v", err)
					}
					if err := m.store.CleanupOldCompleted(); err != nil {
						m.warningMsg = fmt.Sprintf("Warning: failed to cleanup: %v", err)
					}
				}
				m.refresh()
				m.expandedNote = -1
			}
		case "d":
			todo := m.getTodoAtCursor()
			if todo != nil {
				m.confirmingDelete = true
			}
		case "a":
			m.mode = modeAdd
			m.form = NewTodoForm(nil)
			return m, m.form.Init()
		case "e":
			todo := m.getTodoAtCursor()
			if todo != nil && !todo.Completed {
				m.mode = modeEdit
				m.editingTodo = todo
				m.form = NewTodoForm(todo)
				return m, m.form.Init()
			}
		case "r":
			m.refresh()
		}
	}
	return m, nil
}

func (m *MainInteractive) updateSection() {
	if m.cursor < len(m.pendingTodos) {
		m.inCompletedSection = false
	} else {
		m.inCompletedSection = true
	}
}

func (m *MainInteractive) getTodoAtCursor() *models.Todo {
	if m.cursor < len(m.pendingTodos) {
		return m.pendingTodos[m.cursor]
	}
	completedIdx := m.cursor - len(m.pendingTodos)
	if completedIdx < len(m.completedTodos) {
		return m.completedTodos[completedIdx]
	}
	return nil
}

func (m *MainInteractive) refresh() {
	todos, _ := m.store.GetAllTodos()
	m.pendingTodos = []*models.Todo{}
	m.completedTodos = []*models.Todo{}
	for _, t := range todos {
		if t.Completed {
			m.completedTodos = append(m.completedTodos, t)
		} else {
			m.pendingTodos = append(m.pendingTodos, t)
		}
	}
}

func (m MainInteractive) View() string {
	if m.quitting {
		return ""
	}

	// Render form view when in add/edit mode
	if m.mode == modeAdd || m.mode == modeEdit {
		return m.form.View()
	}

	totalItems := len(m.pendingTodos) + len(m.completedTodos)
	if totalItems == 0 {
		help := HelpStyle.Render("a: add • q: quit")
		return MutedStyle.Render("No tasks. Press 'a' to add one.") + "\n\n" + help
	}

	var b strings.Builder

	streak, _ := m.store.GetStreak()
	streaksEnabled, _ := m.store.GetConfig("streaks_enabled")

	title := fmt.Sprintf("Tasks (%d total)", totalItems)
	if streaksEnabled != "false" && streak != nil && streak.CurrentStreak > 0 {
		title += fmt.Sprintf(" | Streak: %d days", streak.CurrentStreak)
	}

	var syncEnabled string
	m.store.GetDB().QueryRow("SELECT value FROM config WHERE key='sync_enabled'").Scan(&syncEnabled)
	if syncEnabled == "true" {
		var peerCount int
		m.store.GetDB().QueryRow("SELECT COUNT(*) FROM peers").Scan(&peerCount)
		if peerCount > 0 {
			title += fmt.Sprintf(" | ⟳ %d device%s", peerCount, map[bool]string{true: "", false: "s"}[peerCount == 1])
		} else {
			title += " | ⟳ No devices"
		}
	}

	b.WriteString(TitleStyle.Render(title) + "\n\n")

	now := time.Now().Unix()

	pendingPages := (len(m.pendingTodos) + m.pageSize - 1) / m.pageSize
	if pendingPages == 0 {
		pendingPages = 1
	}
	completedPages := (len(m.completedTodos) + m.pageSize - 1) / m.pageSize
	totalPages := pendingPages + completedPages

	if m.currentPage < pendingPages {
		b.WriteString(LabelStyle.Render(fmt.Sprintf("Pending (%d):", len(m.pendingTodos))) + "\n")
		start := m.currentPage * m.pageSize
		end := start + m.pageSize
		if end > len(m.pendingTodos) {
			end = len(m.pendingTodos)
		}
		for i := start; i < end; i++ {
			b.WriteString(m.renderTodo(m.pendingTodos[i], i, now))
		}
	} else {
		b.WriteString(LabelStyle.Render(fmt.Sprintf("Completed (%d):", len(m.completedTodos))) + "\n")
		completedPageIdx := m.currentPage - pendingPages
		start := completedPageIdx * m.pageSize
		end := start + m.pageSize
		if end > len(m.completedTodos) {
			end = len(m.completedTodos)
		}
		for i := start; i < end; i++ {
			globalIdx := len(m.pendingTodos) + i
			b.WriteString(m.renderTodo(m.completedTodos[i], globalIdx, now))
		}
	}

	if totalPages > 1 {
		pagination := fmt.Sprintf("\nPage %d/%d", m.currentPage+1, totalPages)
		b.WriteString(MutedStyle.Render(pagination) + "\n")
	}

	if m.warningMsg != "" {
		warningStyle := lipgloss.NewStyle().Foreground(Warning).Bold(true)
		b.WriteString("\n" + warningStyle.Render(m.warningMsg) + "\n")
	}

	var help string
	if m.confirmingDelete {
		help = ErrorStyle.Render("Delete this todo? (y/n, esc to cancel)")
	} else {
		help = HelpStyle.Render("↑/↓: navigate • ←/→: page • a: add • e: edit • c: toggle • d: delete • r: refresh • space: note • q: quit")
	}
	b.WriteString("\n" + help)

	return b.String()
}

func (m MainInteractive) renderTodo(todo *models.Todo, idx int, now int64) string {
	cursor := " "
	if m.cursor == idx {
		cursor = ">"
	}

	status := "[ ]"
	if todo.Completed {
		status = "[x]"
	}

	totalItems := len(m.pendingTodos) + len(m.completedTodos)
	numWidth := len(fmt.Sprintf("%d", totalItems))
	line := fmt.Sprintf("%s %*d. %s %s", cursor, numWidth, idx+1, status, todo.Task)

	if todo.Deadline > 0 {
		deadlineStr := utils.FormatDeadline(todo.Deadline)
		line += fmt.Sprintf(" (%s)", deadlineStr)
	}

	style := UnselectedStyle
	if m.cursor == idx {
		style = SelectedStyle
	}
	if todo.Completed {
		style = CompletedStyle
	} else if todo.Deadline > 0 && todo.Deadline < now {
		style = OverdueStyle
	} else if todo.Deadline > 0 && todo.Deadline < now+3*24*3600 {
		style = SoonStyle
	}

	result := style.Render(line) + "\n"

	if m.expandedNote == idx && todo.Note != "" {
		noteLines := strings.Split(todo.Note, "\n")
		for _, noteLine := range noteLines {
			result += MutedStyle.Render("  │ " + noteLine) + "\n"
		}
	}

	return result
}

func updateStreakForStore(store *storage.Storage) error {
	streaksEnabled, _ := store.GetConfig("streaks_enabled")
	if streaksEnabled == "false" {
		return nil
	}

	streak, err := store.GetStreak()
	if err != nil {
		return fmt.Errorf("failed to get streak: %w", err)
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

	if err := store.UpdateStreak(streak); err != nil {
		return fmt.Errorf("failed to update streak: %w", err)
	}
	return nil
}

func RunMainInteractive(store *storage.Storage, pageSize int) error {
	m := NewMainInteractive(store, pageSize)
	p := tea.NewProgram(m)
	_, err := p.Run()
	return err
}
