package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/opencode-ai/opencode/internal/message"
)

// DiagnosticResult holds a structured diagnostic built from real parsed values.
// This eliminates model fabrication for common system commands.
type DiagnosticResult struct {
	Summary     string
	RiskLevel   string   // "Low", "Medium", "High", "Critical"
	Findings    []string // bullet points with real values
	Remediation []string // action items
}

// Render formats the diagnostic in the same structure the system prompt expects.
func (d *DiagnosticResult) Render() string {
	var b strings.Builder
	b.WriteString("**Summary**: " + d.Summary + "\n")
	b.WriteString("**Risk Level**: " + d.RiskLevel + "\n\n")
	b.WriteString("**Evidence**:\n")
	for _, f := range d.Findings {
		b.WriteString("- " + f + "\n")
	}
	if len(d.Remediation) > 0 {
		b.WriteString("\n**Remediation**:\n")
		for i, r := range d.Remediation {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, r))
		}
	}
	return b.String()
}

// tryStructuredDiagnostic attempts to parse tool output with deterministic Go code
// instead of sending it to the model for Pass 2. Returns (rendered diagnostic, true)
// if successful, or ("", false) to fall through to model Pass 2.
func tryStructuredDiagnostic(toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolCalls) != 1 || len(toolResults) == 0 {
		return "", false
	}

	if toolCalls[0].Name != "bash" {
		return "", false
	}

	command := extractCommand(toolCalls[0].Input)
	if command == "" {
		return "", false
	}

	output := toolResults[0].Content
	if toolResults[0].IsError || output == "" || strings.HasPrefix(output, "Error:") {
		return "", false
	}

	key := detectCommandKey(command)
	switch key {
	case "df":
		return parseDiskUsage(output)
	case "free":
		return parseMemory(output)
	case "uptime":
		return parseUptime(output)
	case "top":
		return parseTop(output)
	case "ps":
		return parseProcesses(output)
	case "ip":
		return parseNetwork(output)
	case "ifconfig":
		return parseIfconfig(output)
	case "ss":
		return parseSockets(output)
	case "lsof":
		return parseLsof(output)
	case "uname":
		return parseUname(output)
	case "sw_vers":
		return parseSwVers(output)
	case "vm_stat":
		return parseVmStat(output)
	case "sysctl":
		return parseSysctl(output)
	case "cat_etc":
		return parseEtcFile(command, output)
	default:
		return "", false
	}
}

// extractCommand parses the JSON input of a bash tool call to get the command string.
func extractCommand(inputJSON string) string {
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(inputJSON), &params); err != nil {
		return ""
	}
	cmd, _ := params["command"].(string)
	return cmd
}

// detectCommandKey maps a full command string to a parser key.
func detectCommandKey(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	base := filepath.Base(fields[0])

	switch base {
	case "df":
		return "df"
	case "free":
		return "free"
	case "uptime":
		return "uptime"
	case "top":
		return "top"
	case "ps":
		return "ps"
	case "ip":
		return "ip"
	case "ifconfig":
		return "ifconfig"
	case "ss":
		return "ss"
	case "lsof":
		return "lsof"
	case "uname":
		return "uname"
	case "sw_vers":
		return "sw_vers"
	case "vm_stat":
		return "vm_stat"
	case "sysctl":
		return "sysctl"
	case "cat":
		if len(fields) > 1 && strings.HasPrefix(fields[1], "/etc/") {
			return "cat_etc"
		}
	}
	return ""
}

// riskFromPercent returns a risk level string given a usage percentage.
func riskFromPercent(pct int) string {
	switch {
	case pct >= 95:
		return "Critical"
	case pct >= 90:
		return "High"
	case pct >= 80:
		return "Medium"
	default:
		return "Low"
	}
}

// --- Parsers ---

