package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/akr411/doit/internal/models"
	"github.com/akr411/doit/internal/utils"
)

const (
	taskMaxChars     = 200
	taskWarnChars    = 150
	noteMaxChars     = 1000
	noteWarnChars    = 800
)

type TodoForm struct {
	taskInput     textinput.Model
	noteInput     textarea.Model
	deadlineInput textinput.Model
	focusIndex    int
	submitted     bool
	err           error
	todo          *models.Todo
	quitting      bool
}

func NewTodoForm(todo *models.Todo) TodoForm {
	ti := textinput.New()
	ti.Placeholder = "Enter task..."
	ti.Focus()
	ti.CharLimit = taskMaxChars
	ti.Width = 60

	na := textarea.New()
	na.Placeholder = "Enter note (optional)..."
	na.CharLimit = noteMaxChars
	na.SetWidth(60)
	na.SetHeight(5)
	na.ShowLineNumbers = false
	na.FocusedStyle.CursorLine = UnselectedStyle
	na.BlurredStyle.CursorLine = UnselectedStyle

	di := textinput.New()
	di.Placeholder = "2h, 1d, 2025-12-31"
	di.Width = 30

	if todo != nil {
		ti.SetValue(todo.Task)
		na.SetValue(todo.Note)
		if todo.Deadline > 0 {
			di.SetValue(utils.FormatDeadline(todo.Deadline))
		}
	}

	return TodoForm{
		taskInput:     ti,
		noteInput:     na,
		deadlineInput: di,
		focusIndex:    0,
		todo:          todo,
	}
}

func (m TodoForm) Init() tea.Cmd {
	return textinput.Blink
}

func (m TodoForm) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Check for submit: ctrl+s (universally supported across all platforms)
		if msg.String() == "ctrl+s" {
			if err := m.validate(); err != nil {
				m.err = err
				return m, nil
			}
			m.submitted = true
			m.quitting = true
			return m, tea.Quit
		}

		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "tab", "shift+tab", "down", "up":
			if msg.String() == "tab" || msg.String() == "down" {
				m.focusIndex++
			} else {
				m.focusIndex--
			}
			if m.focusIndex > 2 {
				m.focusIndex = 0
			}
			if m.focusIndex < 0 {
				m.focusIndex = 2
			}
			m.updateFocus()
			return m, nil
		case "enter":
			if m.focusIndex == 2 {
				if err := m.validate(); err != nil {
					m.err = err
					return m, nil
				}
				m.submitted = true
				m.quitting = true
				return m, tea.Quit
			}
		}
	}

	switch m.focusIndex {
	case 0:
		m.taskInput, cmd = m.taskInput.Update(msg)
		cmds = append(cmds, cmd)
	case 1:
		m.noteInput, cmd = m.noteInput.Update(msg)
		cmds = append(cmds, cmd)
	case 2:
		m.deadlineInput, cmd = m.deadlineInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *TodoForm) updateFocus() {
	m.taskInput.Blur()
	m.noteInput.Blur()
	m.deadlineInput.Blur()

	switch m.focusIndex {
	case 0:
		m.taskInput.Focus()
	case 1:
		m.noteInput.Focus()
	case 2:
		m.deadlineInput.Focus()
	}
}

func (m TodoForm) validate() error {
	task := strings.TrimSpace(m.taskInput.Value())
	if err := utils.ValidateTask(task); err != nil {
		return err
	}
	if len(task) > taskMaxChars {
		return fmt.Errorf("task exceeds %d characters", taskMaxChars)
	}

	note := strings.TrimSpace(m.noteInput.Value())
	if err := utils.ValidateNote(note); err != nil {
		return err
	}
	if len(note) > noteMaxChars {
		return fmt.Errorf("note exceeds %d characters", noteMaxChars)
	}

	deadline := strings.TrimSpace(m.deadlineInput.Value())
	if err := utils.ValidateDeadline(deadline); err != nil {
		return err
	}

	return nil
}

func (m TodoForm) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	b.WriteString(TitleStyle.Render("Todo Form") + "\n\n")

	b.WriteString(LabelStyle.Render("Task:") + "\n")
	b.WriteString(m.taskInput.View() + "\n")
	taskLen := len(m.taskInput.Value())
	if taskLen > 0 {
		validationStyle := ValidationStyle
		if taskLen >= taskMaxChars {
			validationStyle = ValidationErrorStyle
		} else if taskLen >= taskWarnChars {
			validationStyle = ValidationWarningStyle
		}
		b.WriteString(validationStyle.Render(fmt.Sprintf("%d/%d characters", taskLen, taskMaxChars)) + "\n")
	}
	b.WriteString("\n")

	b.WriteString(LabelStyle.Render("Note:") + "\n")
	b.WriteString(m.noteInput.View() + "\n")
	noteLen := len(m.noteInput.Value())
	if noteLen > 0 {
		validationStyle := ValidationStyle
		if noteLen >= noteMaxChars {
			validationStyle = ValidationErrorStyle
		} else if noteLen >= noteWarnChars {
			validationStyle = ValidationWarningStyle
		}
		b.WriteString(validationStyle.Render(fmt.Sprintf("%d/%d characters", noteLen, noteMaxChars)) + "\n")
	}
	b.WriteString("\n")

	b.WriteString(LabelStyle.Render("Deadline:") + "\n")
	b.WriteString(m.deadlineInput.View() + "\n\n")

	if m.err != nil {
		b.WriteString(ErrorStyle.Render("✗ " + m.err.Error()) + "\n\n")
	}

	help := "↑/↓: next field • ctrl+s: submit • esc: cancel"
	b.WriteString(HelpStyle.Render(help))

	return b.String()
}

func (m TodoForm) GetTodo() (*models.Todo, error) {
	if !m.submitted {
		return nil, nil
	}

	task := strings.TrimSpace(m.taskInput.Value())
	note := strings.TrimSpace(m.noteInput.Value())
	deadlineStr := strings.TrimSpace(m.deadlineInput.Value())

	deadline, err := utils.ParseDeadline(deadlineStr)
	if err != nil && deadlineStr != "" {
		return nil, err
	}

	if m.todo != nil {
		m.todo.Task = task
		m.todo.Note = note
		m.todo.Deadline = deadline
		return m.todo, nil
	}

	return models.NewTodo(task, note, deadline), nil
}

func RunTodoForm(todo *models.Todo) (*models.Todo, error) {
	m := NewTodoForm(todo)
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}
	return finalModel.(TodoForm).GetTodo()
}
