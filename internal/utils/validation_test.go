package utils

import (
	"strings"
	"testing"
)

func TestValidateTask(t *testing.T) {
	tests := []struct {
		name    string
		task    string
		wantErr bool
	}{
		{"valid task", "Buy groceries", false},
		{"valid with spaces", "  Task with spaces  ", false},
		{"empty task", "", true},
		{"whitespace only", "   ", false},
		{"tab only", "\t", false},
		{"newline", "test\ntask", true},
		{"carriage return", "test\rtask", true},
		{"null byte", "test\x00task", true},
		{"control char SOH", "test\x01task", true},
		{"control char STX", "test\x02task", true},
		{"bidi override LRO", "test\u202Etask", true},
		{"bidi override RLO", "test\u202Dtask", true},
		{"zero width space", "test\u200Btask", true},
		{"zero width joiner", "test\u200Dtask", true},
		{"zero width non-joiner", "test\u200Ctask", true},
		{"very long acceptable", strings.Repeat("a", 10000), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTask(tt.task)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTask() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateNote(t *testing.T) {
	tests := []struct {
		name    string
		note    string
		wantErr bool
	}{
		{"empty note allowed", "", false},
		{"valid note", "This is a note", false},
		{"newlines allowed", "line1\nline2", false},
		{"tabs allowed", "text\twith\ttabs", false},
		{"very long note", strings.Repeat("a", 50000), false},
		{"null byte", "note\x00text", true},
		{"control char", "note\x01text", true},
		{"bidi override", "note\u202Etext", true},
		{"zero width", "note\u200Btext", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNote(tt.note)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateNote() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateDeadline(t *testing.T) {
	tests := []struct {
		name     string
		deadline string
		wantErr  bool
	}{
		{"empty deadline allowed", "", false},
		{"valid ISO date", "2025-12-31", false},
		{"valid datetime", "2025-12-31 15:00", false},
		{"valid with time", "tomorrow 3pm", false},
		{"null byte", "2025\x0012-31", true},
		{"control char", "2025\x01-12-31", true},
		{"bidi override", "2025\u202E-12-31", true},
		{"zero width", "2025\u200B-12-31", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDeadline(tt.deadline)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDeadline() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