// parseDiskUsage handles df -h output (Linux and macOS).
func parseDiskUsage(output string) (string, bool) {
	lines := strings.Split(output, "\n")
	type entry struct {
		Mount, Size, Used, Avail, Pct string
	}
	var entries []entry

	for _, line := range lines[1:] { // skip header
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		// Find the percentage field
		pctIdx := -1
		for i, f := range fields {
			if strings.HasSuffix(f, "%") {
				if _, err := strconv.Atoi(strings.TrimSuffix(f, "%")); err == nil {
					pctIdx = i
					break
				}
			}
		}
		if pctIdx < 3 {
			continue
		}
		entries = append(entries, entry{
			Mount: fields[len(fields)-1],
			Size:  fields[pctIdx-3],
			Used:  fields[pctIdx-2],
			Avail: fields[pctIdx-1],
			Pct:   fields[pctIdx],
		})
	}

	if len(entries) == 0 {
		return "", false
	}

	maxPct := 0
	for _, e := range entries {
		pct, _ := strconv.Atoi(strings.TrimSuffix(e.Pct, "%"))
		if pct > maxPct {
			maxPct = pct
		}
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("Disk usage: highest partition at %d%%", maxPct),
		RiskLevel: riskFromPercent(maxPct),
	}
	for _, e := range entries {
		d.Findings = append(d.Findings,
			fmt.Sprintf("%s: %s used of %s (%s), %s available",
				e.Mount, e.Used, e.Size, e.Pct, e.Avail))
	}
	if maxPct >= 80 {
		d.Remediation = append(d.Remediation, "Identify large files: `du -sh /* | sort -rh | head -20`")
		d.Remediation = append(d.Remediation, "Clear package caches: `apt clean` or `brew cleanup`")
	}
	return d.Render(), true
}

// parseMemory handles free -h / free -m output (Linux).
func parseMemory(output string) (string, bool) {
	lines := strings.Split(output, "\n")
	var memTotal, memUsed, memAvail, swapTotal, swapUsed string
	var memPct, swapPct int

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		switch {
		case strings.HasPrefix(fields[0], "Mem:"):
			memTotal = fields[1]
			memUsed = fields[2]
			if len(fields) >= 7 {
				memAvail = fields[6] // "available" column
			} else if len(fields) >= 4 {
				memAvail = fields[3] // "free" column as fallback
			}
			total := parseSize(memTotal)
			used := parseSize(memUsed)
			if total > 0 {
				memPct = int(used * 100 / total)
			}
		case strings.HasPrefix(fields[0], "Swap:"):
			swapTotal = fields[1]
			swapUsed = fields[2]
			total := parseSize(swapTotal)
			used := parseSize(swapUsed)
			if total > 0 {
				swapPct = int(used * 100 / total)
			}
		}
	}

	if memTotal == "" {
		return "", false
	}

	risk := "Low"
	switch {
	case memPct >= 90:
		risk = "Critical"
	case memPct >= 80:
		risk = "High"
	case memPct >= 70:
		risk = "Medium"
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("Memory usage at %d%%", memPct),
		RiskLevel: risk,
	}
	d.Findings = append(d.Findings,
		fmt.Sprintf("RAM: %s used of %s (%d%%), %s available", memUsed, memTotal, memPct, memAvail))
	if swapTotal != "" {
		d.Findings = append(d.Findings,
			fmt.Sprintf("Swap: %s used of %s (%d%%)", swapUsed, swapTotal, swapPct))
	}
	if memPct >= 80 {
		d.Remediation = append(d.Remediation, "Identify memory-heavy processes: `ps aux --sort=-%mem | head -10`")
		d.Remediation = append(d.Remediation, "Check for memory leaks in long-running services")
	}
	return d.Render(), true
}

// parseSize converts a size string like "15Gi", "8.2G", "16384" to a float64 in MB.
func parseSize(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	multipliers := map[string]float64{
		"Ki": 1.0 / 1024, "Mi": 1, "Gi": 1024, "Ti": 1024 * 1024,
		"K": 1.0 / 1024, "M": 1, "G": 1024, "T": 1024 * 1024,
		"k": 1.0 / 1024, "m": 1, "g": 1024, "t": 1024 * 1024,
		"B": 1.0 / (1024 * 1024),
	}

	for suffix, mult := range multipliers {
		if strings.HasSuffix(s, suffix) {
			numStr := strings.TrimSuffix(s, suffix)
			if v, err := strconv.ParseFloat(numStr, 64); err == nil {
				return v * mult
			}
			return 0
		}
	}

	// Plain number — assume MB
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	return 0
}

