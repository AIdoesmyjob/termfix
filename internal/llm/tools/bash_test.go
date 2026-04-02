package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBashToolInfo(t *testing.T) {
	tool := NewBashTool(nil)
	info := tool.Info()

	assert.Equal(t, BashToolName, info.Name)
	assert.NotEmpty(t, info.Description, "description should not be empty")

	// Verify "command" is in the required parameters
	found := false
	for _, r := range info.Required {
		if r == "command" {
			found = true
			break
		}
	}
	assert.True(t, found, "required parameters should contain 'command'")
}

func TestTruncateOutput(t *testing.T) {
	t.Run("short string no truncation", func(t *testing.T) {
		input := "hello world"
		result := truncateOutput(input)
		assert.Equal(t, input, result)
	})

	t.Run("string at max length no truncation", func(t *testing.T) {
		input := strings.Repeat("a", MaxOutputLength)
		result := truncateOutput(input)
		assert.Equal(t, input, result)
	})

	t.Run("string exceeds max length gets truncated", func(t *testing.T) {
		input := strings.Repeat("a\n", MaxOutputLength)
		result := truncateOutput(input)
		assert.Less(t, len(result), len(input), "truncated output should be shorter than input")
		assert.Contains(t, result, "truncated", "truncated output should contain truncation message")
	})
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty string", "", 0},
		{"single line no newline", "hello", 1},
		{"three lines", "a\nb\nc", 3},
		{"trailing newline", "a\nb\n", 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := countLines(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func bashCtx() context.Context {
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "test-session")
	ctx = context.WithValue(ctx, MessageIDContextKey, "test-message")
	return ctx
}

func TestBashBannedCommand(t *testing.T) {
	tool := NewBashTool(nil)
	ctx := bashCtx()

	for _, cmd := range bannedCommands {
		t.Run(cmd, func(t *testing.T) {
			input, err := json.Marshal(BashParams{Command: cmd + " http://example.com"})
			require.NoError(t, err)

			resp, err := tool.Run(ctx, ToolCall{
				ID:    "test-call",
				Name:  BashToolName,
				Input: string(input),
			})
			// Banned commands should return an error response (not a Go error)
			if err != nil {
				return // also acceptable
			}
			assert.True(t, resp.IsError, "banned command %q should produce an error response", cmd)
			assert.Contains(t, strings.ToLower(resp.Content), "not allowed",
				"error content for banned command %q should mention 'not allowed'", cmd)
		})
	}
}

func TestBashEmptyCommand(t *testing.T) {
	tool := NewBashTool(nil)
	ctx := bashCtx()

	input, err := json.Marshal(BashParams{Command: ""})
	require.NoError(t, err)

	resp, err := tool.Run(ctx, ToolCall{
		ID:    "test-call",
		Name:  BashToolName,
		Input: string(input),
	})
	if err != nil {
		return // acceptable if it returns a Go-level error
	}
	assert.True(t, resp.IsError, "empty command should produce an error response")
}

func TestFormatBashOutput_StdoutOnly(t *testing.T) {
	result := formatBashOutput("hello world", "", 0, false)
	assert.Equal(t, "hello world", result)
	assert.NotContains(t, result, "<stderr>")
}

func TestFormatBashOutput_StderrOnly(t *testing.T) {
	result := formatBashOutput("", "some warning", 0, false)
	assert.Equal(t, "<stderr>\nsome warning\n</stderr>", result)
}

func TestFormatBashOutput_Both(t *testing.T) {
	result := formatBashOutput("stdout line", "stderr line", 0, false)
	assert.Equal(t, "stdout line\n<stderr>\nstderr line\n</stderr>", result)
}

func TestFormatBashOutput_ExitCode(t *testing.T) {
	result := formatBashOutput("output", "", 1, false)
	assert.Equal(t, "output\nExit code 1", result)
}

func TestFormatBashOutput_Interrupted(t *testing.T) {
	result := formatBashOutput("partial", "", 0, true)
	assert.Equal(t, "partial\nCommand was aborted before completion", result)
}

func TestFormatBashOutput_StderrAndExitCode(t *testing.T) {
	result := formatBashOutput("", "error msg", 2, false)
	assert.Equal(t, "<stderr>\nerror msg\n</stderr>\nExit code 2", result)
}

func TestFormatBashOutput_Empty(t *testing.T) {
	result := formatBashOutput("", "", 0, false)
	assert.Equal(t, "", result)
}

func TestBashMissingContextValues(t *testing.T) {
	tool := NewBashTool(nil)
	// Use a bare context without session/message IDs
	ctx := context.Background()

	input, err := json.Marshal(BashParams{Command: "echo hello"})
	require.NoError(t, err)

	resp, err := tool.Run(ctx, ToolCall{
		ID:    "test-call",
		Name:  BashToolName,
		Input: string(input),
	})
	// Should error because context values are missing
	if err != nil {
		assert.Error(t, err)
		return
	}
	assert.True(t, resp.IsError, "missing context values should produce an error response")
}
