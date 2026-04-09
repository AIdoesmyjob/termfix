package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AIdoesmyjob/termfix/internal/config"
	"github.com/AIdoesmyjob/termfix/internal/diagnose"
	"github.com/AIdoesmyjob/termfix/internal/llm/models"
	"github.com/AIdoesmyjob/termfix/internal/llm/prompt"
	"github.com/AIdoesmyjob/termfix/internal/llm/provider"
	"github.com/AIdoesmyjob/termfix/internal/llm/tools"
	"github.com/AIdoesmyjob/termfix/internal/logging"
	"github.com/AIdoesmyjob/termfix/internal/message"
	"github.com/AIdoesmyjob/termfix/internal/permission"
	"github.com/AIdoesmyjob/termfix/internal/pubsub"
	"github.com/AIdoesmyjob/termfix/internal/session"
)

// Common errors
var (
	ErrRequestCancelled = errors.New("request cancelled by user")
	ErrSessionBusy      = errors.New("session is currently processing another request")
)

// knowledgePattern matches queries asking for definitions/explanations.
// These don't need tool calls — the model can answer from training knowledge.
// Without this, the 0.8B model calls bash for "what is SSH" when tools are available.
var knowledgePattern = regexp.MustCompile(`(?i)^(what is|what are|what does|explain|define|describe)\b`)

// numericPattern matches numbers with units (e.g., "78%", "22G", "512Mi", "3.8Gi")
// used to detect fabricated values in model output.
var numericPattern = regexp.MustCompile(`\d+(?:\.\d+)?%|\d+(?:\.\d+)?[GMKT]i?[Bb]?`)

// percentPattern matches N% values in tool output for smart summarization.
var percentPattern = regexp.MustCompile(`(\d+)%`)

type AgentEventType string

const (
	AgentEventTypeError     AgentEventType = "error"
	AgentEventTypeResponse  AgentEventType = "response"
	AgentEventTypeSummarize AgentEventType = "summarize"
)

type AgentEvent struct {
	Type    AgentEventType
	Message message.Message
	Error   error

	// When summarizing
	SessionID string
	Progress  string
	Done      bool
}

type Service interface {
	pubsub.Suscriber[AgentEvent]
	Model() models.Model
	Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error)
	Cancel(sessionID string)
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error)
	Summarize(ctx context.Context, sessionID string) error
}

type agent struct {
	*pubsub.Broker[AgentEvent]
	sessions session.Service
	messages message.Service

	tools    []tools.BaseTool
	provider provider.Provider
	// diagnosticProvider uses a separate pass-2 prompt that is optimized
	// for grounded analysis instead of tool selection.
	diagnosticProvider provider.Provider

	titleProvider     provider.Provider
	summarizeProvider provider.Provider

	activeRequests sync.Map
}

func NewAgent(
	agentName config.AgentName,
	sessions session.Service,
	messages message.Service,
	agentTools []tools.BaseTool,
) (Service, error) {
	agentProvider, err := createAgentProvider(agentName, "")
	if err != nil {
		return nil, err
	}
	var diagnosticProvider provider.Provider
	if agentName == config.AgentCoder {
		diagnosticProvider, err = createAgentProvider(agentName, prompt.GetAgentDiagnosticPrompt(agentName, agentProvider.Model().Provider))
		if err != nil {
			return nil, err
		}
	}
	var titleProvider provider.Provider
	// Only generate titles for the coder agent
	if agentName == config.AgentCoder {
		titleProvider, err = createAgentProvider(config.AgentTitle, "")
		if err != nil {
			return nil, err
		}
	}
	var summarizeProvider provider.Provider
	if agentName == config.AgentCoder {
		summarizeProvider, err = createAgentProvider(config.AgentSummarizer, "")
		if err != nil {
			return nil, err
		}
	}

	// Use slim tool descriptions for local models to save ~400 tokens
	effectiveTools := agentTools
	if agentProvider.Model().Provider == models.ProviderLocal {
		effectiveTools = tools.WrapToolsForLocalModel(agentTools)
	}

	agent := &agent{
		Broker:             pubsub.NewBroker[AgentEvent](),
		provider:           agentProvider,
		diagnosticProvider: diagnosticProvider,
		messages:           messages,
		sessions:           sessions,
		tools:              effectiveTools,
		titleProvider:      titleProvider,
		summarizeProvider:  summarizeProvider,
		activeRequests:     sync.Map{},
	}

	return agent, nil
}

func (a *agent) Model() models.Model {
	return a.provider.Model()
}

func (a *agent) Cancel(sessionID string) {
	// Cancel regular requests
	if cancelFunc, exists := a.activeRequests.LoadAndDelete(sessionID); exists {
		if cancel, ok := cancelFunc.(context.CancelFunc); ok {
			logging.InfoPersist(fmt.Sprintf("Request cancellation initiated for session: %s", sessionID))
			cancel()
		}
	}

	// Also check for summarize requests
	if cancelFunc, exists := a.activeRequests.LoadAndDelete(sessionID + "-summarize"); exists {
		if cancel, ok := cancelFunc.(context.CancelFunc); ok {
			logging.InfoPersist(fmt.Sprintf("Summarize cancellation initiated for session: %s", sessionID))
			cancel()
		}
	}
}

func (a *agent) IsBusy() bool {
	busy := false
	a.activeRequests.Range(func(key, value interface{}) bool {
		if cancelFunc, ok := value.(context.CancelFunc); ok {
			if cancelFunc != nil {
				busy = true
				return false // Stop iterating
			}
		}
		return true // Continue iterating
	})
	return busy
}

