package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/AIdoesmyjob/termfix/internal/diagnose"
	"github.com/AIdoesmyjob/termfix/internal/message"
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
	if len(toolCalls) == 0 || len(toolResults) == 0 {
		return "", false
	}

	// For multi-tool results, try structured parsing on the last bash tool call
	lastIdx := -1
	for i := len(toolCalls) - 1; i >= 0; i-- {
		if toolCalls[i].Name == "bash" {
			lastIdx = i
			break
		}
	}
	if lastIdx < 0 || lastIdx >= len(toolResults) {
		return "", false
	}

	command := extractCommand(toolCalls[lastIdx].Input)
	if command == "" {
		return "", false
	}

	output := stripStreamTags(toolResults[lastIdx].Content)
	if toolResults[lastIdx].IsError || output == "" || strings.HasPrefix(output, "Error:") {
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

func tryStructuredRecipeDiagnostic(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if recipe == nil || len(toolCalls) == 0 || len(toolResults) == 0 {
		return "", false
	}

	// Strip stderr tags from all tool results before parsing
	cleaned := make([]message.ToolResult, len(toolResults))
	for i, tr := range toolResults {
		cleaned[i] = tr
		cleaned[i].Content = stripStreamTags(tr.Content)
	}
	toolResults = cleaned

	switch recipe.Name {
	case diagnose.RecipeServiceFailure:
		return parseServiceFailureRecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipeDNSResolution:
		return parseDNSRecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipeNetworkConnectivity:
		return parseNetworkRecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipeDiskUsage:
		return parseDiskUsageRecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipeMemoryPressure:
		return parseMemoryPressureRecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipePerformanceCPU:
		return parsePerformanceCPURecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipeDockerCrash:
		return parseDockerCrashRecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipeBuildFailure:
		return parseBuildFailureRecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipePermission:
		return parsePermissionRecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipePortConflict:
		return parsePortConflictRecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipeSSL:
		return parseSSLRecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipeGit:
		return parseGitRecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipeCron:
		return parseCronRecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipePackage:
		return parsePackageRecipe(recipe, toolCalls, toolResults)
	case diagnose.RecipeProcess:
		return parseProcessRecipe(recipe, toolCalls, toolResults)
	default:
		return "", false
	}
}

// stripStreamTags removes <stderr>...</stderr> wrapper tags from tool output.
func stripStreamTags(output string) string {
	output = strings.ReplaceAll(output, "<stderr>\n", "")
	output = strings.ReplaceAll(output, "\n</stderr>", "")
	output = strings.ReplaceAll(output, "<stderr>", "")
	output = strings.ReplaceAll(output, "</stderr>", "")
	return output
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

func parseServiceFailureRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolResults) == 0 {
		return "", false
	}

	// Detect platform from the command in the first tool call.
	cmd := extractCommand(toolCalls[0].Input)
	if strings.Contains(cmd, "launchctl") {
		return parseMacServiceFailure(recipe, toolCalls, toolResults)
	}
	return parseLinuxServiceFailure(recipe, toolCalls, toolResults)
}

func parseLinuxServiceFailure(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolCalls) < 2 || len(toolResults) < 2 {
		return "", false
	}
	statusOutput := toolResults[0].Content
	logOutput := toolResults[1].Content
	if strings.TrimSpace(statusOutput) == "" || strings.TrimSpace(logOutput) == "" {
		return "", false
	}

	statusLines := grepLines(statusOutput, `(?i)(Active:|Loaded:|Main PID:|status=|failed)`)
	errorLines := grepLines(logOutput, `(?i)(error|failed|fatal|panic|denied|permission|address already in use|exception|invalid|not found|refused)`)
	if len(statusLines) == 0 && len(errorLines) == 0 {
		return "", false
	}

	serviceName := recipe.ServiceName
	if serviceName == "" {
		serviceName = "service"
	}
	rootCause := detectRootCause(errorLines)

	risk := "High"
	if rootCause == "port conflict" || rootCause == "permission error" {
		risk = "Critical"
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("%s is failing to start due to %s", serviceName, rootCause),
		RiskLevel: risk,
	}
	for _, line := range firstN(statusLines, 4) {
		d.Findings = append(d.Findings, line)
	}
	for _, line := range firstN(errorLines, 4) {
		d.Findings = append(d.Findings, line)
	}

	switch rootCause {
	case "port conflict":
		d.Remediation = append(d.Remediation, "Find the conflicting listener and free the port before restarting the service")
	case "permission error":
		d.Remediation = append(d.Remediation, "Fix the file, directory, or capability permissions the service needs at startup")
	case "invalid configuration":
		d.Remediation = append(d.Remediation, "Validate and correct the service configuration before restarting")
	default:
		d.Remediation = append(d.Remediation, "Review the recent service errors and fix the failing dependency or configuration")
	}
	d.Remediation = append(d.Remediation, fmt.Sprintf("Re-run `systemctl status %s --no-pager --full -n 20` after applying the fix", serviceName))
	return d.Render(), true
}

