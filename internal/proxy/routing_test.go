package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/policy"
)

// TestHandler_MultiProviderRouting verifies that the proxy routes requests
// to different providers based on the requested model name.
func TestHandler_MultiProviderRouting(t *testing.T) {
	t.Setenv("OPENAI_KEY", "sk-openai-test")
	t.Setenv("ANTHROPIC_KEY", "sk-ant-test")

	var capturedAuth string
	var capturedPath string
	var capturedHeaders http.Header

	handler, ts, upstream := setupTestHandler(t, map[string]config.ProviderConfig{
		"openai": {
			// Default bearer auth, default path
		},
		"anthropic": {
			AuthScheme: "header",
			AuthHeader: "x-api-key",
			ChatPath:   "/v1/messages",
			Headers: map[string]string{
				"anthropic-version": "2023-06-01",
			},
		},
	}, func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedPath = r.URL.Path
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "test-response",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "hello"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		}); err != nil {
			t.Fatalf("encode upstream response: %v", err)
		}
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "OPENAI_KEY",
		Providers: map[string][]*policy.ProviderPolicy{
			"openai": {{
				BaseKeyEnv: "OPENAI_KEY",
				ModelRegex: "^gpt-",
			}},
			"anthropic": {{
				BaseKeyEnv: "ANTHROPIC_KEY",
				ModelRegex: "^claude",
			}},
		},
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	ts.Save(hashForTest(handler, "tkn_multi"), pol)

	// Test 1: GPT model routes to openai with bearer auth
	t.Run("gpt model uses bearer auth", func(t *testing.T) {
		capturedAuth = ""
		capturedPath = ""
		req := httptest.NewRequest("POST", "/v1/chat/completions",
			strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer tkn_multi")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		if capturedAuth != "Bearer sk-openai-test" {
			t.Errorf("expected Bearer auth with openai key, got %q", capturedAuth)
		}
		// Default path used (no ChatPath override for openai)
		if capturedPath != "/v1/chat/completions" {
			t.Errorf("expected path /v1/chat/completions, got %q", capturedPath)
		}
	})

	// Test 2: Claude model routes to anthropic with header auth and custom path
	t.Run("claude model uses header auth and custom path", func(t *testing.T) {
		capturedAuth = ""
		capturedPath = ""
		req := httptest.NewRequest("POST", "/v1/chat/completions",
			strings.NewReader(`{"model":"claude-3-opus","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer tkn_multi")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		// Anthropic uses header auth, not bearer
		if capturedHeaders.Get("x-api-key") != "sk-ant-test" {
			t.Errorf("expected x-api-key = sk-ant-test, got %q", capturedHeaders.Get("x-api-key"))
		}
		// Anthropic has a custom chat path
		if capturedPath != "/v1/messages" {
			t.Errorf("expected path /v1/messages, got %q", capturedPath)
		}
		// Anthropic extra header
		if capturedHeaders.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("expected anthropic-version = 2023-06-01, got %q", capturedHeaders.Get("anthropic-version"))
		}
	})
}

// TestHandler_MultiProviderPerModelBudget verifies that per-model max_tokens
// from provider policies are enforced independently.
func TestHandler_MultiProviderPerModelBudget(t *testing.T) {
	t.Setenv("BUDGET_KEY", "sk-budget-test")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-budget",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		}); err != nil {
			t.Fatalf("encode upstream response: %v", err)
		}
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "BUDGET_KEY",
		MaxTokens:  1000000, // Large global budget
		Providers: map[string][]*policy.ProviderPolicy{
			"openai": {
				{
					BaseKeyEnv: "BUDGET_KEY",
					Model:      "gpt-4o",
					MaxTokens:  50000,
				},
				{
					BaseKeyEnv: "BUDGET_KEY",
					ModelRegex: "^gpt-4o-mini",
					MaxTokens:  500000,
				},
			},
		},
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	ts.Save(hashForTest(handler, "tkn_budget"), pol)

	// Verify gpt-4o resolves to 50000 budget
	resolved := pol.ResolveForModel("gpt-4o")
	if resolved.MaxTokens != 50000 {
		t.Errorf("gpt-4o MaxTokens = %d, want 50000", resolved.MaxTokens)
	}

	// Verify gpt-4o-mini resolves to 500000 budget
	resolved = pol.ResolveForModel("gpt-4o-mini")
	if resolved.MaxTokens != 500000 {
		t.Errorf("gpt-4o-mini MaxTokens = %d, want 500000", resolved.MaxTokens)
	}

	// Verify unmatched model gets global budget
	resolved = pol.ResolveForModel("other-model")
	if resolved.MaxTokens != 1000000 {
		t.Errorf("other-model MaxTokens = %d, want 1000000", resolved.MaxTokens)
	}
}

// TestHandler_ProviderUpstreamURLOverride verifies that when a provider policy
// specifies an upstream_url, requests are routed to that URL.
func TestHandler_ProviderUpstreamURLOverride(t *testing.T) {
	t.Setenv("OVERRIDE_KEY", "sk-override-test")

	// Create the override upstream (the one the provider policy points to)
	override := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-override",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "from override"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		}); err != nil {
			t.Fatalf("encode override response: %v", err)
		}
	}))
	defer override.Close()

	handler, ts, defaultUpstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("default upstream should not be called when provider has upstream_url")
	})
	defer defaultUpstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "OVERRIDE_KEY",
		Providers: map[string][]*policy.ProviderPolicy{
			"custom": {{
				BaseKeyEnv:  "OVERRIDE_KEY",
				UpstreamURL: override.URL,
				ModelRegex:  "^custom-",
			}},
		},
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	ts.Save(hashForTest(handler, "tkn_provurl"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"custom-model","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer tkn_provurl")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "from override") {
		t.Errorf("expected response from override upstream, got: %s", rr.Body.String())
	}
}

// TestHandler_UnmatchedModelUsesGlobalPolicy verifies that a request with a
// model not matching any provider falls back to global policy settings.
func TestHandler_UnmatchedModelUsesGlobalPolicy(t *testing.T) {
	t.Setenv("GLOBAL_KEY", "sk-global-test")
	t.Setenv("PROVIDER_KEY", "sk-provider-test")

	var capturedAuth string
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-global",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		}); err != nil {
			t.Fatalf("encode upstream response: %v", err)
		}
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "GLOBAL_KEY",
		Providers: map[string][]*policy.ProviderPolicy{
			"openai": {{
				BaseKeyEnv: "PROVIDER_KEY",
				Model:      "gpt-4o", // Only matches gpt-4o
			}},
		},
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	ts.Save(hashForTest(handler, "tkn_global"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"unknown-model","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer tkn_global")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	// Should use global key since no provider matched
	if capturedAuth != "Bearer sk-global-test" {
		t.Errorf("expected global key auth, got %q", capturedAuth)
	}
}

// TestHandler_ProviderPromptInjection verifies that provider prompts are
// prepended and global prompts are included when routing to a matched provider.
func TestHandler_ProviderPromptInjection(t *testing.T) {
	t.Setenv("PROMPT_KEY", "sk-prompt-test")

	var capturedBody map[string]interface{}
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode captured request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-prompt",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		}); err != nil {
			t.Fatalf("encode upstream response: %v", err)
		}
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "PROMPT_KEY",
		Prompts: []policy.Message{
			{Role: "system", Content: "Global instruction"},
		},
		Providers: map[string][]*policy.ProviderPolicy{
			"openai": {{
				BaseKeyEnv: "PROMPT_KEY",
				ModelRegex: "^gpt-",
				Prompts: []policy.Message{
					{Role: "system", Content: "Provider instruction"},
				},
			}},
		},
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	ts.Save(hashForTest(handler, "tkn_prompt"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer tkn_prompt")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	messages, ok := capturedBody["messages"].([]interface{})
	if !ok {
		t.Fatal("expected messages in captured body")
	}

	// Should have: provider prompt, global prompt, user message = 3 total
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages (2 prompts + 1 user), got %d", len(messages))
	}

	// First message: provider prompt (prepended)
	msg0, _ := messages[0].(map[string]interface{})
	if msg0["content"] != "Provider instruction" {
		t.Errorf("messages[0] content = %q, want Provider instruction", msg0["content"])
	}

	// Second message: global prompt
	msg1, _ := messages[1].(map[string]interface{})
	if msg1["content"] != "Global instruction" {
		t.Errorf("messages[1] content = %q, want Global instruction", msg1["content"])
	}

	// Third message: original user message
	msg2, _ := messages[2].(map[string]interface{})
	if msg2["content"] != "hi" {
		t.Errorf("messages[2] content = %q, want hi", msg2["content"])
	}
}

// TestHandler_QueryAuthRouting verifies query-based auth for providers
// like Google Gemini that use ?key= parameter.
func TestHandler_QueryAuthRouting(t *testing.T) {
	t.Setenv("GEMINI_KEY", "AIza-test-key")
	t.Setenv("OPENAI_KEY", "sk-openai-test")

	var lastQueryKey string
	var lastBearerAuth string
	handler, ts, upstream := setupTestHandler(t, map[string]config.ProviderConfig{
		"google_gemini": {
			AuthScheme: "query",
		},
		"openai": {},
	}, func(w http.ResponseWriter, r *http.Request) {
		lastQueryKey = r.URL.Query().Get("key")
		lastBearerAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "test-resp",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		}); err != nil {
			t.Fatalf("encode upstream response: %v", err)
		}
	})
	defer upstream.Close()

	pol := &policy.Policy{
		Providers: map[string][]*policy.ProviderPolicy{
			"google_gemini": {{
				BaseKeyEnv: "GEMINI_KEY",
				ModelRegex: "^gemini",
			}},
			"openai": {{
				BaseKeyEnv: "OPENAI_KEY",
				ModelRegex: "^gpt-",
			}},
		},
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	ts.Save(hashForTest(handler, "tkn_qauth"), pol)

	// Gemini request uses query auth
	t.Run("gemini uses query auth", func(t *testing.T) {
		lastQueryKey = ""
		lastBearerAuth = ""
		req := httptest.NewRequest("POST", "/v1/chat/completions",
			strings.NewReader(`{"model":"gemini-pro","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer tkn_qauth")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		if lastQueryKey != "AIza-test-key" {
			t.Errorf("expected query key = AIza-test-key, got %q", lastQueryKey)
		}
	})

	// OpenAI request uses bearer auth
	t.Run("openai uses bearer auth", func(t *testing.T) {
		lastQueryKey = ""
		lastBearerAuth = ""
		req := httptest.NewRequest("POST", "/v1/chat/completions",
			strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("Authorization", "Bearer tkn_qauth")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		if lastBearerAuth != "Bearer sk-openai-test" {
			t.Errorf("expected bearer auth, got %q", lastBearerAuth)
		}
	})
}
