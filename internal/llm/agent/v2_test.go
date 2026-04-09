package agent

import (
	"context"
	"testing"

	"github.com/AIdoesmyjob/termfix/internal/diagnose"
	"github.com/AIdoesmyjob/termfix/internal/llm/tools"
	"github.com/AIdoesmyjob/termfix/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRecipeParsers(t *testing.T) {
	t.Run("permission recipe parser", func(t *testing.T) {
		recipe := &diagnose.Recipe{Name: diagnose.RecipePermission, ServiceName: "/var/log/app.log"}
		toolCalls := []message.ToolCall{{Name: "bash", Input: `{"command":"ls -la /var/log/app.log; id"}`}}
		toolResults := []message.ToolResult{{Content: "-rw-r----- 1 root adm 12345 Apr 08 /var/log/app.log\nuid=1000(user) gid=1000(user)"}}
		result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
		assert.True(t, ok)
		assert.Contains(t, result, "Permission check")
	})

	t.Run("port conflict parser - conflict found", func(t *testing.T) {
		recipe := &diagnose.Recipe{Name: diagnose.RecipePortConflict, ServiceName: "8080"}
		toolCalls := []message.ToolCall{{Name: "bash", Input: `{"command":"lsof -iTCP -sTCP:LISTEN -P -n"}`}}
		toolResults := []message.ToolResult{{Content: "node    1234 user   22u  IPv4 12345      0t0  TCP *:8080 (LISTEN)"}}
		result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
		assert.True(t, ok)
		assert.Contains(t, result, "Port 8080 is already in use")
		assert.Contains(t, result, "High")
	})

	t.Run("port conflict parser - no conflict", func(t *testing.T) {
		recipe := &diagnose.Recipe{Name: diagnose.RecipePortConflict, ServiceName: "9999"}
		toolCalls := []message.ToolCall{{Name: "bash", Input: `{"command":"lsof -iTCP -sTCP:LISTEN -P -n"}`}}
		toolResults := []message.ToolResult{{Content: "node    1234 user   22u  IPv4 12345      0t0  TCP *:8080 (LISTEN)"}}
		result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
		assert.True(t, ok)
		assert.Contains(t, result, "Port 9999 is not currently in use")
	})

	t.Run("git recipe parser - merge conflict", func(t *testing.T) {
		recipe := &diagnose.Recipe{Name: diagnose.RecipeGit}
		toolCalls := []message.ToolCall{
			{Name: "bash", Input: `{"command":"git status"}`},
			{Name: "bash", Input: `{"command":"git log --oneline -5"}`},
		}
		toolResults := []message.ToolResult{
			{Content: "On branch main\nYou have unmerged paths.\n  both modified: src/main.go"},
			{Content: "abc1234 Fix bug\ndef5678 Add feature"},
		}
		result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
		assert.True(t, ok)
		assert.Contains(t, result, "Merge conflicts detected")
		assert.Contains(t, result, "High")
	})

	t.Run("ssl recipe parser", func(t *testing.T) {
		recipe := &diagnose.Recipe{Name: diagnose.RecipeSSL}
		toolCalls := []message.ToolCall{
			{Name: "bash", Input: `{"command":"openssl s_client -connect localhost:443"}`},
			{Name: "bash", Input: `{"command":"date -u"}`},
		}
		toolResults := []message.ToolResult{
			{Content: "notBefore=Jan 1 00:00:00 2024 GMT\nnotAfter=Dec 31 23:59:59 2024 GMT\nsubject= /CN=localhost"},
			{Content: "Wed Apr  9 00:00:00 UTC 2026"},
		}
		result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
		assert.True(t, ok)
		assert.Contains(t, result, "SSL certificate")
		assert.Contains(t, result, "notAfter")
		assert.Contains(t, result, "Current UTC time")
	})

	t.Run("cron recipe parser", func(t *testing.T) {
		recipe := &diagnose.Recipe{Name: diagnose.RecipeCron}
		toolCalls := []message.ToolCall{{Name: "bash", Input: `{"command":"crontab -l 2>&1"}`}}
		toolResults := []message.ToolResult{{Content: "# m h  dom mon dow  command\n*/5 * * * * /usr/bin/backup.sh\n0 2 * * * /usr/bin/cleanup.sh"}}
		result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
		assert.True(t, ok)
		assert.Contains(t, result, "2 cron entries found")
	})

	t.Run("process recipe parser - zombies", func(t *testing.T) {
		recipe := &diagnose.Recipe{Name: diagnose.RecipeProcess}
		toolCalls := []message.ToolCall{{Name: "bash", Input: `{"command":"ps aux | grep defunct"}`}}
		toolResults := []message.ToolResult{{Content: "root 1234 0.0 0.0 0 0 ? Z 12:00 0:00 [myapp] <defunct>"}}
		result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
		assert.True(t, ok)
		assert.Contains(t, result, "zombie")
	})

	t.Run("package recipe parser - errors", func(t *testing.T) {
		recipe := &diagnose.Recipe{Name: diagnose.RecipePackage}
		toolCalls := []message.ToolCall{{Name: "bash", Input: `{"command":"apt list --upgradable"}`}}
		toolResults := []message.ToolResult{{Content: "E: dpkg was interrupted, run dpkg --configure -a\nE: broken packages"}}
		result, ok := tryStructuredRecipeDiagnostic(recipe, toolCalls, toolResults)
		assert.True(t, ok)
		assert.Contains(t, result, "Package manager issues")
		assert.Contains(t, result, "High")
	})
}

