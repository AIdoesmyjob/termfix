package agent

import (
	"strings"
	"testing"

	"github.com/AIdoesmyjob/termfix/internal/diagnose"
	"github.com/AIdoesmyjob/termfix/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// Realistic macOS launchctl list output (full `launchctl list | head -50`)
// ============================================================

const realisticLaunchctlList = `PID	Status	Label
389	0	com.apple.Finder
-	0	com.apple.Safari
-	0	com.apple.Preview
12345	0	com.apple.WindowServer
-	78	com.example.mycrashingapp
-	0	com.apple.dock
67890	0	com.apple.loginwindow
-	1	com.apple.some.failed.agent
-	127	com.example.misconfigured
456	0	com.apple.mdworker
-	0	com.apple.AirPlayXPCHelper
321	0	com.apple.SystemUIServer`

// ============================================================
// Realistic macOS log show output
// ============================================================

const realisticLogShowOutput = `Timestamp                       Thread     Type        Activity             PID    TTL
2026-04-01 14:20:01.234567-0700 0x1a2b     Default     0x0                  123    0    myapp: starting up
2026-04-01 14:20:02.345678-0700 0x1a2b     Default     0x0                  123    0    myapp: loading configuration from /etc/myapp/config.yml
2026-04-01 14:20:03.456789-0700 0x1a2b     Error       0x0                  123    0    myapp: error: failed to bind to 0.0.0.0:8080 - address already in use
2026-04-01 14:20:03.567890-0700 0x1a2b     Error       0x0                  123    0    myapp: fatal: cannot start listener, address already in use
2026-04-01 14:20:03.678901-0700 0x1a2b     Default     0x0                  123    0    myapp: shutting down gracefully
2026-04-01 14:20:03.789012-0700 0x1a2b     Default     0x0                  123    0    myapp: cleanup complete, exiting with code 78`

const realisticLogShowPermissionDenied = `Timestamp                       Thread     Type        Activity             PID    TTL
2026-04-01 14:20:01.234567-0700 0x1a2b     Default     0x0                  456    0    myapp: starting up
2026-04-01 14:20:02.345678-0700 0x1a2b     Error       0x0                  456    0    myapp: error: permission denied opening /var/run/myapp.sock
2026-04-01 14:20:02.456789-0700 0x1a2b     Error       0x0                  456    0    myapp: fatal: cannot create socket, permission denied
2026-04-01 14:20:02.567890-0700 0x1a2b     Default     0x0                  456    0    myapp: exiting`

const realisticLogShowMissingDep = `Timestamp                       Thread     Type        Activity             PID    TTL
2026-04-01 14:20:01.234567-0700 0x1a2b     Default     0x0                  789    0    myapp: initializing
2026-04-01 14:20:02.345678-0700 0x1a2b     Error       0x0                  789    0    myapp: error: libssl.dylib not found
2026-04-01 14:20:02.456789-0700 0x1a2b     Error       0x0                  789    0    myapp: failed to load required library`

const realisticLogShowClean = `Timestamp                       Thread     Type        Activity             PID    TTL
2026-04-01 14:20:01.234567-0700 0x1a2b     Default     0x0                  321    0    myapp: starting up
2026-04-01 14:20:02.345678-0700 0x1a2b     Default     0x0                  321    0    myapp: listening on 0.0.0.0:8080
2026-04-01 14:20:03.456789-0700 0x1a2b     Default     0x0                  321    0    myapp: ready to accept connections`

const realisticLogShowInvalidConfig = `Timestamp                       Thread     Type        Activity             PID    TTL
2026-04-01 14:20:01.234567-0700 0x1a2b     Default     0x0                  111    0    myapp: loading config
2026-04-01 14:20:02.345678-0700 0x1a2b     Error       0x0                  111    0    myapp: error: invalid YAML in /etc/myapp/config.yml at line 42
2026-04-01 14:20:02.456789-0700 0x1a2b     Error       0x0                  111    0    myapp: failed to parse configuration`

// ============================================================
// Full recipe flow: macOS service failure with port conflict
// ============================================================

func TestMacOS_FullRecipeFlow_PortConflict(t *testing.T) {
	// Simulate what happens when user types "myapp won't start" on macOS
	recipe := &diagnose.Recipe{
		Name:        diagnose.RecipeServiceFailure,
		IssueClass:  diagnose.IssueService,
		ServiceName: "myapp",
	}

	// macOS initial command: launchctl list | grep -i 'myapp'
	// macOS follow-up: log show --predicate 'process == "myapp"' --last 5m --style compact
	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | grep -i 'myapp'"}`},
		{Name: "bash", Input: `{"command":"log show --predicate 'process == \"myapp\"' --last 5m --style compact 2>/dev/null | tail -40"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "-\t78\tcom.example.myapp"},
		{Content: realisticLogShowOutput},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok, "should parse macOS service failure with port conflict")

	// Verify correct diagnosis
	assert.Contains(t, result, "port conflict", "should detect port conflict root cause")
	assert.Contains(t, result, "Critical", "port conflict should be Critical risk")
	assert.Contains(t, result, "address already in use", "should include actual error text")
	assert.Contains(t, result, "exit code: 78", "should report the actual exit code")
	assert.Contains(t, result, "PID: -", "should show service is not running")
	assert.Contains(t, result, "launchctl list | grep -i myapp", "remediation should use launchctl, not systemctl")
	assert.NotContains(t, result, "systemctl", "should NOT mention systemctl on macOS")
}

// ============================================================
// Full recipe flow: macOS service failure with permission error
// ============================================================

func TestMacOS_FullRecipeFlow_PermissionDenied(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:        diagnose.RecipeServiceFailure,
		IssueClass:  diagnose.IssueService,
		ServiceName: "myapp",
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | grep -i 'myapp'"}`},
		{Name: "bash", Input: `{"command":"log show --predicate 'process == \"myapp\"' --last 5m --style compact 2>/dev/null | tail -40"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "-\t1\tcom.example.myapp"},
		{Content: realisticLogShowPermissionDenied},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)

	assert.Contains(t, result, "permission error")
	assert.Contains(t, result, "Critical")
	assert.Contains(t, result, "permission denied")
	assert.NotContains(t, result, "systemctl")
}

// ============================================================
// Full recipe flow: macOS service failure with missing dependency
// ============================================================

func TestMacOS_FullRecipeFlow_MissingDep(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:        diagnose.RecipeServiceFailure,
		IssueClass:  diagnose.IssueService,
		ServiceName: "myapp",
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | grep -i 'myapp'"}`},
		{Name: "bash", Input: `{"command":"log show --predicate 'process == \"myapp\"' --last 5m --style compact 2>/dev/null | tail -40"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "-\t1\tcom.example.myapp"},
		{Content: realisticLogShowMissingDep},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)

	assert.Contains(t, result, "missing dependency or file")
	assert.Contains(t, result, "not found")
	assert.Contains(t, result, "High")
}

// ============================================================
// Full recipe flow: macOS service failure with invalid config
// ============================================================

func TestMacOS_FullRecipeFlow_InvalidConfig(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:        diagnose.RecipeServiceFailure,
		IssueClass:  diagnose.IssueService,
		ServiceName: "myapp",
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | grep -i 'myapp'"}`},
		{Name: "bash", Input: `{"command":"log show --predicate 'process == \"myapp\"' --last 5m --style compact 2>/dev/null | tail -40"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "-\t1\tcom.example.myapp"},
		{Content: realisticLogShowInvalidConfig},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)

	assert.Contains(t, result, "invalid configuration")
	assert.Contains(t, result, "Validate and correct")
}

// ============================================================
// Service is running, no errors — should report Low risk
// ============================================================

func TestMacOS_FullRecipeFlow_ServiceHealthy(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:        diagnose.RecipeServiceFailure,
		IssueClass:  diagnose.IssueService,
		ServiceName: "myapp",
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | grep -i 'myapp'"}`},
		{Name: "bash", Input: `{"command":"log show --predicate 'process == \"myapp\"' --last 5m --style compact 2>/dev/null | tail -40"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "54321\t0\tcom.example.myapp"},
		{Content: realisticLogShowClean},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)

	assert.Contains(t, result, "is running")
	assert.Contains(t, result, "PID 54321")
	assert.Contains(t, result, "Low")
	assert.NotContains(t, result, "Remediation") // no remediation for healthy service
}

// ============================================================
// Service is running but logs show errors — Low risk with warning
// ============================================================

func TestMacOS_FullRecipeFlow_RunningWithErrors(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:        diagnose.RecipeServiceFailure,
		IssueClass:  diagnose.IssueService,
		ServiceName: "myapp",
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | grep -i 'myapp'"}`},
		{Name: "bash", Input: `{"command":"log show --predicate 'process == \"myapp\"' --last 5m --style compact 2>/dev/null | tail -40"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "54321\t0\tcom.example.myapp"},
		{Content: "2026-04-01 14:20:01 myapp: error: connection refused to database\n2026-04-01 14:20:02 myapp: retrying..."},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)

	assert.Contains(t, result, "is running")
	assert.Contains(t, result, "Low")
	assert.Contains(t, result, "connection refused")
	assert.Contains(t, result, "monitor for recurrence")
}

// ============================================================
// No service name: launchctl list with multiple failures
// ============================================================

func TestMacOS_FullRecipeFlow_ListMultipleFailures(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:       diagnose.RecipeServiceFailure,
		IssueClass: diagnose.IssueService,
		// No ServiceName — generic service check
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | head -50"}`},
	}
	toolResults := []message.ToolResult{
		{Content: realisticLaunchctlList},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)

	assert.Contains(t, result, "3 service(s) with non-zero exit codes")
	assert.Contains(t, result, "High")
	assert.Contains(t, result, "com.example.mycrashingapp")
	assert.Contains(t, result, "exit code 78")
	assert.Contains(t, result, "com.apple.some.failed.agent")
	assert.Contains(t, result, "exit code 1")
	assert.Contains(t, result, "com.example.misconfigured")
	assert.Contains(t, result, "exit code 127")
}

