package provider

import (
	"context"

	"github.com/AIdoesmyjob/termfix/internal/llm/tools"
	"github.com/AIdoesmyjob/termfix/internal/message"
)

// MockClient implements ProviderClient for testing purposes.
// It returns configurable responses without making any API calls.
type MockClient struct {
	providerOptions providerClientOptions
	Response        *ProviderResponse
	Events          []ProviderEvent
	SendErr         error
}

func newMockClient(opts providerClientOptions) MockClient {
	return MockClient{
		providerOptions: opts,
		Response: &ProviderResponse{
			Content:      "mock response",
			FinishReason: message.FinishReasonEndTurn,
			Usage:        TokenUsage{InputTokens: 10, OutputTokens: 5},
		},
	}
}

func (m MockClient) send(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*ProviderResponse, error) {
	if m.SendErr != nil {
		return nil, m.SendErr
	}
	return m.Response, nil
}

func (m MockClient) stream(ctx context.Context, messages []message.Message, tools []tools.BaseTool) <-chan ProviderEvent {
	ch := make(chan ProviderEvent)
	go func() {
		defer close(ch)
		if len(m.Events) > 0 {
			for _, event := range m.Events {
				select {
				case <-ctx.Done():
					ch <- ProviderEvent{Type: EventError, Error: ctx.Err()}
					return
				case ch <- event:
				}
			}
			return
		}
		// Default: send content delta + complete event
		ch <- ProviderEvent{
			Type:    EventContentDelta,
			Content: m.Response.Content,
		}
		ch <- ProviderEvent{
			Type:     EventComplete,
			Response: m.Response,
		}
	}()
	return ch
}
