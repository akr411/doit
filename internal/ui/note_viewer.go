package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type NoteViewer struct {
	note   string
	lines  []string
	scroll int
	height int
}

func NewNoteViewer(note string, height int) NoteViewer {
	lines := strings.Split(note, "\n")
	return NoteViewer{
		note:   note,
		lines:  lines,
		scroll: 0,
		height: height - 5,
	}
}

func (m NoteViewer) Init() tea.Cmd {
	return nil
}

func (m NoteViewer) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.scroll > 0 {
				m.scroll--
			}
		case "down", "j":
			maxScroll := len(m.lines) - m.height
			if maxScroll < 0 {
				maxScroll = 0
			}
			if m.scroll < maxScroll {
				m.scroll++
			}
		case "pgup":
			m.scroll -= m.height
			if m.scroll < 0 {
				m.scroll = 0
			}
		case "pgdown":
			maxScroll := len(m.lines) - m.height
			if maxScroll < 0 {
				maxScroll = 0
			}
			m.scroll += m.height
			if m.scroll > maxScroll {
				m.scroll = maxScroll
			}
		}
	}
	return m, nil
}

func (m NoteViewer) View() string {
	var b strings.Builder

	b.WriteString(TitleStyle.Render("Note") + "\n\n")

	end := m.scroll + m.height
	if end > len(m.lines) {
		end = len(m.lines)
	}

	for i := m.scroll; i < end; i++ {
		b.WriteString(m.lines[i] + "\n")
	}

	b.WriteString("\n")

	scrollInfo := ""
	if len(m.lines) > m.height {
		scrollInfo = MutedStyle.Render(fmt.Sprintf("Lines %d-%d of %d", m.scroll+1, end, len(m.lines))) + " • "
	}
	help := "↑/↓: scroll • pgup/pgdown: page • q/esc: close"
	b.WriteString("\n" + scrollInfo + HelpStyle.Render(help))

	return b.String()
}

func RunNoteViewer(note string) error {
	m := NewNoteViewer(note, 40)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
