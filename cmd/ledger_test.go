package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickcrawford/tokenomics/internal/ledger"
)

// writeTestSession creates a session file in the given directory for testing.
func writeTestSession(t *testing.T, dir string, s *ledger.SessionSummary) {
	t.Helper()
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatalf("create sessions dir: %v", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	filename := "2026-02-25_" + s.SessionID + ".json"
	if err := os.WriteFile(filepath.Join(sessDir, filename), data, 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
}

func testSession(id string, model string, provider string, input, output int) *ledger.SessionSummary {
	now := time.Now().UTC()
	return &ledger.SessionSummary{
		SessionID:  id,
		StartedAt:  now.Format(time.RFC3339),
		EndedAt:    now.Add(10 * time.Minute).Format(time.RFC3339),
		DurationMs: 600000,
		Git: ledger.GitInfo{
			Branch:      "feature/test",
			CommitStart: "abc1234",
			CommitEnd:   "def5678",
		},
		Totals: ledger.SessionTotals{
			UsageRollup: ledger.UsageRollup{
				RequestCount: 5,
				InputTokens:  int64(input),
				OutputTokens: int64(output),
				TotalTokens:  int64(input + output),
			},
		},
		ByModel: map[string]*ledger.UsageRollup{
			model: {
				RequestCount: 5,
				InputTokens:  int64(input),
				OutputTokens: int64(output),
				TotalTokens:  int64(input + output),
			},
		},
		ByProvider: map[string]*ledger.UsageRollup{
			provider: {
				RequestCount: 5,
				InputTokens:  int64(input),
				OutputTokens: int64(output),
				TotalTokens:  int64(input + output),
			},
		},
		ByToken: map[string]*ledger.TokenRollup{
			"tok_abc123": {
				UsageRollup: ledger.UsageRollup{
					RequestCount: 5,
					InputTokens:  int64(input),
					OutputTokens: int64(output),
					TotalTokens:  int64(input + output),
				},
				ModelsUsed: []string{model},
				FirstSeen:  now.Format(time.RFC3339),
				LastSeen:   now.Add(9 * time.Minute).Format(time.RFC3339),
			},
		},
		Requests: []ledger.RequestEntry{
			{
				Timestamp:    now,
				TokenHash:    "tok_abc123",
				Model:        model,
				Provider:     provider,
				InputTokens:  input / 5,
				OutputTokens: output / 5,
				StatusCode:   200,
			},
		},
	}
}

func TestRunLedgerSummaryNoSessions(t *testing.T) {
	dir := t.TempDir()
	ledgerDir = dir
	defer func() { ledgerDir = "" }()

	err := runLedgerSummary(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLedgerSummaryWithSessions(t *testing.T) {
	dir := t.TempDir()
	ledgerDir = dir
	defer func() { ledgerDir = "" }()

	writeTestSession(t, dir, testSession("aaaa1111", "gpt-4", "openai", 1000, 500))
	writeTestSession(t, dir, testSession("bbbb2222", "claude-3-opus", "anthropic", 2000, 1000))

	err := runLedgerSummary(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLedgerSummaryJSON(t *testing.T) {
	dir := t.TempDir()
	ledgerDir = dir
	ledgerJSON = true
	defer func() { ledgerDir = ""; ledgerJSON = false }()

	writeTestSession(t, dir, testSession("cccc3333", "gpt-4", "openai", 5000, 2500))

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runLedgerSummary(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = old

	var output map[string]interface{}
	if err := json.NewDecoder(r).Decode(&output); err != nil {
		t.Fatalf("failed to decode JSON output: %v", err)
	}

	sessions, ok := output["sessions"].(float64)
	if !ok || sessions != 1 {
		t.Errorf("expected 1 session, got %v", output["sessions"])
	}
}

func TestRunLedgerSessionsList(t *testing.T) {
	dir := t.TempDir()
	ledgerDir = dir
	defer func() { ledgerDir = "" }()

	writeTestSession(t, dir, testSession("dddd4444", "gpt-4", "openai", 1000, 500))
	writeTestSession(t, dir, testSession("eeee5555", "claude-3-opus", "anthropic", 2000, 1000))

	err := runLedgerSessions(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLedgerSessionsEmpty(t *testing.T) {
	dir := t.TempDir()
	ledgerDir = dir
	defer func() { ledgerDir = "" }()

	err := runLedgerSessions(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLedgerShowFound(t *testing.T) {
	dir := t.TempDir()
	ledgerDir = dir
	defer func() { ledgerDir = "" }()

	writeTestSession(t, dir, testSession("ff006677", "gpt-4", "openai", 3000, 1500))

	err := runLedgerShow(nil, []string{"ff006677"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLedgerShowPrefix(t *testing.T) {
	dir := t.TempDir()
	ledgerDir = dir
	defer func() { ledgerDir = "" }()

	writeTestSession(t, dir, testSession("aabb8899", "gpt-4", "openai", 3000, 1500))

	err := runLedgerShow(nil, []string{"aabb"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunLedgerShowNotFound(t *testing.T) {
	dir := t.TempDir()
	ledgerDir = dir
	defer func() { ledgerDir = "" }()

	writeTestSession(t, dir, testSession("11223344", "gpt-4", "openai", 1000, 500))

	err := runLedgerShow(nil, []string{"99999999"})
	if err == nil {
		t.Fatal("expected error for missing session")
	}
}

func TestRunLedgerShowJSON(t *testing.T) {
	dir := t.TempDir()
	ledgerDir = dir
	ledgerJSON = true
	defer func() { ledgerDir = ""; ledgerJSON = false }()

	writeTestSession(t, dir, testSession("55667788", "claude-3-opus", "anthropic", 4000, 2000))

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runLedgerShow(nil, []string{"55667788"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = old

	var output ledger.SessionSummary
	if err := json.NewDecoder(r).Decode(&output); err != nil {
		t.Fatalf("failed to decode JSON output: %v", err)
	}

	if output.SessionID != "55667788" {
		t.Errorf("expected session 55667788, got %s", output.SessionID)
	}
	if output.Totals.TotalTokens != 6000 {
		t.Errorf("expected 6000 total tokens, got %d", output.Totals.TotalTokens)
	}
}

func TestMergeRollups(t *testing.T) {
	dst := map[string]*ledger.UsageRollup{
		"gpt-4": {RequestCount: 5, InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500},
	}
	src := map[string]*ledger.UsageRollup{
		"gpt-4":         {RequestCount: 3, InputTokens: 600, OutputTokens: 300, TotalTokens: 900, CachedInputTokens: 100},
		"claude-3-opus": {RequestCount: 2, InputTokens: 400, OutputTokens: 200, TotalTokens: 600},
	}

	mergeRollups(dst, src)

	if dst["gpt-4"].RequestCount != 8 {
		t.Errorf("gpt-4 request count: got %d, want 8", dst["gpt-4"].RequestCount)
	}
	if dst["gpt-4"].TotalTokens != 2400 {
		t.Errorf("gpt-4 total tokens: got %d, want 2400", dst["gpt-4"].TotalTokens)
	}
	if dst["gpt-4"].CachedInputTokens != 100 {
		t.Errorf("gpt-4 cached: got %d, want 100", dst["gpt-4"].CachedInputTokens)
	}
	if dst["claude-3-opus"] == nil {
		t.Fatal("missing claude-3-opus")
	}
	if dst["claude-3-opus"].RequestCount != 2 {
		t.Errorf("claude request count: got %d, want 2", dst["claude-3-opus"].RequestCount)
	}
}

func TestFormatTime(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2026-02-25T10:30:00Z", "2026-02-25T10:30:00"},
		{"2026-02-25T10:30:00+00:00", "2026-02-25T10:30:00"},
		{"short", "short"},
	}
	for _, tt := range tests {
		got := formatTime(tt.input)
		if got != tt.expected {
			t.Errorf("formatTime(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestGetLedgerDir(t *testing.T) {
	// With explicit override
	ledgerDir = "/custom/dir"
	if got := getLedgerDir(); got != "/custom/dir" {
		t.Errorf("expected /custom/dir, got %s", got)
	}

	// Fallback to default - should be absolute path to .tokenomics
	ledgerDir = ""
	got := getLedgerDir()
	if !filepath.IsAbs(got) || !strings.Contains(got, ".tokenomics") {
		t.Errorf("expected absolute path containing .tokenomics, got %s", got)
	}
}
