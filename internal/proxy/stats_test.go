package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecord(t *testing.T) {
	tests := []struct {
		name         string
		tokenHash    string
		model        string
		baseKeyEnv   string
		inputTokens  int
		outputTokens int
		isError      bool
	}{
		{
			name:         "normal request",
			tokenHash:    "abc123def456ghij",
			model:        "gpt-4",
			baseKeyEnv:   "OPENAI_KEY",
			inputTokens:  100,
			outputTokens: 50,
			isError:      false,
		},
		{
			name:         "error request",
			tokenHash:    "error_token_hash_long",
			model:        "gpt-3.5-turbo",
			baseKeyEnv:   "OPENAI_KEY",
			inputTokens:  10,
			outputTokens: 0,
			isError:      true,
		},
		{
			name:         "zero tokens",
			tokenHash:    "zero_token_abcdef",
			model:        "claude-3",
			baseKeyEnv:   "ANTHROPIC_KEY",
			inputTokens:  0,
			outputTokens: 0,
			isError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := NewUsageStats()
			u.Record(tt.tokenHash, tt.model, tt.baseKeyEnv, tt.inputTokens, tt.outputTokens, tt.isError)

			snap := u.Snapshot()
			if len(snap) != 1 {
				t.Fatalf("Snapshot length = %d, want 1", len(snap))
			}

			entry := snap[0]
			if entry.Model != tt.model {
				t.Errorf("Model = %q, want %q", entry.Model, tt.model)
			}
			if entry.BaseKeyEnv != tt.baseKeyEnv {
				t.Errorf("BaseKeyEnv = %q, want %q", entry.BaseKeyEnv, tt.baseKeyEnv)
			}
			if entry.RequestCount != 1 {
				t.Errorf("RequestCount = %d, want 1", entry.RequestCount)
			}
			if entry.InputTokens != int64(tt.inputTokens) {
				t.Errorf("InputTokens = %d, want %d", entry.InputTokens, tt.inputTokens)
			}
			if entry.OutputTokens != int64(tt.outputTokens) {
				t.Errorf("OutputTokens = %d, want %d", entry.OutputTokens, tt.outputTokens)
			}
			if entry.TotalTokens != int64(tt.inputTokens+tt.outputTokens) {
				t.Errorf("TotalTokens = %d, want %d", entry.TotalTokens, tt.inputTokens+tt.outputTokens)
			}
			wantErr := int64(0)
			if tt.isError {
				wantErr = 1
			}
			if entry.ErrorCount != wantErr {
				t.Errorf("ErrorCount = %d, want %d", entry.ErrorCount, wantErr)
			}
		})
	}
}

func TestRecord_Aggregation(t *testing.T) {
	u := NewUsageStats()

	// Record two requests to the same model+key
	u.Record("tok1_abcdef1234567890", "gpt-4", "KEY1", 100, 50, false)
	u.Record("tok2_abcdef1234567890", "gpt-4", "KEY1", 200, 100, true)

	snap := u.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 aggregated entry, got %d", len(snap))
	}

	entry := snap[0]
	if entry.RequestCount != 2 {
		t.Errorf("RequestCount = %d, want 2", entry.RequestCount)
	}
	if entry.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", entry.InputTokens)
	}
	if entry.OutputTokens != 150 {
		t.Errorf("OutputTokens = %d, want 150", entry.OutputTokens)
	}
	if entry.TotalTokens != 450 {
		t.Errorf("TotalTokens = %d, want 450", entry.TotalTokens)
	}
	if entry.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", entry.ErrorCount)
	}
}

func TestRecord_DifferentModels(t *testing.T) {
	u := NewUsageStats()

	u.Record("tok_abcdef1234567890", "gpt-4", "KEY1", 100, 50, false)
	u.Record("tok_abcdef1234567890", "claude-3", "KEY2", 200, 100, false)

	snap := u.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries for different models, got %d", len(snap))
	}
}

func TestRecord_SessionTracking(t *testing.T) {
	u := NewUsageStats()

	// Long hash should be truncated to 16 chars
	longHash := "abcdef1234567890extra_stuff"
	u.Record(longHash, "gpt-4", "KEY1", 100, 50, false)

	sessions := u.SessionSnapshot()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	sess := sessions[0]
	if sess.TokenHash != "abcdef1234567890" {
		t.Errorf("TokenHash = %q, want %q", sess.TokenHash, "abcdef1234567890")
	}
	if sess.RequestCount != 1 {
		t.Errorf("RequestCount = %d, want 1", sess.RequestCount)
	}
	if sess.LastModel != "gpt-4" {
		t.Errorf("LastModel = %q, want %q", sess.LastModel, "gpt-4")
	}
	if sess.FirstSeen.IsZero() {
		t.Error("FirstSeen should not be zero")
	}
	if sess.LastSeen.IsZero() {
		t.Error("LastSeen should not be zero")
	}
}

func TestRecord_ShortHash(t *testing.T) {
	u := NewUsageStats()

	// Short hash (< 16) should not be truncated
	shortHash := "short"
	u.Record(shortHash, "gpt-4", "KEY1", 10, 5, false)

	sessions := u.SessionSnapshot()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].TokenHash != "short" {
		t.Errorf("TokenHash = %q, want %q", sessions[0].TokenHash, "short")
	}
}

