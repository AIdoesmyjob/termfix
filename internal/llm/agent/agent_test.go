package agent

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/opencode-ai/opencode/internal/diagnose"
	"github.com/opencode-ai/opencode/internal/llm/tools"
	"github.com/opencode-ai/opencode/internal/message"
	"github.com/opencode-ai/opencode/internal/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentCancel(t *testing.T) {
	a := &agent{
		Broker:         pubsub.NewBroker[AgentEvent](),
		activeRequests: sync.Map{},
	}

	cancelCalled := false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = ctx

	a.activeRequests.Store("session-1", context.CancelFunc(func() {
		cancelCalled = true
	}))

	a.Cancel("session-1")

	assert.True(t, cancelCalled, "expected cancel func to be called")

	// Entry should be deleted
	_, loaded := a.activeRequests.Load("session-1")
	assert.False(t, loaded, "expected activeRequests entry to be deleted")
}

func TestAgentCancelWithSummarize(t *testing.T) {
	a := &agent{
		Broker:         pubsub.NewBroker[AgentEvent](),
		activeRequests: sync.Map{},
	}

	mainCancelled := false
	summarizeCancelled := false

	a.activeRequests.Store("session-1", context.CancelFunc(func() {
		mainCancelled = true
	}))
	a.activeRequests.Store("session-1-summarize", context.CancelFunc(func() {
		summarizeCancelled = true
	}))

	a.Cancel("session-1")

	assert.True(t, mainCancelled, "expected main cancel func to be called")
	assert.True(t, summarizeCancelled, "expected summarize cancel func to be called")

	_, loaded := a.activeRequests.Load("session-1")
	assert.False(t, loaded)
	_, loaded = a.activeRequests.Load("session-1-summarize")
	assert.False(t, loaded)
}

func TestAgentIsSessionBusy(t *testing.T) {
	a := &agent{
		Broker:         pubsub.NewBroker[AgentEvent](),
		activeRequests: sync.Map{},
	}

	// Not busy initially
	assert.False(t, a.IsSessionBusy("session-1"))

	// Store a cancel func to make it busy
	a.activeRequests.Store("session-1", context.CancelFunc(func() {}))
	assert.True(t, a.IsSessionBusy("session-1"))

	// Other sessions should not be busy
	assert.False(t, a.IsSessionBusy("session-2"))

	// Delete and verify no longer busy
	a.activeRequests.Delete("session-1")
	assert.False(t, a.IsSessionBusy("session-1"))
}

func TestAgentIsBusy(t *testing.T) {
	a := &agent{
		Broker:         pubsub.NewBroker[AgentEvent](),
		activeRequests: sync.Map{},
	}

	// Empty map — not busy
	assert.False(t, a.IsBusy())

	// Store an entry — busy
	a.activeRequests.Store("session-1", context.CancelFunc(func() {}))
	assert.True(t, a.IsBusy())

	// Store another
	a.activeRequests.Store("session-2", context.CancelFunc(func() {}))
	assert.True(t, a.IsBusy())

	// Remove one — still busy
	a.activeRequests.Delete("session-1")
	assert.True(t, a.IsBusy())

	// Remove all — not busy
	a.activeRequests.Delete("session-2")
	assert.False(t, a.IsBusy())
}

func TestAgentErrHelper(t *testing.T) {
	a := &agent{
		Broker:         pubsub.NewBroker[AgentEvent](),
		activeRequests: sync.Map{},
	}

	testErr := errors.New("something went wrong")
	event := a.err(testErr)

	require.Equal(t, AgentEventTypeError, event.Type)
	require.Error(t, event.Error)
	assert.Equal(t, "something went wrong", event.Error.Error())
}

func TestAgentCancelNonExistent(t *testing.T) {
	a := &agent{
		Broker:         pubsub.NewBroker[AgentEvent](),
		activeRequests: sync.Map{},
	}

	// Should not panic when cancelling a non-existent session
	a.Cancel("does-not-exist")

	_, loaded := a.activeRequests.Load("does-not-exist")
	assert.False(t, loaded)
}

func TestToolCallSummary(t *testing.T) {
	t.Run("single tool call", func(t *testing.T) {
		msg := message.Message{
			Parts: []message.ContentPart{
				message.ToolCall{Name: "bash", Input: `{"command":"df -h"}`, Finished: true},
			},
		}
		result := toolCallSummary(msg)
		assert.Equal(t, `bash({"command":"df -h"})`, result)
	})

	t.Run("multiple tool calls", func(t *testing.T) {
		msg := message.Message{
			Parts: []message.ContentPart{
				message.ToolCall{Name: "bash", Input: `{"command":"ls"}`, Finished: true},
				message.ToolCall{Name: "view", Input: `{"path":"/etc/hosts"}`, Finished: true},
			},
		}
		result := toolCallSummary(msg)
		assert.Contains(t, result, "bash")
		assert.Contains(t, result, "view")
		assert.Contains(t, result, ", ")
	})

	t.Run("no tool calls", func(t *testing.T) {
		msg := message.Message{
			Parts: []message.ContentPart{
				message.TextContent{Text: "just text"},
			},
		}
		result := toolCallSummary(msg)
		assert.Equal(t, "", result)
	})
}

