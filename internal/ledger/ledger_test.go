package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOpenAndClose(t *testing.T) {
	dir := t.TempDir()

	l, err := Open(dir, false, false)
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

	l, err := Open(dir, true, false)
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

	l, err := Open(dir, false, false)
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
	l, err := Open(dir, false, false)
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
	l, err := Open(dir, false, false)
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

func TestConcurrentRecordRequest(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir, false, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	const numGoroutines = 50
	const requestsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(g int) {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				l.RecordRequest(RequestEntry{
					Timestamp:    time.Now().UTC(),
					TokenHash:    "tok_concurrent",
					Model:        "gpt-4",
					Provider:     "openai",
					InputTokens:  10,
					OutputTokens: 5,
					StatusCode:   200,
				})
			}
		}(i)
	}
	wg.Wait()

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sessions, _ := ReadSessionFiles(dir)
	s := sessions[0]
	expected := int64(numGoroutines * requestsPerGoroutine)
	if s.Totals.RequestCount != expected {
		t.Errorf("expected %d requests, got %d", expected, s.Totals.RequestCount)
	}
}

func TestMultipleSessions(t *testing.T) {
	dir := t.TempDir()

	// Create two sessions in the same directory
	l1, err := Open(dir, false, false)
	if err != nil {
		t.Fatalf("Open l1: %v", err)
	}
	l1.RecordRequest(RequestEntry{
		Timestamp: time.Now().UTC(), TokenHash: "tok1", Model: "gpt-4",
		InputTokens: 100, OutputTokens: 50, StatusCode: 200,
	})
	if err := l1.Close(); err != nil {
		t.Fatalf("Close l1: %v", err)
	}

	l2, err := Open(dir, false, false)
	if err != nil {
		t.Fatalf("Open l2: %v", err)
	}
	l2.RecordRequest(RequestEntry{
		Timestamp: time.Now().UTC(), TokenHash: "tok2", Model: "claude-3-opus",
		InputTokens: 200, OutputTokens: 100, StatusCode: 200,
	})
	if err := l2.Close(); err != nil {
		t.Fatalf("Close l2: %v", err)
	}

	sessions, err := ReadSessionFiles(dir)
	if err != nil {
		t.Fatalf("ReadSessionFiles: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}

	// Each session should have unique IDs
	if sessions[0].SessionID == sessions[1].SessionID {
		t.Error("sessions should have different IDs")
	}
}

func TestReadSessionFilesSkipsCorrupted(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatalf("create dir: %v", err)
	}

	// Write a valid session file
	valid := &SessionSummary{
		SessionID: "valid123",
		StartedAt: time.Now().Format(time.RFC3339),
		EndedAt:   time.Now().Format(time.RFC3339),
		Totals:    SessionTotals{UsageRollup: UsageRollup{RequestCount: 1}},
		ByModel:   make(map[string]*UsageRollup),
		ByProvider: make(map[string]*UsageRollup),
		ByToken:   make(map[string]*TokenRollup),
	}
	data, _ := json.Marshal(valid)
	if err := os.WriteFile(filepath.Join(sessDir, "2026-02-25_valid123.json"), data, 0o644); err != nil {
		t.Fatalf("write valid session: %v", err)
	}

	// Write a corrupted file
	if err := os.WriteFile(filepath.Join(sessDir, "2026-02-25_corrupt.json"), []byte("{invalid json"), 0o644); err != nil {
		t.Fatalf("write corrupted session: %v", err)
	}

	// Write a non-JSON file (should be skipped)
	if err := os.WriteFile(filepath.Join(sessDir, "notes.txt"), []byte("not a session"), 0o644); err != nil {
		t.Fatalf("write non-json file: %v", err)
	}

	sessions, err := ReadSessionFiles(dir)
	if err != nil {
		t.Fatalf("ReadSessionFiles: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 valid session (skipping corrupted), got %d", len(sessions))
	}
	if sessions[0].SessionID != "valid123" {
		t.Errorf("expected session valid123, got %s", sessions[0].SessionID)
	}
}

func TestEmptySessionClose(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir, false, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Close immediately with no requests
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sessions, _ := ReadSessionFiles(dir)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	s := sessions[0]
	if s.Totals.RequestCount != 0 {
		t.Errorf("expected 0 requests, got %d", s.Totals.RequestCount)
	}
	if s.DurationMs < 0 {
		t.Errorf("expected non-negative duration, got %d", s.DurationMs)
	}
}

func TestRecordMemoryNoWriter(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir, false, false) // memory=false
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer l.Close()

	// Should be a no-op, not an error
	err = l.RecordMemory("tok1", "user", "gpt-4", "hello")
	if err != nil {
		t.Errorf("RecordMemory with no writer should return nil, got: %v", err)
	}
}

