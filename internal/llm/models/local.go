package models

import (
	"cmp"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/AIdoesmyjob/termfix/internal/logging"
	"github.com/spf13/viper"
)

// httpClient is a shared client with a timeout for local model discovery.
var httpClient = &http.Client{Timeout: 5 * time.Second}

const (
	ProviderLocal ModelProvider = "local"

	localModelsPath        = "v1/models"
	lmStudioBetaModelsPath = "api/v0/models"
)

func init() {
	endpoint := os.Getenv("LOCAL_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:8012"
	}
	if endpoint != "" {
		localEndpoint, err := url.Parse(endpoint)
		if err != nil {
			logging.Debug("Failed to parse local endpoint",
				"error", err,
				"endpoint", endpoint,
			)
			return
		}

		load := func(url *url.URL, path string) []localModel {
			url.Path = path
			return listLocalModels(url.String())
		}

		models := load(localEndpoint, lmStudioBetaModelsPath)

		if len(models) == 0 {
			models = load(localEndpoint, localModelsPath)
		}

		if len(models) == 0 {
			logging.Debug("No local models found",
				"endpoint", endpoint,
			)
			return
		}

		loadLocalModels(models)

		viper.SetDefault("providers.local.apiKey", "dummy")
		ProviderPopularity[ProviderLocal] = 0
	}
}

type localModelList struct {
	Data []localModel `json:"data"`
}

type localModel struct {
	ID                  string `json:"id"`
	Object              string `json:"object"`
	Type                string `json:"type"`
	Publisher           string `json:"publisher"`
	Arch                string `json:"arch"`
	CompatibilityType   string `json:"compatibility_type"`
	Quantization        string `json:"quantization"`
	State               string `json:"state"`
	MaxContextLength    int64  `json:"max_context_length"`
	LoadedContextLength int64  `json:"loaded_context_length"`
}

func listLocalModels(modelsEndpoint string) []localModel {
	res, err := httpClient.Get(modelsEndpoint)
	if err != nil {
		logging.Debug("Failed to list local models",
			"error", err,
			"endpoint", modelsEndpoint,
		)
		return []localModel{}
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		logging.Debug("Failed to list local models",
			"status", res.StatusCode,
			"endpoint", modelsEndpoint,
		)
		return []localModel{}
	}

	var modelList localModelList
	if err = json.NewDecoder(res.Body).Decode(&modelList); err != nil {
		logging.Debug("Failed to list local models",
			"error", err,
			"endpoint", modelsEndpoint,
		)
		return []localModel{}
	}

	var supportedModels []localModel
	for _, model := range modelList.Data {
		if strings.HasSuffix(modelsEndpoint, lmStudioBetaModelsPath) {
			if model.Object != "model" || model.Type != "llm" {
				logging.Debug("Skipping unsupported LMStudio model",
					"endpoint", modelsEndpoint,
					"id", model.ID,
					"object", model.Object,
					"type", model.Type,
				)

				continue
			}
		}

		supportedModels = append(supportedModels, model)
	}

	return supportedModels
}

func loadLocalModels(models []localModel) {
	for i, m := range models {
		model := convertLocalModel(m)
		SupportedModels[model.ID] = model

		if i == 0 || m.State == "loaded" {
			viper.SetDefault("agents.coder.model", model.ID)
			viper.SetDefault("agents.summarizer.model", model.ID)
			viper.SetDefault("agents.task.model", model.ID)
			viper.SetDefault("agents.title.model", model.ID)
		}
	}
}

func isQwen35Model(modelID string) bool {
	lower := strings.ToLower(modelID)
	return strings.Contains(lower, "qwen3.5") || strings.Contains(lower, "qwen3_5")
}

func convertLocalModel(model localModel) Model {
	contextWindow := cmp.Or(model.LoadedContextLength, int64(4096))
	defaultMaxTokens := contextWindow

	// Qwen 3.5 0.8B profile: two-pass architecture with capped generation
	// Pass 1 (tool selection) needs ~5700 tokens (system + tools + user), so 8192 context is required.
	// Pass 2 (diagnostic) uses ~1900 tokens (system + user + tool output, no tools).
	// DefaultMaxTokens capped at 1024 to prevent runaway generation in either pass.
	if isQwen35Model(model.ID) {
		if contextWindow > 8192 || contextWindow == 4096 {
			contextWindow = 8192
		}
		defaultMaxTokens = 1024
	}

	return Model{
		ID:                  ModelID("local." + model.ID),
		Name:                friendlyModelName(model.ID),
		Provider:            ProviderLocal,
		APIModel:            model.ID,
		ContextWindow:       contextWindow,
		DefaultMaxTokens:    defaultMaxTokens,
		CanReason:           false,
		SupportsAttachments: true,
	}
}

var modelInfoRegex = regexp.MustCompile(`(?i)^([a-z0-9]+)(?:[-_]?([rv]?\d[\.\d]*))?(?:[-_]?([a-z]+))?.*`)

func friendlyModelName(modelID string) string {
	mainID := modelID
	tag := ""

	if slash := strings.LastIndex(mainID, "/"); slash != -1 {
		mainID = mainID[slash+1:]
	}

	if at := strings.Index(modelID, "@"); at != -1 {
		mainID = modelID[:at]
		tag = modelID[at+1:]
	}

	match := modelInfoRegex.FindStringSubmatch(mainID)
	if match == nil {
		return modelID
	}

	capitalize := func(s string) string {
		if s == "" {
			return ""
		}
		runes := []rune(s)
		runes[0] = unicode.ToUpper(runes[0])
		return string(runes)
	}

	family := capitalize(match[1])
	version := ""
	label := ""

	if len(match) > 2 && match[2] != "" {
		version = strings.ToUpper(match[2])
	}

	if len(match) > 3 && match[3] != "" {
		label = capitalize(match[3])
	}

	var parts []string
	if family != "" {
		parts = append(parts, family)
	}
	if version != "" {
		parts = append(parts, version)
	}
	if label != "" {
		parts = append(parts, label)
	}
	if tag != "" {
		parts = append(parts, tag)
	}

	return strings.Join(parts, " ")
}
