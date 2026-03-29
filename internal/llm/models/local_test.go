package models

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFriendlyModelName(t *testing.T) {
	tests := []struct {
		name     string
		modelID  string
		contains string
	}{
		{
			name:     "qwen gguf model",
			modelID:  "qwen3.5-0.8b-q4_k_m.gguf",
			contains: "Qwen",
		},
		{
			name:     "llama with tag contains Llama",
			modelID:  "llama-3-8b@latest",
			contains: "Llama",
		},
		{
			name:     "llama with tag contains latest",
			modelID:  "llama-3-8b@latest",
			contains: "latest",
		},
		{
			name:     "model with slash",
			modelID:  "model/name",
			contains: "Name",
		},
		{
			name:     "plain model name",
			modelID:  "gpt4",
			contains: "Gpt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := friendlyModelName(tt.modelID)
			assert.Contains(t, result, tt.contains,
				"friendlyModelName(%q) = %q, expected it to contain %q",
				tt.modelID, result, tt.contains)
		})
	}
}

func TestConvertLocalModel(t *testing.T) {
	lm := localModel{
		ID:                  "test-model",
		LoadedContextLength: 8192,
	}

	result := convertLocalModel(lm)

	assert.Equal(t, ModelID("local.test-model"), result.ID)
	assert.Equal(t, ProviderLocal, result.Provider)
	assert.Equal(t, int64(8192), result.ContextWindow)
}

func TestConvertLocalModelDefaults(t *testing.T) {
	lm := localModel{
		ID:                  "default-ctx-model",
		LoadedContextLength: 0,
	}

	result := convertLocalModel(lm)

	assert.Equal(t, int64(4096), result.ContextWindow,
		"ContextWindow should default to 4096 when LoadedContextLength is 0")
}

func TestIsQwen35Model(t *testing.T) {
	assert.True(t, isQwen35Model("qwen3.5-0.8b-q4_k_m.gguf"))
	assert.True(t, isQwen35Model("Qwen3.5-1B"))
	assert.True(t, isQwen35Model("qwen3_5-0.8b"))
	assert.False(t, isQwen35Model("qwen2.5-7b"))
	assert.False(t, isQwen35Model("llama-3-8b"))
	assert.False(t, isQwen35Model("gpt-4o"))
}

func TestConvertLocalModelQwen35Defaults(t *testing.T) {
	lm := localModel{
		ID:                  "qwen3.5-0.8b-q4_k_m.gguf",
		LoadedContextLength: 8192,
	}

	result := convertLocalModel(lm)

	assert.Equal(t, int64(8192), result.ContextWindow,
		"Qwen 3.5 should keep 8192 context (Pass 1 needs ~5700 tokens for system+tools)")
	assert.Equal(t, int64(1024), result.DefaultMaxTokens,
		"Qwen 3.5 should have maxTokens of 1024 to prevent runaway generation")
}

func TestConvertLocalModelQwen35DefaultContext(t *testing.T) {
	// When LoadedContextLength is 0 (default 4096), Qwen 3.5 should bump to 8192
	lm := localModel{
		ID:                  "qwen3_5-0.8b",
		LoadedContextLength: 0,
	}

	result := convertLocalModel(lm)

	assert.Equal(t, int64(8192), result.ContextWindow,
		"Qwen 3.5 with default 4096 context should be bumped to 8192")
	assert.Equal(t, int64(1024), result.DefaultMaxTokens)
}

func TestConvertLocalModelQwen35LargeContext(t *testing.T) {
	// Qwen 3.5 with context > 8192 gets capped to 8192 (practical limit for 0.8B)
	lm := localModel{
		ID:                  "qwen3_5-0.8b",
		LoadedContextLength: 16384,
	}

	result := convertLocalModel(lm)

	assert.Equal(t, int64(8192), result.ContextWindow,
		"Qwen 3.5 with >8192 loaded context should be capped to 8192")
	assert.Equal(t, int64(1024), result.DefaultMaxTokens)
}

func TestConvertLocalModelNonQwenUnchanged(t *testing.T) {
	lm := localModel{
		ID:                  "llama-3-8b",
		LoadedContextLength: 8192,
	}

	result := convertLocalModel(lm)

	assert.Equal(t, int64(8192), result.ContextWindow,
		"Non-Qwen models should use LoadedContextLength as-is")
	assert.Equal(t, int64(8192), result.DefaultMaxTokens,
		"Non-Qwen models should use contextWindow as defaultMaxTokens")
}

func TestListLocalModels_MockServer(t *testing.T) {
	t.Run("valid response", func(t *testing.T) {
		models := []localModel{
			{
				ID:                  "test-model-1",
				Object:              "model",
				Type:                "llm",
				Publisher:           "test-pub",
				MaxContextLength:    4096,
				LoadedContextLength: 2048,
			},
			{
				ID:                  "test-model-2",
				Object:              "model",
				Type:                "llm",
				Publisher:           "test-pub-2",
				MaxContextLength:    8192,
				LoadedContextLength: 4096,
			},
		}

		response := struct {
			Data []localModel `json:"data"`
		}{Data: models}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		result := listLocalModels(server.URL)
		require.Len(t, result, 2)
		assert.Equal(t, "test-model-1", result[0].ID)
		assert.Equal(t, "test-model-2", result[1].ID)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{invalid json`))
		}))
		defer server.Close()

		result := listLocalModels(server.URL)
		assert.Empty(t, result, "should return empty slice for invalid JSON")
	})

	t.Run("empty response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"data": []}`))
		}))
		defer server.Close()

		result := listLocalModels(server.URL)
		assert.Empty(t, result, "should return empty slice for empty data")
	})

	t.Run("server error 500", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		result := listLocalModels(server.URL)
		assert.Empty(t, result, "should return empty slice on server error")
	})
}
