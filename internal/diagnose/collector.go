package diagnose

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type FactSection struct {
	Title   string
	Content string
}

type SystemFacts struct {
	Platform    string
	CollectedAt time.Time
	Sections    []FactSection
}

func CollectFacts() *SystemFacts {
	return CollectFactsForIssue(IssueGeneral)
}

func CollectFactsForIssue(issueClass IssueClass) *SystemFacts {
	facts := &SystemFacts{
		Platform:    runtime.GOOS,
		CollectedAt: time.Now(),
	}

	addCommonFacts(facts)
	collectPlannedFacts(facts, planFactsForIssue(issueClass))
	return facts
}

func Format(facts *SystemFacts) string {
	var b strings.Builder
	b.WriteString("# System Facts\n")
	b.WriteString(fmt.Sprintf("Platform: %s\n", facts.Platform))
	b.WriteString(fmt.Sprintf("Collected: %s\n\n", facts.CollectedAt.Format(time.RFC3339)))

	for _, section := range facts.Sections {
		writeSection(&b, section.Title, section.Content)
	}

	return b.String()
}

func ClassifyIssue(userError string) IssueClass {
	normalized := strings.ToLower(strings.TrimSpace(userError))

	switch {
	case normalized == "", strings.Contains(normalized, "general system health check"), strings.Contains(normalized, "system health"):
		return IssueGeneral
	case containsAny(normalized, "permission denied", "access denied", "chmod", "403"):
		return IssuePermission
	case containsAny(normalized, "address already in use", "eaddrinuse", "bind failed") ||
		(containsAny(normalized, "port") && containsAny(normalized, "in use", "conflict", "busy", "listen", "occupied")):
		return IssuePort
	case containsAny(normalized, "ssl", "tls", "certificate", "cert", "x509", "expired cert"):
		return IssueSSL
	case containsAny(normalized, "merge conflict", "detached head", "git pull", "git push", "git rebase"):
		return IssueGit
	case containsAny(normalized, "cron", "crontab", "scheduled", "timer"):
		return IssueCron
	case containsAny(normalized, "zombie", "defunct", "too many open files", "ulimit"):
		return IssueProcess
	case containsAny(normalized, "apt", "dpkg", "brew", "pip", "locked", "broken package"):
		return IssuePackage
	case containsAny(normalized, "disk", "storage", "space", "filesystem", "partition", "full"):
		return IssueDisk
	case containsAny(normalized, "memory", "ram", "swap", "oom", "out of memory"):
		return IssueMemory
	case containsAny(normalized, "cpu", "slow", "sluggish", "hang", "stuck", "beachball", "load", "performance"):
		return IssuePerformance
	case containsAny(normalized, "dns", "resolve", "hostname", "domain", "resolv"):
		return IssueDNS
	case containsAny(normalized, "network", "internet", "wifi", "ethernet", "latency", "packet", "routing", "route", "connectivity", "offline"):
		return IssueNetwork
	case containsAny(normalized, "docker", "container", "image", "dockerfile", "compose", "pod"):
		return IssueDocker
	case containsAny(normalized, "build", "compile", "npm", "yarn", "pnpm", "cargo", "go build", "make", "webpack", "vite", "tsc", "typescript"):
		return IssueBuild
	case containsAny(normalized, "service", "daemon", "systemd", "launchd", "failed to start", "won't start", "wont start", "crash", "restarting"):
		return IssueService
	default:
		if ExtractServiceName(normalized) != "" {
			return IssueService
		}
		return IssueGeneral
	}
}

type factCollector struct {
	Title string
	Run   func() string
}