// ============================================================
// No service name: all services healthy
// ============================================================

func TestMacOS_FullRecipeFlow_AllServicesHealthy(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:       diagnose.RecipeServiceFailure,
		IssueClass: diagnose.IssueService,
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | head -50"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "PID\tStatus\tLabel\n389\t0\tcom.apple.Finder\n12345\t0\tcom.apple.WindowServer\n67890\t0\tcom.apple.loginwindow"},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)

	assert.Contains(t, result, "All listed services running normally")
	assert.Contains(t, result, "Low")
	assert.Contains(t, result, "3 services checked")
}

// ============================================================
// Edge case: launchctl grep returns empty (service not found)
// ============================================================

func TestMacOS_EdgeCase_ServiceNotFound(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:        diagnose.RecipeServiceFailure,
		IssueClass:  diagnose.IssueService,
		ServiceName: "nonexistent",
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | grep -i 'nonexistent'"}`},
		{Name: "bash", Input: `{"command":"log show --predicate 'process == \"nonexistent\"' --last 5m --style compact 2>/dev/null | tail -40"}`},
	}
	// grep returns empty when service not found
	toolResults := []message.ToolResult{
		{Content: ""},
		{Content: ""},
	}

	_, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	assert.False(t, ok, "should fall through to model when launchctl grep is empty")
}

