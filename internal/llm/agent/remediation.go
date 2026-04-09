package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/AIdoesmyjob/termfix/internal/llm/tools"
	"github.com/AIdoesmyjob/termfix/internal/logging"
	"github.com/AIdoesmyjob/termfix/internal/message"
)

const baseRemediationPrompt = `Based on the diagnosis, propose ONE fix command.
Rules:
- Use sudo only if necessary
- Prefer reversible operations
- Never delete data without backup
- Never modify /etc/passwd, /etc/shadow, /boot/*
Diagnosis: %s
Propose the fix using the bash tool, or respond with text if no automated fix is safe.`

// remediationBanned lists destructive command patterns that must never be executed in remediation.
var remediationBanned = []string{
	"rm -rf /",
	"rm -rf /*",
	"mkfs",
	"dd if=",
	"shutdown",
	"reboot",
	"> /dev/sd",
	"> /dev/nvme",
	"passwd",
	"/etc/shadow",
	"/boot/",
	":(){ :",
	"chmod -R 777 /",
	"chown -R",
}

// isRemediationBanned checks if a command matches any banned remediation pattern.
func isRemediationBanned(command string) bool {
	lower := strings.ToLower(command)
	for _, banned := range remediationBanned {
		if strings.Contains(lower, banned) {
			return true
		}
	}
	return false
}

// tryRemediation attempts to generate and execute a fix command after diagnosis.
// Returns nil if remediation is not attempted or fails gracefully.
func (a *agent) tryRemediation(ctx context.Context, sessionID, diagnosticContent string, allToolCalls []message.ToolCall, allToolResults []message.ToolResult) *AgentEvent {
	logging.Info("Fix mode enabled, attempting remediation")

	remediationPrompt := fmt.Sprintf(baseRemediationPrompt, diagnosticContent)
	remMsg, err := a.createUserMessage(ctx, sessionID, remediationPrompt, nil)
	if err != nil {
		logging.ErrorPersist(fmt.Sprintf("failed to create remediation message: %v", err))
		return nil
	}

	// Send with tools so the model can propose a bash command
	remResponse, err := a.provider.SendMessages(ctx, []message.Message{remMsg}, a.tools)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			evt := a.err(ErrRequestCancelled)
			return &evt
		}
		logging.ErrorPersist(fmt.Sprintf("failed to generate remediation: %v", err))
		return nil
	}

	// If model returned text only (no fix command), save it and return
	if remResponse.FinishReason != message.FinishReasonToolUse || len(remResponse.ToolCalls) == 0 {
		if remResponse.Content != "" {
			remMessage, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
				Role:  message.Assistant,
				Parts: []message.ContentPart{message.TextContent{Text: "**Remediation**: " + remResponse.Content}},
				Model: a.provider.Model().ID,
			})
			if err == nil {
				remMessage.AddFinish(remResponse.FinishReason)
				_ = a.messages.Update(ctx, remMessage)
				evt := AgentEvent{Type: AgentEventTypeResponse, Message: remMessage, Done: true}
				return &evt
			}
		}
		return nil
	}

	// Execute remediation tool calls with safety checks
	remAgentMsg, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: "**Remediation**:"}},
		Model: a.provider.Model().ID,
	})
	if err != nil {
		logging.ErrorPersist(fmt.Sprintf("failed to create remediation assistant message: %v", err))
		return nil
	}
	remAgentMsg.SetToolCalls(remResponse.ToolCalls)
	remAgentMsg.AddFinish(remResponse.FinishReason)
	ctx = context.WithValue(ctx, tools.MessageIDContextKey, remAgentMsg.ID)
	if err := a.messages.Update(ctx, remAgentMsg); err != nil {
		logging.ErrorPersist(fmt.Sprintf("failed to update remediation message: %v", err))
		return nil
	}

	var toolResults *message.Message
	for _, tc := range remResponse.ToolCalls {
		// Check banned commands before execution
		command := extractCommand(tc.Input)
		if isRemediationBanned(command) {
			logging.ErrorPersist(fmt.Sprintf("Remediation blocked dangerous command: %s", command))
			// Create a blocked message
			blockedMsg, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
				Role:  message.Assistant,
				Parts: []message.ContentPart{message.TextContent{Text: fmt.Sprintf("**Remediation blocked**: `%s` is not safe to execute automatically.", command)}},
				Model: a.provider.Model().ID,
			})
			if err == nil {
				blockedMsg.AddFinish(message.FinishReasonEndTurn)
				_ = a.messages.Update(ctx, blockedMsg)
				evt := AgentEvent{Type: AgentEventTypeResponse, Message: blockedMsg, Done: true}
				return &evt
			}
			return nil
		}

		// Execute — the existing permission system handles user approval
		toolResults, err = a.executeToolCall(ctx, sessionID, tc, toolResults)
		if err != nil {
			evt := a.err(err)
			return &evt
		}
	}

	if toolResults != nil {
		if err := a.messages.Update(ctx, *toolResults); err != nil {
			logging.ErrorPersist(fmt.Sprintf("failed to update remediation tool results: %v", err))
		}
		evt := AgentEvent{Type: AgentEventTypeResponse, Message: *toolResults, Done: true}
		return &evt
	}

	return nil
}
