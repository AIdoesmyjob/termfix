package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewToolInfo(t *testing.T) {
	tool := NewViewTool(nil)
	info := tool.Info()

	assert.Equal(t, ViewToolName, info.Name)
	assert.NotEmpty(t, info.Description, "description should not be empty")

	found := false
	for _, r := range info.Required {
		if r == "file_path" {
			found = true
			break
		}
	}
	assert.True(t, found, "required parameters should contain 'file_path'")
}

func TestAddLineNumbers(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		startLine int
		checks    func(t *testing.T, result string)
	}{
		{
			name:      "basic two lines",
			content:   "line1\nline2",
			startLine: 1,
			checks: func(t *testing.T, result string) {
				lines := strings.Split(result, "\n")
				require.GreaterOrEqual(t, len(lines), 2)
				assert.Contains(t, lines[0], "1")
				assert.Contains(t, lines[0], "line1")
				assert.Contains(t, lines[1], "2")
				assert.Contains(t, lines[1], "line2")
			},
		},
		{
			name:      "start at offset",
			content:   "alpha\nbeta",
			startLine: 10,
			checks: func(t *testing.T, result string) {
				assert.Contains(t, result, "10")
				assert.Contains(t, result, "alpha")
				assert.Contains(t, result, "11")
				assert.Contains(t, result, "beta")
			},
		},
		{
			name:      "single line",
			content:   "only one line",
			startLine: 1,
			checks: func(t *testing.T, result string) {
				assert.Contains(t, result, "1")
				assert.Contains(t, result, "only one line")
			},
		},
		{
			name:      "empty string",
			content:   "",
			startLine: 1,
			checks: func(t *testing.T, result string) {
				assert.Contains(t, result, "1")
				assert.Contains(t, result, "|")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := addLineNumbers(tc.content, tc.startLine)
			tc.checks(t, result)
		})
	}
}

func TestIsImageFile(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		isImage  bool
		imgType  string
	}{
		{"png file", "screenshot.png", true, "PNG"},
		{"jpg file", "photo.jpg", true, "JPEG"},
		{"jpeg file", "photo.jpeg", true, "JPEG"},
		{"gif file", "animation.gif", true, "GIF"},
		{"webp file", "image.webp", true, "WebP"},
		{"svg file", "icon.svg", true, "SVG"},
		{"go file", "main.go", false, ""},
		{"txt file", "readme.txt", false, ""},
		{"no extension", "Makefile", false, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isImg, imgType := isImageFile(tc.filePath)
			assert.Equal(t, tc.isImage, isImg, "isImageFile(%q) image check", tc.filePath)
			if tc.isImage {
				assert.Equal(t, tc.imgType, imgType, "isImageFile(%q) type", tc.filePath)
			}
		})
	}
}

func TestReadTextFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	content := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0644))

	t.Run("read entire file", func(t *testing.T) {
		result, totalLines, err := readTextFile(filePath, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, 5, totalLines, "file should have 5 lines")
		assert.Contains(t, result, "line 1")
		assert.Contains(t, result, "line 5")
	})

	t.Run("read with offset and limit", func(t *testing.T) {
		result, _, err := readTextFile(filePath, 3, 2)
		require.NoError(t, err)
		assert.Contains(t, result, "line 3")
		assert.Contains(t, result, "line 4")
		assert.NotContains(t, result, "line 1")
		assert.NotContains(t, result, "line 5")
	})

	t.Run("read with offset beyond file", func(t *testing.T) {
		result, _, err := readTextFile(filePath, 100, 5)
		require.NoError(t, err)
		assert.Empty(t, result, "reading beyond file should return empty")
	})
}

func TestReadTextFileNonExistent(t *testing.T) {
	_, _, err := readTextFile("/tmp/nonexistent-file-12345.txt", 0, 0)
	assert.Error(t, err, "reading non-existent file should return error")
}

func TestViewRunMissingFile(t *testing.T) {
	tool := NewViewTool(nil)
	ctx := context.Background()

	input, err := json.Marshal(ViewParams{FilePath: "/tmp/absolutely-does-not-exist-99999.txt"})
	require.NoError(t, err)

	resp, err := tool.Run(ctx, ToolCall{
		ID:    "test-call",
		Name:  ViewToolName,
		Input: string(input),
	})
	if err != nil {
		assert.Error(t, err)
		return
	}
	assert.True(t, resp.IsError, "viewing non-existent file should produce an error response")
}

func TestViewRunValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "viewable.go")

	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("// line %d", i))
	}
	content := strings.Join(lines, "\n") + "\n"
	require.NoError(t, os.WriteFile(filePath, []byte(content), 0644))

	tool := NewViewTool(nil)
	ctx := context.Background()

	t.Run("view full file", func(t *testing.T) {
		input, err := json.Marshal(ViewParams{FilePath: filePath})
		require.NoError(t, err)

		resp, err := tool.Run(ctx, ToolCall{
			ID:    "test-call",
			Name:  ViewToolName,
			Input: string(input),
		})
		require.NoError(t, err)
		assert.False(t, resp.IsError, "viewing valid file should not error")
		assert.Contains(t, resp.Content, "line 1")
		assert.Contains(t, resp.Content, "line 10")
	})

	t.Run("view with offset and limit", func(t *testing.T) {
		input, err := json.Marshal(ViewParams{FilePath: filePath, Offset: 5, Limit: 3})
		require.NoError(t, err)

		resp, err := tool.Run(ctx, ToolCall{
			ID:    "test-call",
			Name:  ViewToolName,
			Input: string(input),
		})
		require.NoError(t, err)
		assert.False(t, resp.IsError, "viewing with offset/limit should not error")
		assert.Contains(t, resp.Content, "line 5")
		assert.Contains(t, resp.Content, "line 7")
		assert.NotContains(t, resp.Content, "line 4")
	})
}

func TestViewRunEmptyFilePath(t *testing.T) {
	tool := NewViewTool(nil)
	ctx := context.Background()

	input, err := json.Marshal(ViewParams{FilePath: ""})
	require.NoError(t, err)

	resp, err := tool.Run(ctx, ToolCall{
		ID:    "test-call",
		Name:  ViewToolName,
		Input: string(input),
	})
	if err != nil {
		assert.Error(t, err)
		return
	}
	assert.True(t, resp.IsError, "empty file path should produce an error response")
}