var (
	// uptimeLoadLinux matches "load average: 1.23, 0.98, 0.76"
	uptimeLoadLinux = regexp.MustCompile(`load average:\s*([\d.]+),\s*([\d.]+),\s*([\d.]+)`)
	// uptimeLoadMac matches "load averages: 1.23 0.98 0.76"
	uptimeLoadMac = regexp.MustCompile(`load averages?:\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)`)
	// uptimeDuration matches "up 42 days, 3:15" or "up 3:15" or "up 42 days"
	uptimeDuration = regexp.MustCompile(`up\s+(.+?),\s*\d+\s+user`)
)

// parseUptime handles uptime output (Linux and macOS).
func parseUptime(output string) (string, bool) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", false
	}

	// Extract load averages
	var load1, load5, load15 string
	if m := uptimeLoadLinux.FindStringSubmatch(output); len(m) == 4 {
		load1, load5, load15 = m[1], m[2], m[3]
	} else if m := uptimeLoadMac.FindStringSubmatch(output); len(m) == 4 {
		load1, load5, load15 = m[1], m[2], m[3]
	}

	// Extract uptime duration
	uptimeStr := "unknown"
	if m := uptimeDuration.FindStringSubmatch(output); len(m) == 2 {
		uptimeStr = strings.TrimSpace(m[1])
	}

	// Determine risk from 1-minute load average
	risk := "Low"
	if load1 != "" {
		if l, err := strconv.ParseFloat(load1, 64); err == nil {
			switch {
			case l >= 8:
				risk = "Critical"
			case l >= 4:
				risk = "High"
			case l >= 2:
				risk = "Medium"
			}
		}
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("System up %s", uptimeStr),
		RiskLevel: risk,
	}
	d.Findings = append(d.Findings, fmt.Sprintf("Uptime: %s", uptimeStr))
	if load1 != "" {
		d.Findings = append(d.Findings,
			fmt.Sprintf("Load averages: %s (1m), %s (5m), %s (15m)", load1, load5, load15))
	}
	if risk == "High" || risk == "Critical" {
		d.Remediation = append(d.Remediation, "Identify CPU-heavy processes: `top -bn1 | head -20`")
		d.Remediation = append(d.Remediation, "Check for runaway processes: `ps aux --sort=-%cpu | head -10`")
	}
	return d.Render(), true
}

var (
	topCPULine  = regexp.MustCompile(`%Cpu.*?(\d+\.?\d*)\s*id`)
	topMemLine  = regexp.MustCompile(`MiB Mem\s*:\s*([\d.]+)\s+total.*?([\d.]+)\s+free.*?([\d.]+)\s+used`)
	topTaskLine = regexp.MustCompile(`Tasks:\s*(\d+)\s+total.*?(\d+)\s+running`)
)