func (a *agent) IsSessionBusy(sessionID string) bool {
	_, busy := a.activeRequests.Load(sessionID)
	return busy
}

func (a *agent) generateTitle(ctx context.Context, sessionID string, content string) error {
	if content == "" {
		return nil
	}
	if a.titleProvider == nil {
		return nil
	}
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	parts := []message.ContentPart{message.TextContent{Text: content}}
	response, err := a.titleProvider.SendMessages(
		ctx,
		[]message.Message{
			{
				Role:  message.User,
				Parts: parts,
			},
		},
		make([]tools.BaseTool, 0),
	)
	if err != nil {
		return err
	}

	title := strings.TrimSpace(strings.ReplaceAll(response.Content, "\n", " "))
	if title == "" {
		return nil
	}

	session.Title = title
	_, err = a.sessions.Save(ctx, session)
	return err
}

func (a *agent) err(err error) AgentEvent {
	return AgentEvent{
		Type:  AgentEventTypeError,
		Error: err,
	}
}

func (a *agent) Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error) {
	if !a.provider.Model().SupportsAttachments && attachments != nil {
		attachments = nil
	}
	events := make(chan AgentEvent)
	if a.IsSessionBusy(sessionID) {
		return nil, ErrSessionBusy
	}

	genCtx, cancel := context.WithCancel(ctx)

	a.activeRequests.Store(sessionID, cancel)
	go func() {
		logging.Debug("Request started", "sessionID", sessionID)
		defer logging.RecoverPanic("agent.Run", func() {
			events <- a.err(fmt.Errorf("panic while running the agent"))
		})
		var attachmentParts []message.ContentPart
		for _, attachment := range attachments {
			attachmentParts = append(attachmentParts, message.BinaryContent{Path: attachment.FilePath, MIMEType: attachment.MimeType, Data: attachment.Content})
		}
		result := a.processGeneration(genCtx, sessionID, content, attachmentParts)
		if result.Error != nil && !errors.Is(result.Error, ErrRequestCancelled) && !errors.Is(result.Error, context.Canceled) {
			logging.ErrorPersist(result.Error.Error())
		}
		logging.Debug("Request completed", "sessionID", sessionID)
		a.activeRequests.Delete(sessionID)
		cancel()
		a.Publish(pubsub.CreatedEvent, result)
		events <- result
		close(events)
	}()
	return events, nil
}

// maxIterations is the maximum number of tool-call rounds before forcing diagnosis.
const maxIterations = 3

// maxEvidenceBytes caps the total accumulated tool output to prevent context overflow.
const maxEvidenceBytes = 3000

// perResultCap limits individual tool result size before accumulation.
const perResultCap = 1500