func TestToolResultContent(t *testing.T) {
	t.Run("single result", func(t *testing.T) {
		msg := message.Message{
			Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "1", Content: "disk usage output"},
			},
		}
		result := toolResultContent(&msg)
		assert.Contains(t, result, "disk usage output")
	})

	t.Run("multiple results", func(t *testing.T) {
		msg := message.Message{
			Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "1", Content: "result 1"},
				message.ToolResult{ToolCallID: "2", Content: "result 2"},
			},
		}
		result := toolResultContent(&msg)
		assert.Contains(t, result, "result 1")
		assert.Contains(t, result, "result 2")
	})

	t.Run("truncation at 2000 chars", func(t *testing.T) {
		longContent := strings.Repeat("x", 2500)
		msg := message.Message{
			Parts: []message.ContentPart{
				message.ToolResult{ToolCallID: "1", Content: longContent},
			},
		}
		result := toolResultContent(&msg)
		assert.True(t, len(result) < 2500, "result should be truncated")
		assert.Contains(t, result, "... (truncated)")
	})

	t.Run("no results", func(t *testing.T) {
		msg := message.Message{
			Parts: []message.ContentPart{
				message.TextContent{Text: "just text"},
			},
		}
		result := toolResultContent(&msg)
		assert.Equal(t, "", result)
	})
}

func TestRouteDiagnosticIntent(t *testing.T) {
	t.Run("disk issues route to bash", func(t *testing.T) {
		call := routeDiagnosticIntent(diagnose.SelectRecipe("disk space is running low"))
		require.NotNil(t, call)
		assert.Equal(t, tools.BashToolName, call.Name)

		var input map[string]string
		require.NoError(t, json.Unmarshal([]byte(call.Input), &input))
		assert.Equal(t, "df -h", input["command"])
	})

	t.Run("knowledge queries do not route", func(t *testing.T) {
		call := routeDiagnosticIntent(diagnose.SelectRecipe("what is DNS"))
		assert.Nil(t, call)
	})

	t.Run("service issues prefer service status probe", func(t *testing.T) {
		call := routeDiagnosticIntent(diagnose.SelectRecipe("nginx won't start after reboot"))
		require.NotNil(t, call)

		var input map[string]string
		require.NoError(t, json.Unmarshal([]byte(call.Input), &input))
		if runtime.GOOS == "darwin" {
			assert.Contains(t, input["command"], "launchctl")
			assert.Contains(t, input["command"], "nginx")
		} else {
			assert.Contains(t, input["command"], "systemctl status")
			assert.Contains(t, input["command"], "nginx")
		}
	})
}

func TestRouteDiagnosticIntentMatchesClassifier(t *testing.T) {
	issue := diagnose.ClassifyIssue("dns resolution is broken")
	require.Equal(t, diagnose.IssueDNS, issue)

	call := routeDiagnosticIntent(diagnose.SelectRecipe("dns resolution is broken"))
	require.NotNil(t, call)

	var input map[string]string
	require.NoError(t, json.Unmarshal([]byte(call.Input), &input))
	if runtime.GOOS == "darwin" {
		assert.Equal(t, "scutil --dns", input["command"])
	} else {
		assert.Equal(t, "cat /etc/resolv.conf", input["command"])
	}
}

func TestBuildStructuredEvidence(t *testing.T) {
	recipe := diagnose.SelectRecipe("nginx won't start")
	require.NotNil(t, recipe)

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"systemctl status nginx --no-pager --full -n 20"}`},
		{Name: "bash", Input: `{"command":"journalctl -u nginx -n 40 --no-pager"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "Loaded: loaded (/usr/lib/systemd/system/nginx.service)\nActive: failed (Result: exit-code)\nMain PID: 1234 (code=exited, status=1/FAILURE)"},
		{Content: "Apr 01 10:00:00 host nginx[1234]: bind() to 0.0.0.0:80 failed (98: Address already in use)\nApr 01 10:00:00 host systemd[1]: nginx.service: Failed with result 'exit-code'."},
	}

	evidence := buildStructuredEvidence(recipe, toolCalls, toolResults)
	assert.Contains(t, evidence, "Evidence Bundle")
	assert.Contains(t, evidence, "Recipe: service_failure")
	assert.Contains(t, evidence, "Service: nginx")
	assert.Contains(t, evidence, "Service status")
	assert.Contains(t, evidence, "Recent service errors")
	assert.Contains(t, evidence, "Address already in use")
}

func TestBuildPass2ContentUsesEvidenceBundle(t *testing.T) {
	recipe := diagnose.SelectRecipe("dns resolution is broken")
	require.NotNil(t, recipe)

	toolCalls := []message.ToolCall{
		{Name: "bash", Input: `{"command":"cat /etc/resolv.conf"}`},
		{Name: "bash", Input: `{"command":"ip route"}`},
	}
	toolResults := []message.ToolResult{
		{Content: "nameserver 1.1.1.1\nnameserver 8.8.8.8\nsearch corp.local"},
		{Content: "default via 192.168.1.1 dev eth0"},
	}

	content := buildPass2Content("dns is broken", recipe, toolCalls, toolResults)
	assert.Contains(t, content, "Use this compact evidence bundle")
	assert.Contains(t, content, "Recipe: dns_resolution")
	assert.Contains(t, content, "Nameservers")
	assert.Contains(t, content, "Default route")
	assert.NotContains(t, content, "I ran `")
}
