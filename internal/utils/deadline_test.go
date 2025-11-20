package utils

import (
	"strings"
	"testing"
	"time"
)

func TestParseDeadline_AbsoluteFormat(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{
			name:      "valid absolute format",
			input:     "2025-11-16 14:30",
			wantError: false,
		},
		{
			name:      "invalid month",
			input:     "2025-13-01 14:30",
			wantError: true,
		},
		{
			name:      "invalid format",
			input:     "2025/11/16 14:30",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDeadline(tt.input)
			if tt.wantError {
				if err == nil {
					t.Errorf("ParseDeadline(%s) expected error but got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("ParseDeadline(%s) unexpected error: %v", tt.input, err)
				}
				if result == nil {
					t.Errorf("ParseDeadline(%s) returned nil time", tt.input)
				}
			}
		})
	}
}

func TestParseDeadline_RelativeFormat_SingleUnits(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name          string
		input         string
		expectedDelta time.Duration
		tolerance     time.Duration
	}{
		{
			name:          "30 minutes",
			input:         "30m",
			expectedDelta: 30 * time.Minute,
			tolerance:     time.Second,
		},
		{
			name:          "2 hours",
			input:         "2h",
			expectedDelta: 2 * time.Hour,
			tolerance:     time.Second,
		},
		{
			name:          "1 day",
			input:         "1d",
			expectedDelta: 24 * time.Hour,
			tolerance:     time.Second,
		},
		{
			name:          "2 weeks",
			input:         "2w",
			expectedDelta: 2 * 7 * 24 * time.Hour,
			tolerance:     time.Second,
		},
		{
			name:          "1 month",
			input:         "1M",
			expectedDelta: 30 * 24 * time.Hour,
			tolerance:     48 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDeadline(tt.input)
			if err != nil {
				t.Fatalf("ParseDeadline(%s) unexpected error: %v", tt.input, err)
			}
			if result == nil {
				t.Fatalf("ParseDeadline(%s) returned nil", tt.input)
			}

			actualDelta := result.Sub(now)
			if strings.Contains(tt.input, "M") {
				expectedMonths, _ := time.ParseDuration("720h")
				if actualDelta < expectedMonths-tt.tolerance || actualDelta > expectedMonths+tt.tolerance {
					t.Errorf("ParseDeadline(%s) delta = %v, expected around %v (+-%v)",
						tt.input, actualDelta, tt.expectedDelta, tt.tolerance)
				}
			} else {
				diff := actualDelta - tt.expectedDelta
				if diff < -tt.tolerance || diff > tt.tolerance {
					t.Errorf("ParseDeadline(%s) delta = %v, expected %v (+-%v)",
						tt.input, actualDelta, tt.expectedDelta, tt.tolerance)
				}
			}
		})
	}
}

func TestParseDeadline_RelativeFormat_Combinations(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name          string
		input         string
		expectedDelta time.Duration
		tolerance     time.Duration
	}{
		{
			name:          "2 days 3 hours",
			input:         "2d 3h",
			expectedDelta: 2*24*time.Hour + 3*time.Hour,
			tolerance:     time.Second,
		},
		{
			name:          "1 week 2 days",
			input:         "1w 2d",
			expectedDelta: 7*24*time.Hour + 2*24*time.Hour,
			tolerance:     time.Second,
		},
		{
			name:          "complex combination",
			input:         "1w 2d 3h 30m",
			expectedDelta: 7*24*time.Hour + 2*24*time.Hour + 3*time.Hour + 30*time.Minute,
			tolerance:     time.Second,
		},
		{
			name:          "order independence",
			input:         "30m 3h 2d",
			expectedDelta: 2*24*time.Hour + 3*time.Hour + 30*time.Minute,
			tolerance:     time.Second,
		},
		{
			name:          "no spaces",
			input:         "2d3h30m",
			expectedDelta: 2*24*time.Hour + 3*time.Hour + 30*time.Minute,
			tolerance:     time.Second,
		},
		{
			name:          "multiple spaces",
			input:         "2d  3h   30m",
			expectedDelta: 2*24*time.Hour + 3*time.Hour + 30*time.Minute,
			tolerance:     time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDeadline(tt.input)
			if err != nil {
				t.Fatalf("ParseDeadline(%s) unexpected error: %v", tt.input, err)
			}
			if result == nil {
				t.Fatalf("ParseDeadline(%s) returned nil", tt.input)
			}

			actualDelta := result.Sub(now)
			diff := actualDelta - tt.expectedDelta
			if diff < -tt.tolerance || diff > tt.tolerance {
				t.Errorf("ParseDeadline(%s) delta = %v, expected %v (+-%v)",
					tt.input, actualDelta, tt.expectedDelta, tt.tolerance)
			}
		})
	}
}