func (a *agent) processGeneration(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart) AgentEvent {
	// List existing messages; if none, start title generation asynchronously.
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to list messages: %w", err))
	}
	if len(msgs) == 0 {
		go func() {
			defer logging.RecoverPanic("agent.Run", func() {
				logging.ErrorPersist("panic while generating title")
			})
			titleErr := a.generateTitle(context.Background(), sessionID, content)
			if titleErr != nil {
				logging.ErrorPersist(fmt.Sprintf("failed to generate title: %v", titleErr))
			}
		}()
	}
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to get session: %w", err))
	}
	if session.SummaryMessageID != "" {
		summaryMsgInex := -1
		for i, msg := range msgs {
			if msg.ID == session.SummaryMessageID {
				summaryMsgInex = i
				break
			}
		}
		if summaryMsgInex != -1 {
			msgs = msgs[summaryMsgInex:]
			msgs[0].Role = message.User
		}
	}

	userMsg, err := a.createUserMessage(ctx, sessionID, content, attachmentParts)
	if err != nil {
		return a.err(fmt.Errorf("failed to create user message: %w", err))
	}
	// Append the new user message to the conversation history.
	msgHistory := append(msgs, userMsg)

	// Check for cancellation
	select {
	case <-ctx.Done():
		return a.err(ctx.Err())
	default:
	}

	// Set up context for tool execution
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)

	// Accumulated state across iterations
	var allToolCalls []message.ToolCall
	var allToolResults []message.ToolResult
	var allToolResultMsgs []*message.Message
	var routedRecipe *diagnose.Recipe
	totalEvidenceLen := 0

	// MULTI-TURN TOOL LOOP: up to maxIterations rounds
	for iteration := 0; iteration < maxIterations; iteration++ {
		var response *provider.ProviderResponse
		pass1Tools := a.tools

		if isKnowledgeQuery(content) {
			pass1Tools = nil
		}

		// Iteration 0: recipe routing OR model pass1
		if iteration == 0 {
			routedRecipe = diagnose.SelectRecipe(content)
			if routed := routeDiagnosticIntent(routedRecipe); routed != nil {
				response = &provider.ProviderResponse{
					ToolCalls:    []message.ToolCall{*routed},
					FinishReason: message.FinishReasonToolUse,
				}
				logging.Info("Iteration 0 routed deterministically", "recipe", routedRecipe.Name, "tool", routed.Name, "input", routed.Input)
			}
			if response == nil {
				response, err = a.provider.SendMessages(ctx, msgHistory, pass1Tools)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						return a.err(ErrRequestCancelled)
					}
					return a.err(fmt.Errorf("failed to send iteration %d messages: %w", iteration, err))
				}
			}
		} else {
			// Iterations 1+: fresh context with accumulated evidence
			iterCtx := buildIterationContext(content, routedRecipe, allToolCalls, allToolResults, maxIterations-iteration)
			iterMsg, iterErr := a.createUserMessage(ctx, sessionID, iterCtx, nil)
			if iterErr != nil {
				return a.err(fmt.Errorf("failed to create iteration %d message: %w", iteration, iterErr))
			}
			response, err = a.provider.SendMessages(ctx, []message.Message{iterMsg}, pass1Tools)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return a.err(ErrRequestCancelled)
				}
				return a.err(fmt.Errorf("failed to send iteration %d messages: %w", iteration, err))
			}
		}

		// Save the response as an assistant message
		agentMessage, createErr := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
			Role:  message.Assistant,
			Parts: []message.ContentPart{message.TextContent{Text: response.Content}},
			Model: a.provider.Model().ID,
		})
		if createErr != nil {
			return a.err(fmt.Errorf("failed to create iteration %d message: %w", iteration, createErr))
		}
		agentMessage.SetToolCalls(response.ToolCalls)
		agentMessage.AddFinish(response.FinishReason)
		ctx = context.WithValue(ctx, tools.MessageIDContextKey, agentMessage.ID)
		if err := a.messages.Update(ctx, agentMessage); err != nil {
			return a.err(fmt.Errorf("failed to update iteration %d message: %w", iteration, err))
		}

		logging.Info("Iteration result", "iteration", iteration, "finishReason", response.FinishReason, "toolCalls", len(response.ToolCalls), "content", response.Content)

		// If no tool calls, return directly (knowledge question or text-only response)
		if response.FinishReason != message.FinishReasonToolUse || len(response.ToolCalls) == 0 {
			// If we have accumulated evidence, go to Pass 2 instead
			if len(allToolCalls) > 0 {
				break
			}
			return AgentEvent{
				Type:    AgentEventTypeResponse,
				Message: agentMessage,
				Done:    true,
			}
		}

		// Execute the tool calls
		var toolResults *message.Message
		for _, tc := range response.ToolCalls {
			toolResults, err = a.executeToolCall(ctx, sessionID, tc, toolResults)
			if err != nil {
				return a.err(err)
			}
		}

		// Recipe follow-up (only on iteration 0)
		if iteration == 0 && routedRecipe != nil && toolResults != nil {
			if followUpCommand := routedRecipe.FollowUpCommand(toolResultContent(toolResults)); followUpCommand != "" {
				followUp := newBashToolCall(fmt.Sprintf("%s_follow_up", routedRecipe.Name), followUpCommand)
				agentMessage.AddToolCall(*followUp)
				if err := a.messages.Update(ctx, agentMessage); err != nil {
					return a.err(fmt.Errorf("failed to update message with follow-up: %w", err))
				}
				logging.Info("Executing deterministic follow-up", "recipe", routedRecipe.Name, "command", followUpCommand)
				toolResults, err = a.executeToolCall(ctx, sessionID, *followUp, toolResults)
				if err != nil {
					return a.err(err)
				}
			}
		}

		if toolResults != nil {
			if err := a.messages.Update(ctx, *toolResults); err != nil {
				return a.err(fmt.Errorf("failed to update tool results: %w", err))
			}
		}

		// Accumulate tool calls and results
		for _, tc := range agentMessage.ToolCalls() {
			allToolCalls = append(allToolCalls, tc)
		}
		if toolResults != nil {
			for _, tr := range toolResults.ToolResults() {
				truncated := truncateToolResult(tr.Content, perResultCap)
				totalEvidenceLen += len(truncated)
				allToolResults = append(allToolResults, message.ToolResult{
					ToolCallID: tr.ToolCallID,
					Content:    truncated,
					IsError:    tr.IsError,
				})
			}
			allToolResultMsgs = append(allToolResultMsgs, toolResults)
		}

		// Budget guard: stop if total evidence exceeds cap
		if totalEvidenceLen > maxEvidenceBytes {
			logging.Info("Evidence budget exceeded, proceeding to diagnosis", "totalLen", totalEvidenceLen)
			break
		}

		// Check for cancellation
		select {
		case <-ctx.Done():
			return a.err(ctx.Err())
		default:
		}
	}

	// PASS 2: Diagnostic generation
	if len(allToolCalls) == 0 {
		// Should not normally happen, but defensive
		return a.err(fmt.Errorf("no tool calls after iteration loop"))
	}

	// Collect all tool output for fabrication checking
	var allToolOutput strings.Builder
	for _, tr := range allToolResults {
		allToolOutput.WriteString(tr.Content)
		allToolOutput.WriteString("\n")
	}
	toolOutput := allToolOutput.String()
	if len(toolOutput) > 4000 {
		toolOutput = toolOutput[:4000]
	}

	// Try structured diagnostic first — deterministic parsing, zero fabrication.
	if structured, ok := tryStructuredDiagnostic(allToolCalls, allToolResults); ok {
		return a.finishWithRemediation(ctx, sessionID, structured, allToolCalls, allToolResults)
	}
	if structured, ok := tryStructuredRecipeDiagnostic(routedRecipe, allToolCalls, allToolResults); ok {
		return a.finishWithRemediation(ctx, sessionID, structured, allToolCalls, allToolResults)
	}

	// Fallback: model-based Pass 2 for unrecognized commands
	pass2Content := buildPass2Content(content, routedRecipe, allToolCalls, allToolResults)
	pass2Msg, pass2Err := a.createUserMessage(ctx, sessionID, pass2Content, nil)
	if pass2Err != nil {
		return a.err(fmt.Errorf("failed to create pass 2 user message: %w", pass2Err))
	}
	pass2History := []message.Message{pass2Msg}

	// Pass 2: non-streaming, no tools — forces text-only diagnostic generation
	diagnosticProvider := a.provider
	if a.diagnosticProvider != nil {
		diagnosticProvider = a.diagnosticProvider
	}
	pass2Response, pass2Err := diagnosticProvider.SendMessages(ctx, pass2History, nil)
	if pass2Err != nil {
		if errors.Is(pass2Err, context.Canceled) {
			return a.err(ErrRequestCancelled)
		}
		return a.err(fmt.Errorf("failed to generate diagnostic: %w", pass2Err))
	}

	// Truncate repetitive output from small models, then strip lines containing
	// fabricated numeric values (numbers not present in the original tool output).
	diagnosticContent := stripFabricatedValues(truncateRepetition(pass2Response.Content), toolOutput)

	diagnosticMessage, createErr := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: diagnosticContent}},
		Model: diagnosticProvider.Model().ID,
	})
	if createErr != nil {
		return a.err(fmt.Errorf("failed to create diagnostic message: %w", createErr))
	}
	diagnosticMessage.AddFinish(pass2Response.FinishReason)
	if err := a.messages.Update(ctx, diagnosticMessage); err != nil {
		return a.err(fmt.Errorf("failed to update diagnostic message: %w", err))
	}

	// Try remediation if fix mode is enabled
	if config.Get().FixMode {
		if remResult := a.tryRemediation(ctx, sessionID, diagnosticContent, allToolCalls, allToolResults); remResult != nil {
			return *remResult
		}
	}

	return AgentEvent{
		Type:    AgentEventTypeResponse,
		Message: diagnosticMessage,
		Done:    true,
	}
}