// ============================================================
// Edge case: launchctl list with only header (no services)
// ============================================================

func TestMacOS_EdgeCase_EmptyServiceList(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:       diagnose.RecipeServiceFailure,
		IssueClass: diagnose.IssueService,
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | head -50"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "PID\tStatus\tLabel\n"},
	}

	_, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	assert.False(t, ok, "should fall through when launchctl list has only header")
}

// ============================================================
// Edge case: launchctl output with extra whitespace/formatting
// ============================================================

func TestMacOS_EdgeCase_WeirdFormatting(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:       diagnose.RecipeServiceFailure,
		IssueClass: diagnose.IssueService,
	}

	// Real launchctl can have varying whitespace
	output := "PID\tStatus\tLabel\n" +
		"  389\t0\tcom.apple.Finder\n" +
		"  -\t  127\t  com.apple.broken.thing  \n" +
		"\n" + // blank line
		"12345\t0\tcom.apple.running"

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | head -50"}`},
	}
	toolResults := []message.ToolResult{
		{Content: output},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)

	// Should still find the failed service
	assert.Contains(t, result, "non-zero exit codes")
	assert.Contains(t, result, "com.apple.broken.thing")
}

// ============================================================
// Edge case: launchctl list with spaces in label names
// ============================================================

func TestMacOS_EdgeCase_SpacesInLabel(t *testing.T) {
	// Some third-party services may have unusual label formats
	output := "PID\tStatus\tLabel\n" +
		"-\t1\tcom.example.My App Service\n" +
		"123\t0\tcom.apple.ok"

	entries := parseLaunchctlList(output)
	require.Len(t, entries, 2)
	assert.Equal(t, "com.example.My App Service", entries[0].Label)
	assert.Equal(t, 1, entries[0].ExitCode)
}

