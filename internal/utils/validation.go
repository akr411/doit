package utils

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var (
	dangerousChars = []rune{
		0x00,                   // Null byte
		0x01, 0x02, 0x03, 0x04, // Control chars
		0x05, 0x06, 0x07, 0x08, // Control chars
		0x0B,             // Vertical tab
		0x0C,             // Form feed
		0x0E, 0x0F,       // Control chars
		0x10, 0x11, 0x12, 0x13, // Control chars
		0x14, 0x15, 0x16, 0x17, // Control chars
		0x18, 0x19, 0x1A, 0x1B, // Control chars
		0x1C, 0x1D, 0x1E, 0x1F, // Control chars
		0x7F,       // DEL
		0x200B,     // Zero-width space
		0x200C,     // Zero-width non-joiner
		0x200D,     // Zero-width joiner
		0x200E,     // Left-to-right mark
		0x200F,     // Right-to-left mark
		0x202A,     // Left-to-right embedding
		0x202B,     // Right-to-left embedding
		0x202C,     // Pop directional formatting
		0x202D,     // Left-to-right override
		0x202E,     // Right-to-left override
		0x2066,     // Left-to-right isolate
		0x2067,     // Right-to-left isolate
		0x2068,     // First strong isolate
		0x2069,     // Pop directional isolate
		0xFEFF,     // Byte order mark / zero-width no-break space
	}

	deadlineAllowedPattern = regexp.MustCompile(`^[a-zA-Z0-9\s\-:/.]+$`)
)

func containsDangerousChar(s string) (bool, rune) {
	for _, r := range s {
		for _, d := range dangerousChars {
			if r == d {
				return true, r
			}
		}
	}
	return false, 0
}

func formatCharName(r rune) string {
	switch {
	case r == 0x00:
		return "null byte"
	case r < 0x20:
		return fmt.Sprintf("control character (0x%02X)", r)
	case r == 0x7F:
		return "DEL character"
	case r >= 0x200B && r <= 0x200F:
		return "zero-width character"
	case r >= 0x202A && r <= 0x202E:
		return "bidirectional override character"
	case r >= 0x2066 && r <= 0x2069:
		return "bidirectional isolate character"
	case r == 0xFEFF:
		return "byte order mark"
	default:
		return fmt.Sprintf("illegal character (U+%04X)", r)
	}
}

func ValidateTask(task string) error {
	if task == "" {
		return fmt.Errorf("task cannot be empty")
	}

	if strings.ContainsAny(task, "\n\r") {
		return fmt.Errorf("task cannot contain newlines")
	}

	if has, r := containsDangerousChar(task); has {
		return fmt.Errorf("task contains %s", formatCharName(r))
	}

	return nil
}

func ValidateNote(note string) error {
	for _, r := range note {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		for _, d := range dangerousChars {
			if r == d {
				return fmt.Errorf("note contains %s", formatCharName(r))
			}
		}
	}
	return nil
}

func ValidateDeadline(deadline string) error {
	if deadline == "" {
		return nil
	}

	if !deadlineAllowedPattern.MatchString(deadline) {
		for _, r := range deadline {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(" -:/.", r) {
				return fmt.Errorf("deadline contains invalid character: %q", string(r))
			}
		}
		return fmt.Errorf("deadline contains invalid characters")
	}

	return nil
}

func ProcessNoteInput(note string) string {
	note = strings.ReplaceAll(note, "\\n", "\n")
	note = strings.ReplaceAll(note, "\r\n", "\n")
	note = strings.ReplaceAll(note, "\r", "\n")
	return note
}