// finishWithRemediation returns a structured diagnostic, optionally followed by remediation.
func (a *agent) finishWithRemediation(ctx context.Context, sessionID, diagnosticText string, allToolCalls []message.ToolCall, allToolResults []message.ToolResult) AgentEvent {
	diagnosticMessage, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{message.TextContent{Text: diagnosticText}},
		Model: a.provider.Model().ID,
	})
	if err != nil {
		return a.err(fmt.Errorf("failed to create diagnostic message: %w", err))
	}
	diagnosticMessage.AddFinish(message.FinishReasonEndTurn)
	if err := a.messages.Update(ctx, diagnosticMessage); err != nil {
		return a.err(fmt.Errorf("failed to update diagnostic message: %w", err))
	}

	// Try remediation if fix mode is enabled
	if config.Get().FixMode {
		if remResult := a.tryRemediation(ctx, sessionID, diagnosticText, allToolCalls, allToolResults); remResult != nil {
			return *remResult
		}
	}

	return AgentEvent{Type: AgentEventTypeResponse, Message: diagnosticMessage, Done: true}
}

// buildIterationContext builds a compact prompt for iterations 1+ with accumulated evidence.
func buildIterationContext(userContent string, recipe *diagnose.Recipe, toolCalls []message.ToolCall, toolResults []message.ToolResult, remainingProbes int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("User issue: %s\n\n", userContent))
	b.WriteString("Evidence so far:\n")
	for i := 0; i < len(toolCalls) && i < len(toolResults); i++ {
		command := extractCommand(toolCalls[i].Input)
		if command == "" {
			command = toolCalls[i].Name
		}
		summary := smartSummarize(recipe, command, toolResults[i].Content)
		b.WriteString(fmt.Sprintf("- `%s` → %s\n", command, strings.TrimSpace(summary)))
	}
	b.WriteString(fmt.Sprintf("\nYou have %d more probe(s). Call a tool to gather more evidence, or respond with text to diagnose.\n", remainingProbes))
	return b.String()
}

// smartSummarize produces a compact summary of tool output, using context-aware
// extraction for known commands to maximize information density within ~200 chars.
func smartSummarize(recipe *diagnose.Recipe, command, output string) string {
	// Try context-aware summaries for common commands first
	if s := summarizeCommandSmart(command, output); s != "" {
		return s
	}
	// Fall back to recipe-specific summarizers
	if recipe != nil {
		if s := summarizeProbe(recipe, command, output); s != "" {
			return s
		}
	}
	// Generic fallback
	if s := summarizeGenericOutput(output); s != "" {
		return s
	}
	// Last resort: first 200 chars
	if len(output) > 200 {
		return output[:200] + "..."
	}
	return output
}

// summarizeCommandSmart extracts the most diagnostic signal from known commands.
func summarizeCommandSmart(command, output string) string {
	if strings.TrimSpace(output) == "" {
		return ""
	}
	// df -h: extract only partitions above 80%
	if strings.HasPrefix(command, "df") {
		var high []string
		for _, line := range strings.Split(output, "\n") {
			for _, m := range percentPattern.FindAllStringSubmatch(line, -1) {
				if len(m) >= 2 {
					pct, _ := strconv.Atoi(m[1])
					if pct >= 80 {
						fields := strings.Fields(line)
						if len(fields) > 0 {
							high = append(high, fmt.Sprintf("%s at %d%%", fields[len(fields)-1], pct))
						}
					}
				}
			}
		}
		if len(high) > 0 {
			return strings.Join(high, ", ")
		}
		return "all partitions below 80%"
	}
	// ps: just top 3 processes
	if strings.HasPrefix(command, "ps") {
		lines := firstNonEmptyLines(output, 4) // header + 3
		if len(lines) > 1 {
			return strings.Join(lines[1:], "; ")
		}
	}
	// git status: modified/untracked count
	if strings.HasPrefix(command, "git status") {
		modified := len(grepLines(output, `modified:`))
		untracked := len(grepLines(output, `Untracked files`))
		conflicts := len(grepLines(output, `both modified`))
		parts := []string{}
		if conflicts > 0 {
			parts = append(parts, fmt.Sprintf("%d conflicts", conflicts))
		}
		if modified > 0 {
			parts = append(parts, fmt.Sprintf("%d modified", modified))
		}
		if untracked > 0 {
			parts = append(parts, "has untracked files")
		}
		if len(parts) > 0 {
			return strings.Join(parts, ", ")
		}
		return "clean working directory"
	}
	return ""
}