func parseMacServiceFailure(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	serviceName := recipe.ServiceName
	if serviceName == "" {
		serviceName = "service"
	}

	// Case A — 1 result: launchctl list (no specific service name)
	if len(toolResults) == 1 {
		return parseMacServiceList(serviceName, toolResults[0].Content)
	}

	// Case B — 2 results: launchctl list | grep + log show
	if len(toolResults) < 2 {
		return "", false
	}

	grepOutput := toolResults[0].Content
	logOutput := toolResults[1].Content
	if strings.TrimSpace(grepOutput) == "" {
		return "", false
	}

	entries := parseLaunchctlList(grepOutput)

	// Determine service state from launchctl grep
	var pid string
	var exitCode int
	if len(entries) > 0 {
		pid = entries[0].PID
		exitCode = entries[0].ExitCode
	}

	// Parse log output for errors
	errorLines := grepLines(logOutput, `(?i)(error|failed|fatal|panic|denied|permission|address already in use|exception|invalid|not found|refused)`)
	rootCause := detectRootCause(errorLines)

	// Service is running (has a PID and zero exit code)
	if pid != "-" && pid != "" && exitCode == 0 {
		d := DiagnosticResult{
			Summary:   fmt.Sprintf("%s is running (PID %s)", serviceName, pid),
			RiskLevel: "Low",
		}
		d.Findings = append(d.Findings, fmt.Sprintf("PID: %s, exit code: %d", pid, exitCode))
		if len(errorLines) > 0 {
			for _, line := range firstN(errorLines, 4) {
				d.Findings = append(d.Findings, line)
			}
			d.Remediation = append(d.Remediation, "Service is running but recent logs contain errors — monitor for recurrence")
		}
		return d.Render(), true
	}

	// Service is not running or has non-zero exit
	risk := "High"
	if rootCause == "port conflict" || rootCause == "permission error" {
		risk = "Critical"
	}

	summary := fmt.Sprintf("%s is not running (exit code %d)", serviceName, exitCode)
	if rootCause != "service failure" {
		summary = fmt.Sprintf("%s is failing to start due to %s", serviceName, rootCause)
	}

	d := DiagnosticResult{
		Summary:   summary,
		RiskLevel: risk,
	}
	d.Findings = append(d.Findings, fmt.Sprintf("PID: %s, exit code: %d", pid, exitCode))
	for _, line := range firstN(errorLines, 4) {
		d.Findings = append(d.Findings, line)
	}

	switch rootCause {
	case "port conflict":
		d.Remediation = append(d.Remediation, "Find the conflicting listener and free the port before restarting the service")
	case "permission error":
		d.Remediation = append(d.Remediation, "Fix the file, directory, or capability permissions the service needs at startup")
	case "invalid configuration":
		d.Remediation = append(d.Remediation, "Validate and correct the service configuration before restarting")
	default:
		d.Remediation = append(d.Remediation, "Review the recent service errors and fix the failing dependency or configuration")
	}
	d.Remediation = append(d.Remediation, fmt.Sprintf("Re-run `launchctl list | grep -i %s` after applying the fix", serviceName))
	return d.Render(), true
}

// parseMacServiceList handles the case where launchctl list output (no specific service)
// is the only tool result. Scans for services with non-zero exit codes.
func parseMacServiceList(serviceName, output string) (string, bool) {
	if strings.TrimSpace(output) == "" {
		return "", false
	}

	entries := parseLaunchctlList(output)
	if len(entries) == 0 {
		return "", false
	}

	var failed []launchctlEntry
	for _, e := range entries {
		if e.ExitCode != 0 {
			failed = append(failed, e)
		}
	}

	if len(failed) == 0 {
		d := DiagnosticResult{
			Summary:   "All listed services running normally",
			RiskLevel: "Low",
		}
		d.Findings = append(d.Findings, fmt.Sprintf("%d services checked, all with exit code 0", len(entries)))
		return d.Render(), true
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("%d service(s) with non-zero exit codes", len(failed)),
		RiskLevel: "High",
	}
	for _, e := range firstN(failed, 8) {
		d.Findings = append(d.Findings, fmt.Sprintf("%s — exit code %d (PID: %s)", e.Label, e.ExitCode, e.PID))
	}
	d.Remediation = append(d.Remediation, "Investigate failed services with `launchctl list | grep -i <service>` and check logs with `log show`")
	return d.Render(), true
}

// detectRootCause scans error lines for common failure keywords and returns
// a human-readable root cause string.
func detectRootCause(errorLines []string) string {
	rootCause := "service failure"
	for _, line := range errorLines {
		lower := strings.ToLower(line)
		switch {
		case strings.Contains(lower, "address already in use"):
			rootCause = "port conflict"
		case strings.Contains(lower, "permission denied") || strings.Contains(lower, "denied"):
			rootCause = "permission error"
		case strings.Contains(lower, "not found"):
			rootCause = "missing dependency or file"
		case strings.Contains(lower, "invalid"):
			rootCause = "invalid configuration"
		}
		if rootCause != "service failure" {
			break
		}
	}
	return rootCause
}

func parseDNSRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolCalls) < 2 || len(toolResults) < 2 {
		return "", false
	}
	dnsOutput := toolResults[0].Content
	routeOutput := toolResults[1].Content
	if strings.TrimSpace(dnsOutput) == "" && strings.TrimSpace(routeOutput) == "" {
		return "", false
	}

	nameservers := grepLines(dnsOutput, `(?i)(^nameserver\s+|nameserver\[[0-9]+\])`)
	searchDomains := grepLines(dnsOutput, `(?i)^(search|domain)\s+`)
	defaultRoutes := grepLines(routeOutput, `(?i)^(default|0\.0\.0\.0)`)
	if len(nameservers) == 0 && len(defaultRoutes) == 0 {
		return "", false
	}

	risk := "Low"
	summary := "DNS and routing look present"
	if len(nameservers) == 0 {
		risk = "High"
		summary = "No DNS nameservers found in resolver configuration"
	} else if len(defaultRoutes) == 0 {
		risk = "High"
		summary = "DNS is configured but no default route is visible"
	} else if len(searchDomains) == 0 {
		risk = "Medium"
		summary = "DNS nameservers are configured; verify resolution path and search domain needs"
	}

	d := DiagnosticResult{
		Summary:   summary,
		RiskLevel: risk,
	}
	if len(nameservers) > 0 {
		d.Findings = append(d.Findings, "Nameservers: "+strings.Join(firstN(nameservers, 3), "; "))
	}
	if len(searchDomains) > 0 {
		d.Findings = append(d.Findings, "Search domains: "+strings.Join(firstN(searchDomains, 2), "; "))
	}
	if len(defaultRoutes) > 0 {
		d.Findings = append(d.Findings, "Default route: "+strings.Join(firstN(defaultRoutes, 2), "; "))
	}
	if len(nameservers) == 0 {
		d.Remediation = append(d.Remediation, "Add or restore valid nameserver entries before retrying resolution")
	}
	if len(defaultRoutes) == 0 {
		d.Remediation = append(d.Remediation, "Restore a default route so DNS queries can leave the host")
	}
	if len(nameservers) > 0 && len(defaultRoutes) > 0 {
		d.Remediation = append(d.Remediation, "Test name resolution directly against a configured resolver to confirm query success")
	}
	return d.Render(), true
}

func parseNetworkRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolCalls) < 2 || len(toolResults) < 2 {
		return "", false
	}
	interfaceOutput := toolResults[0].Content
	routeOutput := toolResults[1].Content

	defaultRoutes := grepLines(routeOutput, `(?i)^(default|0\.0\.0\.0)`)
	ifaceLines := firstNonEmptyLines(interfaceOutput, 6)
	if len(defaultRoutes) == 0 && len(ifaceLines) == 0 {
		return "", false
	}

	risk := "Medium"
	summary := "Interfaces detected; verify route and interface state"
	if len(defaultRoutes) == 0 {
		risk = "High"
		summary = "Interface data is present but no default route is visible"
	}

	d := DiagnosticResult{
		Summary:   summary,
		RiskLevel: risk,
	}
	if len(ifaceLines) > 0 {
		d.Findings = append(d.Findings, "Interface summary: "+strings.Join(ifaceLines, "; "))
	}
	if len(defaultRoutes) > 0 {
		d.Findings = append(d.Findings, "Default route: "+strings.Join(firstN(defaultRoutes, 2), "; "))
	}
	if len(defaultRoutes) == 0 {
		d.Remediation = append(d.Remediation, "Restore a default route or reconnect the interface that should carry outbound traffic")
	} else {
		d.Remediation = append(d.Remediation, "Validate the active interface and gateway with a direct connectivity check")
	}
	return d.Render(), true
}

func parseDiskUsageRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	// Single result: delegate to single-command parser
	if len(toolResults) == 1 {
		return parseDiskUsage(toolResults[0].Content)
	}
	if len(toolResults) < 2 {
		return "", false
	}

	// Tool 0: df -h output, Tool 1: du output (top dirs)
	dfResult, dfOK := parseDiskUsage(toolResults[0].Content)
	if !dfOK {
		return "", false
	}

	duLines := firstNonEmptyLines(toolResults[1].Content, 10)
	if len(duLines) == 0 {
		return dfResult, true
	}

	// Merge: append du findings into the df diagnostic
	// Parse the df result back minimally — just append du as extra findings
	var b strings.Builder
	b.WriteString(dfResult)
	b.WriteString("\n**Top space consumers**:\n")
	for _, line := range duLines {
		b.WriteString("- " + line + "\n")
	}
	return b.String(), true
}

func parseMemoryPressureRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	// Single result: delegate to appropriate single-command parser
	if len(toolResults) == 1 {
		cmd := extractCommand(toolCalls[0].Input)
		if strings.Contains(cmd, "vm_stat") {
			return parseVmStat(toolResults[0].Content)
		}
		return parseMemory(toolResults[0].Content)
	}
	if len(toolResults) < 2 {
		return "", false
	}

	// Tool 0: free -h / vm_stat, Tool 1: ps aux --sort=-%mem
	cmd0 := extractCommand(toolCalls[0].Input)
	var memResult string
	var memOK bool
	if strings.Contains(cmd0, "vm_stat") {
		memResult, memOK = parseVmStat(toolResults[0].Content)
	} else {
		memResult, memOK = parseMemory(toolResults[0].Content)
	}
	if !memOK {
		return "", false
	}

	psLines := firstNonEmptyLines(toolResults[1].Content, 6)
	if len(psLines) <= 1 {
		return memResult, true
	}

	var b strings.Builder
	b.WriteString(memResult)
	b.WriteString("\n**Top memory consumers**:\n")
	for _, line := range psLines[1:] { // skip ps header
		b.WriteString("- " + line + "\n")
	}
	return b.String(), true
}

func parsePerformanceCPURecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	// Single result: delegate to uptime parser
	if len(toolResults) == 1 {
		return parseUptime(toolResults[0].Content)
	}
	if len(toolResults) < 2 {
		return "", false
	}

	// Tool 0: uptime, Tool 1: ps aux --sort=-%cpu
	uptimeResult, uptimeOK := parseUptime(toolResults[0].Content)
	if !uptimeOK {
		return "", false
	}

	psLines := firstNonEmptyLines(toolResults[1].Content, 6)
	if len(psLines) <= 1 {
		return uptimeResult, true
	}

	var b strings.Builder
	b.WriteString(uptimeResult)
	b.WriteString("\n**Top CPU consumers**:\n")
	for _, line := range psLines[1:] { // skip ps header
		b.WriteString("- " + line + "\n")
	}
	return b.String(), true
}

// launchctlLine matches launchctl list columnar output: PID(or "-")  ExitCode(may be negative)  Label
var launchctlLine = regexp.MustCompile(`^\s*(-|\d+)\s+(-?\d+)\s+(\S.+)$`)

type launchctlEntry struct {
	PID      string
	ExitCode int
	Label    string
}

// parseLaunchctlList parses launchctl list columnar output and returns entries
// with non-zero exit codes (i.e. failed services).
func parseLaunchctlList(output string) []launchctlEntry {
	var entries []launchctlEntry
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip header line
		if strings.HasPrefix(trimmed, "PID") {
			continue
		}
		m := launchctlLine.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		exitCode, _ := strconv.Atoi(m[2])
		entries = append(entries, launchctlEntry{
			PID:      m[1],
			ExitCode: exitCode,
			Label:    strings.TrimSpace(m[3]),
		})
	}
	return entries
}

var sizePattern = regexp.MustCompile(`^\d+(\.\d+)?[BKMGTP]?i?$`)

