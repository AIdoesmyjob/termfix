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
	RecipePermission          RecipeName = "permission"
	RecipePortConflict        RecipeName = "port_conflict"
	RecipeSSL                 RecipeName = "ssl"
	RecipeGit                 RecipeName = "git"
	RecipeCron                RecipeName = "cron"
	RecipePackage             RecipeName = "package"
	RecipeProcess             RecipeName = "process"
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
	case IssuePermission:
		path := ExtractPath(strings.ToLower(userInput))
		cmd := "id"
		if path != "" {
			cmd = fmt.Sprintf("ls -la %s; id", shellEscapeToken(path))
		}
		return &Recipe{
			Name:           RecipePermission,
			IssueClass:     issueClass,
			InitialCommand: cmd,
			ServiceName:    path,
		}
	case IssuePort:
		cmd := "ss -tlnp"
		if runtime.GOOS == "darwin" {
			cmd = "lsof -iTCP -sTCP:LISTEN -P -n"
		}
		port := ExtractPort(strings.ToLower(userInput))
		return &Recipe{
			Name:           RecipePortConflict,
			IssueClass:     issueClass,
			InitialCommand: cmd,
			ServiceName:    port,
		}
	case IssueSSL:
		cmd := "openssl s_client -connect localhost:443 </dev/null 2>/dev/null | openssl x509 -noout -dates -subject"
		return &Recipe{
			Name:           RecipeSSL,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueGit:
		return &Recipe{
			Name:           RecipeGit,
			IssueClass:     issueClass,
			InitialCommand: "git status",
		}
	case IssueCron:
		cmd := "crontab -l 2>&1"
		if runtime.GOOS == "linux" {
			cmd = "crontab -l 2>&1; systemctl list-timers --no-pager 2>/dev/null | head -20"
		}
		return &Recipe{
			Name:           RecipeCron,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssuePackage:
		cmd := buildPackageCommand()
		return &Recipe{
			Name:           RecipePackage,
			IssueClass:     issueClass,
			InitialCommand: cmd,
		}
	case IssueProcess:
		normalized := strings.ToLower(userInput)
		cmd := "ps aux | grep -E 'Z|defunct' | head -20"
		if containsAny(normalized, "open files", "ulimit") {
			cmd = "ulimit -n; lsof 2>/dev/null | awk '{print $1}' | sort | uniq -c | sort -rn | head -10"
		}
		return &Recipe{
			Name:           RecipeProcess,
			IssueClass:     issueClass,
			InitialCommand: cmd,
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
			return "ps -eo pid,rss,comm -r | head -10"
		}
		return "ps -eo pid,rss,comm --sort=-rss | head -10"
	case RecipePerformanceCPU:
		if runtime.GOOS == "darwin" {
			return "ps -eo pid,pcpu,comm -r | head -10"
		}
		return "ps -eo pid,pcpu,comm --sort=-pcpu | head -10"
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
	case RecipePermission:
		path := r.ServiceName
		if path != "" {
			return fmt.Sprintf("stat %s", shellEscapeToken(path))
		}
		return ""
	case RecipePortConflict:
		port := r.ServiceName
		if port != "" {
			if runtime.GOOS == "darwin" {
				return fmt.Sprintf("lsof -iTCP:%s -sTCP:LISTEN -P -n", shellEscapeToken(port))
			}
			return fmt.Sprintf("ss -tlnp | grep %s", shellEscapeToken(port))
		}
		return ""
	case RecipeSSL:
		return "date -u"
	case RecipeGit:
		return "git log --oneline -5"
	case RecipeCron:
		if runtime.GOOS == "darwin" {
			return "log show --predicate 'process == \"cron\"' --last 10m --style compact 2>/dev/null | tail -20"
		}
		return "journalctl -u cron -n 20 --no-pager 2>/dev/null || journalctl -u crond -n 20 --no-pager 2>/dev/null"
	case RecipePackage:
		return ""
	case RecipeProcess:
		return "lsof 2>/dev/null | awk '{print $1}' | sort | uniq -c | sort -rn | head -10"
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

func buildPackageCommand() string {
	if runtime.GOOS == "darwin" {
		return "brew doctor 2>&1 | head -30"
	}
	return "apt list --upgradable 2>/dev/null | head -20 || dpkg --audit 2>/dev/null | head -20"
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
