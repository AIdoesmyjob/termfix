package models

import (
	"cmp"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/opencode-ai/opencode/internal/logging"
	"github.com/spf13/viper"
)

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

		// Query /props for actual loaded context size (llama-server)
		propsCtx := getPropsContextSize(endpoint)
		loadLocalModels(models, propsCtx)

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
	res, err := http.Get(modelsEndpoint)
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

// getPropsContextSize queries the /props endpoint to get the actual loaded context size.
// Returns 0 if unavailable (non-llama-server backends).
func getPropsContextSize(endpoint string) int64 {
	propsURL := strings.TrimRight(endpoint, "/") + "/props"
	res, err := http.Get(propsURL)
	if err != nil {
		return 0
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 0
	}
	var props struct {
		DefaultGenerationSettings struct {
			NCtx int64 `json:"n_ctx"`
		} `json:"default_generation_settings"`
		TotalSlots int64 `json:"total_slots"`
	}
	if err = json.NewDecoder(res.Body).Decode(&props); err != nil {
		return 0
	}
	// Per-slot context = total context / number of slots
	nCtx := props.DefaultGenerationSettings.NCtx
	if props.TotalSlots > 1 {
		nCtx = nCtx / props.TotalSlots
	}
	return nCtx
}

func loadLocalModels(models []localModel, propsCtx int64) {
	for i, m := range models {
		model := convertLocalModel(m, propsCtx)
		SupportedModels[model.ID] = model

		if i == 0 || m.State == "loaded" {
			viper.SetDefault("agents.coder.model", model.ID)
			viper.SetDefault("agents.summarizer.model", model.ID)
			viper.SetDefault("agents.task.model", model.ID)
			viper.SetDefault("agents.title.model", model.ID)
		}
	}
}

func convertLocalModel(model localModel, propsCtx int64) Model {
	// Prefer: /props context (actual loaded) > LM Studio fields > fallback
	ctxLen := cmp.Or(propsCtx, model.LoadedContextLength, model.MaxContextLength, 2048)
	// Reserve half the context for output, half for input (system prompt + tools + history)
	maxOut := ctxLen / 2
	logging.Debug("Local model context",
		"model", model.ID,
		"propsCtx", propsCtx,
		"resolved", ctxLen,
		"maxOut", maxOut,
	)
	if maxOut < 512 {
		maxOut = 512
	}
	return Model{
		ID:                  ModelID("local." + model.ID),
		Name:                friendlyModelName(model.ID),
		Provider:            ProviderLocal,
		APIModel:            model.ID,
		ContextWindow:       ctxLen,
		DefaultMaxTokens:    int64(maxOut),
		CanReason:           false,
		SupportsAttachments: false,
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
