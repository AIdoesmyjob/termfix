package prompt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/opencode-ai/opencode/internal/config"
	"github.com/opencode-ai/opencode/internal/llm/models"
	"github.com/opencode-ai/opencode/internal/llm/tools"
)

func CoderPrompt(provider models.ModelProvider) string {
	envInfo := getEnvironmentInfo(provider)
	return fmt.Sprintf("%s\n\n%s", baseTermfixPrompt, envInfo)
}

const baseTermfixPrompt = `You are termfix, an offline system troubleshooting assistant running in a terminal.

You diagnose system issues using read-only inspection tools: bash (for running commands), file viewer, glob, and grep.
You CANNOT modify files — only inspect and diagnose.

When diagnosing issues, structure your response as:
- **Summary**: One-line description of the issue
- **Root Cause**: What is causing the problem
- **Risk Level**: Low / Medium / High / Critical
- **Evidence**: Commands run and their relevant output
- **Remediation**: Step-by-step fix instructions for the user
- **Rollback**: How to undo the fix if needed

Be concise and honest about uncertainty. If you are not sure, say so.
When running bash commands, explain what each command does.
Keep responses short — this is a terminal interface.

When /diagnose context is provided with system facts, use those as your starting point rather than re-collecting the same information.`

func getEnvironmentInfo(provider models.ModelProvider) string {
	cwd := config.WorkingDirectory()
	isGit := isGitRepo(cwd)
	platform := runtime.GOOS
	date := time.Now().Format("1/2/2006")

	// Skip project listing for local models to save context tokens
	if provider == models.ProviderLocal {
		return fmt.Sprintf(`<env>
Working directory: %s
Is directory a git repo: %s
Platform: %s
Today's date: %s
</env>`, cwd, boolToYesNo(isGit), platform, date)
	}

	ls := tools.NewLsTool()
	r, _ := ls.Run(context.Background(), tools.ToolCall{
		Input: `{"path":"."}`,
	})
	return fmt.Sprintf(`Here is useful information about the environment you are running in:
<env>
Working directory: %s
Is directory a git repo: %s
Platform: %s
Today's date: %s
</env>
<project>
%s
</project>
		`, cwd, boolToYesNo(isGit), platform, date, r.Content)
}

func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func boolToYesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}
