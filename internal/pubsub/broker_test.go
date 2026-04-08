package pubsub

import (
	"context"
	"testing"
	"time"
)

func TestNewBroker(t *testing.T) {
	b := NewBroker[string]()
	if b == nil {
		t.Fatal("NewBroker() returned nil")
	}
	if b.GetSubscriberCount() != 0 {
		t.Errorf("new broker subscriber count = %d, want 0", b.GetSubscriberCount())
	}
}

func TestNewBrokerWithOptions(t *testing.T) {
	b := NewBrokerWithOptions[string](32, 500)
	if b == nil {
		t.Fatal("NewBrokerWithOptions() returned nil")
	}
	if b.maxEvents != 500 {
		t.Errorf("maxEvents = %d, want 500", b.maxEvents)
	}
}

func TestSubscribeAndPublish(t *testing.T) {
	tests := []struct {
		name      string
		eventType EventType
		payload   string
	}{
		{name: "created event", eventType: CreatedEvent, payload: "item1"},
		{name: "updated event", eventType: UpdatedEvent, payload: "item2"},
		{name: "deleted event", eventType: DeletedEvent, payload: "item3"},
		{name: "empty payload", eventType: CreatedEvent, payload: ""},
		{name: "custom event type", eventType: EventType("custom"), payload: "custom_data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBroker[string]()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			ch := b.Subscribe(ctx)

			b.Publish(tt.eventType, tt.payload)

			select {
			case event := <-ch:
				if event.Type != tt.eventType {
					t.Errorf("event type = %v, want %v", event.Type, tt.eventType)
				}
				if event.Payload != tt.payload {
					t.Errorf("event payload = %v, want %v", event.Payload, tt.payload)
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for event")
			}
		})
	}
}

func TestSubscriberCount(t *testing.T) {
	b := NewBroker[string]()

	if got := b.GetSubscriberCount(); got != 0 {
		t.Fatalf("initial subscriber count = %d, want 0", got)
	}

	ctx1, cancel1 := context.WithCancel(context.Background())
	b.Subscribe(ctx1)

	if got := b.GetSubscriberCount(); got != 1 {
		t.Fatalf("after 1 subscribe, count = %d, want 1", got)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	b.Subscribe(ctx2)

	if got := b.GetSubscriberCount(); got != 2 {
		t.Fatalf("after 2 subscribes, count = %d, want 2", got)
	}

	// Cancel first subscriber and wait for goroutine to clean up
	cancel1()
	time.Sleep(50 * time.Millisecond)

	if got := b.GetSubscriberCount(); got != 1 {
		t.Errorf("after canceling 1 subscriber, count = %d, want 1", got)
	}

	cancel2()
	time.Sleep(50 * time.Millisecond)

	if got := b.GetSubscriberCount(); got != 0 {
		t.Errorf("after canceling all subscribers, count = %d, want 0", got)
	}
}

func TestMultipleSubscribers(t *testing.T) {
	b := NewBroker[int]()

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	ch1 := b.Subscribe(ctx1)
	ch2 := b.Subscribe(ctx2)

	b.Publish(CreatedEvent, 42)

	// Both subscribers should receive the event
	for i, ch := range []<-chan Event[int]{ch1, ch2} {
		select {
		case event := <-ch:
			if event.Payload != 42 {
				t.Errorf("subscriber %d: payload = %d, want 42", i, event.Payload)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for event", i)
		}
	}
}

func TestPublishToNoSubscribers(t *testing.T) {
	b := NewBroker[string]()
	// Should not panic
	b.Publish(CreatedEvent, "no one listening")
}

func TestShutdown(t *testing.T) {
	b := NewBroker[string]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := b.Subscribe(ctx)

	if got := b.GetSubscriberCount(); got != 1 {
		t.Fatalf("before shutdown, count = %d, want 1", got)
	}

	b.Shutdown()

	if got := b.GetSubscriberCount(); got != 0 {
		t.Errorf("after shutdown, count = %d, want 0", got)
	}

	// Channel should be closed after shutdown
	select {
	case _, ok := <-ch:
		if ok {
			// Draining buffered events is fine; keep draining
			for range ch {
			}
		}
		// Channel is closed, as expected
	case <-time.After(time.Second):
		t.Error("channel was not closed after shutdown")
	}
}

func TestShutdownIdempotent(t *testing.T) {
	b := NewBroker[string]()
	// Multiple shutdowns should not panic
	b.Shutdown()
	b.Shutdown()
	b.Shutdown()
}

func TestPublishAfterShutdown(t *testing.T) {
	b := NewBroker[string]()
	b.Shutdown()
	// Should not panic
	b.Publish(CreatedEvent, "after shutdown")
}

func TestSubscribeAfterShutdown(t *testing.T) {
	b := NewBroker[string]()
	b.Shutdown()

	ctx := context.Background()
	ch := b.Subscribe(ctx)

	// Channel returned after shutdown should be immediately closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after subscribing to shut-down broker")
		}
	case <-time.After(time.Second):
		t.Error("timed out; channel should be immediately closed")
	}
}

func TestSubscriberCancelDuringShutdown(t *testing.T) {
	b := NewBroker[string]()

	ctx, cancel := context.WithCancel(context.Background())
	b.Subscribe(ctx)

	// Shutdown and cancel concurrently should not race
	go cancel()
	b.Shutdown()

	// Allow goroutines to settle
	time.Sleep(50 * time.Millisecond)
}

func TestPublishDropsWhenBufferFull(t *testing.T) {
	// Create a broker with a small buffer (uses default bufferSize=64)
	b := NewBroker[int]()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := b.Subscribe(ctx)

	// Publish more events than the buffer can hold without reading
	for i := 0; i < bufferSize+10; i++ {
		b.Publish(CreatedEvent, i)
	}

	// Should be able to read up to bufferSize events
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count != bufferSize {
		t.Errorf("received %d events, want %d (buffer size)", count, bufferSize)
	}
}

func TestBrokerWithStructPayload(t *testing.T) {
	type Item struct {
		Name  string
		Value int
	}

	b := NewBroker[Item]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := b.Subscribe(ctx)

	expected := Item{Name: "test", Value: 99}
	b.Publish(CreatedEvent, expected)

	select {
	case event := <-ch:
		if event.Payload.Name != expected.Name || event.Payload.Value != expected.Value {
			t.Errorf("payload = %+v, want %+v", event.Payload, expected)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for struct event")
	}
}