// ============================================================
// Platform dispatch: ensures systemctl commands go to Linux path
// ============================================================

func TestPlatformDispatch_SystemctlGoesToLinux(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:        diagnose.RecipeServiceFailure,
		IssueClass:  diagnose.IssueService,
		ServiceName: "nginx",
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"systemctl status nginx --no-pager --full -n 20"}`},
		{Name: "bash", Input: `{"command":"journalctl -u nginx -n 40 --no-pager"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "Active: active (running)\nLoaded: loaded\nMain PID: 5678"},
		{Content: "Apr 01 nginx: started successfully"},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)
	assert.Contains(t, result, "systemctl status")
	assert.NotContains(t, result, "launchctl")
}

// ============================================================
// Platform dispatch: ensures launchctl commands go to macOS path
// ============================================================

func TestPlatformDispatch_LaunchctlGoesToMacOS(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:        diagnose.RecipeServiceFailure,
		IssueClass:  diagnose.IssueService,
		ServiceName: "myapp",
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | grep -i 'myapp'"}`},
		{Name: "bash", Input: `{"command":"log show --predicate 'process == \"myapp\"' --last 5m --style compact 2>/dev/null | tail -40"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "-\t1\tcom.example.myapp"},
		{Content: "2026-04-01 14:20:01 myapp: error: something failed"},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)
	assert.Contains(t, result, "launchctl")
	assert.NotContains(t, result, "systemctl")
}

// ============================================================
// Evidence summarization: log show with no errors
// ============================================================

func TestSummarizeServiceProbe_LogShowNoErrors(t *testing.T) {
	cmd := `log show --predicate 'process == "myapp"' --last 5m --style compact`
	output := realisticLogShowClean

	result := summarizeServiceProbe(cmd, output)
	assert.Contains(t, result, "Recent service logs")
	assert.NotContains(t, result, "errors")
}

// ============================================================
// Evidence summarization: log show with errors
// ============================================================

func TestSummarizeServiceProbe_LogShowWithErrors(t *testing.T) {
	cmd := `log show --predicate 'process == "myapp"' --last 5m --style compact`
	output := realisticLogShowOutput

	result := summarizeServiceProbe(cmd, output)
	assert.Contains(t, result, "Recent service errors")
	assert.Contains(t, result, "address already in use")
}

// ============================================================
// Evidence summarization: log show with empty output
// ============================================================

func TestSummarizeServiceProbe_LogShowEmpty(t *testing.T) {
	cmd := `log show --predicate 'process == "myapp"' --last 5m --style compact`
	result := summarizeServiceProbe(cmd, "")
	assert.Empty(t, result)
}

// ============================================================
// Evidence summarization: launchctl list all healthy
// ============================================================

func TestSummarizeServiceProbe_LaunchctlAllHealthy(t *testing.T) {
	cmd := "launchctl list | head -50"
	output := "PID\tStatus\tLabel\n389\t0\tcom.apple.Finder\n12345\t0\tcom.apple.WindowServer"

	result := summarizeServiceProbe(cmd, output)
	// No failed services, so it should fall through to generic listing
	assert.Contains(t, result, "Service listing")
}

// ============================================================
// Full evidence bundle: macOS service failure recipe
// ============================================================

func TestBuildStructuredEvidence_MacOS_ServiceFailure(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:        diagnose.RecipeServiceFailure,
		IssueClass:  diagnose.IssueService,
		ServiceName: "myapp",
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | grep -i 'myapp'"}`},
		{Name: "bash", Input: `{"command":"log show --predicate 'process == \"myapp\"' --last 5m --style compact 2>/dev/null | tail -40"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "-\t78\tcom.example.myapp"},
		{Content: realisticLogShowOutput},
	}

	evidence := buildStructuredEvidence(recipe, toolCalls, toolResults)
	assert.Contains(t, evidence, "Recipe: service_failure")
	assert.Contains(t, evidence, "Service: myapp")
	assert.Contains(t, evidence, "Probe count: 2")
	assert.Contains(t, evidence, "launchctl list | grep -i")
	assert.Contains(t, evidence, "log show")
	// Should use summarizeServiceProbe handlers
	assert.Contains(t, evidence, "Failed services")
	assert.Contains(t, evidence, "Recent service errors")
}

// ============================================================
// Regression: Linux service failure still works unchanged
// ============================================================

func TestLinuxServiceFailure_Regression_AllRootCauses(t *testing.T) {
	tests := []struct {
		name      string
		logLine   string
		wantCause string
		wantRisk  string
	}{
		{
			name:      "port conflict",
			logLine:   "nginx: bind() to 0.0.0.0:80 failed (98: Address already in use)",
			wantCause: "port conflict",
			wantRisk:  "Critical",
		},
		{
			name:      "permission denied",
			logLine:   "nginx: open() \"/var/log/nginx/error.log\" failed (13: Permission denied)",
			wantCause: "permission error",
			wantRisk:  "Critical",
		},
		{
			name:      "not found",
			logLine:   "nginx: [emerg] open() \"/etc/nginx/nginx.conf\" failed (2: No such file or directory, not found)",
			wantCause: "missing dependency or file",
			wantRisk:  "High",
		},
		{
			name:      "invalid config",
			logLine:   "nginx: [emerg] invalid number of arguments in \"server_name\" directive",
			wantCause: "invalid configuration",
			wantRisk:  "High",
		},
		{
			name:      "generic failure",
			logLine:   "nginx: [emerg] unexpected end of file, connection refused",
			wantCause: "service failure",
			wantRisk:  "High",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recipe := &diagnose.Recipe{
				Name:        diagnose.RecipeServiceFailure,
				IssueClass:  diagnose.IssueService,
				ServiceName: "nginx",
			}

			toolCalls := []message.ToolCall{
				{Name: "bash", Input: `{"command":"systemctl status nginx --no-pager --full -n 20"}`},
				{Name: "bash", Input: `{"command":"journalctl -u nginx -n 40 --no-pager"}`},
			}
			toolResults := []message.ToolResult{
				{Content: "Active: failed (Result: exit-code)\nLoaded: loaded\nMain PID: 1234 (code=exited, status=1/FAILURE)"},
				{Content: tt.logLine},
			}

			result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
			require.True(t, ok)
			assert.Contains(t, result, tt.wantCause)
			assert.Contains(t, result, tt.wantRisk)
			assert.Contains(t, result, "systemctl status nginx")
		})
	}
}

// ============================================================
// Edge case: Linux path with only 1 tool result should reject
// ============================================================

func TestLinuxServiceFailure_SingleResult_Rejects(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:        diagnose.RecipeServiceFailure,
		IssueClass:  diagnose.IssueService,
		ServiceName: "nginx",
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"systemctl status nginx --no-pager --full -n 20"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "Active: failed\nLoaded: loaded\nMain PID: 1234"},
	}

	_, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	assert.False(t, ok, "Linux path should reject with only 1 tool result")
}

// ============================================================
// Edge case: tool result with IsError flag
// ============================================================

func TestMacOS_EdgeCase_ToolError(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:        diagnose.RecipeServiceFailure,
		IssueClass:  diagnose.IssueService,
		ServiceName: "myapp",
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | grep -i 'myapp'"}`},
		{Name: "bash", Input: `{"command":"log show --predicate 'process == \"myapp\"' --last 5m --style compact 2>/dev/null | tail -40"}`},
	}
	// grep found the service, but log show failed
	toolResults := []message.ToolResult{
		{Content: "-\t78\tcom.example.myapp"},
		{Content: ""},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok, "should still work with empty log output")
	// Should detect failure from launchctl grep but no specific root cause
	assert.Contains(t, result, "not running")
	assert.Contains(t, result, "exit code 78")
}