// truncateToolResult smartly truncates a tool result to maxLen chars,
// keeping the first and last portions for context.
func truncateToolResult(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}
	half := maxLen / 2
	return content[:half] + "\n... (truncated) ...\n" + content[len(content)-half:]
}


func (a *agent) createUserMessage(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart) (message.Message, error) {
	parts := []message.ContentPart{message.TextContent{Text: content}}
	parts = append(parts, attachmentParts...)
	return a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: parts,
	})
}

func (a *agent) streamAndHandleEvents(ctx context.Context, sessionID string, msgHistory []message.Message) (message.Message, *message.Message, error) {
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	eventChan := a.provider.StreamResponse(ctx, msgHistory, a.tools)

	assistantMsg, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{},
		Model: a.provider.Model().ID,
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create assistant message: %w", err)
	}

	// Add the session and message ID into the context if needed by tools.
	ctx = context.WithValue(ctx, tools.MessageIDContextKey, assistantMsg.ID)

	// Process each event in the stream.
	for event := range eventChan {
		if processErr := a.processEvent(ctx, sessionID, &assistantMsg, event); processErr != nil {
			a.finishMessage(ctx, &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, nil, processErr
		}
		if ctx.Err() != nil {
			a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, nil, ctx.Err()
		}
	}

	toolResults := make([]message.ToolResult, len(assistantMsg.ToolCalls()))
	toolCalls := assistantMsg.ToolCalls()
	for i, toolCall := range toolCalls {
		select {
		case <-ctx.Done():
			a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
			// Make all future tool calls cancelled
			for j := i; j < len(toolCalls); j++ {
				toolResults[j] = message.ToolResult{
					ToolCallID: toolCalls[j].ID,
					Content:    "Tool execution canceled by user",
					IsError:    true,
				}
			}
			goto out
		default:
			// Continue processing
			var tool tools.BaseTool
			for _, availableTool := range a.tools {
				if availableTool.Info().Name == toolCall.Name {
					tool = availableTool
					break
				}
				// Monkey patch for Copilot Sonnet-4 tool repetition obfuscation
				// if strings.HasPrefix(toolCall.Name, availableTool.Info().Name) &&
				// 	strings.HasPrefix(toolCall.Name, availableTool.Info().Name+availableTool.Info().Name) {
				// 	tool = availableTool
				// 	break
				// }
			}

			// Tool not found
			if tool == nil {
				toolResults[i] = message.ToolResult{
					ToolCallID: toolCall.ID,
					Content:    fmt.Sprintf("Tool not found: %s", toolCall.Name),
					IsError:    true,
				}
				continue
			}
			toolResult, toolErr := tool.Run(ctx, tools.ToolCall{
				ID:    toolCall.ID,
				Name:  toolCall.Name,
				Input: toolCall.Input,
			})
			if toolErr != nil {
				if errors.Is(toolErr, permission.ErrorPermissionDenied) {
					toolResults[i] = message.ToolResult{
						ToolCallID: toolCall.ID,
						Content:    "Permission denied",
						IsError:    true,
					}
					for j := i + 1; j < len(toolCalls); j++ {
						toolResults[j] = message.ToolResult{
							ToolCallID: toolCalls[j].ID,
							Content:    "Tool execution canceled by user",
							IsError:    true,
						}
					}
					a.finishMessage(ctx, &assistantMsg, message.FinishReasonPermissionDenied)
					break
				}
			}
			toolResults[i] = message.ToolResult{
				ToolCallID: toolCall.ID,
				Content:    toolResult.Content,
				Metadata:   toolResult.Metadata,
				IsError:    toolResult.IsError,
			}
		}
	}
out:
	if len(toolResults) == 0 {
		return assistantMsg, nil, nil
	}
	parts := make([]message.ContentPart, 0)
	for _, tr := range toolResults {
		parts = append(parts, tr)
	}
	msg, err := a.messages.Create(context.Background(), assistantMsg.SessionID, message.CreateMessageParams{
		Role:  message.Tool,
		Parts: parts,
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create cancelled tool message: %w", err)
	}

	return assistantMsg, &msg, err
}

// streamDiagnostic calls the provider with NO tools, forcing text-only generation.
// This is Pass 2 of the two-pass architecture for small models.
func (a *agent) streamDiagnostic(ctx context.Context, sessionID string, msgHistory []message.Message) (message.Message, error) {
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	diagnosticProvider := a.provider
	if a.diagnosticProvider != nil {
		diagnosticProvider = a.diagnosticProvider
	}
	eventChan := diagnosticProvider.StreamResponse(ctx, msgHistory, nil) // NO tools

	assistantMsg, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{},
		Model: diagnosticProvider.Model().ID,
	})
	if err != nil {
		return assistantMsg, fmt.Errorf("failed to create diagnostic message: %w", err)
	}

	for event := range eventChan {
		if processErr := a.processEvent(ctx, sessionID, &assistantMsg, event); processErr != nil {
			a.finishMessage(ctx, &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, processErr
		}
		if ctx.Err() != nil {
			a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, ctx.Err()
		}
	}

	return assistantMsg, nil
}

