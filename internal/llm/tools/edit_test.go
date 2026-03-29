package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEditToolInfo(t *testing.T) {
	tool := NewEditTool(nil, nil, nil)
	info := tool.Info()

	assert.Equal(t, EditToolName, info.Name)
	assert.NotEmpty(t, info.Description, "description should not be empty")

	requiredParams := map[string]bool{
		"file_path":  false,
		"old_string": false,
		"new_string": false,
	}
	for _, r := range info.Required {
		if _, ok := requiredParams[r]; ok {
			requiredParams[r] = true
		}
	}
	for param, found := range requiredParams {
		assert.True(t, found, "required parameters should contain %q", param)
	}
}
