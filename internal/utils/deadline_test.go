package utils

import (
	"strings"
	"testing"
	"time"
)

func TestParseDeadlineRelative(t *testing.T) {
	tests := []struct {
		input    string
		wantDiff int64
	}{
		{"30m", 30 * 60},
		{"2h", 2 * 3600},
		{"1d", 24 * 3600},
		{"3d", 3 * 24 * 3600},
		{"1w", 7 * 24 * 3600},
		{"2w", 14 * 24 * 3600},
		{"1M", 30 * 24 * 3600},
		{"1y", 365 * 24 * 3600},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseDeadline(tt.input)
			if err != nil {
				t.Fatalf("ParseDeadline(%s) failed: %v", tt.input, err)
			}

			now := time.Now().Unix()
			diff := result - now

			expectedMin := tt.wantDiff - 5
			expectedMax := tt.wantDiff + 5

			if diff < expectedMin || diff > expectedMax {
				t.Errorf("ParseDeadline(%s): expected ~%d seconds from now, got %d", tt.input, tt.wantDiff, diff)
			}
		})
	}
}

func TestParseDeadlineAbsolute(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"2025-12-25"},
		{"2026-01-01"},
		{"2025-12-31 23:59"},
		{"2025-12-25 14:30"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseDeadline(tt.input)
			if err != nil {
				t.Fatalf("ParseDeadline(%s) failed: %v", tt.input, err)
			}

			if result == 0 {
				t.Error("expected non-zero timestamp")
			}

			parsedTime := time.Unix(result, 0)
			if parsedTime.Year() < 2025 {
				t.Errorf("expected year >= 2025, got %d", parsedTime.Year())
			}
		})
	}
}

func TestParseDeadlineNegative(t *testing.T) {
	tests := []string{"-2h", "-30m", "-1d"}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := ParseDeadline(input)
			if err == nil {
				t.Errorf("ParseDeadline(%s) should error (negative not supported)", input)
			}
		})
	}
}

func TestParseDeadlineInvalid(t *testing.T) {
	tests := []string{
		"invalid",
		"xyz",
		"2025-99-99",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := ParseDeadline(input)
			if err == nil {
				t.Errorf("ParseDeadline(%s) should return error", input)
			}
		})
	}
}

func TestParseDeadlineLargeValues(t *testing.T) {
	tests := []string{"25h", "100d", "52w"}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			result, err := ParseDeadline(input)
			if err != nil {
				t.Fatalf("ParseDeadline(%s) should succeed: %v", input, err)
			}

			now := time.Now().Unix()
			if result <= now {
				t.Errorf("ParseDeadline(%s) should return future time", input)
			}
		})
	}
}

func TestParseDeadlineEmpty(t *testing.T) {
	result, err := ParseDeadline("")
	if err != nil {
		t.Fatalf("ParseDeadline('') should succeed: %v", err)
	}

	if result != 0 {
		t.Errorf("expected 0 for empty string, got %d", result)
	}
}

func TestFormatDeadline(t *testing.T) {
	now := time.Now().Unix()

	tests := []struct {
		name     string
		deadline int64
		want     string
	}{
		{"zero", 0, ""},
		{"overdue_minutes", now - 1800, "overdue 30m"},
		{"overdue_hours", now - 3600, "overdue 1h"},
		{"overdue_days", now - (2 * 24 * 3600), "overdue 2d"},
		{"due_minutes", now + 1801, "due in 30m"},
		{"due_1hour", now + 3601, "due in 1h"},
		{"due_hours", now + (5*3600 + 1), "due in 5h"},
		{"due_tomorrow", now + (25 * 3600), "due tomorrow"},
		{"due_2days", now + (49 * 3600), "due in 2d"},
		{"due_3days", now + (73 * 3600), "due 20"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDeadline(tt.deadline)

			if tt.want == "" {
				if result != "" {
					t.Errorf("expected empty string for zero deadline, got '%s'", result)
				}
				return
			}

			if tt.name == "due_3days" {
				if !strings.Contains(result, "due 20") {
					t.Errorf("expected date format for 3+ days, got '%s'", result)
				}
				return
			}

			if !strings.Contains(result, tt.want) {
				t.Errorf("expected '%s', got '%s'", tt.want, result)
			}
		})
	}
}

func TestFormatDeadlineEdgeCases(t *testing.T) {
	now := time.Now().Unix()

	result := FormatDeadline(now + 59)
	if result != "due in <1m" {
		t.Errorf("expected 'due in <1m' for < 1 minute, got '%s'", result)
	}

	result = FormatDeadline(now + 1801)
	if result != "due in 30m" {
		t.Errorf("expected 'due in 30m' for 30 minutes, got '%s'", result)
	}

	result = FormatDeadline(now + 3541)
	if !strings.Contains(result, "due in 59m") && !strings.Contains(result, "due in 58m") {
		t.Errorf("expected 'due in 58-59m' for ~59 minutes, got '%s'", result)
	}

	result = FormatDeadline(now + 86399)
	if !strings.Contains(result, "due in 23h") {
		t.Errorf("expected 'due in 23h' for almost 24h, got '%s'", result)
	}

	result = FormatDeadline(now + 86401)
	if result != "due tomorrow" {
		t.Errorf("expected 'due tomorrow' for 24h+1s, got '%s'", result)
	}

	result = FormatDeadline(now + (49 * 3600))
	if !strings.Contains(result, "due in 2d") {
		t.Errorf("expected 'due in 2d', got '%s'", result)
	}

	result = FormatDeadline(now + (73 * 3600))
	if !strings.HasPrefix(result, "due 20") {
		t.Errorf("expected date format for 3+ days, got '%s'", result)
	}
}

func TestFormatDeadline1HourBugRegression(t *testing.T) {
	result := FormatDeadline(time.Now().Unix() + 3598)
	if strings.Contains(result, "0h") {
		t.Errorf("regression: 1h deadline viewed ~2s later should show minutes, not '0h'; got '%s'", result)
	}
	if !strings.Contains(result, "59m") && !strings.Contains(result, "58m") {
		t.Errorf("expected ~59m for nearly-1h deadline, got '%s'", result)
	}
}

