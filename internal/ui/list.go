package ui

import (
	"fmt"
	"strings"

	"github.com/akr411/doit/internal/models"
	"github.com/akr411/doit/internal/storage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const pageSize = 10

// ListModel represents the list view model
type ListModel struct {
	storage          storage.Storage
	todos            []*models.Todo
	topUpcoming      []*models.Todo
	todosNoDeadline  []*models.Todo
	streak           *storage.Streak
	cursor           int
	expanded         map[int]bool
	currentPage      int
	showHelp         bool
	err              error
	loading          bool
	confirmingDelete bool
	todoToDelete     *models.Todo
}

type dataLoadedMsg struct {
	todos  []*models.Todo
	streak *storage.Streak
}

type errMsg struct{ error }

// NewListModel creates a new list model
func NewListModel(storage storage.Storage) *ListModel {
	m := &ListModel{
		storage:          storage,
		expanded:         make(map[int]bool),
		loading:          true,
		confirmingDelete: false,
		todoToDelete:     nil,
	}
	return m
}

// Init initializes the list model
func (m *ListModel) Init() tea.Cmd {
	return m.loadData
}

func (m *ListModel) loadData() tea.Msg {
	todos, err := m.storage.GetAllTodos()
	if err != nil {
		return errMsg{err}
	}

	streak, err := m.storage.GetStreak()
	if err != nil {
		streak = &storage.Streak{
			CurrentStreak:    0,
			MaxStreak:        0,
			TotalCompleted:   0,
			DailyCompletions: make(map[string]int),
		}
	}

	return dataLoadedMsg{
		todos:  todos,
		streak: streak,
	}
}

func (m *ListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case dataLoadedMsg:
		m.loading = false
		m.todos = msg.todos
		m.streak = msg.streak

		m.topUpcoming = storage.GetTopUpcomingTodos(m.todos, 10)

		m.todosNoDeadline = storage.GetTodosWithoutDeadline(m.todos)
		return m, nil

	case errMsg:
		m.err = msg.error
		m.loading = false
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.ensureCursorVisible()
			}

		case "down", "j":
			if m.cursor < len(m.getVisibleTodos())-1 {
				m.cursor++
				m.ensureCursorVisible()
			}

		case "Space":
			m.expanded[m.cursor] = !m.expanded[m.cursor]

		case "c":
			if err := m.toggleComplete(); err != nil {
				m.err = err
			}
			return m, m.loadData

		case "d":
			if !m.confirmingDelete {
				todo := m.getCurrentTodo()
				if todo != nil {
					m.confirmingDelete = true
					m.todoToDelete = todo
				}
			}
			return m, nil

		case "n":
			if m.confirmingDelete {
				m.confirmingDelete = false
				m.todoToDelete = nil
				return m, nil
			}
			return NewFormModel(m.storage), nil

		case "y":
			if m.confirmingDelete && m.todoToDelete != nil {
				if err := m.storage.DeleteTodo(m.todoToDelete.ID); err != nil {
					m.err = err
				}
				m.confirmingDelete = false
				m.todoToDelete = nil
				return m, m.loadData
			}
			return m, nil

		case "r":
			m.loading = true
			return m, m.loadData

		case "?", "h":
			m.showHelp = !m.showHelp

		case "pgup", "b":
			if m.currentPage > 0 {
				m.currentPage--
				m.cursor = 0
			}

		case "pgdown", "f":
			visibleTodos := m.getVisibleTodos()
			if (m.currentPage+1)*pageSize < len(visibleTodos) {
				m.currentPage++
				m.cursor = 0
			}
		}
	}

	return m, nil
}

