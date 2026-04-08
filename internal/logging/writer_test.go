package logging

import (
	"context"
	"testing"
	"time"

	"github.com/AIdoesmyjob/termfix/internal/pubsub"
)

// newTestLogData creates a fresh LogData for isolated testing.
func newTestLogData() *LogData {
	return &LogData{
		messages: make([]LogMessage, 0),
		Broker:   pubsub.NewBroker[LogMessage](),
	}
}

func TestLogDataAddAndList(t *testing.T) {
	tests := []struct {
		name     string
		messages []LogMessage
		wantLen  int
	}{
		{
			name:     "empty log data",
			messages: nil,
			wantLen:  0,
		},
		{
			name: "single message",
			messages: []LogMessage{
				{ID: "1", Level: "info", Message: "hello"},
			},
			wantLen: 1,
		},
		{
			name: "multiple messages",
			messages: []LogMessage{
				{ID: "1", Level: "info", Message: "first"},
				{ID: "2", Level: "debug", Message: "second"},
				{ID: "3", Level: "error", Message: "third"},
			},
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ld := newTestLogData()
			for _, msg := range tt.messages {
				ld.Add(msg)
			}

			got := ld.List()
			if len(got) != tt.wantLen {
				t.Fatalf("List() returned %d messages, want %d", len(got), tt.wantLen)
			}

			// Verify messages are in insertion order
			for i, msg := range tt.messages {
				if got[i].ID != msg.ID {
					t.Errorf("List()[%d].ID = %q, want %q", i, got[i].ID, msg.ID)
				}
				if got[i].Message != msg.Message {
					t.Errorf("List()[%d].Message = %q, want %q", i, got[i].Message, msg.Message)
				}
			}
		})
	}
}

