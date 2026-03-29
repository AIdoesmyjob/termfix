package provider

import (
	"testing"

	"github.com/opencode-ai/opencode/internal/llm/models"
	"github.com/opencode-ai/opencode/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithAPIKey(t *testing.T) {
	opts := providerClientOptions{}
	WithAPIKey("key123")(&opts)
	assert.Equal(t, "key123", opts.apiKey)
}

func TestWithAPIKeyEmpty(t *testing.T) {
	opts := providerClientOptions{}
	WithAPIKey("")(&opts)
	assert.Equal(t, "", opts.apiKey)
}

func TestWithModel(t *testing.T) {
	model := models.Model{
		ID:       "test-model",
		Name:     "Test Model",
		Provider: models.ProviderOpenAI,
	}
	opts := providerClientOptions{}
	WithModel(model)(&opts)
	assert.Equal(t, model, opts.model)
	assert.Equal(t, models.ModelID("test-model"), opts.model.ID)
}

func TestWithMaxTokens(t *testing.T) {
	opts := providerClientOptions{}
	WithMaxTokens(1000)(&opts)
	assert.Equal(t, int64(1000), opts.maxTokens)
}

func TestWithMaxTokensZero(t *testing.T) {
	opts := providerClientOptions{}
	WithMaxTokens(0)(&opts)
	assert.Equal(t, int64(0), opts.maxTokens)
}

func TestWithSystemMessage(t *testing.T) {
	opts := providerClientOptions{}
	WithSystemMessage("You are a helpful assistant.")(&opts)
	assert.Equal(t, "You are a helpful assistant.", opts.systemMessage)
}

func TestWithSystemMessageEmpty(t *testing.T) {
	opts := providerClientOptions{}
	WithSystemMessage("")(&opts)
	assert.Equal(t, "", opts.systemMessage)
}

func TestMultipleOptions(t *testing.T) {
	model := models.Model{
		ID:       "gpt-4",
		Name:     "GPT-4",
		Provider: models.ProviderOpenAI,
	}
	opts := providerClientOptions{}

	WithAPIKey("sk-test")(&opts)
	WithModel(model)(&opts)
	WithMaxTokens(2048)(&opts)
	WithSystemMessage("system prompt")(&opts)

	assert.Equal(t, "sk-test", opts.apiKey)
	assert.Equal(t, model, opts.model)
	assert.Equal(t, int64(2048), opts.maxTokens)
	assert.Equal(t, "system prompt", opts.systemMessage)
}

func TestCleanMessagesFiltersEmpty(t *testing.T) {
	bp := &baseProvider[OpenAIClient]{}

	messages := []message.Message{
		{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: "hello"}},
		},
		{
			Role:  message.User,
			Parts: []message.ContentPart{},
		},
		{
			Role:  message.Assistant,
			Parts: nil,
		},
		{
			Role:  message.Assistant,
			Parts: []message.ContentPart{message.TextContent{Text: "world"}},
		},
	}

	cleaned := bp.cleanMessages(messages)

	require.Len(t, cleaned, 2)
	assert.Equal(t, "hello", cleaned[0].Content().Text)
	assert.Equal(t, "world", cleaned[1].Content().Text)
}

func TestCleanMessagesAllEmpty(t *testing.T) {
	bp := &baseProvider[OpenAIClient]{}

	messages := []message.Message{
		{Role: message.User, Parts: nil},
		{Role: message.User, Parts: []message.ContentPart{}},
	}

	cleaned := bp.cleanMessages(messages)
	assert.Empty(t, cleaned)
}

func TestCleanMessagesNoneEmpty(t *testing.T) {
	bp := &baseProvider[OpenAIClient]{}

	messages := []message.Message{
		{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: "first"}},
		},
		{
			Role:  message.Assistant,
			Parts: []message.ContentPart{message.TextContent{Text: "second"}},
		},
	}

	cleaned := bp.cleanMessages(messages)
	require.Len(t, cleaned, 2)
	assert.Equal(t, "first", cleaned[0].Content().Text)
	assert.Equal(t, "second", cleaned[1].Content().Text)
}

func TestCleanMessagesEmptyInput(t *testing.T) {
	bp := &baseProvider[OpenAIClient]{}

	cleaned := bp.cleanMessages(nil)
	assert.Empty(t, cleaned)

	cleaned = bp.cleanMessages([]message.Message{})
	assert.Empty(t, cleaned)
}

func TestNewProviderUnsupported(t *testing.T) {
	_, err := NewProvider("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider not supported")
}

func TestNewProviderMockWorks(t *testing.T) {
	p, err := NewProvider(models.ProviderMock)
	require.NoError(t, err)
	assert.NotNil(t, p)
}
