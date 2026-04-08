package provider

import (
	"testing"

	"github.com/openai/openai-go"
	"github.com/AIdoesmyjob/termfix/internal/llm/models"
	"github.com/AIdoesmyjob/termfix/internal/message"
	"github.com/stretchr/testify/assert"
)

func TestFinishReason(t *testing.T) {
	client := &openaiClient{}

	tests := []struct {
		name     string
		input    string
		expected message.FinishReason
	}{
		{
			name:     "stop maps to EndTurn",
			input:    "stop",
			expected: message.FinishReasonEndTurn,
		},
		{
			name:     "length maps to MaxTokens",
			input:    "length",
			expected: message.FinishReasonMaxTokens,
		},
		{
			name:     "tool_calls maps to ToolUse",
			input:    "tool_calls",
			expected: message.FinishReasonToolUse,
		},
		{
			name:     "unknown string maps to Unknown",
			input:    "unknown",
			expected: message.FinishReasonUnknown,
		},
		{
			name:     "empty string maps to Unknown",
			input:    "",
			expected: message.FinishReasonUnknown,
		},
		{
			name:     "arbitrary string maps to Unknown",
			input:    "content_filter",
			expected: message.FinishReasonUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.finishReason(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWithReasoningEffort(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "low is accepted",
			input:    "low",
			expected: "low",
		},
		{
			name:     "medium is accepted",
			input:    "medium",
			expected: "medium",
		},
		{
			name:     "high is accepted",
			input:    "high",
			expected: "high",
		},
		{
			name:     "invalid falls back to medium",
			input:    "invalid",
			expected: "medium",
		},
		{
			name:     "empty falls back to medium",
			input:    "",
			expected: "medium",
		},
		{
			name:     "uppercase is invalid and falls back to medium",
			input:    "HIGH",
			expected: "medium",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := openaiOptions{}
			WithReasoningEffort(tt.input)(&opts)
			assert.Equal(t, tt.expected, opts.reasoningEffort)
		})
	}
}

func TestWithOpenAIBaseURL(t *testing.T) {
	opts := openaiOptions{}
	WithOpenAIBaseURL("https://custom.api.example.com/v1")(&opts)
	assert.Equal(t, "https://custom.api.example.com/v1", opts.baseURL)
}

func TestWithOpenAIBaseURLEmpty(t *testing.T) {
	opts := openaiOptions{}
	WithOpenAIBaseURL("")(&opts)
	assert.Equal(t, "", opts.baseURL)
}

func TestWithOpenAIExtraHeaders(t *testing.T) {
	headers := map[string]string{
		"X-Custom-Header": "value1",
		"Authorization":   "Bearer token",
	}
	opts := openaiOptions{}
	WithOpenAIExtraHeaders(headers)(&opts)

	assert.Equal(t, headers, opts.extraHeaders)
	assert.Equal(t, "value1", opts.extraHeaders["X-Custom-Header"])
	assert.Equal(t, "Bearer token", opts.extraHeaders["Authorization"])
}

func TestWithOpenAIExtraHeadersNil(t *testing.T) {
	opts := openaiOptions{}
	WithOpenAIExtraHeaders(nil)(&opts)
	assert.Nil(t, opts.extraHeaders)
}

func TestWithOpenAIDisableCache(t *testing.T) {
	opts := openaiOptions{}
	assert.False(t, opts.disableCache, "disableCache should default to false")

	WithOpenAIDisableCache()(&opts)
	assert.True(t, opts.disableCache)
}

func TestOpenAIOptionsChaining(t *testing.T) {
	opts := openaiOptions{}

	WithOpenAIBaseURL("https://api.example.com")(&opts)
	WithReasoningEffort("high")(&opts)
	WithOpenAIExtraHeaders(map[string]string{"X-Key": "val"})(&opts)
	WithOpenAIDisableCache()(&opts)

	assert.Equal(t, "https://api.example.com", opts.baseURL)
	assert.Equal(t, "high", opts.reasoningEffort)
	assert.Equal(t, map[string]string{"X-Key": "val"}, opts.extraHeaders)
	assert.True(t, opts.disableCache)
}

func TestPreparedParamsEmptyTools(t *testing.T) {
	client := &openaiClient{
		providerOptions: providerClientOptions{
			model:     models.Model{APIModel: "test-model"},
			maxTokens: 1024,
		},
		options: openaiOptions{reasoningEffort: "medium"},
	}

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("test"),
	}

	t.Run("nil tools omits tools field", func(t *testing.T) {
		params := client.preparedParams(msgs, nil)
		assert.Nil(t, params.Tools, "nil tools should not populate params.Tools")
	})

	t.Run("empty tools omits tools field", func(t *testing.T) {
		params := client.preparedParams(msgs, []openai.ChatCompletionToolParam{})
		assert.Nil(t, params.Tools, "empty tools should not populate params.Tools")
	})

	t.Run("non-empty tools populates tools field", func(t *testing.T) {
		tools := []openai.ChatCompletionToolParam{
			{Function: openai.FunctionDefinitionParam{Name: "bash"}},
		}
		params := client.preparedParams(msgs, tools)
		assert.Len(t, params.Tools, 1, "non-empty tools should be included")
	})
}
