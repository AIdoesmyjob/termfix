package diagnose

import (
	"fmt"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

type RecipeName string

const (
	RecipeDiskUsage           RecipeName = "disk_usage"
	RecipeMemoryPressure      RecipeName = "memory_pressure"
	RecipePerformanceCPU      RecipeName = "performance_cpu"
	RecipeDNSResolution       RecipeName = "dns_resolution"
	RecipeNetworkConnectivity RecipeName = "network_connectivity"
	RecipeServiceFailure      RecipeName = "service_failure"
	RecipeDockerCrash         RecipeName = "docker_crash"
	RecipeBuildFailure        RecipeName = "build_failure"
)

type Recipe struct {
	Name           RecipeName
	IssueClass     IssueClass
	InitialCommand string
	ServiceName    string
}

func SelectRecipe(userInput string) *Recipe {
	if looksLikeKnowledgeQuery(userInput) {
		return nil
	}

	issueClass := ClassifyIssue(userInput)
	serviceName := ExtractServiceName(strings.ToLower(userInput))

	switch issueClass {
	case IssueDisk:
		return &Recipe{
			Name:           RecipeDiskUsage,
			IssueClass:     issueClass,
			InitialCommand: "df -h",
		}
	case IssueMemory:
		cmd := "free -h"
		if runtime.GOOS == "darwin" {
			cmd = "vm_stat"
		}
		return &Recipe{
			Name:           RecipeMemoryPressure,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssuePerformance:
		return &Recipe{
			Name:           RecipePerformanceCPU,
			IssueClass:     issueClass,
			InitialCommand: "uptime",
		}
	case IssueDNS:
		cmd := "cat /etc/resolv.conf"
		if runtime.GOOS == "darwin" {
			cmd = "scutil --dns"
		}
		return &Recipe{
			Name:           RecipeDNSResolution,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueNetwork:
		cmd := "ip -o addr show"
		if runtime.GOOS == "darwin" {
			cmd = "ifconfig"
		}
		return &Recipe{
			Name:           RecipeNetworkConnectivity,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueDocker:
		containerName := ExtractContainerName(strings.ToLower(userInput))
		cmd := "docker ps -a --format 'table {{.Names}}\\t{{.Status}}\\t{{.Image}}' | head -20"
		if containerName != "" {
			cmd = fmt.Sprintf("docker inspect --format '{{.State.Status}} exit:{{.State.ExitCode}} oom:{{.State.OOMKilled}}' %s", shellEscapeToken(containerName))
		}
		return &Recipe{
			Name:           RecipeDockerCrash,
			IssueClass:     issueClass,
			InitialCommand: cmd,
			ServiceName:    containerName,
		}
	case IssueBuild:
		buildTool := ExtractBuildTool(strings.ToLower(userInput))
		cmd := buildCommandForTool(buildTool)
		return &Recipe{
			Name:           RecipeBuildFailure,
			IssueClass:     issueClass,
			InitialCommand: cmd,
			ServiceName:    buildTool,
		}
	case IssueService:
		cmd := "systemctl --failed --no-pager --plain"
		if runtime.GOOS == "darwin" {
			cmd = "launchctl list | head -50"
		}
		if serviceName != "" {
			if runtime.GOOS == "darwin" {
				cmd = fmt.Sprintf("launchctl list | grep -i %s", shellEscapeToken(serviceName))
			} else {
				cmd = fmt.Sprintf("systemctl status %s --no-pager --full -n 20", shellEscapeToken(serviceName))
			}
		}
		return &Recipe{
			Name:           RecipeServiceFailure,
			IssueClass:     issueClass,
			InitialCommand: cmd,
			ServiceName:    serviceName,
		}
	default:
		return nil
	}
}

func (r *Recipe) FollowUpCommand(firstOutput string) string {
	if r == nil || strings.TrimSpace(firstOutput) == "" {
		return ""
	}

	switch r.Name {
	case RecipeDiskUsage:
		if !hasHighDiskUsage(firstOutput) {
			return ""
		}
		if runtime.GOOS == "darwin" {
			return "du -xhd 1 /System/Volumes/Data 2>/dev/null | sort -hr | head -15"
		}
		return "du -xhd 1 / 2>/dev/null | sort -hr | head -15"
	case RecipeMemoryPressure:
		if runtime.GOOS == "darwin" {
			return "ps aux -armem | head -10"
		}
		return "ps aux --sort=-%mem | head -10"
	case RecipePerformanceCPU:
		if runtime.GOOS == "darwin" {
			return "ps aux -arcpu | head -10"
		}
		return "ps aux --sort=-%cpu | head -10"
	case RecipeDNSResolution:
		if runtime.GOOS == "darwin" {
			return "netstat -rn"
		}
		return "ip route"
	case RecipeNetworkConnectivity:
		if runtime.GOOS == "darwin" {
			return "netstat -rn"
		}
		return "ip route"
	case RecipeDockerCrash:
		if r.ServiceName == "" {
			return ""
		}
		return fmt.Sprintf("docker logs --tail 40 %s 2>&1", shellEscapeToken(r.ServiceName))
	case RecipeBuildFailure:
		return ""
	case RecipeServiceFailure:
		if r.ServiceName == "" {
			return ""
		}
		if runtime.GOOS == "darwin" {
			return fmt.Sprintf("log show --predicate 'process == \"%s\"' --last 5m --style compact 2>/dev/null | tail -40", r.ServiceName)
		}
		return fmt.Sprintf("journalctl -u %s -n 40 --no-pager", shellEscapeToken(r.ServiceName))
	default:
		return ""
	}
}

var pctRe = regexp.MustCompile(`(\d+)%`)

func hasHighDiskUsage(output string) bool {
	for _, match := range pctRe.FindAllStringSubmatch(output, -1) {
		if len(match) < 2 {
			continue
		}
		pct, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		if pct >= 85 {
			return true
		}
	}
	return false
}

func shellEscapeToken(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func buildCommandForTool(tool string) string {
	switch tool {
	case "npm":
		return "npm run build 2>&1 | tail -40"
	case "yarn":
		return "yarn build 2>&1 | tail -40"
	case "pnpm":
		return "pnpm run build 2>&1 | tail -40"
	case "cargo":
		return "cargo build 2>&1 | tail -40"
	case "go":
		return "go build ./... 2>&1 | tail -40"
	case "make":
		return "make 2>&1 | tail -40"
	case "tsc":
		return "tsc --noEmit 2>&1 | tail -40"
	default:
		return "ls package.json Cargo.toml go.mod Makefile 2>/dev/null"
	}
}

func looksLikeKnowledgeQuery(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	return strings.HasPrefix(normalized, "what is ") ||
		strings.HasPrefix(normalized, "what are ") ||
		strings.HasPrefix(normalized, "what does ") ||
		strings.HasPrefix(normalized, "explain ") ||
		strings.HasPrefix(normalized, "define ") ||
		strings.HasPrefix(normalized, "describe ")
}
