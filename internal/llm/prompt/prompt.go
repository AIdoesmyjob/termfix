package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/AIdoesmyjob/termfix/internal/config"
	"github.com/AIdoesmyjob/termfix/internal/llm/models"
	"github.com/AIdoesmyjob/termfix/internal/logging"
)

func GetAgentPrompt(agentName config.AgentName, provider models.ModelProvider) string {
	basePrompt := ""
	switch agentName {
	case config.AgentCoder:
		basePrompt = CoderToolSelectionPrompt(provider)
	case config.AgentTitle:
		basePrompt = TitlePrompt(provider)
	case config.AgentTask:
		basePrompt = TaskPrompt(provider)
	case config.AgentSummarizer:
		basePrompt = SummarizerPrompt(provider)
	default:
		basePrompt = "You are a helpful assistant"
	}

	if agentName == config.AgentCoder || agentName == config.AgentTask {
		// Add context from project-specific instruction files if they exist
		contextContent := getContextFromPaths()
		logging.Debug("Context content", "Context", contextContent)
		if contextContent != "" {
			return fmt.Sprintf("%s\n\n# Project-Specific Context\n Make sure to follow the instructions in the context below\n%s", basePrompt, contextContent)
		}
	}
	return basePrompt
}

func GetAgentDiagnosticPrompt(agentName config.AgentName, provider models.ModelProvider) string {
	switch agentName {
	case config.AgentCoder:
		return CoderDiagnosticPrompt(provider)
	default:
		return GetAgentPrompt(agentName, provider)
	}
}

var (
	onceContext    sync.Once
	contextContent string
)

func getContextFromPaths() string {
	onceContext.Do(func() {
		var (
			cfg          = config.Get()
			workDir      = cfg.WorkingDir
			contextPaths = cfg.ContextPaths
		)

		contextContent = processContextPaths(workDir, contextPaths)
	})

	return contextContent
}

func processContextPaths(workDir string, paths []string) string {
	results := make([]string, 0)
	processedFiles := make(map[string]bool)

	for _, path := range paths {
		if strings.HasSuffix(path, "/") {
			_ = filepath.WalkDir(filepath.Join(workDir, path), func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					lowerPath := strings.ToLower(path)
					if !processedFiles[lowerPath] {
						processedFiles[lowerPath] = true
						if result := processFile(path); result != "" {
							results = append(results, result)
						}
					}
				}
				return nil
			})
		} else {
			fullPath := filepath.Join(workDir, path)
			lowerPath := strings.ToLower(fullPath)
			if !processedFiles[lowerPath] {
				processedFiles[lowerPath] = true
				if result := processFile(fullPath); result != "" {
					results = append(results, result)
				}
			}
		}
	}

	return strings.Join(results, "\n")
}

func processFile(filePath string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return "# From:" + filePath + "\n" + string(content)
}