// ============================================================
// parseLaunchctlList: stress test with many entries
// ============================================================

func TestParseLaunchctlList_ManyEntries(t *testing.T) {
	var b strings.Builder
	b.WriteString("PID\tStatus\tLabel\n")
	for i := 0; i < 100; i++ {
		if i%10 == 0 {
			b.WriteString("-\t1\tcom.example.failed" + strings.Repeat("x", i) + "\n")
		} else {
			b.WriteString("123\t0\tcom.example.ok" + strings.Repeat("x", i) + "\n")
		}
	}

	entries := parseLaunchctlList(b.String())
	assert.Len(t, entries, 100)

	failedCount := 0
	for _, e := range entries {
		if e.ExitCode != 0 {
			failedCount++
		}
	}
	assert.Equal(t, 10, failedCount)
}

// ============================================================
// Verify macOS list-only caps findings at 8
// ============================================================

func TestMacOS_ListOnly_FindingsCapped(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:       diagnose.RecipeServiceFailure,
		IssueClass: diagnose.IssueService,
	}

	// Create output with 15 failed services
	var b strings.Builder
	b.WriteString("PID\tStatus\tLabel\n")
	for i := 0; i < 15; i++ {
		b.WriteString("-\t1\tcom.example.failed" + string(rune('a'+i)) + "\n")
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | head -50"}`},
	}
	toolResults := []message.ToolResult{
		{Content: b.String()},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)
	assert.Contains(t, result, "15 service(s) with non-zero exit codes")

	// Count the number of evidence bullet points (lines starting with "- com.")
	lines := strings.Split(result, "\n")
	findingCount := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "- com.example.failed") {
			findingCount++
		}
	}
	assert.LessOrEqual(t, findingCount, 8, "findings should be capped at 8")
}