func TestSnapshot_Empty(t *testing.T) {
	u := NewUsageStats()
	snap := u.Snapshot()
	if len(snap) != 0 {
		t.Fatalf("expected empty snapshot, got %d entries", len(snap))
	}
}

func TestSnapshot_SortedByTotalTokens(t *testing.T) {
	u := NewUsageStats()

	u.Record("tok1_abcdef12345678", "small-model", "KEY", 10, 5, false)
	u.Record("tok2_abcdef12345678", "large-model", "KEY2", 1000, 500, false)
	u.Record("tok3_abcdef12345678", "medium-model", "KEY3", 100, 50, false)

	snap := u.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(snap))
	}

	// Should be sorted descending by TotalTokens
	for i := 0; i < len(snap)-1; i++ {
		if snap[i].TotalTokens < snap[i+1].TotalTokens {
			t.Errorf("snapshot not sorted: entry[%d].TotalTokens=%d < entry[%d].TotalTokens=%d",
				i, snap[i].TotalTokens, i+1, snap[i+1].TotalTokens)
		}
	}
}

func TestSessionSnapshot_Empty(t *testing.T) {
	u := NewUsageStats()
	sessions := u.SessionSnapshot()
	if len(sessions) != 0 {
		t.Fatalf("expected empty session snapshot, got %d entries", len(sessions))
	}
}

func TestSessionSnapshot_SortedByTotalTokens(t *testing.T) {
	u := NewUsageStats()

	u.Record("small_abcdef1234567", "m1", "K", 10, 5, false)
	u.Record("large_abcdef1234567", "m2", "K", 1000, 500, false)
	u.Record("medium_abcdef123456", "m3", "K", 100, 50, false)

	sessions := u.SessionSnapshot()
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}

	for i := 0; i < len(sessions)-1; i++ {
		if sessions[i].TotalTokens < sessions[i+1].TotalTokens {
			t.Errorf("sessions not sorted: entry[%d].TotalTokens=%d < entry[%d].TotalTokens=%d",
				i, sessions[i].TotalTokens, i+1, sessions[i+1].TotalTokens)
		}
	}
}

func TestStatsHandler(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		setupRecords   func(u *UsageStats)
		wantStatusCode int
		checkBody      func(t *testing.T, body map[string]interface{})
	}{
		{
			name:           "GET on empty stats returns valid JSON",
			method:         http.MethodGet,
			setupRecords:   func(u *UsageStats) {},
			wantStatusCode: http.StatusOK,
			checkBody: func(t *testing.T, body map[string]interface{}) {
				t.Helper()
				totals, ok := body["totals"].(map[string]interface{})
				if !ok {
					t.Fatal("missing or invalid 'totals' in response")
				}
				if totals["request_count"].(float64) != 0 {
					t.Errorf("request_count = %v, want 0", totals["request_count"])
				}
			},
		},
		{
			name:   "GET with recorded data returns correct totals",
			method: http.MethodGet,
			setupRecords: func(u *UsageStats) {
				u.Record("tok1_abcdef12345678", "gpt-4", "KEY", 100, 50, false)
				u.Record("tok2_abcdef12345678", "gpt-4", "KEY", 200, 100, true)
			},
			wantStatusCode: http.StatusOK,
			checkBody: func(t *testing.T, body map[string]interface{}) {
				t.Helper()
				totals, ok := body["totals"].(map[string]interface{})
				if !ok {
					t.Fatal("missing or invalid 'totals' in response")
				}
				if totals["request_count"].(float64) != 2 {
					t.Errorf("request_count = %v, want 2", totals["request_count"])
				}
				if totals["input_tokens"].(float64) != 300 {
					t.Errorf("input_tokens = %v, want 300", totals["input_tokens"])
				}
				if totals["output_tokens"].(float64) != 150 {
					t.Errorf("output_tokens = %v, want 150", totals["output_tokens"])
				}
				if totals["total_tokens"].(float64) != 450 {
					t.Errorf("total_tokens = %v, want 450", totals["total_tokens"])
				}
				if totals["error_count"].(float64) != 1 {
					t.Errorf("error_count = %v, want 1", totals["error_count"])
				}

				// Check by_model_and_key and by_token are present
				if _, ok := body["by_model_and_key"]; !ok {
					t.Error("missing 'by_model_and_key' in response")
				}
				if _, ok := body["by_token"]; !ok {
					t.Error("missing 'by_token' in response")
				}
			},
		},
		{
			name:           "POST returns 405 Method Not Allowed",
			method:         http.MethodPost,
			setupRecords:   func(u *UsageStats) {},
			wantStatusCode: http.StatusMethodNotAllowed,
			checkBody:      nil,
		},
		{
			name:           "PUT returns 405 Method Not Allowed",
			method:         http.MethodPut,
			setupRecords:   func(u *UsageStats) {},
			wantStatusCode: http.StatusMethodNotAllowed,
			checkBody:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := NewUsageStats()
			tt.setupRecords(u)

			req := httptest.NewRequest(tt.method, "/stats", nil)
			w := httptest.NewRecorder()

			u.StatsHandler(w, req)

			resp := w.Result()
			if resp.StatusCode != tt.wantStatusCode {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatusCode)
			}

			if tt.checkBody != nil {
				if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
					t.Errorf("Content-Type = %q, want %q", ct, "application/json")
				}

				var body map[string]interface{}
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode response body: %v", err)
				}
				tt.checkBody(t, body)
			}
		})
	}
}
