package tools

import (
	"context"
	"encoding/json"
)

type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]any
	Required    []string
}

type toolResponseType string

type (
	sessionIDContextKey string
	messageIDContextKey string
)

const (
	ToolResponseTypeText  toolResponseType = "text"
	ToolResponseTypeImage toolResponseType = "image"

	SessionIDContextKey sessionIDContextKey = "session_id"
	MessageIDContextKey messageIDContextKey = "message_id"
)

type ToolResponse struct {
	Type     toolResponseType `json:"type"`
	Content  string           `json:"content"`
	Metadata string           `json:"metadata,omitempty"`
	IsError  bool             `json:"is_error"`
}

func NewTextResponse(content string) ToolResponse {
	return ToolResponse{
		Type:    ToolResponseTypeText,
		Content: content,
	}
}

func WithResponseMetadata(response ToolResponse, metadata any) ToolResponse {
	if metadata != nil {
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			return response
		}
		response.Metadata = string(metadataBytes)
	}
	return response
}

func NewTextErrorResponse(content string) ToolResponse {
	return ToolResponse{
		Type:    ToolResponseTypeText,
		Content: content,
		IsError: true,
	}
}

type ToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input"`
}

type BaseTool interface {
	Info() ToolInfo
	Run(ctx context.Context, params ToolCall) (ToolResponse, error)
}

// SlimToolWrapper wraps a BaseTool with a shorter description for local models.
// This saves ~400 tokens in the prompt, leaving more room for evidence.
type SlimToolWrapper struct {
	inner       BaseTool
	description string
}

func (s *SlimToolWrapper) Info() ToolInfo {
	info := s.inner.Info()
	info.Description = s.description
	return info
}

func (s *SlimToolWrapper) Run(ctx context.Context, params ToolCall) (ToolResponse, error) {
	return s.inner.Run(ctx, params)
}

// SlimDescriptions maps tool names to compact descriptions for local models.
var SlimDescriptions = map[string]string{
	"bash": "Run a shell command and return its output. Banned commands: curl, wget, nc, telnet. Use for system inspection: df, ps, ss, systemctl, docker, git, lsof, openssl, etc.",
	"view": "Read a file and return its contents. Use for config files, logs, /etc/* files.",
	"grep": "Search file contents for a regex pattern. Returns matching lines.",
	"glob": "Find files matching a glob pattern. Returns file paths.",
}

// WrapToolsForLocalModel returns tools with slim descriptions for local model inference.
func WrapToolsForLocalModel(baseTools []BaseTool) []BaseTool {
	result := make([]BaseTool, len(baseTools))
	for i, t := range baseTools {
		if desc, ok := SlimDescriptions[t.Info().Name]; ok {
			result[i] = &SlimToolWrapper{inner: t, description: desc}
		} else {
			result[i] = t
		}
	}
	return result
}

func GetContextValues(ctx context.Context) (string, string) {
	sessionID := ctx.Value(SessionIDContextKey)
	messageID := ctx.Value(MessageIDContextKey)
	if sessionID == nil {
		return "", ""
	}
	if messageID == nil {
		return sessionID.(string), ""
	}
	return sessionID.(string), messageID.(string)
}
