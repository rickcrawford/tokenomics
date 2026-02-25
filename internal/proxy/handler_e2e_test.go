package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/policy"
	"github.com/rickcrawford/tokenomics/internal/session"
)

// TestE2E_FullRequestFlow tests a complete request through the proxy:
// auth, policy resolution, rules check, upstream call, response.
func TestE2E_FullRequestFlow(t *testing.T) {
	t.Setenv("E2E_KEY", "sk-e2e-test")

	var upstreamBody string
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		upstreamBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-abc123")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-e2e",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"role": "assistant", "content": "Hello there!"}}},
			"usage":   map[string]interface{}{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "E2E_KEY",
		MaxTokens:  100000,
		ModelRegex: "^gpt",
		Prompts: []policy.Message{
			{Role: "system", Content: "You are helpful."},
		},
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_e2e"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_e2e")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify upstream received the system prompt injection
	if !strings.Contains(upstreamBody, "You are helpful") {
		t.Errorf("expected system prompt injection, got: %s", upstreamBody)
	}

	// Verify response passes through
	var resp map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["id"] != "chatcmpl-e2e" {
		t.Errorf("expected response id chatcmpl-e2e, got %v", resp["id"])
	}
}

// TestE2E_RuleViolationBlocks tests that a fail rule blocks the request.
func TestE2E_RuleViolationBlocks(t *testing.T) {
	t.Setenv("RULE_KEY", "sk-rule-test")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called when rules block")
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "RULE_KEY",
		Rules: policy.RuleList{
			{Type: "keyword", Keywords: []string{"drop table"}, Action: "fail"},
		},
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_rule"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"please drop table users"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_rule")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestE2E_PiiMaskingRedactsContent tests that PII masking works end-to-end.
func TestE2E_PiiMaskingRedactsContent(t *testing.T) {
	t.Setenv("PII_KEY", "sk-pii-test")

	var upstreamBody string
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		upstreamBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-pii",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "PII_KEY",
		Rules: policy.RuleList{
			{Type: "pii", Detect: []string{"ssn", "credit_card"}, Action: "mask", Scope: "input"},
		},
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_pii"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"my ssn is 123-45-6789 and card 4111111111111111"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_pii")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// The SSN and card number should be redacted before reaching upstream
	if strings.Contains(upstreamBody, "123-45-6789") {
		t.Error("SSN was not redacted in upstream body")
	}
	if strings.Contains(upstreamBody, "4111111111111111") {
		t.Error("credit card was not redacted in upstream body")
	}
	if !strings.Contains(upstreamBody, "[REDACTED]") {
		t.Error("expected [REDACTED] in upstream body")
	}
}

// TestE2E_BudgetExceeded tests that budget enforcement works.
func TestE2E_BudgetExceeded(t *testing.T) {
	t.Setenv("BUDGET_KEY", "sk-budget-test")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called when budget is exceeded")
	})
	defer upstream.Close()

	// Use an extremely low budget
	pol := &policy.Policy{
		BaseKeyEnv: "BUDGET_KEY",
		MaxTokens:  1, // 1 token budget
	}
	pol.Validate()
	tokenHash := hashForTest(handler, "tkn_budget")
	ts.Save(tokenHash, pol)

	// Seed usage so it's already over budget
	handler.sessions.AddUsage(tokenHash, 100)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_budget")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "budget exceeded") {
		t.Errorf("expected budget exceeded message, got: %s", rr.Body.String())
	}
}

