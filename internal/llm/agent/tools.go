package agent

import (
	"github.com/opencode-ai/opencode/internal/history"
	"github.com/opencode-ai/opencode/internal/llm/tools"
	"github.com/opencode-ai/opencode/internal/lsp"
	"github.com/opencode-ai/opencode/internal/message"
	"github.com/opencode-ai/opencode/internal/permission"
	"github.com/opencode-ai/opencode/internal/session"
)

func CoderAgentTools(
	permissions permission.Service,
	sessions session.Service,
	messages message.Service,
	history history.Service,
	lspClients map[string]*lsp.Client,
) []tools.BaseTool {
	t := []tools.BaseTool{
		tools.NewBashTool(permissions),
		tools.NewViewTool(nil),
		tools.NewGlobTool(),
		tools.NewGrepTool(),
	}
	if len(lspClients) > 0 {
		t = append(t, tools.NewDiagnosticsTool(lspClients))
	}
	return t
}

func TaskAgentTools(lspClients map[string]*lsp.Client) []tools.BaseTool {
	return []tools.BaseTool{
		tools.NewViewTool(nil),
		tools.NewGlobTool(),
		tools.NewGrepTool(),
	}
}
