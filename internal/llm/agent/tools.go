package agent

import (
	"github.com/AIdoesmyjob/termfix/internal/history"
	"github.com/AIdoesmyjob/termfix/internal/llm/tools"
	"github.com/AIdoesmyjob/termfix/internal/lsp"
	"github.com/AIdoesmyjob/termfix/internal/message"
	"github.com/AIdoesmyjob/termfix/internal/permission"
	"github.com/AIdoesmyjob/termfix/internal/session"
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