func TestMultiToolStructuredDiagnostic(t *testing.T) {
	// tryStructuredDiagnostic should work with multiple tool calls (uses last bash call)
	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"uptime"}`},
		{Name: "bash", Input: `{"command":"df -h"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "up 10 days, load average: 0.5, 0.3, 0.2"},
		{Content: "Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda1        50G   45G    5G  90% /"},
	}
	result, ok := tryStructuredDiagnostic(toolCalls, toolResults)
	assert.True(t, ok)
	assert.Contains(t, result, "90%")
}

func TestTruncateToolResult(t *testing.T) {
	short := "hello world"
	assert.Equal(t, short, truncateToolResult(short, 100))

	long := make([]byte, 2000)
	for i := range long {
		long[i] = 'x'
	}
	result := truncateToolResult(string(long), 500)
	assert.True(t, len(result) < 2000)
	assert.Contains(t, result, "truncated")
}

func TestBuildIterationContext(t *testing.T) {
	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"df -h"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda1        50G   45G    5G  90% /"},
	}

	ctx := buildIterationContext("disk is full", nil, toolCalls, toolResults, 2)
	assert.Contains(t, ctx, "User issue: disk is full")
	assert.Contains(t, ctx, "Evidence so far:")
	assert.Contains(t, ctx, "`df -h`")
	assert.Contains(t, ctx, "2 more probe(s)")
}

func TestSmartSummarize(t *testing.T) {
	t.Run("df highlights high usage", func(t *testing.T) {
		output := "Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda1        50G   45G    5G  90% /\n/dev/sdb1        100G   30G   70G  30% /data"
		s := smartSummarize(nil, "df -h", output)
		assert.Contains(t, s, "/ at 90%")
		assert.NotContains(t, s, "/data")
	})

	t.Run("df all low", func(t *testing.T) {
		output := "Filesystem      Size  Used Avail Use% Mounted on\n/dev/sda1        50G   20G   30G  40% /"
		s := smartSummarize(nil, "df -h", output)
		assert.Contains(t, s, "below 80%")
	})

	t.Run("git status clean", func(t *testing.T) {
		s := smartSummarize(nil, "git status", "On branch main\nnothing to commit, working tree clean")
		assert.Contains(t, s, "clean")
	})

	t.Run("git status dirty", func(t *testing.T) {
		s := smartSummarize(nil, "git status", "On branch main\nChanges not staged:\n  modified: foo.go\n  modified: bar.go")
		assert.Contains(t, s, "2 modified")
	})
}

func TestRemediationBanned(t *testing.T) {
	assert.True(t, isRemediationBanned("rm -rf /"))
	assert.True(t, isRemediationBanned("rm -rf /*"))
	assert.True(t, isRemediationBanned("mkfs.ext4 /dev/sda1"))
	assert.True(t, isRemediationBanned("dd if=/dev/zero of=/dev/sda"))
	assert.True(t, isRemediationBanned("shutdown -h now"))
	assert.True(t, isRemediationBanned("reboot"))
	assert.True(t, isRemediationBanned("chmod -R 777 /"))
	assert.True(t, isRemediationBanned("echo foo > /dev/sda"))
	assert.True(t, isRemediationBanned("cat /etc/shadow"))

	// Safe commands should not be banned
	assert.False(t, isRemediationBanned("systemctl restart nginx"))
	assert.False(t, isRemediationBanned("sudo apt install -f"))
	assert.False(t, isRemediationBanned("chmod 644 /var/log/app.log"))
	assert.False(t, isRemediationBanned("kill -9 1234"))
	assert.False(t, isRemediationBanned("rm /tmp/lockfile"))
}

func TestSlimToolWrapper(t *testing.T) {
	// Create a mock tool
	mockTool := &mockBaseTool{
		name: "bash",
		desc: "This is a very long description that takes up lots and lots of tokens in the prompt. It contains git commit instructions, PR creation guidance, and other irrelevant long-form content that wastes precious context tokens when used with small local models that have limited 8K context windows.",
	}

	wrapped := tools.WrapToolsForLocalModel([]tools.BaseTool{mockTool})
	require.Len(t, wrapped, 1)

	info := wrapped[0].Info()
	assert.Equal(t, "bash", info.Name)
	assert.NotEqual(t, mockTool.desc, info.Description)
	assert.True(t, len(info.Description) < len(mockTool.desc), "slim description should be shorter")
	assert.Contains(t, info.Description, "shell command")
}

// mockBaseTool implements tools.BaseTool for testing
type mockBaseTool struct {
	name string
	desc string
}

func (m *mockBaseTool) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name:        m.name,
		Description: m.desc,
		Parameters:  map[string]any{"command": map[string]any{"type": "string"}},
		Required:    []string{"command"},
	}
}

func (m *mockBaseTool) Run(_ context.Context, _ tools.ToolCall) (tools.ToolResponse, error) {
	return tools.NewTextResponse("ok"), nil
}