// View renders the list
func (m *ListModel) View() string {
	if m.loading {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9333EA")).
			Render("Loading todos...")
	}

	if m.err != nil {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444")).
			Render("Error: " + m.err.Error())
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7C3AED")).
		Bold(true).
		MarginBottom(1)

	streakStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#7C3AED")).
		Foreground(lipgloss.Color("#FFFFFF")).
		Padding(0, 1).
		MarginBottom(1)

	sectionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#9333EA")).
		Bold(true).
		MarginTop(1).
		MarginBottom(1)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#8B5CF6")).
		Padding(0, 1)

	normalStyle := lipgloss.NewStyle().
		Padding(0, 1)

	completeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#9CA3AF")).
		Strikethrough(true).
		Padding(0, 1)

	overdueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#EF4444")).
		Bold(true)

	upcomingStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F59E0B"))

	descriptionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#9CA3AF")).
		PaddingLeft(3)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		PaddingLeft(1)

	var s strings.Builder

	s.WriteString(titleStyle.Render(" Todo List"))

	if m.streak != nil && m.streak.CurrentStreak > 0 {
		streakText := fmt.Sprintf(" Streak: %d days | Max: %d days | Total: %d completed",
			m.streak.CurrentStreak, m.streak.MaxStreak, m.streak.TotalCompleted)
		s.WriteString(streakStyle.Render(streakText))
		s.WriteString("\n")
	}

	if len(m.topUpcoming) > 0 {
		s.WriteString(sectionStyle.Render(" Upcoming Deadlines (Top 10)"))
		s.WriteString("\n")
	}

	visibleTodos := m.getVisibleTodos()
	start := m.currentPage * pageSize
	end := start + pageSize
	if end > len(visibleTodos) {
		end = len(visibleTodos)
	}

	currentIndex := 0

	// Render top upcoming todos
	for _, todo := range m.topUpcoming {
		if currentIndex >= start && currentIndex < end {
			s.WriteString(m.renderTodo(todo, currentIndex, currentIndex == m.cursor,
				selectedStyle, normalStyle, completeStyle, overdueStyle, upcomingStyle, descriptionStyle))
			s.WriteString("\n")
		}
		currentIndex++
	}

	// Todos without deadline section
	if len(m.todosNoDeadline) > 0 {
		if currentIndex > 0 {
			s.WriteString("\n")
		}
		s.WriteString(sectionStyle.Render(" No Deadline"))
		s.WriteString("\n")
	}

	for _, todo := range m.todosNoDeadline {
		if currentIndex >= start && currentIndex < end {
			s.WriteString(m.renderTodo(todo, currentIndex, currentIndex == m.cursor,
				sectionStyle, normalStyle, completeStyle, overdueStyle, upcomingStyle, descriptionStyle))
			s.WriteString("\n")
		}
		currentIndex++
	}

	// Completed todos section
	completedCount := 0
	for _, todo := range m.todos {
		if todo.Completed {
			if completedCount == 0 && currentIndex > 0 {
				s.WriteString("\n")
				s.WriteString(sectionStyle.Render("🗹 Completed"))
				s.WriteString("\n")
			}
			if currentIndex >= start && currentIndex < end {
				s.WriteString(m.renderTodo(todo, currentIndex, currentIndex == m.cursor,
					sectionStyle, normalStyle, completeStyle, overdueStyle, upcomingStyle, descriptionStyle))
				s.WriteString("\n")
			}
			currentIndex++
			completedCount++
		}
	}

	if len(visibleTodos) > pageSize {
		pageInfo := fmt.Sprintf("\n Page %d/%d", m.currentPage+1, (len(visibleTodos)+pageSize-1)/pageSize)
		s.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Render(pageInfo))
	}

	if m.showHelp {
		s.WriteString("\n")
		s.WriteString(helpStyle.Render("Commands:\n"))
		s.WriteString(helpStyle.Render("↑/↓/j/k: Navigate • Space: Expand • c: Complete • d: Delete • n: New • r: Refresh • q: Quit"))
	} else {
		s.WriteString("\n")
		s.WriteString(helpStyle.Render("Press ? for help"))
	}

	if m.confirmingDelete && m.todoToDelete != nil {
		dialogStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#FF6B6B")).
			Padding(1, 2).
			Background(lipgloss.Color("#1A1A2E")).
			Foreground(lipgloss.Color("#FFFFFF"))

		warningStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFA500")).
			Bold(true)

		titleStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B")).
			Bold(true)

		var dialog strings.Builder
		dialog.WriteString(warningStyle.Render("⚠  Delete Confirmation"))
		dialog.WriteString("\n\n")
		dialog.WriteString("Are you sure you want to delete this todo?\n\n")
		dialog.WriteString(titleStyle.Render("Title: "))
		dialog.WriteString(m.todoToDelete.Title)
		dialog.WriteString("\n\n")
		dialog.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#4CAF50")).Render("[y] Yes  "))
		dialog.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B")).Render("[n] No  "))
		dialog.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF")).Render("[esc] Cancel"))

		dialogContent := dialogStyle.Render(dialog.String())

		width := lipgloss.Width(dialogContent)
		height := lipgloss.Height(dialogContent)
		viewWidth := 80
		viewHeight := 24

		leftPadding := (viewWidth - width) / 2
		topPadding := (viewHeight - height) / 2

		var finalView strings.Builder
		lines := strings.Split(s.String(), "\n")

		for i, line := range lines {
			if i >= topPadding && i < topPadding+height {
				relativeLineIndex := i - topPadding
				dialogLines := strings.Split(dialogContent, "\n")
				if relativeLineIndex < len(dialogLines) {
					finalView.WriteString(strings.Repeat(" ", leftPadding))
					finalView.WriteString(dialogLines[relativeLineIndex])
				} else {
					finalView.WriteString(line)
				}
			} else {
				finalView.WriteString(line)
			}
			if i < len(lines)-1 {
				finalView.WriteString("\n")
			}
		}

		return finalView.String()
	}

	return s.String()
}