// ============================================================
// Real macOS launchctl output with negative exit codes (-9 = SIGKILL)
// ============================================================

const realMacOSLaunchctlOutput = `PID	Status	Label
-	0	com.apple.SafariHistoryServiceAgent
69301	-9	com.apple.progressd
-	0	com.apple.enhancedloggingd
31749	-9	com.apple.cloudphotod
44166	-9	com.apple.MENotificationService
744	0	com.apple.Finder
28465	-9	com.apple.homed
-	0	com.apple.quicklook
-	1	ai.paragon-sync.weekly
-	0	us.zoom.updater
783	0	com.apple.mediaremoteagent`

func TestParseLaunchctlList_RealMacOS_NegativeExitCodes(t *testing.T) {
	entries := parseLaunchctlList(realMacOSLaunchctlOutput)
	require.Len(t, entries, 11)

	// Check that negative exit codes are parsed correctly
	var failed []launchctlEntry
	for _, e := range entries {
		if e.ExitCode != 0 {
			failed = append(failed, e)
		}
	}
	assert.Len(t, failed, 5, "should find 5 services with non-zero exit codes")

	// Verify specific entries
	progressd := entries[1]
	assert.Equal(t, "69301", progressd.PID)
	assert.Equal(t, -9, progressd.ExitCode)
	assert.Equal(t, "com.apple.progressd", progressd.Label)

	paragon := entries[8]
	assert.Equal(t, "-", paragon.PID)
	assert.Equal(t, 1, paragon.ExitCode)
	assert.Equal(t, "ai.paragon-sync.weekly", paragon.Label)
}

func TestMacOS_RealOutput_ListWithNegativeExitCodes(t *testing.T) {
	recipe := &diagnose.Recipe{
		Name:       diagnose.RecipeServiceFailure,
		IssueClass: diagnose.IssueService,
	}

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"launchctl list | head -50"}`},
	}
	toolResults := []message.ToolResult{
		{Content: realMacOSLaunchctlOutput},
	}

	result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
	require.True(t, ok)

	assert.Contains(t, result, "5 service(s) with non-zero exit codes")
	assert.Contains(t, result, "High")
	assert.Contains(t, result, "com.apple.progressd")
	assert.Contains(t, result, "exit code -9")
	assert.Contains(t, result, "ai.paragon-sync.weekly")
}

func TestSummarizeServiceProbe_LaunchctlWithNegativeExitCodes(t *testing.T) {
	cmd := "launchctl list | head -50"
	result := summarizeServiceProbe(cmd, realMacOSLaunchctlOutput)
	assert.Contains(t, result, "Failed services")
	assert.Contains(t, result, "-9") // should detect negative exit codes as failures
	assert.Contains(t, result, "com.apple.progressd")
}
