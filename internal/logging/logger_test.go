package logging

import (
	"testing"
)

func TestGetSessionPrefix(t *testing.T) {
	tests := []struct {
		name      string
		sessionId string
		want      string
	}{
		{
			name:      "standard uuid",
			sessionId: "550e8400-e29b-41d4-a716-446655440000",
			want:      "550e8400",
		},
		{
			name:      "exactly 8 chars",
			sessionId: "abcdefgh",
			want:      "abcdefgh",
		},
		{
			name:      "longer than 8 chars",
			sessionId: "abcdefghijklmnop",
			want:      "abcdefgh",
		},
		{
			name:      "numeric session id",
			sessionId: "1234567890",
			want:      "12345678",
		},
		{
			name:      "hex string",
			sessionId: "deadbeefcafe",
			want:      "deadbeef",
		},
		{
			name:      "special characters",
			sessionId: "a-b_c.d!efghijk",
			want:      "a-b_c.d!",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetSessionPrefix(tt.sessionId)
			if got != tt.want {
				t.Errorf("GetSessionPrefix(%q) = %q, want %q", tt.sessionId, got, tt.want)
			}
		})
	}
}

func TestGetSessionPrefixPanicsOnShortInput(t *testing.T) {
	// GetSessionPrefix slices sessionId[:8], so inputs shorter than 8 chars
	// will cause a panic. We verify this behavior.
	shortInputs := []string{"", "a", "1234567"}

	for _, input := range shortInputs {
		t.Run("panics on: "+input, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("GetSessionPrefix(%q) did not panic, but expected panic for input shorter than 8 chars", input)
				}
			}()
			GetSessionPrefix(input)
		})
	}
}