// parseTop handles top -bn1 output (Linux).
func parseTop(output string) (string, bool) {
	lines := strings.Split(output, "\n")
	if len(lines) < 5 {
		return "", false
	}

	var cpuIdle float64
	var memTotal, memFree, memUsed string
	var tasksTotal, tasksRunning string
	var topProcs []string

	for _, line := range lines {
		if m := topCPULine.FindStringSubmatch(line); len(m) == 2 {
			cpuIdle, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := topMemLine.FindStringSubmatch(line); len(m) == 4 {
			memTotal, memFree, memUsed = m[1], m[2], m[3]
		}
		if m := topTaskLine.FindStringSubmatch(line); len(m) == 3 {
			tasksTotal, tasksRunning = m[1], m[2]
		}
	}

	// Collect top processes (lines after the header that have PID data)
	inProcs := false
	for _, line := range lines {
		if strings.Contains(line, "PID") && strings.Contains(line, "COMMAND") {
			inProcs = true
			continue
		}
		if inProcs && len(strings.TrimSpace(line)) > 0 {
			topProcs = append(topProcs, strings.TrimSpace(line))
			if len(topProcs) >= 5 {
				break
			}
		}
	}

	cpuUsed := 100.0 - cpuIdle
	risk := "Low"
	switch {
	case cpuUsed >= 95:
		risk = "Critical"
	case cpuUsed >= 90:
		risk = "High"
	case cpuUsed >= 70:
		risk = "Medium"
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("CPU usage at %.1f%%", cpuUsed),
		RiskLevel: risk,
	}
	if tasksTotal != "" {
		d.Findings = append(d.Findings, fmt.Sprintf("Tasks: %s total, %s running", tasksTotal, tasksRunning))
	}
	d.Findings = append(d.Findings, fmt.Sprintf("CPU: %.1f%% used (%.1f%% idle)", cpuUsed, cpuIdle))
	if memTotal != "" {
		d.Findings = append(d.Findings,
			fmt.Sprintf("Memory: %s MiB used of %s MiB total (%s MiB free)", memUsed, memTotal, memFree))
	}
	for _, proc := range topProcs {
		fields := strings.Fields(proc)
		if len(fields) >= 12 {
			d.Findings = append(d.Findings,
				fmt.Sprintf("Process: %s (PID %s) — CPU: %s%%, MEM: %s%%",
					fields[len(fields)-1], fields[0], fields[8], fields[9]))
		}
	}
	if risk != "Low" {
		d.Remediation = append(d.Remediation, "Identify CPU-heavy processes and consider throttling or restarting them")
	}
	return d.Render(), true
}

// parseProcesses handles ps aux output.
func parseProcesses(output string) (string, bool) {
	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return "", false
	}

	type proc struct {
		User, PID, CPU, MEM, Command string
	}
	var procs []proc
	var maxCPU, maxMem float64

	for _, line := range lines[1:] { // skip header
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}
		cpu, _ := strconv.ParseFloat(fields[2], 64)
		mem, _ := strconv.ParseFloat(fields[3], 64)
		cmd := strings.Join(fields[10:], " ")
		procs = append(procs, proc{
			User: fields[0], PID: fields[1],
			CPU: fields[2], MEM: fields[3],
			Command: cmd,
		})
		if cpu > maxCPU {
			maxCPU = cpu
		}
		if mem > maxMem {
			maxMem = mem
		}
	}

	if len(procs) == 0 {
		return "", false
	}

	risk := "Low"
	switch {
	case maxCPU >= 80 || maxMem >= 80:
		risk = "High"
	case maxCPU >= 50 || maxMem >= 50:
		risk = "Medium"
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("%d processes running, highest CPU: %.1f%%, highest MEM: %.1f%%", len(procs), maxCPU, maxMem),
		RiskLevel: risk,
	}
	d.Findings = append(d.Findings, fmt.Sprintf("Total processes: %d", len(procs)))

	// Show top 5 by CPU
	shown := 0
	for _, p := range procs {
		cpu, _ := strconv.ParseFloat(p.CPU, 64)
		if cpu > 0.5 || shown < 5 {
			cmdShort := p.Command
			if len(cmdShort) > 60 {
				cmdShort = cmdShort[:60] + "..."
			}
			d.Findings = append(d.Findings,
				fmt.Sprintf("PID %s (%s): CPU %s%%, MEM %s%% — %s",
					p.PID, p.User, p.CPU, p.MEM, cmdShort))
			shown++
			if shown >= 10 {
				break
			}
		}
	}
	if risk != "Low" {
		d.Remediation = append(d.Remediation, "Investigate high-usage processes for possible issues")
	}
	return d.Render(), true
}

var (
	ipIfaceLine = regexp.MustCompile(`^\d+:\s+(\S+):.*state\s+(\S+)`)
	ipInetLine  = regexp.MustCompile(`^\s+inet\s+(\S+)`)
	ipInet6Line = regexp.MustCompile(`^\s+inet6\s+(\S+)`)
)

