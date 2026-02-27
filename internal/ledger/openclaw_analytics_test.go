package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenClawAnalytics_ByMetadataKey(t *testing.T) {
	dir := t.TempDir()

	// Create test session with OpenClaw metadata
	session := &SessionSummary{
		SessionID: "test_sess_1",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		EndedAt:   time.Now().UTC().Add(10 * time.Second).Format(time.RFC3339),
		DurationMs: 10000,
		Requests: []RequestEntry{
			{
				TokenHash:    "token_hash_1",
				Model:        "gpt-4",
				InputTokens:  100,
				OutputTokens: 50,
				StatusCode:   200,
				Metadata: map[string]string{
					"team":     "ml",
					"channel":  "alerts",
					"agent_id": "agent_123",
				},
			},
			{
				TokenHash:    "token_hash_1",
				Model:        "gpt-3.5",
				InputTokens:  50,
				OutputTokens: 25,
				StatusCode:   200,
				Metadata: map[string]string{
					"team":     "ml",
					"channel":  "general",
					"agent_id": "agent_456",
				},
			},
			{
				TokenHash:    "token_hash_2",
				Model:        "gpt-4",
				InputTokens:  200,
				OutputTokens: 100,
				StatusCode:   200,
				Error:        "rate limited",
				Metadata: map[string]string{
					"team":     "platform",
					"channel":  "alerts",
					"agent_id": "agent_123",
				},
			},
		},
	}

	// Write session to disk
	sessDir := filepath.Join(dir, "sessions")
	os.MkdirAll(sessDir, 0755)
	data, _ := json.Marshal(session)
	os.WriteFile(filepath.Join(sessDir, "2026-02-27_test.json"), data, 0644)

	oca := NewOpenClawAnalytics(dir)

	tests := []struct {
		name        string
		key         string
		checkValue  string
		expectedReq int64
		expectedIn  int64
		expectedOut int64
		expectedErr int64
	}{
		{
			name:        "by_team",
			key:         "team",
			checkValue:  "ml",
			expectedReq: 2,
			expectedIn:  150,
			expectedOut: 75,
			expectedErr: 0,
		},
		{
			name:        "by_agent_id",
			key:         "agent_id",
			checkValue:  "agent_123",
			expectedReq: 2,
			expectedIn:  300,
			expectedOut: 150,
			expectedErr: 1,
		},
		{
			name:        "by_channel",
			key:         "channel",
			checkValue:  "alerts",
			expectedReq: 2,
			expectedIn:  300,
			expectedOut: 150,
			expectedErr: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics, err := oca.ByMetadataKey(tt.key)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			metric, ok := metrics[tt.checkValue]
			if !ok {
				t.Fatalf("expected to find %q in results", tt.checkValue)
			}

			if metric.RequestCount != tt.expectedReq {
				t.Errorf("request count: expected %d, got %d", tt.expectedReq, metric.RequestCount)
			}
			if metric.InputTokens != tt.expectedIn {
				t.Errorf("input tokens: expected %d, got %d", tt.expectedIn, metric.InputTokens)
			}
			if metric.OutputTokens != tt.expectedOut {
				t.Errorf("output tokens: expected %d, got %d", tt.expectedOut, metric.OutputTokens)
			}
			if metric.ErrorCount != tt.expectedErr {
				t.Errorf("error count: expected %d, got %d", tt.expectedErr, metric.ErrorCount)
			}

			totalTokens := metric.InputTokens + metric.OutputTokens
			if metric.TotalTokens != totalTokens {
				t.Errorf("total tokens: expected %d, got %d", totalTokens, metric.TotalTokens)
			}
		})
	}
}

func TestOpenClawAnalytics_ByTeamAndChannel(t *testing.T) {
	dir := t.TempDir()

	session := &SessionSummary{
		SessionID: "test_sess_1",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		EndedAt:   time.Now().UTC().Add(5 * time.Second).Format(time.RFC3339),
		DurationMs: 5000,
		Requests: []RequestEntry{
			{
				TokenHash:    "token_1",
				Model:        "gpt-4",
				InputTokens:  100,
				OutputTokens: 50,
				StatusCode:   200,
				Metadata: map[string]string{
					"team":    "ml",
					"channel": "alerts",
				},
			},
			{
				TokenHash:    "token_1",
				Model:        "gpt-3.5",
				InputTokens:  50,
				OutputTokens: 25,
				StatusCode:   200,
				Metadata: map[string]string{
					"team":    "ml",
					"channel": "general",
				},
			},
			{
				TokenHash:    "token_1",
				Model:        "gpt-4",
				InputTokens:  200,
				OutputTokens: 100,
				StatusCode:   200,
				Metadata: map[string]string{
					"team":    "platform",
					"channel": "alerts",
				},
			},
		},
	}

	sessDir := filepath.Join(dir, "sessions")
	os.MkdirAll(sessDir, 0755)
	data, _ := json.Marshal(session)
	os.WriteFile(filepath.Join(sessDir, "2026-02-27_test.json"), data, 0644)

	oca := NewOpenClawAnalytics(dir)
	metrics, err := oca.ByTeamAndChannel()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name        string
		key         string
		expectedReq int64
		expectedIn  int64
	}{
		{
			name:        "ml/alerts",
			key:         "ml/alerts",
			expectedReq: 1,
			expectedIn:  100,
		},
		{
			name:        "ml/general",
			key:         "ml/general",
			expectedReq: 1,
			expectedIn:  50,
		},
		{
			name:        "platform/alerts",
			key:         "platform/alerts",
			expectedReq: 1,
			expectedIn:  200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metric, ok := metrics[tt.key]
			if !ok {
				t.Fatalf("expected to find %q in results", tt.key)
			}

			if metric.RequestCount != tt.expectedReq {
				t.Errorf("request count: expected %d, got %d", tt.expectedReq, metric.RequestCount)
			}
			if metric.InputTokens != tt.expectedIn {
				t.Errorf("input tokens: expected %d, got %d", tt.expectedIn, metric.InputTokens)
			}
		})
	}
}