func looksLikeSize(s string) bool {
	return sizePattern.MatchString(s) || s == "0"
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
		size := fields[pctIdx-3]
		if !looksLikeSize(size) {
			continue
		}
		entries = append(entries, entry{
			Mount: fields[len(fields)-1],
			Size:  size,
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

	// macOS top -l1 header regexes
	macTopProcesses = regexp.MustCompile(`Processes:\s*(\d+)\s+total.*?(\d+)\s+running`)
	macTopCPU       = regexp.MustCompile(`CPU usage:\s*([\d.]+)%\s*user.*?([\d.]+)%\s*sys.*?([\d.]+)%\s*idle`)
	macTopMem       = regexp.MustCompile(`PhysMem:\s*(\S+)\s+used.*?(\S+)\s+unused`)
	macTopLoad      = regexp.MustCompile(`Load Avg:\s*([\d.]+),\s*([\d.]+),\s*([\d.]+)`)
)

// parseTop handles top output (Linux top -bn1 or macOS top -l1).
func parseTop(output string) (string, bool) {
	if strings.Contains(output, "CPU usage:") {
		return parseMacTop(output)
	}

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

// parseMacTop handles macOS top -l1 output.
func parseMacTop(output string) (string, bool) {
	lines := strings.Split(output, "\n")
	if len(lines) < 4 {
		return "", false
	}

	var cpuUser, cpuSys, cpuIdle float64
	var memUsed, memUnused string
	var tasksTotal, tasksRunning string
	var load1, load5, load15 string

	for _, line := range lines {
		if m := macTopCPU.FindStringSubmatch(line); len(m) == 4 {
			cpuUser, _ = strconv.ParseFloat(m[1], 64)
			cpuSys, _ = strconv.ParseFloat(m[2], 64)
			cpuIdle, _ = strconv.ParseFloat(m[3], 64)
		}
		if m := macTopMem.FindStringSubmatch(line); len(m) == 3 {
			memUsed, memUnused = m[1], m[2]
		}
		if m := macTopProcesses.FindStringSubmatch(line); len(m) == 3 {
			tasksTotal, tasksRunning = m[1], m[2]
		}
		if m := macTopLoad.FindStringSubmatch(line); len(m) == 4 {
			load1, load5, load15 = m[1], m[2], m[3]
		}
	}

	cpuUsed := cpuUser + cpuSys
	if cpuIdle == 0 && cpuUsed == 0 {
		return "", false
	}

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
	d.Findings = append(d.Findings, fmt.Sprintf("CPU: %.1f%% user, %.1f%% sys, %.1f%% idle", cpuUser, cpuSys, cpuIdle))
	if load1 != "" {
		d.Findings = append(d.Findings, fmt.Sprintf("Load Avg: %s (1m), %s (5m), %s (15m)", load1, load5, load15))
	}
	if memUsed != "" {
		d.Findings = append(d.Findings, fmt.Sprintf("Memory: %s used, %s unused", memUsed, memUnused))
	}

	// Collect top processes (lines after the header that have PID data)
	inProcs := false
	var topProcs []string
	for _, line := range lines {
		if strings.Contains(line, "PID") && (strings.Contains(line, "COMMAND") || strings.Contains(line, "CMD")) {
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
	for _, proc := range topProcs {
		fields := strings.Fields(proc)
		if len(fields) >= 3 {
			d.Findings = append(d.Findings, fmt.Sprintf("Process: %s", strings.Join(fields, " ")))
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

	// Sort by CPU descending so top consumers appear first
	sort.Slice(procs, func(i, j int) bool {
		ci, _ := strconv.ParseFloat(procs[i].CPU, 64)
		cj, _ := strconv.ParseFloat(procs[j].CPU, 64)
		return ci > cj
	})

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

	// Show top 10 from sorted list
	limit := 10
	if limit > len(procs) {
		limit = len(procs)
	}
	for _, p := range procs[:limit] {
		cmdShort := p.Command
		if len(cmdShort) > 60 {
			cmdShort = cmdShort[:60] + "..."
		}
		d.Findings = append(d.Findings,
			fmt.Sprintf("PID %s (%s): CPU %s%%, MEM %s%% — %s",
				p.PID, p.User, p.CPU, p.MEM, cmdShort))
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

	os := fields[0] // "Linux" or "Darwin"
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
var vmStatPageSize = regexp.MustCompile(`page size of (\d+) bytes`)

// parseVmStat handles macOS vm_stat output.
func parseVmStat(output string) (string, bool) {
	lines := strings.Split(output, "\n")
	if len(lines) < 3 {
		return "", false
	}

	// Extract page size from first line
	pageSize := 4096 // default
	if m := vmStatPageSize.FindStringSubmatch(lines[0]); len(m) == 2 {
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

// parseDockerCrashRecipe handles docker inspect + docker logs output.
func parseDockerCrashRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolResults) == 0 {
		return "", false
	}

	containerName := recipe.ServiceName
	if containerName == "" {
		containerName = "container"
	}

	firstOutput := strings.TrimSpace(toolResults[0].Content)
	if firstOutput == "" {
		return "", false
	}

	// Case A: docker ps -a listing (no specific container)
	cmd := extractCommand(toolCalls[0].Input)
	if strings.Contains(cmd, "docker ps") {
		return parseDockerListing(firstOutput)
	}

	// Case B: docker inspect output
	oom := strings.Contains(firstOutput, "oom:true") || strings.Contains(firstOutput, "OOMKilled:true")
	exitCodeRe := regexp.MustCompile(`exit:(\d+)`)
	exitCodeStr := "0"
	exitCode := 0
	if m := exitCodeRe.FindStringSubmatch(firstOutput); len(m) == 2 {
		exitCodeStr = m[1]
		exitCode, _ = strconv.Atoi(m[1])
	}
	status := ""
	if fields := strings.Fields(firstOutput); len(fields) > 0 {
		status = fields[0]
	}

	// Collect log errors if we have a second result
	var logErrors []string
	if len(toolResults) >= 2 {
		logOutput := strings.TrimSpace(toolResults[1].Content)
		logErrors = grepLines(logOutput, `(?i)(error|fatal|panic|killed|oom|denied|permission|refused|not found|exception)`)
	}

	// Container is running
	if status == "running" && exitCode == 0 && !oom {
		d := DiagnosticResult{
			Summary:   fmt.Sprintf("%s is running normally", containerName),
			RiskLevel: "Low",
		}
		d.Findings = append(d.Findings, fmt.Sprintf("Status: %s, exit code: %s", status, exitCodeStr))
		if len(logErrors) > 0 {
			for _, line := range firstN(logErrors, 4) {
				d.Findings = append(d.Findings, line)
			}
			d.Remediation = append(d.Remediation, "Container is running but logs contain errors — monitor for recurrence")
		}
		return d.Render(), true
	}

	// Determine root cause
	rootCause := "container crash"
	risk := "High"
	if oom {
		rootCause = "OOM killed — container exceeded memory limit"
		risk = "Critical"
	} else if exitCode == 137 {
		rootCause = "killed by SIGKILL (exit 137) — likely OOM or manual kill"
		risk = "Critical"
	} else if exitCode == 1 {
		rootCause = "application error (exit 1)"
		if len(logErrors) > 0 {
			for _, line := range logErrors {
				lower := strings.ToLower(line)
				if strings.Contains(lower, "not found") {
					rootCause = "missing dependency or file (exit 1)"
					break
				}
				if strings.Contains(lower, "permission") || strings.Contains(lower, "denied") {
					rootCause = "permission error (exit 1)"
					risk = "Critical"
					break
				}
			}
		}
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("%s crashed: %s", containerName, rootCause),
		RiskLevel: risk,
	}
	d.Findings = append(d.Findings, fmt.Sprintf("Status: %s, exit code: %s, OOMKilled: %v", status, exitCodeStr, oom))
	for _, line := range firstN(logErrors, 6) {
		d.Findings = append(d.Findings, line)
	}

	if oom || exitCode == 137 {
		d.Remediation = append(d.Remediation, "Increase container memory limit or optimize application memory usage")
	} else {
		d.Remediation = append(d.Remediation, "Review container logs and fix the application error before restarting")
	}
	d.Remediation = append(d.Remediation, fmt.Sprintf("Restart with: `docker restart %s`", containerName))
	return d.Render(), true
}

// parseDockerListing handles docker ps -a listing output when no specific container is targeted.
func parseDockerListing(output string) (string, bool) {
	lines := firstNonEmptyLines(output, 30)
	if len(lines) <= 1 {
		return "", false
	}

	exited := grepLines(output, `(?i)exited`)
	restarting := grepLines(output, `(?i)restarting`)

	risk := "Low"
	summary := fmt.Sprintf("%d containers listed", len(lines)-1)
	if len(exited) > 0 || len(restarting) > 0 {
		risk = "Medium"
		summary = fmt.Sprintf("%d containers listed, %d exited, %d restarting",
			len(lines)-1, len(exited), len(restarting))
	}
	if len(restarting) > 0 {
		risk = "High"
	}

	d := DiagnosticResult{
		Summary:   summary,
		RiskLevel: risk,
	}
	for _, line := range firstN(lines, 10) {
		d.Findings = append(d.Findings, line)
	}
	if len(exited)+len(restarting) > 0 {
		d.Remediation = append(d.Remediation, "Inspect crashed containers: `docker inspect <name>` and `docker logs <name>`")
	}
	return d.Render(), true
}

// parseBuildFailureRecipe handles build tool output (npm, go, cargo, etc.).
func parseBuildFailureRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolResults) == 0 {
		return "", false
	}

	output := strings.TrimSpace(toolResults[0].Content)
	if output == "" {
		return "", false
	}

	buildTool := recipe.ServiceName
	if buildTool == "" {
		buildTool = "build"
	}

	// Check if this was the auto-detect command (ls for manifest files)
	cmd := extractCommand(toolCalls[0].Input)
	if strings.HasPrefix(cmd, "ls ") {
		return parseBuildAutoDetect(output)
	}

	// Look for error lines
	errorLines := grepLines(output, `(?i)(error|ERR!|ERESOLVE|cannot find|undefined:|imported and not used|syntax error|mismatched types|error\[E|fatal:|undefined reference|failed)`)

	if len(errorLines) == 0 {
		d := DiagnosticResult{
			Summary:   fmt.Sprintf("%s build completed successfully", buildTool),
			RiskLevel: "Low",
		}
		d.Findings = append(d.Findings, "No error patterns found in build output")
		snippet := firstNonEmptyLines(output, 4)
		for _, line := range snippet {
			d.Findings = append(d.Findings, line)
		}
		return d.Render(), true
	}

	risk := "Medium"
	if len(errorLines) >= 5 {
		risk = "High"
	}
	if len(errorLines) >= 10 {
		risk = "Critical"
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("%s build failed with %d error(s)", buildTool, len(errorLines)),
		RiskLevel: risk,
	}
	for _, line := range firstN(errorLines, 8) {
		d.Findings = append(d.Findings, line)
	}
	if len(errorLines) > 8 {
		d.Findings = append(d.Findings, fmt.Sprintf("... and %d more errors", len(errorLines)-8))
	}
	d.Remediation = append(d.Remediation, "Fix the errors listed above and re-run the build")
	return d.Render(), true
}

// parseBuildAutoDetect handles `ls package.json Cargo.toml ...` output for build tool detection.
func parseBuildAutoDetect(output string) (string, bool) {
	files := firstNonEmptyLines(output, 10)
	if len(files) == 0 {
		d := DiagnosticResult{
			Summary:   "No build manifest files found in current directory",
			RiskLevel: "Low",
		}
		d.Findings = append(d.Findings, "No package.json, Cargo.toml, go.mod, or Makefile detected")
		d.Remediation = append(d.Remediation, "Ensure you are in the correct project directory")
		return d.Render(), true
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("Detected %d build manifest(s)", len(files)),
		RiskLevel: "Low",
	}
	for _, f := range files {
		d.Findings = append(d.Findings, f)
	}
	d.Remediation = append(d.Remediation, "Run the build command for the detected project type")
	return d.Render(), true
}

func parsePermissionRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolResults) == 0 || strings.TrimSpace(toolResults[0].Content) == "" {
		return "", false
	}
	output := toolResults[0].Content
	path := recipe.ServiceName
	if path == "" {
		path = "target"
	}

	permLines := grepLines(output, `(?i)(^[-dlrwxsStT]{10}|permission|denied|uid=|gid=)`)
	if len(permLines) == 0 {
		return "", false
	}

	risk := "Medium"
	for _, line := range permLines {
		if strings.Contains(strings.ToLower(line), "denied") {
			risk = "High"
			break
		}
	}

	d := DiagnosticResult{
		Summary:   fmt.Sprintf("Permission check for %s", path),
		RiskLevel: risk,
	}
	for _, line := range firstN(permLines, 6) {
		d.Findings = append(d.Findings, line)
	}
	if risk == "High" {
		d.Remediation = append(d.Remediation, "Check file ownership and permissions with `ls -la`")
		d.Remediation = append(d.Remediation, "Fix with `chmod` or `chown` as needed")
	}
	return d.Render(), true
}

func parsePortConflictRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolResults) == 0 || strings.TrimSpace(toolResults[0].Content) == "" {
		return "", false
	}
	output := toolResults[0].Content
	port := recipe.ServiceName

	lines := firstNonEmptyLines(output, 20)
	if len(lines) == 0 {
		return "", false
	}

	// If a specific port was mentioned, filter for it
	var relevant []string
	if port != "" {
		for _, line := range lines {
			if strings.Contains(line, ":"+port) || strings.Contains(line, "LISTEN") {
				relevant = append(relevant, line)
			}
		}
	}
	if len(relevant) == 0 {
		relevant = lines
	}

	risk := "Medium"
	summary := fmt.Sprintf("Listening ports check")
	if port != "" {
		conflictFound := false
		for _, line := range relevant {
			if strings.Contains(line, ":"+port) {
				conflictFound = true
				break
			}
		}
		if conflictFound {
			risk = "High"
			summary = fmt.Sprintf("Port %s is already in use", port)
		} else {
			summary = fmt.Sprintf("Port %s is not currently in use", port)
			risk = "Low"
		}
	}

	d := DiagnosticResult{
		Summary:   summary,
		RiskLevel: risk,
	}
	for _, line := range firstN(relevant, 8) {
		d.Findings = append(d.Findings, line)
	}
	if risk == "High" {
		d.Remediation = append(d.Remediation, fmt.Sprintf("Identify and stop the process using port %s, or configure your service to use a different port", port))
	}
	return d.Render(), true
}

var certDateRe = regexp.MustCompile(`(?i)(not(?:Before|After)\s*=\s*.+|subject\s*=\s*.+)`)

func parseSSLRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolResults) == 0 || strings.TrimSpace(toolResults[0].Content) == "" {
		return "", false
	}
	output := toolResults[0].Content

	certLines := certDateRe.FindAllString(output, -1)
	if len(certLines) == 0 {
		// Check if connection failed entirely
		if strings.Contains(strings.ToLower(output), "connect") && strings.Contains(strings.ToLower(output), "error") {
			d := DiagnosticResult{
				Summary:   "SSL connection failed — no certificate retrieved",
				RiskLevel: "High",
			}
			d.Findings = append(d.Findings, "Could not establish SSL/TLS connection")
			d.Remediation = append(d.Remediation, "Verify the service is running and listening on the expected port with SSL/TLS enabled")
			return d.Render(), true
		}
		return "", false
	}

	risk := "Low"
	summary := "SSL certificate details retrieved"
	for _, line := range certLines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "notafter") {
			// We can't easily parse the date format here, so just flag it
			summary = "SSL certificate found — check expiry dates"
			risk = "Medium"
		}
	}

	d := DiagnosticResult{
		Summary:   summary,
		RiskLevel: risk,
	}
	for _, line := range firstN(certLines, 4) {
		d.Findings = append(d.Findings, strings.TrimSpace(line))
	}
	// Include date check from follow-up
	if len(toolResults) >= 2 {
		dateOutput := strings.TrimSpace(toolResults[1].Content)
		if dateOutput != "" {
			d.Findings = append(d.Findings, "Current UTC time: "+dateOutput)
		}
	}
	d.Remediation = append(d.Remediation, "Compare certificate notAfter date with current time to check for expiry")
	d.Remediation = append(d.Remediation, "Renew the certificate if expired or expiring soon")
	return d.Render(), true
}

func parseGitRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolResults) == 0 || strings.TrimSpace(toolResults[0].Content) == "" {
		return "", false
	}
	output := toolResults[0].Content

	risk := "Low"
	summary := "Git repository status"

	hasConflict := strings.Contains(output, "both modified") || strings.Contains(output, "Unmerged")
	hasDetached := strings.Contains(output, "HEAD detached")
	hasDirty := strings.Contains(output, "Changes not staged") || strings.Contains(output, "Changes to be committed")

	if hasConflict {
		risk = "High"
		summary = "Merge conflicts detected"
	} else if hasDetached {
		risk = "Medium"
		summary = "HEAD is in detached state"
	} else if hasDirty {
		risk = "Low"
		summary = "Working directory has uncommitted changes"
	}

	statusLines := firstNonEmptyLines(output, 8)
	d := DiagnosticResult{
		Summary:   summary,
		RiskLevel: risk,
	}
	for _, line := range statusLines {
		d.Findings = append(d.Findings, line)
	}

	// Include log from follow-up
	if len(toolResults) >= 2 {
		logLines := firstNonEmptyLines(toolResults[1].Content, 5)
		for _, line := range logLines {
			d.Findings = append(d.Findings, "log: "+line)
		}
	}

	if hasConflict {
		d.Remediation = append(d.Remediation, "Resolve conflicts in the listed files, then `git add` and `git commit`")
	} else if hasDetached {
		d.Remediation = append(d.Remediation, "Create a branch to save work: `git checkout -b <branch-name>`")
	} else if hasDirty {
		d.Remediation = append(d.Remediation, "Commit or stash changes before proceeding: `git stash` or `git commit -am 'message'`")
	}
	return d.Render(), true
}

func parseCronRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolResults) == 0 || strings.TrimSpace(toolResults[0].Content) == "" {
		return "", false
	}
	output := toolResults[0].Content

	noCrontab := strings.Contains(strings.ToLower(output), "no crontab")
	lines := firstNonEmptyLines(output, 10)
	if len(lines) == 0 {
		return "", false
	}

	risk := "Low"
	summary := "Cron configuration"
	if noCrontab {
		summary = "No crontab entries for current user"
	} else {
		// Count actual cron entries (lines not starting with #)
		entryCount := 0
		for _, line := range lines {
			if !strings.HasPrefix(strings.TrimSpace(line), "#") {
				entryCount++
			}
		}
		summary = fmt.Sprintf("%d cron entries found", entryCount)
	}

	d := DiagnosticResult{
		Summary:   summary,
		RiskLevel: risk,
	}
	for _, line := range firstN(lines, 8) {
		d.Findings = append(d.Findings, line)
	}
	if len(toolResults) >= 2 {
		logLines := grepLines(toolResults[1].Content, `(?i)(error|failed|no mta|cannot|denied)`)
		for _, line := range firstN(logLines, 4) {
			d.Findings = append(d.Findings, "log: "+line)
			risk = "Medium"
		}
	}
	d.RiskLevel = risk
	if noCrontab {
		d.Remediation = append(d.Remediation, "Create a crontab with `crontab -e`")
	} else {
		d.Remediation = append(d.Remediation, "Review cron timing and command paths for correctness")
	}
	return d.Render(), true
}

func parsePackageRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolResults) == 0 || strings.TrimSpace(toolResults[0].Content) == "" {
		return "", false
	}
	output := toolResults[0].Content
	lines := firstNonEmptyLines(output, 15)
	if len(lines) == 0 {
		return "", false
	}

	errorLines := grepLines(output, `(?i)(error|warning|broken|locked|dpkg was interrupted|E:)`)
	risk := "Low"
	summary := "Package manager status"
	if len(errorLines) > 0 {
		risk = "High"
		summary = "Package manager issues detected"
	}

	d := DiagnosticResult{
		Summary:   summary,
		RiskLevel: risk,
	}
	if len(errorLines) > 0 {
		for _, line := range firstN(errorLines, 6) {
			d.Findings = append(d.Findings, line)
		}
	} else {
		for _, line := range firstN(lines, 8) {
			d.Findings = append(d.Findings, line)
		}
	}
	if risk == "High" {
		d.Remediation = append(d.Remediation, "Fix broken packages: `sudo dpkg --configure -a` or `brew cleanup`")
		d.Remediation = append(d.Remediation, "Clear package locks if present")
	}
	return d.Render(), true
}

func parseProcessRecipe(recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult) (string, bool) {
	if len(toolResults) == 0 || strings.TrimSpace(toolResults[0].Content) == "" {
		return "", false
	}
	output := toolResults[0].Content
	lines := firstNonEmptyLines(output, 15)
	if len(lines) == 0 {
		return "", false
	}

	// Check for zombie processes
	zombieLines := grepLines(output, `(?i)(Z|defunct)`)
	hasZombies := len(zombieLines) > 0

	// Check for ulimit info
	hasUlimit := false
	for _, line := range lines {
		if _, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
			hasUlimit = true
			break
		}
	}

	risk := "Low"
	summary := "Process health check"
	if hasZombies {
		risk = "Medium"
		summary = fmt.Sprintf("%d zombie/defunct process(es) detected", len(zombieLines))
	} else if hasUlimit {
		summary = "File descriptor limits and top consumers"
	}

	d := DiagnosticResult{
		Summary:   summary,
		RiskLevel: risk,
	}
	for _, line := range firstN(lines, 10) {
		d.Findings = append(d.Findings, line)
	}
	if len(toolResults) >= 2 {
		fdLines := firstNonEmptyLines(toolResults[1].Content, 6)
		for _, line := range fdLines {
			d.Findings = append(d.Findings, "fd count: "+line)
		}
	}
	if hasZombies {
		d.Remediation = append(d.Remediation, "Identify the parent process of zombies and restart it, or send SIGCHLD")
	}
	if hasUlimit {
		d.Remediation = append(d.Remediation, "Increase file descriptor limit if needed: edit `/etc/security/limits.conf` or use `ulimit -n <value>`")
	}
	return d.Render(), true
}