// parseNetwork handles ip addr output (Linux).
func parseNetwork(output string) (string, bool) {
	lines := strings.Split(output, "\n")
	type iface struct {
		Name  string
		State string
		IPs   []string
	}
	var ifaces []iface
	var current *iface
	anyDown := false

	for _, line := range lines {
		if m := ipIfaceLine.FindStringSubmatch(line); len(m) == 3 {
			if current != nil {
				ifaces = append(ifaces, *current)
			}
			current = &iface{Name: m[1], State: m[2]}
			if m[2] == "DOWN" {
				anyDown = true
			}
		} else if current != nil {
			if m := ipInetLine.FindStringSubmatch(line); len(m) == 2 {
				current.IPs = append(current.IPs, m[1])
			} else if m := ipInet6Line.FindStringSubmatch(line); len(m) == 2 {
				current.IPs = append(current.IPs, m[1])
			}
		}
	}
	if current != nil {
		ifaces = append(ifaces, *current)
	}

	if len(ifaces) == 0 {
		return "", false
	}

	risk := "Low"
	if anyDown {
		risk = "Medium"
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("%d network interfaces detected", len(ifaces)),
		RiskLevel: risk,
	}
	for _, ifc := range ifaces {
		ips := "no IP assigned"
		if len(ifc.IPs) > 0 {
			ips = strings.Join(ifc.IPs, ", ")
		}
		d.Findings = append(d.Findings,
			fmt.Sprintf("%s: %s — %s", ifc.Name, ifc.State, ips))
	}
	if anyDown {
		d.Remediation = append(d.Remediation, "Check why interfaces are DOWN: `ip link show`")
	}
	return d.Render(), true
}

var (
	ifconfigNameLine   = regexp.MustCompile(`^(\S+):\s+flags=\d+<([^>]*)>`)
	ifconfigInetLine   = regexp.MustCompile(`inet\s+(\d+\.\d+\.\d+\.\d+)`)
	ifconfigErrorsLine = regexp.MustCompile(`(?:RX|TX)\s+errors\s+(\d+)`)
)

// parseIfconfig handles ifconfig output (Linux and macOS).
func parseIfconfig(output string) (string, bool) {
	// Split on interface boundaries (lines starting with non-whitespace)
	blocks := splitIfconfigBlocks(output)
	if len(blocks) == 0 {
		return "", false
	}

	type iface struct {
		Name, Flags, IP string
		Errors          int
	}
	var ifaces []iface
	totalErrors := 0

	for _, block := range blocks {
		var ifc iface
		if m := ifconfigNameLine.FindStringSubmatch(block); len(m) == 3 {
			ifc.Name = m[1]
			ifc.Flags = m[2]
		} else {
			continue
		}
		if m := ifconfigInetLine.FindStringSubmatch(block); len(m) == 2 {
			ifc.IP = m[1]
		}
		for _, m := range ifconfigErrorsLine.FindAllStringSubmatch(block, -1) {
			e, _ := strconv.Atoi(m[1])
			ifc.Errors += e
		}
		totalErrors += ifc.Errors
		ifaces = append(ifaces, ifc)
	}

	if len(ifaces) == 0 {
		return "", false
	}

	risk := "Low"
	if totalErrors > 0 {
		risk = "Medium"
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("%d network interfaces, %d total errors", len(ifaces), totalErrors),
		RiskLevel: risk,
	}
	for _, ifc := range ifaces {
		ip := "no IP"
		if ifc.IP != "" {
			ip = ifc.IP
		}
		errStr := ""
		if ifc.Errors > 0 {
			errStr = fmt.Sprintf(" (%d errors)", ifc.Errors)
		}
		d.Findings = append(d.Findings,
			fmt.Sprintf("%s: %s [%s]%s", ifc.Name, ip, ifc.Flags, errStr))
	}
	if totalErrors > 0 {
		d.Remediation = append(d.Remediation, "Investigate network errors — may indicate hardware or driver issues")
	}
	return d.Render(), true
}

func splitIfconfigBlocks(output string) []string {
	lines := strings.Split(output, "\n")
	var blocks []string
	var current strings.Builder

	for _, line := range lines {
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && current.Len() > 0 {
			blocks = append(blocks, current.String())
			current.Reset()
		}
		current.WriteString(line + "\n")
	}
	if current.Len() > 0 {
		blocks = append(blocks, current.String())
	}
	return blocks
}