func planFactsForIssue(issueClass IssueClass) []factCollector {
	switch issueClass {
	case IssueDisk:
		return []factCollector{
			{Title: "Disk (df -h)", Run: func() string { return runCmd("df", "-h") }},
			{Title: "Inodes (df -i)", Run: func() string { return runCmd("df", "-i") }},
			{Title: "Mounts", Run: func() string { return runCmd("mount") }},
		}
	case IssueMemory:
		return []factCollector{
			{Title: "Uptime", Run: func() string { return runCmd("uptime") }},
			{Title: "Memory", Run: collectMemoryFacts},
			{Title: "Top Memory Processes", Run: collectTopMemoryProcesses},
		}
	case IssuePerformance:
		return []factCollector{
			{Title: "Uptime", Run: func() string { return runCmd("uptime") }},
			{Title: "Memory", Run: collectMemoryFacts},
			{Title: "Top CPU Processes", Run: collectTopCPUProcesses},
		}
	case IssueDNS:
		return []factCollector{
			{Title: "DNS Configuration", Run: collectDNSFacts},
			{Title: "Routes", Run: collectRouteFacts},
			{Title: "Interfaces", Run: collectInterfaceFacts},
		}
	case IssueNetwork:
		return []factCollector{
			{Title: "Interfaces", Run: collectInterfaceFacts},
			{Title: "Routes", Run: collectRouteFacts},
			{Title: "DNS Configuration", Run: collectDNSFacts},
		}
	case IssueDocker:
		return []factCollector{
			{Title: "Docker Info", Run: func() string { return runCmd("docker", "info", "--format", "{{.ServerVersion}}") }},
			{Title: "Containers", Run: func() string {
				return runCmd("docker", "ps", "-a", "--format", "table {{.Names}}\t{{.Status}}\t{{.Image}}")
			}},
		}
	case IssuePermission:
		return []factCollector{
			{Title: "User Info", Run: func() string { return runCmd("id") }},
		}
	case IssuePort:
		return []factCollector{
			{Title: "Listening Ports", Run: collectListeningPorts},
		}
	case IssueSSL:
		return []factCollector{
			{Title: "Current Time (UTC)", Run: func() string { return runCmd("date", "-u") }},
		}
	case IssueGit:
		return []factCollector{
			{Title: "Git Status", Run: func() string { return runCmd("git", "status") }},
		}
	case IssueCron:
		return []factCollector{
			{Title: "Crontab", Run: func() string { return runCmd("sh", "-c", "crontab -l 2>&1") }},
		}
	case IssuePackage:
		return []factCollector{
			{Title: "Uptime", Run: func() string { return runCmd("uptime") }},
		}
	case IssueProcess:
		return []factCollector{
			{Title: "Uptime", Run: func() string { return runCmd("uptime") }},
		}
	case IssueBuild:
		return []factCollector{
			{Title: "Uptime", Run: func() string { return runCmd("uptime") }},
		}
	case IssueService:
		return []factCollector{
			{Title: "Uptime", Run: func() string { return runCmd("uptime") }},
			{Title: "Failed Services", Run: collectFailedServices},
			{Title: "Recent Service Errors", Run: collectRecentServiceErrors},
		}
	default:
		return []factCollector{
			{Title: "Uptime", Run: func() string { return runCmd("uptime") }},
			{Title: "Memory", Run: collectMemoryFacts},
			{Title: "Disk (df -h)", Run: func() string { return runCmd("df", "-h") }},
			{Title: "Interfaces", Run: collectInterfaceFacts},
			{Title: "Routes", Run: collectRouteFacts},
			{Title: "DNS Configuration", Run: collectDNSFacts},
			{Title: "Recent Service Errors", Run: collectRecentServiceErrors},
		}
	}
}

func addCommonFacts(facts *SystemFacts) {
	hostname, _ := os.Hostname()
	if hostname != "" {
		facts.Sections = append(facts.Sections, FactSection{Title: "Hostname", Content: hostname})
	}

	kernel := runCmd("uname", "-a")
	if kernel != "" {
		facts.Sections = append(facts.Sections, FactSection{Title: "Kernel", Content: kernel})
	}
}

func collectPlannedFacts(facts *SystemFacts, collectors []factCollector) {
	for _, collector := range collectors {
		content := strings.TrimSpace(collector.Run())
		if content == "" {
			continue
		}
		facts.Sections = append(facts.Sections, FactSection{
			Title:   collector.Title,
			Content: content,
		})
	}
}

func writeSection(b *strings.Builder, title, content string) {
	if content == "" {
		return
	}
	b.WriteString(fmt.Sprintf("## %s\n```\n%s\n```\n\n", title, strings.TrimSpace(content)))
}

func runCmd(name string, args ...string) string {
	timeout := 3 * time.Second
	if name == "journalctl" || name == "systemctl" || name == "launchctl" {
		timeout = 5 * time.Second
	}
	return runCmdWithTimeout(timeout, name, args...)
}

