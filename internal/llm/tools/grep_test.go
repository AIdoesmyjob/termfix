package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrepToolInfo(t *testing.T) {
	tool := NewGrepTool()
	info := tool.Info()

	assert.Equal(t, GrepToolName, info.Name)
	assert.NotEmpty(t, info.Description, "description should not be empty")

	found := false
	for _, r := range info.Required {
		if r == "pattern" {
			found = true
			break
		}
	}
	assert.True(t, found, "required parameters should contain 'pattern'")
}

func TestEscapeRegexPattern(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text unchanged",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "dot escaped",
			input:    "hello.world",
			expected: `hello\.world`,
		},
		{
			name:     "plus and star escaped",
			input:    "a+b*c",
			expected: `a\+b\*c`,
		},
		{
			name:     "parentheses escaped",
			input:    "func()",
			expected: `func\(\)`,
		},
		{
			name:     "brackets escaped",
			input:    "arr[0]",
			expected: `arr\[0\]`,
		},
		{
			name:     "caret and dollar escaped",
			input:    "^start$",
			expected: `\^start\$`,
		},
		{
			name:     "backslash escaped",
			input:    `path\to`,
			expected: `path\\to`,
		},
		{
			name:     "pipe escaped",
			input:    "a|b",
			expected: `a\|b`,
		},
		{
			name:     "question mark escaped",
			input:    "maybe?",
			expected: `maybe\?`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := escapeRegexPattern(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGlobToRegex(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(t *testing.T, result string)
	}{
		{
			name:  "star dot go",
			input: "*.go",
			check: func(t *testing.T, result string) {
				assert.Contains(t, result, ".go")
				// The star should be converted to a wildcard pattern
				assert.NotEqual(t, "*.go", result, "glob should be transformed")
			},
		},
		{
			name:  "brace expansion",
			input: "*.{ts,tsx}",
			check: func(t *testing.T, result string) {
				// Braces should be converted to alternation
				assert.Contains(t, result, "ts")
				assert.Contains(t, result, "tsx")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := globToRegex(tc.input)
			tc.check(t, result)
		})
	}
}

func TestGrepRunEmptyPattern(t *testing.T) {
	tool := NewGrepTool()
	ctx := context.Background()

	input, err := json.Marshal(GrepParams{Pattern: "", Path: "/tmp"})
	require.NoError(t, err)

	resp, err := tool.Run(ctx, ToolCall{
		ID:    "test-call",
		Name:  GrepToolName,
		Input: string(input),
	})
	if err != nil {
		assert.Error(t, err)
		return
	}
	assert.True(t, resp.IsError, "empty pattern should produce an error response")
}

func TestGrepRunWithTempDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files with searchable content
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "main.go"),
		[]byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello world\")\n}\n"),
		0644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "lib.go"),
		[]byte("package main\n\nfunc helper() string {\n\treturn \"helper result\"\n}\n"),
		0644,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "notes.txt"),
		[]byte("some notes\nnothing to see here\n"),
		0644,
	))

	tool := NewGrepTool()
	ctx := context.Background()

	t.Run("search for func", func(t *testing.T) {
		input, err := json.Marshal(GrepParams{Pattern: "func", Path: tmpDir})
		require.NoError(t, err)

		resp, err := tool.Run(ctx, ToolCall{
			ID:    "test-call",
			Name:  GrepToolName,
			Input: string(input),
		})
		require.NoError(t, err)
		assert.False(t, resp.IsError, "grep should not return error for valid pattern")
		assert.Contains(t, resp.Content, "func", "result should contain matched content")
	})

	t.Run("search with include filter", func(t *testing.T) {
		input, err := json.Marshal(GrepParams{Pattern: "package", Path: tmpDir, Include: "*.go"})
		require.NoError(t, err)

		resp, err := tool.Run(ctx, ToolCall{
			ID:    "test-call",
			Name:  GrepToolName,
			Input: string(input),
		})
		require.NoError(t, err)
		assert.False(t, resp.IsError, "grep should not return error")
		assert.Contains(t, resp.Content, "package", "result should contain matched content")
	})

	t.Run("no matches", func(t *testing.T) {
		input, err := json.Marshal(GrepParams{Pattern: "ZZZZNOTFOUND", Path: tmpDir})
		require.NoError(t, err)

		resp, err := tool.Run(ctx, ToolCall{
			ID:    "test-call",
			Name:  GrepToolName,
			Input: string(input),
		})
		// No matches may return error or empty response depending on implementation
		if err != nil {
			return
		}
		// If no error, content should be empty or indicate no matches
		assert.NotContains(t, resp.Content, "ZZZZNOTFOUND")
	})
}

func TestGrepLiteralText(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file with regex special characters in content
	require.NoError(t, os.WriteFile(
		filepath.Join(tmpDir, "special.txt"),
		[]byte("price is $100.00\nregex: a+b*c\n"),
		0644,
	))

	tool := NewGrepTool()
	ctx := context.Background()

	input, err := json.Marshal(GrepParams{
		Pattern:     "$100.00",
		Path:        tmpDir,
		LiteralText: true,
	})
	require.NoError(t, err)

	resp, err := tool.Run(ctx, ToolCall{
		ID:    "test-call",
		Name:  GrepToolName,
		Input: string(input),
	})
	require.NoError(t, err)
	assert.False(t, resp.IsError, "literal text grep should not error")
	assert.Contains(t, resp.Content, "100", "should find the literal text match")
}