// truncateRepetition detects when a small model enters a generation loop
// (repeating the same phrase/paragraph) and truncates the output.
func truncateRepetition(content string) string {
	if len(content) < 200 {
		return content
	}
	// Split into sentences/paragraphs and detect repetition
	lines := strings.Split(content, "\n")
	seen := make(map[string]int)
	var result []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			result = append(result, line)
			continue
		}
		seen[trimmed]++
		if seen[trimmed] > 2 {
			// This line has appeared 3+ times — stop here
			break
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

// isKnowledgeQuery returns true if the user's query is asking for a definition or
// explanation (e.g., "what is SSH", "explain TCP vs UDP"). These queries should be
// answered from training knowledge without calling tools.
func isKnowledgeQuery(content string) bool {
	return knowledgePattern.MatchString(strings.TrimSpace(content))
}

// stripFabricatedValues removes lines from the model's response that contain numeric
// values (with units or %) not found in the original command output. Small models (0.8B)
// hallucinate completely wrong numbers when analyzing tool output (e.g., "124%" when the
// output only shows "78%").
//
// Leniency rules (to avoid over-stripping):
//   - Exact match in output numbers → valid
//   - Bare number (stripped of unit) appears somewhere in output → valid
//   - Small integers (1-2 digits) → valid (often counts, not data values)
//
// If filtering would leave too little content (<50 chars), falls back to raw content.
func stripFabricatedValues(content, toolOutput string) string {
	if toolOutput == "" || content == "" {
		return content
	}

	// Build set of valid numbers from tool output
	validNums := make(map[string]bool)
	for _, n := range numericPattern.FindAllString(toolOutput, -1) {
		validNums[n] = true
	}
	if len(validNums) == 0 {
		return content // No numbers in output to validate against
	}

	isFabricated := func(num string) bool {
		// Exact match
		if validNums[num] {
			return false
		}
		// Strip unit suffix to get bare number
		bare := strings.TrimRight(num, "%GMKTiBb")
		// Small integers (1-2 digits) are OK — represent counts, not data
		if len(bare) <= 2 {
			return false
		}
		// Bare number appears somewhere in tool output
		if strings.Contains(toolOutput, bare) {
			return false
		}
		return true
	}

	// Process content line by line — remove lines with fabricated numbers
	lines := strings.Split(content, "\n")
	var result []string
	for _, line := range lines {
		numsInLine := numericPattern.FindAllString(line, -1)
		hasFabricated := false
		for _, n := range numsInLine {
			if isFabricated(n) {
				hasFabricated = true
				break
			}
		}
		if !hasFabricated {
			result = append(result, line)
		}
	}

	filtered := strings.Join(result, "\n")
	// If filtering removed too much, redact fabricated numbers inline instead
	if len(strings.TrimSpace(filtered)) < 50 {
		return numericPattern.ReplaceAllStringFunc(content, func(match string) string {
			if isFabricated(match) {
				return "[?]"
			}
			return match
		})
	}
	return filtered
}

// sanitizeToolInput cleans up tool call inputs from small local models that sometimes
// hallucinate extra text inside parameter values. For bash commands, this extracts
// only the first meaningful line (the actual command) and discards everything else.
func sanitizeToolInput(toolName, input string) string {
	if toolName == "bash" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(input), &parsed); err != nil {
			return input
		}
		cmd, ok := parsed["command"].(string)
		if !ok || cmd == "" {
			return input
		}
		// Take only the first non-empty line as the command.
		// The model often generates the actual command on line 1,
		// then hallucinates analysis/output on subsequent lines.
		// Note: llama-server may produce literal "\n" text or actual newlines
		// depending on how the grammar parser serializes the content.
		cmd = strings.ReplaceAll(cmd, `\n`, "\n") // normalize literal \n to real newlines
		lines := strings.Split(cmd, "\n")
		cleanCmd := ""
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Stop at lines that look like hallucinated output or XML tags
			if strings.HasPrefix(line, "<") || strings.HasPrefix(line, "**") || strings.HasPrefix(line, "#") {
				break
			}
			cleanCmd = line
			break
		}
		if cleanCmd == "" {
			return input
		}
		parsed["command"] = cleanCmd
		// Remove any hallucinated extra keys
		clean := map[string]interface{}{"command": cleanCmd}
		if timeout, ok := parsed["timeout"]; ok {
			clean["timeout"] = timeout
		}
		result, err := json.Marshal(clean)
		if err != nil {
			return input
		}
		return string(result)
	}

	// For view tool, sanitize file_path
	if toolName == "view" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(input), &parsed); err != nil {
			return input
		}
		fp, ok := parsed["file_path"].(string)
		if !ok {
			return input
		}
		// Normalize literal \n, take first line only, trim spaces
		fp = strings.ReplaceAll(fp, `\n`, "\n")
		fp = strings.TrimSpace(strings.Split(fp, "\n")[0])
		// Strip quotes and non-path characters
		fp = strings.Trim(fp, "\"'`")
		// Reject obviously invalid paths (hallucinated descriptions, spaces-only, etc.)
		if fp == "" || strings.ContainsAny(fp, "\t") || !strings.ContainsAny(fp, "/.") {
			return input
		}
		clean := map[string]interface{}{"file_path": fp}
		if offset, ok := parsed["offset"]; ok {
			clean["offset"] = offset
		}
		if limit, ok := parsed["limit"]; ok {
			clean["limit"] = limit
		}
		result, err := json.Marshal(clean)
		if err != nil {
			return input
		}
		return string(result)
	}

	return input
}

