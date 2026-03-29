package message

import (
	"encoding/base64"
	"testing"

	"github.com/opencode-ai/opencode/internal/llm/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextContent(t *testing.T) {
	tc := TextContent{Text: "hello world"}
	assert.Equal(t, "hello world", tc.String())

	// Verify it satisfies ContentPart (isPart is unexported but callable within package)
	var cp ContentPart = tc
	cp.isPart()
	assert.NotNil(t, cp)
}

func TestReasoningContent(t *testing.T) {
	rc := ReasoningContent{Thinking: "let me think about this"}
	assert.Equal(t, "let me think about this", rc.String())

	var cp ContentPart = rc
	cp.isPart()
	assert.NotNil(t, cp)
}

func TestBinaryContent(t *testing.T) {
	data := []byte("fake png data")
	bc := BinaryContent{
		Path:     "/tmp/test.png",
		MIMEType: "image/png",
		Data:     data,
	}

	result := bc.String(models.ProviderOpenAI)
	encoded := base64.StdEncoding.EncodeToString(data)
	expected := "data:image/png;base64," + encoded
	assert.Equal(t, expected, result)

	var cp ContentPart = bc
	cp.isPart()
	assert.NotNil(t, cp)
}

func TestToolCallIsPart(t *testing.T) {
	tc := ToolCall{
		ID:       "call_123",
		Name:     "read_file",
		Input:    `{"path": "/tmp/test.txt"}`,
		Type:     "function",
		Finished: false,
	}

	assert.Equal(t, "call_123", tc.ID)
	assert.Equal(t, "read_file", tc.Name)
	assert.Equal(t, `{"path": "/tmp/test.txt"}`, tc.Input)
	assert.Equal(t, "function", tc.Type)
	assert.False(t, tc.Finished)

	var cp ContentPart = tc
	cp.isPart()
	assert.NotNil(t, cp)
}

func TestToolResultIsPart(t *testing.T) {
	tr := ToolResult{
		ToolCallID: "call_123",
		Name:       "read_file",
		Content:    "file contents here",
		Metadata:   "some metadata",
		IsError:    false,
	}

	assert.Equal(t, "call_123", tr.ToolCallID)
	assert.Equal(t, "read_file", tr.Name)
	assert.Equal(t, "file contents here", tr.Content)
	assert.Equal(t, "some metadata", tr.Metadata)
	assert.False(t, tr.IsError)

	var cp ContentPart = tr
	cp.isPart()
	assert.NotNil(t, cp)
}

func TestMessageContent(t *testing.T) {
	msg := Message{
		Parts: []ContentPart{
			TextContent{Text: "hello"},
		},
	}
	content := msg.Content()
	assert.Equal(t, "hello", content.Text)
}

func TestMessageContentEmpty(t *testing.T) {
	msg := Message{}
	content := msg.Content()
	assert.Equal(t, "", content.Text)
}

func TestMessageToolCalls(t *testing.T) {
	tc1 := ToolCall{ID: "call_1", Name: "tool_a"}
	tc2 := ToolCall{ID: "call_2", Name: "tool_b"}
	msg := Message{
		Parts: []ContentPart{
			TextContent{Text: "some text"},
			tc1,
			tc2,
		},
	}

	calls := msg.ToolCalls()
	require.Len(t, calls, 2)
	assert.Equal(t, "call_1", calls[0].ID)
	assert.Equal(t, "call_2", calls[1].ID)
}

func TestMessageToolResults(t *testing.T) {
	tr1 := ToolResult{ToolCallID: "call_1", Content: "result 1"}
	tr2 := ToolResult{ToolCallID: "call_2", Content: "result 2"}
	msg := Message{
		Parts: []ContentPart{
			tr1,
			TextContent{Text: "text"},
			tr2,
		},
	}

	results := msg.ToolResults()
	require.Len(t, results, 2)
	assert.Equal(t, "call_1", results[0].ToolCallID)
	assert.Equal(t, "call_2", results[1].ToolCallID)
}

func TestMessageIsFinished(t *testing.T) {
	// Without Finish part
	msg := Message{
		Parts: []ContentPart{
			TextContent{Text: "hello"},
		},
	}
	assert.False(t, msg.IsFinished())

	// With Finish part
	msg.Parts = append(msg.Parts, Finish{Reason: FinishReasonEndTurn, Time: 1000})
	assert.True(t, msg.IsFinished())
}

