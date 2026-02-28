package diagnose

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type SystemFacts struct {
	Hostname  string
	Kernel    string
	Platform  string
	Uptime    string
	Memory    string
	CPU       string
	Disk      string
	Network   string
	Routes    string
	DNS       string
	DmesgTail string
	JournalErrors string
	CollectedAt   time.Time
}

func CollectFacts() *SystemFacts {
	facts := &SystemFacts{
		Platform:    runtime.GOOS,
		CollectedAt: time.Now(),
	}

	if runtime.GOOS != "linux" {
		facts.Hostname, _ = os.Hostname()
		return facts
	}

	facts.Hostname, _ = os.Hostname()
	facts.Kernel = runCmd("uname", "-a")
	facts.Uptime = runCmd("uptime")
	facts.Memory = readFileContents("/proc/meminfo", 20)
	facts.CPU = readFileContents("/proc/cpuinfo", 30)
	facts.Disk = runCmd("df", "-h")
	facts.Network = runCmd("ip", "-o", "addr", "show")
	facts.Routes = runCmd("ip", "route")
	facts.DNS = readFileContents("/etc/resolv.conf", 20)
	facts.DmesgTail = runCmd("dmesg", "--time-format=reltime", "-T")
	facts.JournalErrors = runCmd("journalctl", "-p", "err", "-n", "20", "--no-pager")

	return facts
}

func Format(facts *SystemFacts) string {
	var b strings.Builder
	b.WriteString("# System Facts\n")
	b.WriteString(fmt.Sprintf("Collected: %s\n\n", facts.CollectedAt.Format(time.RFC3339)))

	writeSection(&b, "Hostname", facts.Hostname)
	writeSection(&b, "Kernel", facts.Kernel)
	writeSection(&b, "Uptime", facts.Uptime)
	writeSection(&b, "Memory (/proc/meminfo)", facts.Memory)
	writeSection(&b, "CPU (/proc/cpuinfo)", facts.CPU)
	writeSection(&b, "Disk (df -h)", facts.Disk)
	writeSection(&b, "Network (ip addr)", facts.Network)
	writeSection(&b, "Routes (ip route)", facts.Routes)
	writeSection(&b, "DNS (/etc/resolv.conf)", facts.DNS)
	writeSection(&b, "dmesg (tail)", facts.DmesgTail)
	writeSection(&b, "Journal Errors", facts.JournalErrors)

	return b.String()
}

func writeSection(b *strings.Builder, title, content string) {
	if content == "" {
		return
	}
	b.WriteString(fmt.Sprintf("## %s\n```\n%s\n```\n\n", title, strings.TrimSpace(content)))
}

func runCmd(name string, args ...string) string {
	cmd := exec.Command(name, args...)
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