// toolCallSummary extracts a human-readable summary of tool calls from a message.
func toolCallSummary(msg message.Message) string {
	var parts []string
	for _, tc := range msg.ToolCalls() {
		parts = append(parts, fmt.Sprintf("%s(%s)", tc.Name, tc.Input))
	}
	return strings.Join(parts, ", ")
}

// toolResultContent extracts text content from tool results, capped at 4000 chars.
func toolResultContent(msg *message.Message) string {
	var content string
	for _, tr := range msg.ToolResults() {
		content += tr.Content + "\n"
	}
	if len(content) > 4000 {
		content = content[:4000] + "\n... (truncated)"
	}
	return content
}

func (a *agent) finishMessage(ctx context.Context, msg *message.Message, finishReson message.FinishReason) {
	msg.AddFinish(finishReson)
	_ = a.messages.Update(ctx, *msg)
}

func (a *agent) processEvent(ctx context.Context, sessionID string, assistantMsg *message.Message, event provider.ProviderEvent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing.
	}

	switch event.Type {
	case provider.EventThinkingDelta:
		assistantMsg.AppendReasoningContent(event.Content)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventContentDelta:
		assistantMsg.AppendContent(event.Content)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventToolUseStart:
		assistantMsg.AddToolCall(*event.ToolCall)
		return a.messages.Update(ctx, *assistantMsg)
	// Tool use deltas are not processed incrementally; the full tool input
	// is captured when EventToolUseStop fires via FinishToolCall.
	case provider.EventToolUseStop:
		assistantMsg.FinishToolCall(event.ToolCall.ID)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventError:
		if errors.Is(event.Error, context.Canceled) {
			logging.InfoPersist(fmt.Sprintf("Event processing canceled for session: %s", sessionID))
			return context.Canceled
		}
		logging.ErrorPersist(event.Error.Error())
		return event.Error
	case provider.EventComplete:
		assistantMsg.SetToolCalls(event.Response.ToolCalls)
		assistantMsg.AddFinish(event.Response.FinishReason)
		if err := a.messages.Update(ctx, *assistantMsg); err != nil {
			return fmt.Errorf("failed to update message: %w", err)
		}
		return a.TrackUsage(ctx, sessionID, a.provider.Model(), event.Response.Usage)
	}

	return nil
}

func (a *agent) TrackUsage(ctx context.Context, sessionID string, model models.Model, usage provider.TokenUsage) error {
	sess, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		model.CostPer1MIn/1e6*float64(usage.InputTokens) +
		model.CostPer1MOut/1e6*float64(usage.OutputTokens)

	sess.Cost += cost
	sess.CompletionTokens = usage.OutputTokens + usage.CacheReadTokens
	sess.PromptTokens = usage.InputTokens + usage.CacheCreationTokens

	_, err = a.sessions.Save(ctx, sess)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	return nil
}

func (a *agent) Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error) {
	if a.IsBusy() {
		return models.Model{}, fmt.Errorf("cannot change model while processing requests")
	}

	if err := config.UpdateAgentModel(agentName, modelID); err != nil {
		return models.Model{}, fmt.Errorf("failed to update config: %w", err)
	}

	provider, err := createAgentProvider(agentName, "")
	if err != nil {
		return models.Model{}, fmt.Errorf("failed to create provider for model %s: %w", modelID, err)
	}

	a.provider = provider
	if agentName == config.AgentCoder {
		diagnosticProvider, err := createAgentProvider(agentName, prompt.GetAgentDiagnosticPrompt(agentName, provider.Model().Provider))
		if err != nil {
			return models.Model{}, fmt.Errorf("failed to create diagnostic provider for model %s: %w", modelID, err)
		}
		a.diagnosticProvider = diagnosticProvider
	}

	return a.provider.Model(), nil
}

func (a *agent) Summarize(ctx context.Context, sessionID string) error {
	if a.summarizeProvider == nil {
		return fmt.Errorf("summarize provider not available")
	}

	// Check if session is busy
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}

	// Create a new context with cancellation
	summarizeCtx, cancel := context.WithCancel(ctx)

	// Store the cancel function in activeRequests to allow cancellation
	a.activeRequests.Store(sessionID+"-summarize", cancel)

	go func() {
		defer a.activeRequests.Delete(sessionID + "-summarize")
		defer cancel()
		event := AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Starting summarization...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		// Get all messages from the session
		msgs, err := a.messages.List(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to list messages: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		summarizeCtx = context.WithValue(summarizeCtx, tools.SessionIDContextKey, sessionID)

		if len(msgs) == 0 {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("no messages to summarize"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Analyzing conversation...",
		}
		a.Publish(pubsub.CreatedEvent, event)

		// Add a system message to guide the summarization
		summarizePrompt := "Provide a detailed but concise summary of our conversation above. Focus on information that would be helpful for continuing the conversation, including what we did, what we're doing, which files we're working on, and what we're going to do next."

		// Create a new message with the summarize prompt
		promptMsg := message.Message{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: summarizePrompt}},
		}

		// Append the prompt to the messages
		msgsWithPrompt := append(msgs, promptMsg)

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Generating summary...",
		}

		a.Publish(pubsub.CreatedEvent, event)

		// Send the messages to the summarize provider
		response, err := a.summarizeProvider.SendMessages(
			summarizeCtx,
			msgsWithPrompt,
			make([]tools.BaseTool, 0),
		)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to summarize: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		summary := strings.TrimSpace(response.Content)
		if summary == "" {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("empty summary returned"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Creating new session...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		oldSession, err := a.sessions.Get(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to get session: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		// Create a message in the new session with the summary
		msg, err := a.messages.Create(summarizeCtx, oldSession.ID, message.CreateMessageParams{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.TextContent{Text: summary},
				message.Finish{
					Reason: message.FinishReasonEndTurn,
					Time:   time.Now().Unix(),
				},
			},
			Model: a.summarizeProvider.Model().ID,
		})
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to create summary message: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		oldSession.SummaryMessageID = msg.ID
		oldSession.CompletionTokens = response.Usage.OutputTokens
		oldSession.PromptTokens = 0
		model := a.summarizeProvider.Model()
		usage := response.Usage
		cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
			model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
			model.CostPer1MIn/1e6*float64(usage.InputTokens) +
			model.CostPer1MOut/1e6*float64(usage.OutputTokens)
		oldSession.Cost += cost
		_, err = a.sessions.Save(summarizeCtx, oldSession)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to save session: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
		}

		event = AgentEvent{
			Type:      AgentEventTypeSummarize,
			SessionID: oldSession.ID,
			Progress:  "Summary complete",
			Done:      true,
		}
		a.Publish(pubsub.CreatedEvent, event)
		// Send final success event with the new session ID
	}()

	return nil
}

