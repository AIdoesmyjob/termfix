package agent

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/opencode-ai/opencode/internal/diagnose"
	"github.com/opencode-ai/opencode/internal/message"
)

func buildPass2Content(userContent string, recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) string {
	evidence := buildStructuredEvidence(recipe, toolCalls, toolResults)
	if evidence == "" {
		return fmt.Sprintf(
			"%s\n\nI ran `%s` and got:\n```\n%s\n```\nAnalyze these results.",
			userContent,
			toolCallSummaryFromParts(toolCalls),
			joinToolResults(toolResults),
		)
	}

	return fmt.Sprintf(
		`User issue:
%s

Use this compact evidence bundle to diagnose the issue. Do not invent facts not present below.

%s

Write the diagnosis in the required structured format.`,
		userContent,
		evidence,
	)
}

func buildStructuredEvidence(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) string {
	if len(toolCalls) == 0 || len(toolResults) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Evidence Bundle\n")
	if recipe != nil {
		b.WriteString(fmt.Sprintf("Recipe: %s\n", recipe.Name))
		if recipe.ServiceName != "" {
			b.WriteString(fmt.Sprintf("Service: %s\n", recipe.ServiceName))
		}
	}
	b.WriteString(fmt.Sprintf("Probe count: %d\n\n", min(len(toolCalls), len(toolResults))))

	for i := 0; i < len(toolCalls) && i < len(toolResults); i++ {
		command := extractCommand(toolCalls[i].Input)
		if command == "" {
			command = toolCalls[i].Name
		}
		b.WriteString(fmt.Sprintf("### Probe %d\n", i+1))
		b.WriteString(fmt.Sprintf("Command: `%s`\n", command))

		summary := summarizeProbe(recipe, command, toolResults[i].Content)
		if summary == "" {
			summary = summarizeGenericOutput(toolResults[i].Content)
		}
		if summary == "" {
			summary = "No concise evidence extracted."
		}
		b.WriteString(summary)
		if !strings.HasSuffix(summary, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

func summarizeProbe(recipe *diagnose.Recipe, command, output string) string {
	if strings.TrimSpace(output) == "" {
		return ""
	}
	if recipe == nil {
		return ""
	}

	switch recipe.Name {
	case diagnose.RecipeDiskUsage:
		return summarizeDiskProbe(command, output)
	case diagnose.RecipeMemoryPressure:
		return summarizeMemoryProbe(command, output)
	case diagnose.RecipePerformanceCPU:
		return summarizePerformanceProbe(command, output)
	case diagnose.RecipeDNSResolution:
		return summarizeDNSProbe(command, output)
	case diagnose.RecipeNetworkConnectivity:
		return summarizeNetworkProbe(command, output)
	case diagnose.RecipeServiceFailure:
		return summarizeServiceProbe(command, output)
	default:
		return ""
	}
}

func summarizeDiskProbe(command, output string) string {
	if strings.Contains(command, "df -h") {
		lines := strings.Split(strings.TrimSpace(output), "\n")
		highest := ""
		maxPct := -1
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				continue
			}
			for idx, field := range fields {
				if !strings.HasSuffix(field, "%") {
					continue
				}
				pct, err := strconv.Atoi(strings.TrimSuffix(field, "%"))
				if err != nil {
					continue
				}
				if pct > maxPct && idx+1 < len(fields) {
					maxPct = pct
					highest = fields[len(fields)-1]
				}
				break
			}
		}
		if maxPct >= 0 {
			return fmt.Sprintf("- Highest disk usage: %d%% on %s\n", maxPct, highest)
		}
	}
	if strings.Contains(command, "du ") {
		top := firstNonEmptyLines(output, 5)
		if len(top) > 0 {
			return "- Largest paths:\n" + bulletLines(top)
		}
	}
	return ""
}

func summarizeMemoryProbe(command, output string) string {
	if strings.Contains(command, "free -h") || strings.Contains(command, "vm_stat") {
		lines := firstNonEmptyLines(output, 4)
		if len(lines) > 0 {
			return "- Memory snapshot:\n" + bulletLines(lines)
		}
	}
	if strings.Contains(command, "--sort=-%mem") || strings.Contains(command, "-armem") {
		lines := firstNonEmptyLines(output, 4)
		if len(lines) > 1 {
			return "- Top memory processes:\n" + bulletLines(lines[1:])
		}
	}
	return ""
}

func summarizePerformanceProbe(command, output string) string {
	if strings.Contains(command, "uptime") {
		return fmt.Sprintf("- Load summary: %s\n", strings.TrimSpace(output))
	}
	if strings.Contains(command, "--sort=-%cpu") || strings.Contains(command, "-arcpu") {
		lines := firstNonEmptyLines(output, 4)
		if len(lines) > 1 {
			return "- Top CPU processes:\n" + bulletLines(lines[1:])
		}
	}
	return ""
}

func summarizeDNSProbe(command, output string) string {
	if strings.Contains(command, "resolv.conf") {
		nameservers := grepLines(output, `(?i)^nameserver\s+`)
		search := grepLines(output, `(?i)^(search|domain)\s+`)
		var parts []string
		if len(nameservers) > 0 {
			parts = append(parts, "- Nameservers:\n"+bulletLines(nameservers))
		}
		if len(search) > 0 {
			parts = append(parts, "- Search domains:\n"+bulletLines(search))
		}
		return strings.Join(parts, "\n")
	}
	if strings.Contains(command, "scutil --dns") {
		nameservers := grepLines(output, `nameserver\[[0-9]+\]`)
		if len(nameservers) > 0 {
			return "- DNS resolvers:\n" + bulletLines(firstN(nameservers, 4))
		}
	}
	if strings.Contains(command, "route") || strings.Contains(command, "netstat -rn") {
		defaultRoute := grepLines(output, `(?i)^(default|0\.0\.0\.0)`)
		if len(defaultRoute) > 0 {
			return "- Default route:\n" + bulletLines(firstN(defaultRoute, 2))
		}
	}
	return ""
}

func summarizeNetworkProbe(command, output string) string {
	if strings.Contains(command, "ifconfig") || strings.Contains(command, "ip -o addr show") {
		lines := firstNonEmptyLines(output, 6)
		if len(lines) > 0 {
			return "- Interface summary:\n" + bulletLines(lines)
		}
	}
	if strings.Contains(command, "route") || strings.Contains(command, "netstat -rn") {
		defaultRoute := grepLines(output, `(?i)^(default|0\.0\.0\.0)`)
		if len(defaultRoute) > 0 {
			return "- Default route:\n" + bulletLines(firstN(defaultRoute, 2))
		}
	}
	return ""
}

func summarizeServiceProbe(command, output string) string {
	if strings.Contains(command, "systemctl status") {
		interesting := grepLines(output, `(?i)(Active:|Loaded:|Main PID:|status=|failed)`)
		if len(interesting) > 0 {
			return "- Service status:\n" + bulletLines(firstN(interesting, 5))
		}
	}
	if strings.Contains(command, "journalctl -u") {
		interesting := grepLines(output, `(?i)(error|failed|fatal|panic|denied|permission|address already in use|exception)`)
		if len(interesting) > 0 {
			return "- Recent service errors:\n" + bulletLines(firstN(interesting, 6))
		}
		lines := firstNonEmptyLines(output, 6)
		if len(lines) > 0 {
			return "- Recent service logs:\n" + bulletLines(lines)
		}
	}
	if strings.Contains(command, "systemctl --failed") || strings.Contains(command, "launchctl list") {
		lines := firstNonEmptyLines(output, 6)
		if len(lines) > 0 {
			return "- Service listing:\n" + bulletLines(lines)
		}
	}
	return ""
}

func summarizeGenericOutput(output string) string {
	lines := firstNonEmptyLines(output, 5)
	if len(lines) == 0 {
		return ""
	}
	return "- Key lines:\n" + bulletLines(lines)
}

func toolCallSummaryFromParts(toolCalls []message.ToolCall) string {
	var parts []string
	for _, tc := range toolCalls {
		input := tc.Input
		if len(input) > 500 {
			input = input[:500] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s(%s)", tc.Name, input))
	}
	return strings.Join(parts, ", ")
}

func joinToolResults(toolResults []message.ToolResult) string {
	var b strings.Builder
	for _, tr := range toolResults {
		b.WriteString(tr.Content)
		if !strings.HasSuffix(tr.Content, "\n") {
			b.WriteString("\n")
		}
	}
	content := b.String()
	if len(content) > 2000 {
		content = content[:2000] + "\n... (truncated)"
	}
	return strings.TrimSpace(content)
}

func firstNonEmptyLines(output string, limit int) []string {
	lines := strings.Split(output, "\n")
	result := make([]string, 0, limit)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		result = append(result, line)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func bulletLines(lines []string) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func grepLines(output, pattern string) []string {
	re := regexp.MustCompile(pattern)
	var matches []string
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if re.MatchString(trimmed) {
			matches = append(matches, trimmed)
		}
	}
	return matches
}

func firstN[T any](items []T, n int) []T {
	if len(items) <= n {
		return items
	}
	return items[:n]
}
