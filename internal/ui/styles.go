package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	Primary = lipgloss.Color("#5B8FF9")
	Success = lipgloss.Color("#52C41A")
	Warning = lipgloss.Color("#FAAD14")
	Error   = lipgloss.Color("#FF4D4F")
	Muted   = lipgloss.Color("#8C8C8C")
	White   = lipgloss.Color("#FFFFFF")
)

var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Primary)

	SelectedStyle = lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true)

	UnselectedStyle = lipgloss.NewStyle().
			Foreground(White)

	CompletedStyle = lipgloss.NewStyle().
			Foreground(Success).
			Strikethrough(true)

	OverdueStyle = lipgloss.NewStyle().
			Foreground(Error).
			Bold(true)

	SoonStyle = lipgloss.NewStyle().
			Foreground(Warning)

	MutedStyle = lipgloss.NewStyle().
			Foreground(Muted)

	HelpStyle = lipgloss.NewStyle().
			Foreground(Muted).
			MarginTop(1)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(Error).
			Bold(true)

	SuccessMessageStyle = lipgloss.NewStyle().
				Foreground(Success).
				Bold(true)

	InputStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(Primary).
			Padding(0, 1)

	FocusedInputStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.RoundedBorder()).
				BorderForeground(Primary).
				Padding(0, 1).
				Bold(true)

	LabelStyle = lipgloss.NewStyle().
			Foreground(White).
			Bold(true).
			MarginRight(1)

	ValidationStyle = lipgloss.NewStyle().
			Foreground(Muted).
			Italic(true)

	ValidationWarningStyle = lipgloss.NewStyle().
				Foreground(Warning).
				Italic(true)

	ValidationErrorStyle = lipgloss.NewStyle().
				Foreground(Error).
				Italic(true)

	ButtonStyle = lipgloss.NewStyle().
			Foreground(White).
			Background(Primary).
			Padding(0, 2).
			MarginRight(1)

	ButtonSelectedStyle = lipgloss.NewStyle().
				Foreground(Primary).
				Background(White).
				Padding(0, 2).
				MarginRight(1).
				Bold(true)
)

// Helper functions for colored messages
func PrintSuccess(format string, args ...interface{}) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	fmt.Println(SuccessMessageStyle.Render(msg))
}

func PrintWarning(format string, args ...interface{}) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	warningStyle := lipgloss.NewStyle().Foreground(Warning).Bold(true)
	fmt.Println(warningStyle.Render(msg))
}

func PrintError(format string, args ...interface{}) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	fmt.Println(ErrorStyle.Render(msg))
}
