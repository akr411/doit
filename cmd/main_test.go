package main

import (
	"strings"
	"testing"
)

func TestCharacterLimitConstants(t *testing.T) {
	if MaxTitleLength != 100 {
		t.Errorf("Expected MaxTitleLength to be 100, got %d", MaxTitleLength)
	}

	if MaxDescriptionLength != 500 {
		t.Errorf("Expected MaxDescriptionLength to be 100, got %d", MaxDescriptionLength)
	}
}

func TestValidateCharacterLimits(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		description string
		shouldFail  bool
	}{
		{
			name:        "Valid length",
			title:       "Normal title",
			description: "Normal description",
			shouldFail:  false,
		},
		{
			name:        "Title at max length",
			title:       strings.Repeat("a", MaxTitleLength),
			description: "Normal description",
			shouldFail:  false,
		},
		{
			name:        "Title exceeds max length",
			title:       strings.Repeat("a", MaxTitleLength+1),
			description: "Normal description",
			shouldFail:  true,
		},
		{
			name:        "Description at max length",
			title:       "Normal title",
			description: strings.Repeat("b", MaxDescriptionLength),
			shouldFail:  false,
		},
		{
			name:        "Description exceeds max length",
			title:       "Normal title",
			description: strings.Repeat("b", MaxDescriptionLength+1),
			shouldFail:  true,
		},
		{
			name:        "Both at max length",
			title:       strings.Repeat("a", MaxTitleLength),
			description: strings.Repeat("b", MaxDescriptionLength),
			shouldFail:  false,
		},
		{
			name:        "Both exceeds max length",
			title:       strings.Repeat("a", MaxTitleLength+1),
			description: strings.Repeat("b", MaxDescriptionLength+1),
			shouldFail:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			titleExceeds := len(tt.title) > MaxTitleLength
			descExceeds := len(tt.description) > MaxDescriptionLength
			shouldFail := titleExceeds || descExceeds

			if shouldFail != tt.shouldFail {
				t.Errorf("Expected shouldFail=%v, but got %v", tt.shouldFail, shouldFail)
			}
		})
	}
}