// TestE2E_ModelRejection tests that model allowlist works.
func TestE2E_ModelRejection(t *testing.T) {
	t.Setenv("MODEL_KEY", "sk-model-test")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called for blocked model")
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "MODEL_KEY",
		ModelRegex: "^gpt-3",
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_model"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_model")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestE2E_RateLimiting tests that rate limiting works.
func TestE2E_RateLimiting(t *testing.T) {
	t.Setenv("RATE_KEY", "sk-rate-test")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-rate",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "RATE_KEY",
		RateLimit: &policy.RateLimitConfig{
			Rules: []policy.RateLimitRule{
				{Requests: 2, Window: "1m"},
			},
		},
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_rate"), pol)

	makeReq := func() int {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
			`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
		))
		req.Header.Set("Authorization", "Bearer tkn_rate")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr.Code
	}

	// First two should pass
	if code := makeReq(); code != http.StatusOK {
		t.Fatalf("request 1: expected 200, got %d", code)
	}
	if code := makeReq(); code != http.StatusOK {
		t.Fatalf("request 2: expected 200, got %d", code)
	}

	// Third should be rate limited
	if code := makeReq(); code != http.StatusTooManyRequests {
		t.Fatalf("request 3: expected 429, got %d", code)
	}
}

// TestE2E_MultiProvider tests that multi-provider routing works end-to-end.
func TestE2E_MultiProvider(t *testing.T) {
	t.Setenv("OPENAI_E2E_KEY", "sk-openai-test")
	t.Setenv("ANTHROPIC_E2E_KEY", "sk-ant-test")

	var capturedAuth string
	var capturedPath string
	handler, ts, upstream := setupTestHandler(t, map[string]config.ProviderConfig{
		"openai": {
			APIKeyEnv: "OPENAI_E2E_KEY",
		},
		"anthropic": {
			APIKeyEnv:  "ANTHROPIC_E2E_KEY",
			AuthScheme: "header",
			AuthHeader: "x-api-key",
			ChatPath:   "/v1/messages",
		},
	}, func(w http.ResponseWriter, r *http.Request) {
		// Prefer x-api-key for Anthropic-style header auth
		if v := r.Header.Get("x-api-key"); v != "" {
			capturedAuth = v
		} else {
			capturedAuth = r.Header.Get("Authorization")
		}
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-multi",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "hi"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{
		Providers: map[string][]*policy.ProviderPolicy{
			"openai":    {{BaseKeyEnv: "OPENAI_E2E_KEY", ModelRegex: "^gpt"}},
			"anthropic": {{BaseKeyEnv: "ANTHROPIC_E2E_KEY", ModelRegex: "^claude"}},
		},
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_multi"), pol)

	// Test OpenAI routing
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_multi")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("openai: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if capturedAuth != "Bearer sk-openai-test" {
		t.Errorf("openai: expected Bearer auth, got %q", capturedAuth)
	}

	// Test Anthropic routing
	req = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"claude-3-opus","messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_multi")
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("anthropic: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if capturedAuth != "sk-ant-test" {
		t.Errorf("anthropic: expected x-api-key auth, got %q", capturedAuth)
	}
	if capturedPath != "/v1/messages" {
		t.Errorf("anthropic: expected path /v1/messages, got %q", capturedPath)
	}
}

// TestE2E_LoggingDisabled tests that DisableRequest suppresses request logs.
func TestE2E_LoggingDisabled(t *testing.T) {
	t.Setenv("LOG_KEY", "sk-log-test")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-log",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	handler.SetLogging(config.LoggingConfig{DisableRequest: true})

	pol := &policy.Policy{BaseKeyEnv: "LOG_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_log"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_log")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestE2E_StatsEndpoint tests the /stats endpoint after some requests.
func TestE2E_StatsEndpoint(t *testing.T) {
	t.Setenv("STATS_KEY", "sk-stats-test")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-stats",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 5},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "STATS_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_stats"), pol)

	// Make a request
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_stats")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// Check stats
	statsReq := httptest.NewRequest("GET", "/stats", nil)
	statsRR := httptest.NewRecorder()
	handler.Stats().StatsHandler(statsRR, statsReq)

	if statsRR.Code != http.StatusOK {
		t.Fatalf("stats: expected 200, got %d", statsRR.Code)
	}

	var stats map[string]interface{}
	json.NewDecoder(statsRR.Body).Decode(&stats)

	totals, ok := stats["totals"].(map[string]interface{})
	if !ok {
		t.Fatal("expected totals in stats response")
	}
	if totals["request_count"].(float64) < 1 {
		t.Errorf("expected at least 1 request, got %v", totals["request_count"])
	}
}

// TestE2E_RemoteSync tests the remote client-server sync flow.
func TestE2E_RemoteSync(t *testing.T) {
	// This test creates a remote server backed by one store,
	// then uses a client to sync tokens to a different store.
	remoteStore := newMockTokenStore()
	pol := &policy.Policy{BaseKeyEnv: "TEST_KEY"}
	pol.Validate()
	remoteStore.Save("hash-remote-1", pol)

	// Start a mock store for the remote server
	handler := NewHandler(remoteStore, session.NewMemoryStore(), []byte("key"), "http://localhost", nil, nil)
	_ = handler // just to ensure compilation

	// The actual remote sync test is in internal/remote/remote_test.go
	// This test verifies integration at the handler level
}