func createAgentProvider(agentName config.AgentName, systemPrompt string) (provider.Provider, error) {
	cfg := config.Get()
	agentConfig, ok := cfg.Agents[agentName]
	if !ok {
		return nil, fmt.Errorf("agent %s not found", agentName)
	}
	model, ok := models.SupportedModels[agentConfig.Model]
	if !ok {
		return nil, fmt.Errorf("model %s not supported", agentConfig.Model)
	}

	providerCfg, ok := cfg.Providers[model.Provider]
	if !ok {
		return nil, fmt.Errorf("provider %s not supported", model.Provider)
	}
	if providerCfg.Disabled {
		return nil, fmt.Errorf("provider %s is not enabled", model.Provider)
	}
	maxTokens := model.DefaultMaxTokens
	if agentConfig.MaxTokens > 0 {
		maxTokens = agentConfig.MaxTokens
	}
	opts := []provider.ProviderClientOption{
		provider.WithAPIKey(providerCfg.APIKey),
		provider.WithModel(model),
		provider.WithMaxTokens(maxTokens),
	}
	if systemPrompt == "" {
		systemPrompt = prompt.GetAgentPrompt(agentName, model.Provider)
	}
	opts = append(opts, provider.WithSystemMessage(systemPrompt))
	if model.Provider == models.ProviderOpenAI || model.Provider == models.ProviderLocal && model.CanReason {
		opts = append(
			opts,
			provider.WithOpenAIOptions(
				provider.WithReasoningEffort(agentConfig.ReasoningEffort),
			),
		)
	} else if model.Provider == models.ProviderAnthropic && model.CanReason && agentName == config.AgentCoder {
		opts = append(
			opts,
			provider.WithAnthropicOptions(
				provider.WithAnthropicShouldThinkFn(provider.DefaultShouldThinkFn),
			),
		)
	}
	agentProvider, err := provider.NewProvider(
		model.Provider,
		opts...,
	)
	if err != nil {
		return nil, fmt.Errorf("could not create provider: %v", err)
	}

	return agentProvider, nil
}

func (a *agent) executeToolCall(ctx context.Context, sessionID string, tc message.ToolCall, toolResults *message.Message) (*message.Message, error) {
	sanitizedInput := sanitizeToolInput(tc.Name, tc.Input)
	for _, availableTool := range a.tools {
		if availableTool.Info().Name != tc.Name {
			continue
		}

		toolCall := tools.ToolCall{
			ID:    tc.ID,
			Name:  tc.Name,
			Input: sanitizedInput,
		}
		result, resultErr := availableTool.Run(ctx, toolCall)
		if resultErr != nil {
			result = tools.ToolResponse{Content: fmt.Sprintf("Error: %v", resultErr)}
		}
		if toolResults == nil {
			trMsg, createErr := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
				Role:  message.Tool,
				Parts: []message.ContentPart{},
			})
			if createErr != nil {
				return nil, fmt.Errorf("failed to create tool result message: %w", createErr)
			}
			toolResults = &trMsg
		}
		toolResults.AddToolResult(message.ToolResult{
			ToolCallID: tc.ID,
			Content:    result.Content,
		})
		return toolResults, nil
	}

	if toolResults == nil {
		trMsg, createErr := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
			Role:  message.Tool,
			Parts: []message.ContentPart{},
		})
		if createErr != nil {
			return nil, fmt.Errorf("failed to create tool result message: %w", createErr)
		}
		toolResults = &trMsg
	}
	toolResults.AddToolResult(message.ToolResult{
		ToolCallID: tc.ID,
		Content:    fmt.Sprintf("Error: tool not found: %s", tc.Name),
		IsError:    true,
	})
	return toolResults, nil
}

func routeDiagnosticIntent(recipe *diagnose.Recipe) *message.ToolCall {
	if recipe == nil || recipe.InitialCommand == "" {
		return nil
	}

	return newBashToolCall(string(recipe.Name), recipe.InitialCommand)
}

func newBashToolCall(label string, command string) *message.ToolCall {
	input, err := json.Marshal(map[string]any{
		"command": command,
	})
	if err != nil {
		return nil
	}

	return &message.ToolCall{
		ID:       fmt.Sprintf("%s_%d", label, time.Now().UnixNano()),
		Name:     tools.BashToolName,
		Input:    string(input),
		Type:     "function",
		Finished: true,
	}
}