func runCmdWithTimeout(timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

func readFileContents(path string, maxLines int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return strings.Join(lines, "\n")
}

func collectMemoryFacts() string {
	if runtime.GOOS == "linux" {
		if out := runCmd("free", "-h"); out != "" {
			return out
		}
		return readFileContents("/proc/meminfo", 20)
	}
	if runtime.GOOS == "darwin" {
		return runCmd("vm_stat")
	}
	return ""
}

func collectTopCPUProcesses() string {
	if runtime.GOOS == "linux" {
		return runCmd("sh", "-lc", "ps aux --sort=-%cpu | head -10")
	}
	if runtime.GOOS == "darwin" {
		return runCmd("sh", "-lc", "ps aux -arcpu | head -10")
	}
	return ""
}

func collectTopMemoryProcesses() string {
	if runtime.GOOS == "linux" {
		return runCmd("sh", "-lc", "ps aux --sort=-%mem | head -10")
	}
	if runtime.GOOS == "darwin" {
		return runCmd("sh", "-lc", "ps aux -armem | head -10")
	}
	return ""
}

func collectInterfaceFacts() string {
	if runtime.GOOS == "linux" {
		return runCmd("ip", "-o", "addr", "show")
	}
	if runtime.GOOS == "darwin" {
		return runCmd("ifconfig")
	}
	return ""
}

func collectRouteFacts() string {
	if runtime.GOOS == "linux" {
		return runCmd("ip", "route")
	}
	if runtime.GOOS == "darwin" {
		return runCmd("netstat", "-rn")
	}
	return ""
}

func collectDNSFacts() string {
	if runtime.GOOS == "linux" {
		if out := readFileContents("/etc/resolv.conf", 20); out != "" {
			return out
		}
	}
	if runtime.GOOS == "darwin" {
		return runCmd("scutil", "--dns")
	}
	return ""
}

func collectListeningPorts() string {
	if runtime.GOOS == "linux" {
		return runCmd("ss", "-tlnp")
	}
	if runtime.GOOS == "darwin" {
		return runCmd("lsof", "-iTCP", "-sTCP:LISTEN", "-P", "-n")
	}
	return ""
}

func collectFailedServices() string {
	if runtime.GOOS == "linux" {
		return runCmd("systemctl", "--failed", "--no-pager", "--plain")
	}
	if runtime.GOOS == "darwin" {
		return runCmd("launchctl", "list")
	}
	return ""
}

func collectRecentServiceErrors() string {
	if runtime.GOOS == "linux" {
		return runCmd("journalctl", "-p", "err", "-n", "30", "--no-pager")
	}
	if runtime.GOOS == "darwin" {
		return runCmd("log", "show", "--last", "5m", "--style", "compact")
	}
	return ""
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

var serviceNameRe = regexp.MustCompile(`\b(nginx|apache2?|httpd|sshd|ssh|docker|postgres(?:ql)?|mysql|mariadb|redis|kubelet|tailscaled|avahi-daemon|caddy|grafana|prometheus|alertmanager|haproxy|traefik|elasticsearch|kibana|logstash|consul|vault|nomad|envoy|minio|rabbitmq|mosquitto|mongod?|memcached|couchdb|influxdb|telegraf|collectd|chrony|ntpd|dnsmasq|unbound|named|bind9?|postfix|dovecot|openvpn|wireguard)\b`)

var serviceNameFallbackRe = regexp.MustCompile(`\b([a-z][a-z0-9_-]{1,30})\s+(?:service|daemon|won'?t\s+start|keeps?\s+(?:crash|restart|fail)|failed|is\s+down)`)

var falsePositiveServices = map[string]bool{
	"the": true, "my": true, "this": true, "a": true,
	"an": true, "our": true, "your": true,
}

var containerNameRe = regexp.MustCompile(`\b(?:container|docker)\s+(?:run|start|stop|restart|logs|inspect|exec)\s+([a-z][a-z0-9_.-]{1,63})\b`)

var falsePositiveContainerNames = map[string]bool{
	"the": true, "my": true, "this": true, "a": true,
	"an": true, "our": true, "your": true,
	"is": true, "are": true, "was": true, "not": true,
	"up": true, "down": true, "and": true, "or": true,
}

func ExtractContainerName(value string) string {
	match := containerNameRe.FindStringSubmatch(value)
	if len(match) >= 2 && !falsePositiveContainerNames[match[1]] {
		return match[1]
	}
	return ""
}

var buildToolRe = regexp.MustCompile(`\b(npm|yarn|pnpm|cargo|go|make|cmake|gradle|maven|mvn|webpack|vite|tsc|rustc|gcc|g\+\+|clang)\b`)

func ExtractBuildTool(value string) string {
	match := buildToolRe.FindStringSubmatch(strings.ToLower(value))
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

var portRe = regexp.MustCompile(`\b(?:port\s+)?(\d{2,5})\b`)

func ExtractPort(value string) string {
	match := portRe.FindStringSubmatch(strings.ToLower(value))
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

var pathRe = regexp.MustCompile(`(?:^|\s)(/?(?:[a-zA-Z0-9._-]+/)+[a-zA-Z0-9._-]+)`)

func ExtractPath(value string) string {
	match := pathRe.FindStringSubmatch(value)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

func ExtractServiceName(value string) string {
	match := serviceNameRe.FindStringSubmatch(value)
	if len(match) >= 2 {
		return match[1]
	}
	// Fallback: pattern like "<name> service" or "<name> won't start"
	fallback := serviceNameFallbackRe.FindStringSubmatch(value)
	if len(fallback) >= 2 && !falsePositiveServices[fallback[1]] {
		return fallback[1]
	}
	return ""
}
