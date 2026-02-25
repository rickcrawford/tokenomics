package events

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWebhookEmitter_Deliver(t *testing.T) {
	var mu sync.Mutex
	var received []Event

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var evt Event
		json.Unmarshal(body, &evt)
		mu.Lock()
		received = append(received, evt)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	emitter := NewWebhookEmitter(WebhookConfig{
		URL:        ts.URL,
		TimeoutSec: 5,
	})

	emitter.Emit(context.Background(), New(TokenCreated, map[string]interface{}{
		"token_hash": "abc123",
	}))

	emitter.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].Type != TokenCreated {
		t.Errorf("type = %q, want %q", received[0].Type, TokenCreated)
	}
	data := received[0].Data
	if data["token_hash"] != "abc123" {
		t.Errorf("token_hash = %v, want abc123", data["token_hash"])
	}
}

func TestWebhookEmitter_SharedSecret(t *testing.T) {
	var gotSecret string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get("X-Webhook-Secret")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	emitter := NewWebhookEmitter(WebhookConfig{
		URL:    ts.URL,
		Secret: "my-secret-123",
	})

	emitter.Emit(context.Background(), New(TokenDeleted, nil))
	emitter.Close()

	if gotSecret != "my-secret-123" {
		t.Errorf("X-Webhook-Secret = %q, want %q", gotSecret, "my-secret-123")
	}
}

func TestWebhookEmitter_HMACSigning(t *testing.T) {
	signingKey := "hmac-key-456"
	var gotSignature string
	var gotBody []byte

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSignature = r.Header.Get("X-Webhook-Signature")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	emitter := NewWebhookEmitter(WebhookConfig{
		URL:        ts.URL,
		SigningKey:  signingKey,
	})

	emitter.Emit(context.Background(), New(RuleViolation, map[string]interface{}{
		"rule_name": "test-rule",
	}))
	emitter.Close()

	if gotSignature == "" {
		t.Fatal("expected X-Webhook-Signature header")
	}
	if !strings.HasPrefix(gotSignature, "sha256=") {
		t.Fatalf("signature should start with sha256=, got %q", gotSignature)
	}

	// Verify the HMAC
	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write(gotBody)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSignature != expected {
		t.Errorf("signature mismatch: got %q, want %q", gotSignature, expected)
	}
}