func (m *ListModel) renderTodo(todo *models.Todo, index int, isSelected bool,
	selectedStyle, normalStyle, completedStyle, overdueStyle, upcomingStyle, descriptionStyle lipgloss.Style,
) string {
	var s strings.Builder

	checkbox := "[ ]"
	if todo.Completed {
		checkbox = "[✔]"
	}

	deadlineInfo := ""
	if todo.Deadline != nil && !todo.Completed {
		days := todo.DaysUntilDeadline()
		if days < 0 {
			deadlineInfo = overdueStyle.Render(fmt.Sprintf(" (Overdue by %d days)", -days))
		} else if days == 0 {
			deadlineInfo = overdueStyle.Render(" (Due today!)")
		} else if days <= 3 {
			deadlineInfo = upcomingStyle.Render(fmt.Sprintf(" (%d days left)", days))
		} else {
			deadlineInfo = fmt.Sprintf(" (%s)", todo.Deadline.Format("Jan 2, 3:04 PM"))
		}
	}

	line := fmt.Sprintf("%s %s%s", checkbox, todo.Title, deadlineInfo)

	if isSelected {
		s.WriteString(selectedStyle.Render(line))
	} else if todo.Completed {
		s.WriteString(completedStyle.Render(line))
	} else {
		s.WriteString(normalStyle.Render(line))
	}

	if m.expanded[index] && todo.Description != "" {
		s.WriteString("\n")
		s.WriteString(descriptionStyle.Render(todo.Description))
	}

	return s.String()
}

func (m *ListModel) getVisibleTodos() []*models.Todo {
	var visible []*models.Todo

	visible = append(visible, m.topUpcoming...)

	visible = append(visible, m.todosNoDeadline...)

	for _, todo := range m.todos {
		if todo.Completed {
			visible = append(visible, todo)
		}
	}

	return visible
}

func (m *ListModel) ensureCursorVisible() {
	visibleCount := len(m.getVisibleTodos())
	pageCount := (visibleCount + pageSize - 1) / pageSize

	targetPage := m.cursor / pageSize
	if targetPage != m.currentPage && targetPage < pageCount {
		m.currentPage = targetPage
	}
}

func (m *ListModel) getCurrentTodo() *models.Todo {
	visible := m.getVisibleTodos()
	if m.cursor >= 0 && m.cursor < len(visible) {
		return visible[m.cursor]
	}
	return nil
}

func (m *ListModel) toggleComplete() error {
	todo := m.getCurrentTodo()
	if todo == nil {
		return fmt.Errorf("no todo selected")
	}

	if todo.Completed {
		todo.MarkIncomplete()
	} else {
		todo.MarkComplete()
	}

	return m.storage.UpdateTodo(todo)
}