func TestOpenClawAnalytics_FilteredSessions(t *testing.T) {
	dir := t.TempDir()

	now := time.Now().UTC()

	session1 := &SessionSummary{
		SessionID: "sess_1",
		StartedAt: now.Add(-10 * time.Minute).Format(time.RFC3339),
		EndedAt:   now.Add(-9 * time.Minute).Format(time.RFC3339),
		Requests: []RequestEntry{
			{
				TokenHash: "token_1",
				Metadata: map[string]string{
					"team": "ml",
				},
			},
		},
	}

	session2 := &SessionSummary{
		SessionID: "sess_2",
		StartedAt: now.Add(-5 * time.Minute).Format(time.RFC3339),
		EndedAt:   now.Add(-4 * time.Minute).Format(time.RFC3339),
		Requests: []RequestEntry{
			{
				TokenHash: "token_2",
				Metadata: map[string]string{
					"team": "platform",
				},
			},
		},
	}

	sessDir := filepath.Join(dir, "sessions")
	os.MkdirAll(sessDir, 0755)

	data1, _ := json.Marshal(session1)
	data2, _ := json.Marshal(session2)
	os.WriteFile(filepath.Join(sessDir, "2026-02-27_sess1.json"), data1, 0644)
	os.WriteFile(filepath.Join(sessDir, "2026-02-27_sess2.json"), data2, 0644)

	oca := NewOpenClawAnalytics(dir)

	// Filter by metadata
	sessions, err := oca.FilteredSessions(SessionFilter{
		MetadataKey: "team",
		MetadataVal: "ml",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionID != "sess_1" {
		t.Errorf("expected sess_1, got %s", sessions[0].SessionID)
	}

	// Filter by time range
	sessions, err = oca.FilteredSessions(SessionFilter{
		StartTime: now.Add(-6 * time.Minute),
		EndTime:   now,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sessions) != 1 {
		t.Errorf("expected 1 session in time range, got %d", len(sessions))
	}
	if sessions[0].SessionID != "sess_2" {
		t.Errorf("expected sess_2, got %s", sessions[0].SessionID)
	}
}

func TestOpenClawAnalytics_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	oca := NewOpenClawAnalytics(dir)

	metrics, err := oca.ByMetadataKey("team")
	if err != nil {
		t.Fatalf("unexpected error on empty dir: %v", err)
	}

	if len(metrics) != 0 {
		t.Errorf("expected empty metrics for empty directory, got %d", len(metrics))
	}
}

func TestOpenClawAnalytics_ModelsUsed(t *testing.T) {
	dir := t.TempDir()

	session := &SessionSummary{
		SessionID: "test_sess_1",
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		EndedAt:   time.Now().UTC().Add(5 * time.Second).Format(time.RFC3339),
		Requests: []RequestEntry{
			{
				TokenHash: "token_1",
				Model:     "gpt-4",
				Metadata: map[string]string{
					"team": "ml",
				},
			},
			{
				TokenHash: "token_1",
				Model:     "gpt-3.5-turbo",
				Metadata: map[string]string{
					"team": "ml",
				},
			},
			{
				TokenHash: "token_1",
				Model:     "gpt-4",
				Metadata: map[string]string{
					"team": "ml",
				},
			},
		},
	}

	sessDir := filepath.Join(dir, "sessions")
	os.MkdirAll(sessDir, 0755)
	data, _ := json.Marshal(session)
	os.WriteFile(filepath.Join(sessDir, "2026-02-27_test.json"), data, 0644)

	oca := NewOpenClawAnalytics(dir)
	metrics, err := oca.ByMetadataKey("team")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	metric := metrics["ml"]
	if len(metric.ModelsUsed) != 2 {
		t.Errorf("expected 2 unique models, got %d: %v", len(metric.ModelsUsed), metric.ModelsUsed)
	}

	// Verify they're sorted
	if metric.ModelsUsed[0] != "gpt-3.5-turbo" || metric.ModelsUsed[1] != "gpt-4" {
		t.Errorf("expected sorted models [gpt-3.5-turbo, gpt-4], got %v", metric.ModelsUsed)
	}
}
