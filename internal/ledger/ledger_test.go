package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenAndClose(t *testing.T) {
	dir := t.TempDir()

	l, err := Open(dir, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if l.SessionID() == "" {
		t.Fatal("expected non-empty session ID")
	}

	// Verify sessions dir created
	if _, err := os.Stat(filepath.Join(dir, "sessions")); err != nil {
		t.Fatalf("sessions dir not created: %v", err)
	}

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify session file was written
	files, err := os.ReadDir(filepath.Join(dir, "sessions"))
	if err != nil {
		t.Fatalf("read sessions dir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 session file, got %d", len(files))
	}
	if filepath.Ext(files[0].Name()) != ".json" {
		t.Fatalf("expected .json file, got %s", files[0].Name())
	}
}

func TestOpenWithMemory(t *testing.T) {
	dir := t.TempDir()

	l, err := Open(dir, true)
	if err != nil {
		t.Fatalf("Open with memory: %v", err)
	}

	// Verify memory dir created
	if _, err := os.Stat(filepath.Join(dir, "memory")); err != nil {
		t.Fatalf("memory dir not created: %v", err)
	}

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestRecordRequestAndSummary(t *testing.T) {
	dir := t.TempDir()

	l, err := Open(dir, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	now := time.Now().UTC()
	l.RecordRequest(RequestEntry{
		Timestamp:    now,
		TokenHash:    "abc123def456",
		Model:        "gpt-4",
		Provider:     "openai",
		InputTokens:  100,
		OutputTokens: 50,
		DurationMs:   200,
		StatusCode:   200,
	})

	l.RecordRequest(RequestEntry{
		Timestamp:    now.Add(time.Second),
		TokenHash:    "abc123def456",
		Model:        "gpt-4",
		Provider:     "openai",
		InputTokens:  200,
		OutputTokens: 100,
		DurationMs:   300,
		StatusCode:   200,
		ProviderMeta: &ProviderMeta{
			CachedInputTokens: 50,
			ReasoningTokens:   20,
			ActualModel:       "gpt-4-0613",
			FinishReason:      "stop",
		},
	})

	l.RecordRequest(RequestEntry{
		Timestamp:    now.Add(2 * time.Second),
		TokenHash:    "xyz789",
		Model:        "claude-3-opus",
		Provider:     "anthropic",
		InputTokens:  150,
		OutputTokens: 75,
		DurationMs:   400,
		StatusCode:   200,
	})

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read the session file
	sessions, err := ReadSessionFiles(dir)
	if err != nil {
		t.Fatalf("ReadSessionFiles: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	s := sessions[0]
	if s.SessionID != l.SessionID() {
		t.Errorf("session ID mismatch: %q vs %q", s.SessionID, l.SessionID())
	}

	// Check totals
	if s.Totals.RequestCount != 3 {
		t.Errorf("expected 3 requests, got %d", s.Totals.RequestCount)
	}
	if s.Totals.InputTokens != 450 {
		t.Errorf("expected 450 input tokens, got %d", s.Totals.InputTokens)
	}
	if s.Totals.OutputTokens != 225 {
		t.Errorf("expected 225 output tokens, got %d", s.Totals.OutputTokens)
	}
	if s.Totals.TotalTokens != 675 {
		t.Errorf("expected 675 total tokens, got %d", s.Totals.TotalTokens)
	}
	if s.Totals.CachedInputTokens != 50 {
		t.Errorf("expected 50 cached input tokens, got %d", s.Totals.CachedInputTokens)
	}
	if s.Totals.ReasoningTokens != 20 {
		t.Errorf("expected 20 reasoning tokens, got %d", s.Totals.ReasoningTokens)
	}

	// Check by model
	if len(s.ByModel) != 2 {
		t.Errorf("expected 2 models, got %d", len(s.ByModel))
	}
	gpt4 := s.ByModel["gpt-4"]
	if gpt4 == nil {
		t.Fatal("missing gpt-4 rollup")
	}
	if gpt4.RequestCount != 2 {
		t.Errorf("gpt-4: expected 2 requests, got %d", gpt4.RequestCount)
	}

	// Check by provider
	if len(s.ByProvider) != 2 {
		t.Errorf("expected 2 providers, got %d", len(s.ByProvider))
	}

	// Check by token
	if len(s.ByToken) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(s.ByToken))
	}
	tokenRollup := s.ByToken["abc123def456"]
	if tokenRollup == nil {
		t.Fatal("missing token rollup for abc123def456")
	}
	if tokenRollup.RequestCount != 2 {
		t.Errorf("token abc123: expected 2 requests, got %d", tokenRollup.RequestCount)
	}
	if len(tokenRollup.ModelsUsed) != 1 {
		t.Errorf("expected 1 model used, got %d", len(tokenRollup.ModelsUsed))
	}

	// Verify requests are stored
	if len(s.Requests) != 3 {
		t.Errorf("expected 3 requests in session, got %d", len(s.Requests))
	}
}

func TestErrorAndRetryCounters(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	l.RecordRequest(RequestEntry{
		Timestamp:   time.Now().UTC(),
		TokenHash:   "tok1",
		Model:       "gpt-4",
		StatusCode:  429,
		RetryCount:  2,
		RuleMatches: []RuleMatchEntry{{Action: "fail", Message: "blocked"}},
	})

	l.RecordRequest(RequestEntry{
		Timestamp:  time.Now().UTC(),
		TokenHash:  "tok1",
		Model:      "gpt-4",
		StatusCode: 500,
		Error:      "internal error",
	})

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sessions, _ := ReadSessionFiles(dir)
	s := sessions[0]

	if s.Totals.ErrorCount != 2 {
		t.Errorf("expected 2 errors, got %d", s.Totals.ErrorCount)
	}
	if s.Totals.RateLimitCount != 1 {
		t.Errorf("expected 1 rate limit, got %d", s.Totals.RateLimitCount)
	}
	if s.Totals.RetryCount != 2 {
		t.Errorf("expected 2 retries, got %d", s.Totals.RetryCount)
	}
	if s.Totals.RuleViolationCount != 1 {
		t.Errorf("expected 1 rule violation, got %d", s.Totals.RuleViolationCount)
	}
}

func TestReadSessionFilesEmpty(t *testing.T) {
	dir := t.TempDir()

	sessions, err := ReadSessionFiles(dir)
	if err != nil {
		t.Fatalf("ReadSessionFiles: %v", err)
	}
	if sessions != nil {
		t.Errorf("expected nil sessions for missing dir, got %v", sessions)
	}
}

func TestSessionSummaryJSON(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	l.RecordRequest(RequestEntry{
		Timestamp:    time.Now().UTC(),
		TokenHash:    "tok1",
		Model:        "gpt-4",
		Provider:     "openai",
		InputTokens:  100,
		OutputTokens: 50,
		StatusCode:   200,
		ProviderMeta: &ProviderMeta{
			ActualModel:  "gpt-4-0613",
			FinishReason: "stop",
		},
	})

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sessions, _ := ReadSessionFiles(dir)
	s := sessions[0]

	// Verify it round-trips through JSON
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SessionSummary
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Totals.RequestCount != 1 {
		t.Errorf("expected 1 request after round-trip, got %d", decoded.Totals.RequestCount)
	}
	if len(decoded.Requests) != 1 {
		t.Fatalf("expected 1 request entry after round-trip, got %d", len(decoded.Requests))
	}
	if decoded.Requests[0].ProviderMeta == nil {
		t.Fatal("expected provider_meta after round-trip")
	}
	if decoded.Requests[0].ProviderMeta.ActualModel != "gpt-4-0613" {
		t.Errorf("expected actual_model gpt-4-0613, got %s", decoded.Requests[0].ProviderMeta.ActualModel)
	}
}

func TestGenerateSessionID(t *testing.T) {
	id1 := generateSessionID()
	id2 := generateSessionID()
	if id1 == "" {
		t.Fatal("empty session ID")
	}
	if id1 == id2 {
		t.Error("session IDs should be unique")
	}
	if len(id1) != 8 {
		t.Errorf("expected 8 char hex ID, got %d chars: %s", len(id1), id1)
	}
}