func TestMessageAppendContent(t *testing.T) {
	msg := Message{}
	msg.AppendContent("hello")
	msg.AppendContent(" world")

	content := msg.Content()
	assert.Equal(t, "hello world", content.Text)
}

func TestMessageAppendReasoningContent(t *testing.T) {
	msg := Message{}
	msg.AppendReasoningContent("step 1")
	msg.AppendReasoningContent(" step 2")

	// Find the ReasoningContent part
	var found bool
	for _, p := range msg.Parts {
		if rc, ok := p.(ReasoningContent); ok {
			assert.Equal(t, "step 1 step 2", rc.Thinking)
			found = true
			break
		}
	}
	assert.True(t, found, "expected ReasoningContent part")
}

func TestMessageAddFinish(t *testing.T) {
	msg := Message{}

	msg.AddFinish(FinishReasonEndTurn)
	assert.True(t, msg.IsFinished())
	assert.Equal(t, FinishReasonEndTurn, msg.FinishReason())

	// Adding another finish should replace the old one
	msg.AddFinish(FinishReasonMaxTokens)
	assert.Equal(t, FinishReasonMaxTokens, msg.FinishReason())

	// Should only have one Finish part
	finishCount := 0
	for _, p := range msg.Parts {
		if _, ok := p.(Finish); ok {
			finishCount++
		}
	}
	assert.Equal(t, 1, finishCount)
}

func TestMessageFinishToolCall(t *testing.T) {
	msg := Message{}
	msg.AddToolCall(ToolCall{
		ID:       "call_1",
		Name:     "read_file",
		Finished: false,
	})

	// Verify not finished initially
	calls := msg.ToolCalls()
	require.Len(t, calls, 1)
	assert.False(t, calls[0].Finished)

	// Finish it
	msg.FinishToolCall("call_1")

	calls = msg.ToolCalls()
	require.Len(t, calls, 1)
	assert.True(t, calls[0].Finished)
}

func TestMessageSetToolCalls(t *testing.T) {
	msg := Message{
		Parts: []ContentPart{
			TextContent{Text: "some text"},
			ToolCall{ID: "old_1", Name: "old_tool"},
			ToolCall{ID: "old_2", Name: "old_tool_2"},
		},
	}

	newCalls := []ToolCall{
		{ID: "new_1", Name: "new_tool"},
		{ID: "new_2", Name: "new_tool_2"},
		{ID: "new_3", Name: "new_tool_3"},
	}
	msg.SetToolCalls(newCalls)

	// Text should still be there
	assert.Equal(t, "some text", msg.Content().Text)

	// Only new tool calls should exist
	calls := msg.ToolCalls()
	require.Len(t, calls, 3)
	assert.Equal(t, "new_1", calls[0].ID)
	assert.Equal(t, "new_2", calls[1].ID)
	assert.Equal(t, "new_3", calls[2].ID)
}

func TestMarshallUnmarshallParts(t *testing.T) {
	parts := []ContentPart{
		TextContent{Text: "hello world"},
		ToolCall{
			ID:       "call_1",
			Name:     "read_file",
			Input:    `{"path": "/tmp/test"}`,
			Type:     "function",
			Finished: true,
		},
		Finish{
			Reason: FinishReasonEndTurn,
			Time:   1234567890,
		},
	}

	data, err := marshallParts(parts)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	roundTripped, err := unmarshallParts(data)
	require.NoError(t, err)
	require.Len(t, roundTripped, 3)

	// Verify TextContent
	tc, ok := roundTripped[0].(TextContent)
	require.True(t, ok, "expected TextContent")
	assert.Equal(t, "hello world", tc.Text)

	// Verify ToolCall
	toolCall, ok := roundTripped[1].(ToolCall)
	require.True(t, ok, "expected ToolCall")
	assert.Equal(t, "call_1", toolCall.ID)
	assert.Equal(t, "read_file", toolCall.Name)
	assert.Equal(t, `{"path": "/tmp/test"}`, toolCall.Input)
	assert.True(t, toolCall.Finished)

	// Verify Finish
	finish, ok := roundTripped[2].(Finish)
	require.True(t, ok, "expected Finish")
	assert.Equal(t, FinishReasonEndTurn, finish.Reason)
	assert.Equal(t, int64(1234567890), finish.Time)
}