func TestRecordMemoryUsesSessionFile(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir, true, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := l.RecordMemory("tok1", "user", "gpt-4", "hello"); err != nil {
		t.Fatalf("RecordMemory: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	memDir := filepath.Join(dir, "memory")
	files, err := os.ReadDir(memDir)
	if err != nil {
		t.Fatalf("read memory dir: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 memory file, got %d", len(files))
	}

	wantPrefix := time.Now().UTC().Format("2006-01-02") + "_" + l.SessionID()
	got := files[0].Name()
	if filepath.Ext(got) != ".md" {
		t.Fatalf("expected .md memory file, got %s", got)
	}
	base := got[:len(got)-3] // trim .md
	if base != wantPrefix {
		t.Fatalf("expected memory filename %q, got %q", wantPrefix+".md", got)
	}
}

func TestTokenRollupMultiModel(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir, false, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	now := time.Now().UTC()
	// Same token, different models
	l.RecordRequest(RequestEntry{
		Timestamp: now, TokenHash: "multi_tok", Model: "gpt-4",
		InputTokens: 100, OutputTokens: 50, StatusCode: 200,
	})
	l.RecordRequest(RequestEntry{
		Timestamp: now.Add(time.Second), TokenHash: "multi_tok", Model: "claude-3-opus",
		InputTokens: 200, OutputTokens: 100, StatusCode: 200,
	})
	l.RecordRequest(RequestEntry{
		Timestamp: now.Add(2 * time.Second), TokenHash: "multi_tok", Model: "gpt-4",
		InputTokens: 150, OutputTokens: 75, StatusCode: 200,
	})

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sessions, _ := ReadSessionFiles(dir)
	tr := sessions[0].ByToken["multi_tok"]
	if tr == nil {
		t.Fatal("missing token rollup")
	}
	if len(tr.ModelsUsed) != 2 {
		t.Errorf("expected 2 models used, got %d: %v", len(tr.ModelsUsed), tr.ModelsUsed)
	}
	if tr.RequestCount != 3 {
		t.Errorf("expected 3 requests, got %d", tr.RequestCount)
	}
	if tr.FirstSeen == tr.LastSeen {
		t.Error("expected different first_seen and last_seen")
	}
}

func TestSessionFileNaming(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir, false, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	files, _ := os.ReadDir(filepath.Join(dir, "sessions"))
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	name := files[0].Name()
	// Should match pattern: YYYY-MM-DD_<hex>.json
	if len(name) < 20 {
		t.Errorf("filename too short: %s", name)
	}
	if name[4] != '-' || name[7] != '-' || name[10] != '_' {
		t.Errorf("unexpected filename format: %s", name)
	}
	if filepath.Ext(name) != ".json" {
		t.Errorf("expected .json extension, got %s", name)
	}
}

func TestCacheCreationTokenRollup(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir, false, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	l.RecordRequest(RequestEntry{
		Timestamp: time.Now().UTC(), TokenHash: "tok1", Model: "claude-3-opus",
		Provider: "anthropic", InputTokens: 500, OutputTokens: 200, StatusCode: 200,
		ProviderMeta: &ProviderMeta{
			CachedInputTokens:  100,
			CacheCreationTokens: 200,
		},
	})
	l.RecordRequest(RequestEntry{
		Timestamp: time.Now().UTC(), TokenHash: "tok1", Model: "claude-3-opus",
		Provider: "anthropic", InputTokens: 300, OutputTokens: 150, StatusCode: 200,
		ProviderMeta: &ProviderMeta{
			CachedInputTokens:  250,
			CacheCreationTokens: 50,
		},
	})

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sessions, _ := ReadSessionFiles(dir)
	s := sessions[0]

	if s.Totals.CachedInputTokens != 350 {
		t.Errorf("expected 350 cached, got %d", s.Totals.CachedInputTokens)
	}
	if s.Totals.CacheCreationTokens != 250 {
		t.Errorf("expected 250 cache creation, got %d", s.Totals.CacheCreationTokens)
	}

	// Check model rollup too
	m := s.ByModel["claude-3-opus"]
	if m.CachedInputTokens != 350 {
		t.Errorf("model cached: expected 350, got %d", m.CachedInputTokens)
	}
	if m.CacheCreationTokens != 250 {
		t.Errorf("model cache creation: expected 250, got %d", m.CacheCreationTokens)
	}
}

func TestContainsStr(t *testing.T) {
	if containsStr(nil, "a") {
		t.Error("nil slice should not contain anything")
	}
	if containsStr([]string{"a", "b"}, "c") {
		t.Error("should not find 'c'")
	}
	if !containsStr([]string{"a", "b"}, "b") {
		t.Error("should find 'b'")
	}
}

func TestRecordCommunicationEvent_PersistedWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir, false, true)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	err = l.RecordCommunicationEvent(CommunicationEvent{
		Type:        CommunicationEventRequestReceived,
		TokenHash:   "abc123",
		Method:      "POST",
		Path:        "/v1/chat/completions",
		ContentType: "application/json",
		Headers: map[string][]string{
			"Content-Type": []string{"application/json"},
		},
		Body:      `{"model":"gpt-4o"}`,
		BodyBytes: 18,
	})
	if err != nil {
		t.Fatalf("RecordCommunicationEvent: %v", err)
	}

	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sessions, err := ReadSessionFiles(dir)
	if err != nil {
		t.Fatalf("ReadSessionFiles: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if len(sessions[0].CommunicationEvents) != 1 {
		t.Fatalf("expected 1 communication event, got %d", len(sessions[0].CommunicationEvents))
	}
	ev := sessions[0].CommunicationEvents[0]
	if ev.Type != CommunicationEventRequestReceived {
		t.Fatalf("expected type %s, got %s", CommunicationEventRequestReceived, ev.Type)
	}
	if ev.Method != "POST" || ev.Path != "/v1/chat/completions" {
		t.Fatalf("unexpected request metadata: %s %s", ev.Method, ev.Path)
	}
}

func TestAddToRollup(t *testing.T) {
	m := make(map[string]*UsageRollup)
	addToRollup(m, "test", 100, 50, 10, 5, 20)
	addToRollup(m, "test", 200, 100, 30, 0, 0)

	r := m["test"]
	if r.RequestCount != 2 {
		t.Errorf("expected 2 requests, got %d", r.RequestCount)
	}
	if r.InputTokens != 300 {
		t.Errorf("expected 300 input, got %d", r.InputTokens)
	}
	if r.OutputTokens != 150 {
		t.Errorf("expected 150 output, got %d", r.OutputTokens)
	}
	if r.TotalTokens != 450 {
		t.Errorf("expected 450 total, got %d", r.TotalTokens)
	}
	if r.CachedInputTokens != 40 {
		t.Errorf("expected 40 cached, got %d", r.CachedInputTokens)
	}
	if r.CacheCreationTokens != 5 {
		t.Errorf("expected 5 cache creation, got %d", r.CacheCreationTokens)
	}
	if r.ReasoningTokens != 20 {
		t.Errorf("expected 20 reasoning, got %d", r.ReasoningTokens)
	}
}
