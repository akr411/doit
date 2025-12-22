package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/akr411/doit/internal/models"
	"github.com/akr411/doit/internal/utils"
)

type SelectionMode int

const (
	SingleSelect SelectionMode = iota
	MultiSelect
)

type SelectionList struct {
	todos        []*models.Todo
	cursor       int
	selected     map[int]bool
	mode         SelectionMode
	pageSize     int
	currentPage  int
	title        string
	helpText     string
	expandedNote int
	quitting     bool
}

func NewSelectionList(todos []*models.Todo, mode SelectionMode, pageSize int, title, helpText string) SelectionList {
	return SelectionList{
		todos:        todos,
		cursor:       0,
		selected:     make(map[int]bool),
		mode:         mode,
		pageSize:     pageSize,
		currentPage:  0,
		title:        title,
		helpText:     helpText,
		expandedNote: -1,
	}
}

func (m SelectionList) Init() tea.Cmd {
	return nil
}

func (m SelectionList) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "esc":
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
					} else {
						m.expandedNote = m.cursor
					}
				}
			}
		case "x":
			if m.mode == MultiSelect {
				if m.selected[m.cursor] {
					delete(m.selected, m.cursor)
				} else {
					m.selected[m.cursor] = true
				}
			}
		case "enter":
			if m.mode == SingleSelect {
				m.selected[m.cursor] = true
			}
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m SelectionList) View() string {
	if m.quitting {
		return ""
	}

	if len(m.todos) == 0 {
		return MutedStyle.Render("No tasks found.")
	}

	var b strings.Builder

	b.WriteString(TitleStyle.Render(m.title) + "\n\n")

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
		} else if m.mode == MultiSelect && m.selected[i] {
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
		if todo.Completed {
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

	b.WriteString("\n" + HelpStyle.Render(m.helpText))

	return b.String()
}

func (m SelectionList) GetSelectedIndices() []int {
	indices := []int{}
	for i := range m.selected {
		indices = append(indices, i)
	}
	return indices
}

func (m SelectionList) GetSelectedTodo() *models.Todo {
	if m.mode == SingleSelect && len(m.selected) > 0 {
		for i := range m.selected {
			return m.todos[i]
		}
	}
	return nil
}

func RunSelectionList(todos []*models.Todo, mode SelectionMode, pageSize int, title, helpText string) (SelectionList, error) {
	m := NewSelectionList(todos, mode, pageSize, title, helpText)
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return m, err
	}
	return finalModel.(SelectionList), nil
}
