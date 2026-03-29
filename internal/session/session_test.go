package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionStruct(t *testing.T) {
	s := Session{
		ID:    "test-id",
		Title: "Test Session",
		Cost:  1.5,
	}
	assert.Equal(t, "test-id", s.ID)
	assert.Equal(t, "Test Session", s.Title)
	assert.Equal(t, 1.5, s.Cost)
	assert.Empty(t, s.ParentSessionID)
	assert.Empty(t, s.SummaryMessageID)
	assert.Zero(t, s.MessageCount)
	assert.Zero(t, s.PromptTokens)
	assert.Zero(t, s.CompletionTokens)
}

func TestNewService(t *testing.T) {
	// Verify NewService doesn't panic with nil (it shouldn't, just stores the reference)
	assert.NotPanics(t, func() {
		svc := NewService(nil)
		assert.NotNil(t, svc)
	})
}
