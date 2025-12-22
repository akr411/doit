package utils

import (
	"strings"
	"testing"
)

func TestProcessNoteInput(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple note", "simple note"},
		{"with\\nnewline", "with\nnewline"},
		{"multiple\\n\\nlines", "multiple\n\nlines"},
		{"Line 1\\nLine 2", "Line 1\nLine 2"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ProcessNoteInput(tt.input)
			if result != tt.want {
				t.Errorf("ProcessNoteInput(%q) = %q, want %q", tt.input, result, tt.want)
			}
		})
	}
}

func TestProcessNoteInputActualCRLF(t *testing.T) {
	input := "Line 1\r\nLine 2"
	result := ProcessNoteInput(input)

	if result != "Line 1\nLine 2" {
		t.Errorf("CRLF should be normalized to LF, got %q", result)
	}

	input = "Line 1\rLine 2"
	result = ProcessNoteInput(input)

	if result != "Line 1\nLine 2" {
		t.Errorf("CR should be normalized to LF, got %q", result)
	}
}

func TestValidateTaskDangerousCharacters(t *testing.T) {
	dangerousCases := []struct {
		name  string
		input string
	}{
		{"null byte", "Task\x00name"},
		{"control char", "Task\x01name"},
		{"DEL", "Task\x7Fname"},
		{"zero-width space", "Task\u200Bname"},
		{"bidi override", "Task\u202Ename"},
	}

	for _, tc := range dangerousCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTask(tc.input)
			if err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
}

func TestValidateTaskValidInputs(t *testing.T) {
	validCases := []string{
		"Simple task",
		"Task with numbers 123",
		"Task with emoji 😀",
		"Task with symbols !@#$%^&*()",
		"Task with unicode: café, naïve",
		"Very long task" + strings.Repeat("a", 200),
	}

	for _, input := range validCases {
		t.Run(input[:min(len(input), 20)], func(t *testing.T) {
			err := ValidateTask(input)
			if err != nil {
				t.Errorf("ValidateTask(%q) should succeed: %v", input, err)
			}
		})
	}
}

func TestValidateTaskNewlines(t *testing.T) {
	err := ValidateTask("Task\nwith\nnewlines")
	if err == nil {
		t.Error("expected error for newlines in task")
	}

	err = ValidateTask("Task\rwith\rcarriage")
	if err == nil {
		t.Error("expected error for carriage returns in task")
	}
}

func TestValidateTaskEmpty(t *testing.T) {
	err := ValidateTask("")
	if err == nil {
		t.Error("expected error for empty task")
	}
}

func TestValidateNoteAllowedCharacters(t *testing.T) {
	validNotes := []string{
		"Simple note",
		"Note\nwith\nmultiple\nlines",
		"Note\twith\ttabs",
		"Note with emoji 🎉",
		"Note with symbols !@#$%",
		strings.Repeat("Long note ", 100),
	}

	for _, note := range validNotes {
		err := ValidateNote(note)
		if err != nil {
			t.Errorf("ValidateNote should succeed for %q: %v", note[:min(len(note), 20)], err)
		}
	}
}

func TestValidateNoteDangerousCharacters(t *testing.T) {
	dangerousNotes := []string{
		"Note\x00with null",
		"Note\x01with control",
		"Note\u202Ewith bidi",
		"Note\u200Bwith zero-width",
	}

	for _, note := range dangerousNotes {
		err := ValidateNote(note)
		if err == nil {
			t.Errorf("expected error for dangerous character in note")
		}
	}
}

func TestValidateDeadlineValidFormats(t *testing.T) {
	validDeadlines := []string{
		"2h",
		"30m",
		"1d",
		"2025-12-25",
		"2025-12-31 23:59",
		"",
	}

	for _, deadline := range validDeadlines {
		err := ValidateDeadline(deadline)
		if err != nil {
			t.Errorf("ValidateDeadline(%q) should succeed: %v", deadline, err)
		}
	}
}

func TestValidateDeadlineInvalidCharacters(t *testing.T) {
	invalidDeadlines := []string{
		"2h\x00",
		"date\x01time",
		"2025\u202E12-25",
	}

	for _, deadline := range invalidDeadlines {
		err := ValidateDeadline(deadline)
		if err == nil {
			t.Error("expected error for deadline with invalid characters")
		}
	}
}

func TestFormatCharName(t *testing.T) {
	tests := []struct {
		char rune
		want string
	}{
		{0x00, "null byte"},
		{0x01, "control character"},
		{0x7F, "DEL character"},
		{0x200B, "zero-width character"},
		{0x202E, "bidirectional override character"},
		{0x2066, "bidirectional isolate character"},
		{0xFEFF, "byte order mark"},
	}

	for _, tt := range tests {
		result := formatCharName(tt.char)
		if !strings.Contains(result, tt.want) {
			t.Errorf("formatCharName(0x%04X) = %q, want substring %q", tt.char, result, tt.want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
