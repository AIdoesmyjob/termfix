package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

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
