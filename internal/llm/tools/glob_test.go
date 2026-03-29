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

func TestGlobToolInfo(t *testing.T) {
	tool := NewGlobTool()
	info := tool.Info()

	assert.Equal(t, GlobToolName, info.Name)
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

func TestGlobRunEmptyPattern(t *testing.T) {
	tool := NewGlobTool()
	ctx := context.Background()

	input, err := json.Marshal(GlobParams{Pattern: "", Path: "/tmp"})
	require.NoError(t, err)

	resp, err := tool.Run(ctx, ToolCall{
		ID:    "test-call",
		Name:  GlobToolName,
		Input: string(input),
	})
	if err != nil {
		assert.Error(t, err)
		return
	}
	assert.True(t, resp.IsError, "empty pattern should produce an error response")
}

func TestGlobRunWithTempDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a.go"), []byte("package a"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("hello"), 0644))
	subDir := filepath.Join(tmpDir, "sub")
	require.NoError(t, os.MkdirAll(subDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "c.go"), []byte("package c"), 0644))

	tool := NewGlobTool()
	ctx := context.Background()

	t.Run("match go files", func(t *testing.T) {
		input, err := json.Marshal(GlobParams{Pattern: "**/*.go", Path: tmpDir})
		require.NoError(t, err)

		resp, err := tool.Run(ctx, ToolCall{
			ID:    "test-call",
			Name:  GlobToolName,
			Input: string(input),
		})
		require.NoError(t, err)
		assert.False(t, resp.IsError, "glob should not return error for valid pattern")
		assert.Contains(t, resp.Content, "a.go", "result should contain a.go")
		assert.Contains(t, resp.Content, "c.go", "result should contain sub/c.go")
		assert.NotContains(t, resp.Content, "b.txt", "result should not contain b.txt")
	})

	t.Run("match txt files", func(t *testing.T) {
		input, err := json.Marshal(GlobParams{Pattern: "*.txt", Path: tmpDir})
		require.NoError(t, err)

		resp, err := tool.Run(ctx, ToolCall{
			ID:    "test-call",
			Name:  GlobToolName,
			Input: string(input),
		})
		require.NoError(t, err)
		assert.False(t, resp.IsError, "glob should not return error for valid pattern")
		assert.Contains(t, resp.Content, "b.txt", "result should contain b.txt")
	})

	t.Run("no matches", func(t *testing.T) {
		input, err := json.Marshal(GlobParams{Pattern: "*.rs", Path: tmpDir})
		require.NoError(t, err)

		resp, err := tool.Run(ctx, ToolCall{
			ID:    "test-call",
			Name:  GlobToolName,
			Input: string(input),
		})
		require.NoError(t, err)
		// No matches should still succeed, just with empty or "no matches" content
		assert.False(t, resp.IsError, "no matches should not be an error")
	})
}