// parseSockets handles ss -tulnp output.
func parseSockets(output string) (string, bool) {
	lines := strings.Split(output, "\n")
	type socket struct {
		State, LocalAddr, Process string
	}
	var sockets []socket

	commonPorts := map[string]bool{
		"22": true, "80": true, "443": true, "3306": true,
		"5432": true, "6379": true, "8080": true, "8443": true,
	}
	exposedCommon := false

	for _, line := range lines[1:] { // skip header
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		state := fields[0]
		localAddr := fields[3]
		if len(localAddr) == 0 {
			localAddr = fields[4] // header alignment varies
		}
		proc := ""
		if len(fields) >= 6 {
			proc = fields[len(fields)-1]
		}
		sockets = append(sockets, socket{State: state, LocalAddr: localAddr, Process: proc})

		// Check if listening on 0.0.0.0 or * on a common port
		parts := strings.Split(localAddr, ":")
		if len(parts) >= 2 {
			port := parts[len(parts)-1]
			host := strings.Join(parts[:len(parts)-1], ":")
			if (host == "0.0.0.0" || host == "*" || host == "[::]") && commonPorts[port] {
				exposedCommon = true
			}
		}
	}

	if len(sockets) == 0 {
		return "", false
	}

	risk := "Low"
	if exposedCommon {
		risk = "Medium"
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("%d listening sockets", len(sockets)),
		RiskLevel: risk,
	}
	for _, s := range sockets {
		d.Findings = append(d.Findings,
			fmt.Sprintf("%s %s %s", s.State, s.LocalAddr, s.Process))
	}
	if exposedCommon {
		d.Remediation = append(d.Remediation, "Review services listening on 0.0.0.0 — consider binding to 127.0.0.1 if not needed externally")
	}
	return d.Render(), true
}

// parseLsof handles lsof -i output.
func parseLsof(output string) (string, bool) {
	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return "", false
	}

	type conn struct {
		Command, PID, User, Name string
	}
	var listening, established []conn

	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		c := conn{
			Command: fields[0],
			PID:     fields[1],
			User:    fields[2],
			Name:    fields[len(fields)-1],
		}
		if strings.Contains(line, "LISTEN") {
			listening = append(listening, c)
		} else if strings.Contains(line, "ESTABLISHED") {
			established = append(established, c)
		}
	}

	total := len(listening) + len(established)
	if total == 0 {
		return "", false
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("%d listening, %d established connections", len(listening), len(established)),
		RiskLevel: "Low",
	}
	if len(listening) > 0 {
		d.Findings = append(d.Findings, fmt.Sprintf("Listening (%d):", len(listening)))
		for _, c := range listening {
			d.Findings = append(d.Findings, fmt.Sprintf("  %s (PID %s, %s) — %s", c.Command, c.PID, c.User, c.Name))
		}
	}
	if len(established) > 0 {
		d.Findings = append(d.Findings, fmt.Sprintf("Established (%d):", len(established)))
		limit := len(established)
		if limit > 10 {
			limit = 10
		}
		for _, c := range established[:limit] {
			d.Findings = append(d.Findings, fmt.Sprintf("  %s (PID %s, %s) — %s", c.Command, c.PID, c.User, c.Name))
		}
		if len(established) > 10 {
			d.Findings = append(d.Findings, fmt.Sprintf("  ... and %d more", len(established)-10))
		}
	}
	return d.Render(), true
}

// parseUname handles uname -a output.
func parseUname(output string) (string, bool) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", false
	}

	fields := strings.Fields(output)
	if len(fields) < 3 {
		return "", false
	}

	os := fields[0]     // "Linux" or "Darwin"
	hostname := fields[1]
	kernel := fields[2]
	arch := ""
	if len(fields) >= 12 {
		// Linux: "Linux hostname 5.15.0 ... x86_64 ... GNU/Linux"
		arch = fields[len(fields)-2]
	} else if len(fields) >= 4 {
		arch = fields[len(fields)-1]
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("%s %s on %s (%s)", os, kernel, hostname, arch),
		RiskLevel: "Low",
	}
	d.Findings = append(d.Findings, fmt.Sprintf("OS: %s", os))
	d.Findings = append(d.Findings, fmt.Sprintf("Hostname: %s", hostname))
	d.Findings = append(d.Findings, fmt.Sprintf("Kernel: %s", kernel))
	if arch != "" {
		d.Findings = append(d.Findings, fmt.Sprintf("Architecture: %s", arch))
	}
	return d.Render(), true
}

// parseSwVers handles macOS sw_vers output.
func parseSwVers(output string) (string, bool) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", false
	}

	info := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			info[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	product := info["ProductName"]
	version := info["ProductVersion"]
	build := info["BuildVersion"]

	if product == "" && version == "" {
		return "", false
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("%s %s (Build %s)", product, version, build),
		RiskLevel: "Low",
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			d.Findings = append(d.Findings, line)
		}
	}
	return d.Render(), true
}