func TestParseDeadline_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"lowercase", "2d 3h"},
		{"uppercase", "2D 3H"},
		{"mixed", "2D 3h"},
	}

	results := make([]*time.Time, len(tests))
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDeadline(tt.input)
			if err != nil {
				t.Fatalf("ParseDeadline(%s) unexpected error: %v", tt.input, err)
			}
			results[i] = result
		})
	}

	if len(results) > 1 {
		firstResult := results[0]
		for i := 1; i < len(results); i++ {
			diff := results[i].Sub(*firstResult)
			if diff < -time.Second || diff > time.Second {
				t.Errorf("Case sensitivity issue: %s produces different result", tests[i].input)
			}
		}
	}
}

func TestParseDeadline_ErrorCases(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantErrText string
	}{
		{
			name:        "empty string",
			input:       "",
			wantErrText: "cannot be empty",
		},
		{
			name:        "whitespaces only",
			input:       "   ",
			wantErrText: "cannot be empty",
		},
		{
			name:        "invalid unit",
			input:       "5x",
			wantErrText: "no valid time units",
		},
		{
			name:        "no number",
			input:       "d",
			wantErrText: "no valid time units",
		},
		{
			name:        "negative value",
			input:       "-2d",
			wantErrText: "invalid characters",
		},
		{
			name:        "zero values",
			input:       "0d",
			wantErrText: "must be positive",
		},
		{
			name:        "mixed valid and invalid",
			input:       "2d 3x",
			wantErrText: "invalid characters",
		},
		{
			name:        "invalid text",
			input:       "tomorrow",
			wantErrText: "no valid time units",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDeadline(tt.input)
			if err == nil {
				t.Errorf("ParseDeadline(%s) expected error but got nil", tt.input)
				return
			}
			if !strings.Contains(err.Error(), tt.wantErrText) {
				t.Errorf("ParseDeadline(%s) error = %v, want error containing %q",
					tt.input, err, tt.wantErrText)
			}
		})
	}
}

func TestParseRelativeTime_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{
			name:      "very large values",
			input:     "999d",
			wantError: false,
		},
		{
			name:      "multiple same units",
			input:     "2d 3d",
			wantError: false,
		},
		{
			name:      "decimal not supported",
			input:     "1.5h",
			wantError: true,
		},
		{
			name:      "special characters",
			input:     "2d+3h",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseRelativeTime(tt.input)
			if tt.wantError {
				if err == nil {
					t.Errorf("parseRelativeTime(%s) expected error but got result: %v", tt.input, result)
				}
			} else {
				if err != nil {
					t.Errorf("parseRelativeTime(%s) unexpected error: %v", tt.input, err)
				}
				if result <= 0 {
					t.Errorf("parseRelativeTime(%s) returned non-positive duration: %v", tt.input, result)
				}
			}
		})
	}
}

func TestFormatDeadlineHelp(t *testing.T) {
	help := FormatDeadlineHelp()

	expectedStrings := []string{
		"Deadline formats",
		"YYYY-MM-DD HH:MM",
		"minutes",
		"hours",
		"days",
		"weeks",
		"months",
		"Combinations",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(help, expected) {
			t.Errorf("FormatDeadlineHelp() missing expected text: %q", expected)
		}
	}
}
