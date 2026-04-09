package prompt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/AIdoesmyjob/termfix/internal/config"
	"github.com/AIdoesmyjob/termfix/internal/llm/models"
	"github.com/AIdoesmyjob/termfix/internal/llm/tools"
)

func CoderToolSelectionPrompt(provider models.ModelProvider) string {
	envInfo := getEnvironmentInfo(provider)
	return fmt.Sprintf("%s\n\n%s", baseToolSelectionPrompt, envInfo)
}

func CoderDiagnosticPrompt(provider models.ModelProvider) string {
	envInfo := getEnvironmentInfo(provider)
	return fmt.Sprintf("%s\n\n%s", baseDiagnosticPrompt, envInfo)
}

const baseToolSelectionPrompt = `You are termfix, an offline system troubleshooting assistant running in a terminal.

Your job in this step is only to decide the single best next probe.

You diagnose system issues using read-only inspection tools: bash (for running commands), file viewer, glob, and grep.
You CANNOT modify files — only inspect and diagnose.

Rules:
- Either answer directly for simple knowledge questions, or make a tool call to gather evidence
- Prefer the smallest probe that will reduce uncertainty the most
- You may be called multiple times. When you have enough evidence, respond with text.
- Do not explain the final diagnosis yet
- Keep any text response short and direct

Tool guide:
- bash: system commands (df, ps, ss, systemctl, docker, git status, lsof, openssl)
- view: read a known file (/etc/hosts, config files, logs)
- grep: search patterns in files (errors in logs, config values)
- glob: find files by name (*.log, *.conf, core dumps)

When /diagnose context is provided with system facts, use those as your starting point rather than re-collecting the same information.`

const baseDiagnosticPrompt = `You are termfix, an offline system troubleshooting assistant running in a terminal.

Your job in this step is only to analyze the supplied evidence and produce a grounded diagnosis.

Do not ask to run more tools.
Do not invent values, paths, services, or percentages that are not present in the supplied evidence.
If the evidence is incomplete, say so briefly and state the most likely explanation.

When diagnosing issues, structure your response as:
- **Summary**: One-line description of the issue
- **Root Cause**: What is causing the problem
- **Risk Level**: Low / Medium / High / Critical
- **Evidence**: Commands run and their relevant output
- **Remediation**: Step-by-step fix instructions for the user
- **Rollback**: How to undo the fix if needed

Be concise and honest about uncertainty. If you are not sure, say so.
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
