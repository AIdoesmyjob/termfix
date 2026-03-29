package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupportedModelsPopulated(t *testing.T) {
	assert.Greater(t, len(SupportedModels), 0, "SupportedModels should be populated by init()")
}

func TestSpecificModelsExist(t *testing.T) {
	model, exists := SupportedModels[BedrockClaude37Sonnet]
	require.True(t, exists, "BedrockClaude37Sonnet should exist in SupportedModels")

	assert.NotEmpty(t, model.Provider, "Provider should not be empty")
	assert.NotEmpty(t, model.APIModel, "APIModel should not be empty")
	assert.GreaterOrEqual(t, model.ContextWindow, int64(0), "ContextWindow should be >= 0")
}

func TestModelFieldsValid(t *testing.T) {
	require.Greater(t, len(SupportedModels), 0, "SupportedModels must not be empty")

	for id, model := range SupportedModels {
		t.Run(string(id), func(t *testing.T) {
			assert.NotEmpty(t, model.ID, "ID should not be empty")
			assert.NotEmpty(t, model.Name, "Name should not be empty")
			assert.NotEmpty(t, model.Provider, "Provider should not be empty")
			assert.NotEmpty(t, model.APIModel, "APIModel should not be empty")
		})
	}
}

func TestProviderPopularity(t *testing.T) {
	assert.Greater(t, len(ProviderPopularity), 0, "ProviderPopularity should have entries")

	knownProviders := []ModelProvider{
		ProviderCopilot,
		ProviderAnthropic,
		ProviderOpenAI,
		ProviderGemini,
		ProviderGROQ,
		ProviderOpenRouter,
		ProviderBedrock,
		ProviderAzure,
		ProviderVertexAI,
		// ProviderLocal is only registered when a local server is running at init time
	}

	for _, provider := range knownProviders {
		_, exists := ProviderPopularity[provider]
		assert.True(t, exists, "ProviderPopularity should contain provider %q", provider)
	}
}