func TestLogDataAddPublishesEvent(t *testing.T) {
	ld := newTestLogData()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := ld.Subscribe(ctx)

	msg := LogMessage{ID: "test-1", Level: "info", Message: "published"}
	ld.Add(msg)

	select {
	case event := <-ch:
		if event.Type != pubsub.CreatedEvent {
			t.Errorf("event type = %q, want %q", event.Type, pubsub.CreatedEvent)
		}
		if event.Payload.ID != msg.ID {
			t.Errorf("event payload ID = %q, want %q", event.Payload.ID, msg.ID)
		}
		if event.Payload.Message != msg.Message {
			t.Errorf("event payload Message = %q, want %q", event.Payload.Message, msg.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for published event")
	}
}

func TestLogDataListReturnsSnapshot(t *testing.T) {
	ld := newTestLogData()
	ld.Add(LogMessage{ID: "1", Message: "first"})

	list1 := ld.List()
	if len(list1) != 1 {
		t.Fatalf("first List() returned %d, want 1", len(list1))
	}

	ld.Add(LogMessage{ID: "2", Message: "second"})

	list2 := ld.List()
	if len(list2) != 2 {
		t.Fatalf("second List() returned %d, want 2", len(list2))
	}
}

func TestWriterParseLogfmt(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantLevel     string
		wantMessage   string
		wantPersist   bool
		wantAttrCount int
		wantErr       bool
	}{
		{
			name:        "basic info log",
			input:       `time=2024-01-15T10:30:00Z level=INFO msg="hello world"` + "\n",
			wantLevel:   "info",
			wantMessage: "hello world",
		},
		{
			name:        "debug log",
			input:       `time=2024-01-15T10:30:00Z level=DEBUG msg="debug message"` + "\n",
			wantLevel:   "debug",
			wantMessage: "debug message",
		},
		{
			name:        "error log",
			input:       `time=2024-01-15T10:30:00Z level=ERROR msg="something failed"` + "\n",
			wantLevel:   "error",
			wantMessage: "something failed",
		},
		{
			name:        "warn log",
			input:       `time=2024-01-15T10:30:00Z level=WARN msg="watch out"` + "\n",
			wantLevel:   "warn",
			wantMessage: "watch out",
		},
		{
			name:          "log with extra attributes",
			input:         `time=2024-01-15T10:30:00Z level=INFO msg="with attrs" source=main.go:42 key=value` + "\n",
			wantLevel:     "info",
			wantMessage:   "with attrs",
			wantAttrCount: 2,
		},
		{
			name:        "log with persist flag",
			input:       `time=2024-01-15T10:30:00Z level=INFO msg="persistent" $_persist=true` + "\n",
			wantLevel:   "info",
			wantMessage: "persistent",
			wantPersist: true,
		},
		{
			name:        "empty message",
			input:       `time=2024-01-15T10:30:00Z level=INFO msg=""` + "\n",
			wantLevel:   "info",
			wantMessage: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset the global defaultLogData for each test
			origLogData := defaultLogData
			defaultLogData = newTestLogData()
			defer func() { defaultLogData = origLogData }()

			w := NewWriter()
			n, err := w.Write([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("Write() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			if n != len(tt.input) {
				t.Errorf("Write() returned n = %d, want %d", n, len(tt.input))
			}

			messages := defaultLogData.List()
			if len(messages) != 1 {
				t.Fatalf("expected 1 message, got %d", len(messages))
			}

			msg := messages[0]
			if msg.Level != tt.wantLevel {
				t.Errorf("Level = %q, want %q", msg.Level, tt.wantLevel)
			}
			if msg.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", msg.Message, tt.wantMessage)
			}
			if msg.Persist != tt.wantPersist {
				t.Errorf("Persist = %v, want %v", msg.Persist, tt.wantPersist)
			}
			if tt.wantAttrCount > 0 && len(msg.Attributes) != tt.wantAttrCount {
				t.Errorf("Attributes count = %d, want %d, attrs: %+v", len(msg.Attributes), tt.wantAttrCount, msg.Attributes)
			}

			// Verify time was parsed
			if msg.Time.IsZero() {
				t.Error("Time should not be zero")
			}

			// Verify ID was generated
			if msg.ID == "" {
				t.Error("ID should not be empty")
			}
		})
	}
}

func TestWriterParseTimestamp(t *testing.T) {
	origLogData := defaultLogData
	defaultLogData = newTestLogData()
	defer func() { defaultLogData = origLogData }()

	w := NewWriter()
	input := `time=2024-06-15T14:30:45Z level=INFO msg="test"` + "\n"
	_, err := w.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	messages := defaultLogData.List()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	msg := messages[0]
	expectedTime := time.Date(2024, 6, 15, 14, 30, 45, 0, time.UTC)
	if !msg.Time.Equal(expectedTime) {
		t.Errorf("parsed time = %v, want %v", msg.Time, expectedTime)
	}
}

func TestWriterParsePersistTime(t *testing.T) {
	origLogData := defaultLogData
	defaultLogData = newTestLogData()
	defer func() { defaultLogData = origLogData }()

	w := NewWriter()
	input := `time=2024-01-15T10:30:00Z level=INFO msg="timed" $_persist_time=5s` + "\n"
	_, err := w.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	messages := defaultLogData.List()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	if messages[0].PersistTime != 5*time.Second {
		t.Errorf("PersistTime = %v, want %v", messages[0].PersistTime, 5*time.Second)
	}
}

func TestWriterMultipleRecords(t *testing.T) {
	origLogData := defaultLogData
	defaultLogData = newTestLogData()
	defer func() { defaultLogData = origLogData }()

	w := NewWriter()
	input := `time=2024-01-15T10:30:00Z level=INFO msg="first"` + "\n" +
		`time=2024-01-15T10:30:01Z level=DEBUG msg="second"` + "\n"
	_, err := w.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	messages := defaultLogData.List()
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	if messages[0].Message != "first" {
		t.Errorf("first message = %q, want %q", messages[0].Message, "first")
	}
	if messages[1].Message != "second" {
		t.Errorf("second message = %q, want %q", messages[1].Message, "second")
	}
}

func TestWriterEmptyInput(t *testing.T) {
	origLogData := defaultLogData
	defaultLogData = newTestLogData()
	defer func() { defaultLogData = origLogData }()

	w := NewWriter()
	n, err := w.Write([]byte{})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 0 {
		t.Errorf("Write() returned n = %d, want 0", n)
	}

	messages := defaultLogData.List()
	if len(messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(messages))
	}
}

func TestWriterInvalidTime(t *testing.T) {
	origLogData := defaultLogData
	defaultLogData = newTestLogData()
	defer func() { defaultLogData = origLogData }()

	w := NewWriter()
	input := `time=not-a-time level=INFO msg="bad time"` + "\n"
	_, err := w.Write([]byte(input))
	if err == nil {
		t.Fatal("Write() expected error for invalid time, got nil")
	}
}

func TestWriterAttributes(t *testing.T) {
	origLogData := defaultLogData
	defaultLogData = newTestLogData()
	defer func() { defaultLogData = origLogData }()

	w := NewWriter()
	input := `time=2024-01-15T10:30:00Z level=INFO msg="attrs" source=main.go:10 request_id=abc-123 user=admin` + "\n"
	_, err := w.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	messages := defaultLogData.List()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}

	msg := messages[0]
	if len(msg.Attributes) != 3 {
		t.Fatalf("expected 3 attributes, got %d: %+v", len(msg.Attributes), msg.Attributes)
	}

	// Check attributes are collected in order
	wantAttrs := []Attr{
		{Key: "source", Value: "main.go:10"},
		{Key: "request_id", Value: "abc-123"},
		{Key: "user", Value: "admin"},
	}
	for i, attr := range wantAttrs {
		if msg.Attributes[i].Key != attr.Key {
			t.Errorf("Attributes[%d].Key = %q, want %q", i, msg.Attributes[i].Key, attr.Key)
		}
		if msg.Attributes[i].Value != attr.Value {
			t.Errorf("Attributes[%d].Value = %q, want %q", i, msg.Attributes[i].Value, attr.Value)
		}
	}
}