var vmStatLine = regexp.MustCompile(`^(.+?):\s+([\d]+)\.?$`)

// parseVmStat handles macOS vm_stat output.
func parseVmStat(output string) (string, bool) {
	lines := strings.Split(output, "\n")
	if len(lines) < 3 {
		return "", false
	}

	// Extract page size from first line
	pageSize := 4096 // default
	if m := regexp.MustCompile(`page size of (\d+) bytes`).FindStringSubmatch(lines[0]); len(m) == 2 {
		pageSize, _ = strconv.Atoi(m[1])
	}

	stats := make(map[string]int64)
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		// Remove surrounding quotes from key
		line = strings.ReplaceAll(line, "\"", "")
		if m := vmStatLine.FindStringSubmatch(line); len(m) == 3 {
			key := strings.TrimSpace(m[1])
			val, _ := strconv.ParseInt(m[2], 10, 64)
			stats[key] = val
		}
	}

	free := stats["Pages free"]
	active := stats["Pages active"]
	inactive := stats["Pages inactive"]
	wired := stats["Pages wired down"]
	compressed := stats["Pages occupied by compressor"]

	totalUseful := free + active + inactive + wired + compressed
	if totalUseful == 0 {
		return "", false
	}

	toMB := func(pages int64) string {
		mb := float64(pages) * float64(pageSize) / (1024 * 1024)
		return fmt.Sprintf("%.0f MB", mb)
	}

	freePct := int(free * 100 / totalUseful)
	risk := "Low"
	switch {
	case freePct <= 5:
		risk = "Critical"
	case freePct <= 10:
		risk = "High"
	case freePct <= 20:
		risk = "Medium"
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("VM: %d%% pages free (%s)", freePct, toMB(free)),
		RiskLevel: risk,
	}
	d.Findings = append(d.Findings, fmt.Sprintf("Page size: %d bytes", pageSize))
	d.Findings = append(d.Findings, fmt.Sprintf("Free: %s (%d pages)", toMB(free), free))
	d.Findings = append(d.Findings, fmt.Sprintf("Active: %s (%d pages)", toMB(active), active))
	d.Findings = append(d.Findings, fmt.Sprintf("Inactive: %s (%d pages)", toMB(inactive), inactive))
	d.Findings = append(d.Findings, fmt.Sprintf("Wired: %s (%d pages)", toMB(wired), wired))
	if compressed > 0 {
		d.Findings = append(d.Findings, fmt.Sprintf("Compressed: %s (%d pages)", toMB(compressed), compressed))
	}
	if risk != "Low" {
		d.Remediation = append(d.Remediation, "Close memory-heavy applications or restart services with memory leaks")
	}
	return d.Render(), true
}

// parseSysctl handles sysctl output (key = value pairs).
func parseSysctl(output string) (string, bool) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", false
	}

	lines := strings.Split(output, "\n")
	d := DiagnosticResult{
		Summary:   fmt.Sprintf("System kernel parameters (%d entries)", len(lines)),
		RiskLevel: "Low",
	}

	shown := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		d.Findings = append(d.Findings, line)
		shown++
		if shown >= 20 {
			d.Findings = append(d.Findings, fmt.Sprintf("... and %d more", len(lines)-shown))
			break
		}
	}
	return d.Render(), true
}

// parseEtcFile handles cat /etc/<file> output.
func parseEtcFile(command, output string) (string, bool) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", false
	}

	// Extract filename from command
	fields := strings.Fields(command)
	filename := "/etc/unknown"
	if len(fields) > 1 {
		filename = fields[len(fields)-1]
	}

	lines := strings.Split(output, "\n")
	// Filter out comment-only lines for summary count
	contentLines := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			contentLines++
		}
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("Contents of %s (%d lines, %d non-comment)", filename, len(lines), contentLines),
		RiskLevel: "Low",
	}

	shown := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		d.Findings = append(d.Findings, trimmed)
		shown++
		if shown >= 30 {
			d.Findings = append(d.Findings, fmt.Sprintf("... and %d more non-comment lines", contentLines-shown))
			break
		}
	}
	return d.Render(), true
}