func TestWebhookEmitter_EventFilter(t *testing.T) {
	var mu sync.Mutex
	var receivedTypes []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedTypes = append(receivedTypes, r.Header.Get("X-Event-Type"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	emitter := NewWebhookEmitter(WebhookConfig{
		URL:    ts.URL,
		Events: []string{"token.*", "rule.violation"},
	})

	emitter.Emit(context.Background(), New(TokenCreated, nil))    // matches token.*
	emitter.Emit(context.Background(), New(TokenDeleted, nil))    // matches token.*
	emitter.Emit(context.Background(), New(RuleViolation, nil))   // matches rule.violation exactly
	emitter.Emit(context.Background(), New(RuleWarning, nil))     // does NOT match
	emitter.Emit(context.Background(), New(BudgetExceeded, nil))  // does NOT match
	emitter.Emit(context.Background(), New(RequestCompleted, nil)) // does NOT match

	emitter.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(receivedTypes) != 3 {
		t.Fatalf("expected 3 events, got %d: %v", len(receivedTypes), receivedTypes)
	}
}

func TestWebhookEmitter_WildcardFilterAll(t *testing.T) {
	var count int
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Empty events filter = everything
	emitter := NewWebhookEmitter(WebhookConfig{
		URL: ts.URL,
	})

	emitter.Emit(context.Background(), New(TokenCreated, nil))
	emitter.Emit(context.Background(), New(RuleViolation, nil))
	emitter.Emit(context.Background(), New(ServerStart, nil))

	emitter.Close()

	mu.Lock()
	defer mu.Unlock()
	if count != 3 {
		t.Fatalf("expected 3 events (no filter), got %d", count)
	}
}

func TestWebhookEmitter_Headers(t *testing.T) {
	var gotEventID, gotEventType, gotContentType, gotUserAgent string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEventID = r.Header.Get("X-Event-ID")
		gotEventType = r.Header.Get("X-Event-Type")
		gotContentType = r.Header.Get("Content-Type")
		gotUserAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	emitter := NewWebhookEmitter(WebhookConfig{URL: ts.URL})
	emitter.Emit(context.Background(), New(ServerStart, nil))
	emitter.Close()

	if !strings.HasPrefix(gotEventID, "evt_") {
		t.Errorf("X-Event-ID = %q, want evt_ prefix", gotEventID)
	}
	if gotEventType != ServerStart {
		t.Errorf("X-Event-Type = %q, want %q", gotEventType, ServerStart)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotUserAgent != "Tokenomics-Webhook/1.0" {
		t.Errorf("User-Agent = %q, want Tokenomics-Webhook/1.0", gotUserAgent)
	}
}

func TestWebhookEmitter_Non2xxRetries(t *testing.T) {
	var attempts int
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	emitter := NewWebhookEmitter(WebhookConfig{
		URL:        ts.URL,
		TimeoutSec: 2,
	})

	emitter.Emit(context.Background(), New(TokenCreated, nil))
	emitter.Close()

	mu.Lock()
	defer mu.Unlock()
	if attempts < 2 {
		t.Errorf("expected at least 2 retry attempts, got %d", attempts)
	}
}

func TestWebhookEmitter_4xxNoRetry(t *testing.T) {
	var attempts int
	var mu sync.Mutex

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer ts.Close()

	emitter := NewWebhookEmitter(WebhookConfig{URL: ts.URL})
	emitter.Emit(context.Background(), New(TokenCreated, nil))
	emitter.Close()

	mu.Lock()
	defer mu.Unlock()
	if attempts != 1 {
		t.Errorf("expected 1 attempt (no retry for 400), got %d", attempts)
	}
}

func TestMultiEmitter(t *testing.T) {
	var mu sync.Mutex
	var counts [2]int

	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts[0]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts1.Close()

	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts[1]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts2.Close()

	emitter := NewMulti(
		NewWebhookEmitter(WebhookConfig{URL: ts1.URL}),
		NewWebhookEmitter(WebhookConfig{URL: ts2.URL}),
	)

	emitter.Emit(context.Background(), New(TokenCreated, nil))
	emitter.Close()

	mu.Lock()
	defer mu.Unlock()
	if counts[0] != 1 || counts[1] != 1 {
		t.Errorf("expected [1,1], got %v", counts)
	}
}

func TestNopEmitter(t *testing.T) {
	nop := Nop{}
	if err := nop.Emit(context.Background(), New(TokenCreated, nil)); err != nil {
		t.Errorf("Nop.Emit returned error: %v", err)
	}
	if err := nop.Close(); err != nil {
		t.Errorf("Nop.Close returned error: %v", err)
	}
}

func TestEventNew(t *testing.T) {
	evt := New(TokenCreated, map[string]interface{}{"key": "value"})

	if evt.Type != TokenCreated {
		t.Errorf("type = %q, want %q", evt.Type, TokenCreated)
	}
	if !strings.HasPrefix(evt.ID, "evt_") {
		t.Errorf("ID = %q, want evt_ prefix", evt.ID)
	}
	if evt.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}
	if _, err := time.Parse(time.RFC3339Nano, evt.Timestamp); err != nil {
		t.Errorf("invalid timestamp format: %v", err)
	}
	if evt.Data["key"] != "value" {
		t.Errorf("data[key] = %v, want value", evt.Data["key"])
	}
}

func TestEventJSON(t *testing.T) {
	evt := New(RuleViolation, map[string]interface{}{"rule": "test"})
	b := evt.JSON()

	var parsed map[string]interface{}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["type"] != RuleViolation {
		t.Errorf("type = %v, want %q", parsed["type"], RuleViolation)
	}
}

func TestMatchesFilter(t *testing.T) {
	tests := []struct {
		name      string
		events    []string
		eventType string
		want      bool
	}{
		{"empty filter matches all", nil, "anything", true},
		{"exact match", []string{"token.created"}, "token.created", true},
		{"exact no match", []string{"token.created"}, "token.deleted", false},
		{"wildcard match", []string{"token.*"}, "token.created", true},
		{"wildcard match deleted", []string{"token.*"}, "token.deleted", true},
		{"wildcard no match", []string{"token.*"}, "rule.violation", false},
		{"multiple patterns", []string{"token.*", "rule.*"}, "rule.violation", true},
		{"star alone matches everything", []string{"*"}, "anything.here", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &WebhookEmitter{cfg: WebhookConfig{Events: tt.events}}
			got := w.matchesFilter(tt.eventType)
			if got != tt.want {
				t.Errorf("matchesFilter(%q) = %v, want %v", tt.eventType, got, tt.want)
			}
		})
	}
}

// Benchmarks

func BenchmarkEventNew(b *testing.B) {
	data := map[string]interface{}{"token_hash": "abc123", "model": "gpt-4"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		New(TokenCreated, data)
	}
}

func BenchmarkEventJSON(b *testing.B) {
	evt := New(TokenCreated, map[string]interface{}{"token_hash": "abc123", "model": "gpt-4"})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evt.JSON()
	}
}

func BenchmarkNopEmit(b *testing.B) {
	nop := Nop{}
	ctx := context.Background()
	evt := New(TokenCreated, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nop.Emit(ctx, evt)
	}
}

func BenchmarkWebhookEmit_Filtered(b *testing.B) {
	// Measures Emit overhead when event is filtered out (no network)
	emitter := &WebhookEmitter{
		cfg:   WebhookConfig{Events: []string{"token.*"}},
		queue: make(chan Event, 256),
		done:  make(chan struct{}),
	}
	ctx := context.Background()
	evt := New(RuleViolation, nil) // won't match token.* filter
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		emitter.Emit(ctx, evt)
	}
}

func BenchmarkWebhookEmit_Queued(b *testing.B) {
	// Measures Emit overhead for queuing (no actual delivery)
	emitter := &WebhookEmitter{
		cfg:   WebhookConfig{},
		queue: make(chan Event, b.N+1),
		done:  make(chan struct{}),
	}
	ctx := context.Background()
	evt := New(TokenCreated, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		emitter.Emit(ctx, evt)
	}
}

func BenchmarkMatchesFilter(b *testing.B) {
	w := &WebhookEmitter{cfg: WebhookConfig{Events: []string{"token.*", "rule.*", "budget.*"}}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.matchesFilter("rule.violation")
	}
}

func BenchmarkSign(b *testing.B) {
	payload := []byte(`{"type":"token.created","data":{"token_hash":"abc123"}}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sign(payload, "my-signing-key")
	}
}
