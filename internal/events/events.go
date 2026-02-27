package events

import (
	"context"
	"encoding/json"
	"time"
)

// Event types emitted by Tokenomics.
const (
	// Token lifecycle
	TokenCreated = "token.created"
	TokenUpdated = "token.updated"
	TokenDeleted = "token.deleted"
	TokenExpired = "token.expired"

	// Policy rule matches
	RuleViolation = "rule.violation"
	RuleWarning   = "rule.warning"
	RuleMatch     = "rule.match"
	RuleMask      = "rule.mask"

	// Budget and rate limiting
	BudgetExceeded = "budget.exceeded"
	BudgetUpdate   = "budget.update"
	RateExceeded   = "rate.exceeded"

	// Request lifecycle
	RequestCompleted = "request.completed"

	// OpenClaw integration
	OpenClawAgentRequest  = "openclaw.agent.request"
	OpenClawAgentSuccess  = "openclaw.agent.success"
	OpenClawAgentError    = "openclaw.agent.error"
	OpenClawRuleViolation = "openclaw.rule.violation"

	// System
	ServerStart = "server.start"
)

// Event is the payload delivered to emitters.
type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Timestamp string                 `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// New creates an Event with a generated ID and current timestamp.
func New(eventType string, data map[string]interface{}) Event {
	return Event{
		ID:        generateID(),
		Type:      eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Data:      data,
	}
}

// JSON returns the event as a JSON byte slice.
func (e Event) JSON() []byte {
	b, _ := json.Marshal(e)
	return b
}

// Emitter is the interface for delivering events to external systems.
// Implementations include webhooks, message buses, log sinks, etc.
type Emitter interface {
	// Emit delivers an event. Implementations should be non-blocking
	// or use their own internal queue to avoid slowing the caller.
	Emit(ctx context.Context, event Event) error

	// Close flushes pending events and releases resources.
	Close() error
}
