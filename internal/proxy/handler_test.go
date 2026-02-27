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
	"github.com/rickcrawford/tokenomics/internal/store"
)

// mockTokenStore is a minimal in-memory store for tests.
type mockTokenStore struct {
	tokens map[string]*policy.Policy
}

func (m *mockTokenStore) Init() error   { return nil }
func (m *mockTokenStore) Close() error  { return nil }
func (m *mockTokenStore) Reload() error { return nil }
func (m *mockTokenStore) Create(hash string, polJSON string, expiresAt string) error {
	p, err := policy.Parse(polJSON)
	if err != nil {
		return err
	}
	m.tokens[hash] = p
	return nil
}
func (m *mockTokenStore) Get(hash string) (*store.TokenRecord, error)                { return nil, nil }
func (m *mockTokenStore) Update(hash string, polJSON string, expiresAt string) error { return nil }
func (m *mockTokenStore) Delete(hash string) error                                   { delete(m.tokens, hash); return nil }
func (m *mockTokenStore) List() ([]store.TokenRecord, error)                         { return nil, nil }
func (m *mockTokenStore) Lookup(hash string) (*policy.Policy, error) {
	return m.tokens[hash], nil
}

// Save directly stores a validated policy (test helper, not part of interface).
func (m *mockTokenStore) Save(hash string, pol *policy.Policy) {
	m.tokens[hash] = pol
}

func newMockTokenStore() *mockTokenStore {
	return &mockTokenStore{tokens: make(map[string]*policy.Policy)}
}

// setupTestHandler creates a handler with a test upstream server and returns the handler,
// a function to store policies, and a cleanup function.
func setupTestHandler(t *testing.T, providers map[string]config.ProviderConfig, upstreamHandler http.HandlerFunc) (*Handler, *mockTokenStore, *httptest.Server) {
	t.Helper()
	upstream := httptest.NewServer(upstreamHandler)

	ts := newMockTokenStore()
	handler := NewHandler(ts, session.NewMemoryStore(), []byte("testkey"), upstream.URL, providers, nil)

	return handler, ts, upstream
}

func TestHandler_BearerAuth(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-test-123")

	var capturedAuth string
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "hi"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "TEST_API_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_test"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer tkn_test")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if capturedAuth != "Bearer sk-test-123" {
		t.Errorf("expected Bearer auth, got %q", capturedAuth)
	}
}

func TestHandler_HeaderAuth(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")

	var capturedAuthHeader string
	var capturedExtraHeader string
	handler, ts, upstream := setupTestHandler(t, map[string]config.ProviderConfig{
		"anthropic": {
			AuthScheme: "header",
			AuthHeader: "x-api-key",
			Headers: map[string]string{
				"anthropic-version": "2023-06-01",
			},
			ChatPath: "/v1/messages",
		},
	}, func(w http.ResponseWriter, r *http.Request) {
		capturedAuthHeader = r.Header.Get("x-api-key")
		capturedExtraHeader = r.Header.Get("anthropic-version")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "msg_test",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "hi"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{
		Providers: map[string][]*policy.ProviderPolicy{
			"anthropic": {{
				BaseKeyEnv: "ANTHROPIC_API_KEY",
				ModelRegex: "^claude",
			}},
		},
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_ant"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"claude-3-opus","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer tkn_ant")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if capturedAuthHeader != "sk-ant-test" {
		t.Errorf("expected x-api-key = %q, got %q", "sk-ant-test", capturedAuthHeader)
	}
	if capturedExtraHeader != "2023-06-01" {
		t.Errorf("expected anthropic-version = %q, got %q", "2023-06-01", capturedExtraHeader)
	}
}

func TestHandler_QueryAuth(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "AIza-test-key")

	var capturedQueryKey string
	var capturedAuthHeader string
	handler, ts, upstream := setupTestHandler(t, map[string]config.ProviderConfig{
		"google_gemini": {
			AuthScheme: "query",
			ChatPath:   "/v1beta/models/gemini-pro:generateContent",
		},
	}, func(w http.ResponseWriter, r *http.Request) {
		capturedQueryKey = r.URL.Query().Get("key")
		capturedAuthHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"responseId": "resp-test",
			"choices":    []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "hi"}}},
			"usage":      map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{
		Providers: map[string][]*policy.ProviderPolicy{
			"google_gemini": {{
				BaseKeyEnv: "GOOGLE_API_KEY",
				ModelRegex: "^gemini",
			}},
		},
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_gem"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gemini-pro","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer tkn_gem")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if capturedQueryKey != "AIza-test-key" {
		t.Errorf("expected query key = %q, got %q", "AIza-test-key", capturedQueryKey)
	}
	if capturedAuthHeader != "" {
		t.Errorf("expected no Authorization header for query auth, got %q", capturedAuthHeader)
	}
}

func TestHandler_ChatPathOverride(t *testing.T) {
	t.Setenv("TEST_KEY", "test-key")

	var capturedPath string
	handler, ts, upstream := setupTestHandler(t, map[string]config.ProviderConfig{
		"cohere": {
			ChatPath: "/v2/chat",
		},
	}, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "test-id",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "hi"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{
		Providers: map[string][]*policy.ProviderPolicy{
			"cohere": {{
				BaseKeyEnv: "TEST_KEY",
				ModelRegex: "^command",
			}},
		},
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_co"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"command-r","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer tkn_co")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if capturedPath != "/v2/chat" {
		t.Errorf("expected path = %q, got %q", "/v2/chat", capturedPath)
	}
}

func TestHandler_ProviderAPIKeyEnvFallback(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "groq-test-key")

	var capturedAuth string
	handler, ts, upstream := setupTestHandler(t, map[string]config.ProviderConfig{
		"groq": {
			APIKeyEnv: "GROQ_API_KEY",
		},
	}, func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "test-id",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "hi"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	// Policy references groq provider but doesn't set base_key_env at global level
	// The handler should fall back to provider config's api_key_env
	pol := &policy.Policy{
		Providers: map[string][]*policy.ProviderPolicy{
			"groq": {{
				BaseKeyEnv: "GROQ_API_KEY",
				ModelRegex: "^llama",
			}},
		},
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_groq"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"llama-3-70b","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer tkn_groq")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if capturedAuth != "Bearer groq-test-key" {
		t.Errorf("expected auth = %q, got %q", "Bearer groq-test-key", capturedAuth)
	}
}

func TestHandler_ProviderNameInResolvedPolicy(t *testing.T) {
	pol := &policy.Policy{
		BaseKeyEnv: "TEST_KEY",
		Providers: map[string][]*policy.ProviderPolicy{
			"openai": {{
				BaseKeyEnv: "OPENAI_KEY",
				Model:      "gpt-4o",
			}},
			"anthropic": {{
				BaseKeyEnv: "ANTHROPIC_KEY",
				ModelRegex: "^claude",
			}},
		},
	}
	pol.Validate()

	resolved := pol.ResolveForModel("gpt-4o")
	if resolved.ProviderName != "openai" {
		t.Errorf("expected ProviderName = %q, got %q", "openai", resolved.ProviderName)
	}

	resolved = pol.ResolveForModel("claude-3-opus")
	if resolved.ProviderName != "anthropic" {
		t.Errorf("expected ProviderName = %q, got %q", "anthropic", resolved.ProviderName)
	}

	// No matching provider should have empty provider name
	resolved = pol.ResolveForModel("unknown-model")
	if resolved.ProviderName != "" {
		t.Errorf("expected empty ProviderName for unmatched model, got %q", resolved.ProviderName)
	}
}

func TestHandler_MultiProviderHeaders(t *testing.T) {
	t.Setenv("TEST_KEY", "test-key")

	var capturedHeaders http.Header
	handler, ts, upstream := setupTestHandler(t, map[string]config.ProviderConfig{
		"custom": {
			Headers: map[string]string{
				"X-Custom-Header": "custom-value",
				"X-Team-Id":       "team-123",
			},
		},
	}, func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "test-id",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "hi"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{
		Providers: map[string][]*policy.ProviderPolicy{
			"custom": {{
				BaseKeyEnv: "TEST_KEY",
			}},
		},
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_custom"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer tkn_custom")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if capturedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("expected X-Custom-Header = %q, got %q", "custom-value", capturedHeaders.Get("X-Custom-Header"))
	}
	if capturedHeaders.Get("X-Team-Id") != "team-123" {
		t.Errorf("expected X-Team-Id = %q, got %q", "team-123", capturedHeaders.Get("X-Team-Id"))
	}
}

func TestHandler_InvalidToken(t *testing.T) {
	handler, _, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called for invalid token")
	})
	defer upstream.Close()

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	req.Header.Set("Authorization", "Bearer invalid-token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestHandler_MissingAuth(t *testing.T) {
	handler, _, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called without auth")
	})
	defer upstream.Close()

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestHandler_Passthrough(t *testing.T) {
	t.Setenv("TEST_KEY", "test-key")

	var capturedPath string
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[]}`))
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "TEST_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_pass"), pol)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer tkn_pass")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if capturedPath != "/v1/models" {
		t.Errorf("expected path = %q, got %q", "/v1/models", capturedPath)
	}

	body, _ := io.ReadAll(rr.Body)
	if !strings.Contains(string(body), `"data"`) {
		t.Errorf("expected passthrough response, got %s", body)
	}
}

func TestHandler_PassthroughXApiKeyHeaderCleaned(t *testing.T) {
	t.Setenv("PT_XAPI_KEY", "sk-pt-real-key")

	var upstreamHeaders http.Header
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[]}`))
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "PT_XAPI_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_pt_xapi"), pol)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	// Send wrapped token via x-api-key header
	req.Header.Set("x-api-key", "tkn_pt_xapi")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify wrapped token was NOT sent to upstream
	if upstreamHeaders.Get("x-api-key") == "tkn_pt_xapi" {
		t.Error("wrapped token (tkn_pt_xapi) was sent to upstream via x-api-key - should have been cleaned")
	}

	// Verify real key was sent via Authorization header
	authHeader := upstreamHeaders.Get("Authorization")
	if !strings.Contains(authHeader, "sk-pt-real-key") {
		t.Errorf("expected real API key in Authorization header, got %q", authHeader)
	}
}

func TestHandler_PassthroughAuthHeaderCleaned(t *testing.T) {
	t.Setenv("PT_AUTH_KEY", "sk-pt-auth-real")

	var upstreamHeaders http.Header
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[]}`))
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "PT_AUTH_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_pt_auth"), pol)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	// Send wrapped token via Authorization header
	req.Header.Set("Authorization", "Bearer tkn_pt_auth")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify wrapped token was NOT sent to upstream
	authHeader := upstreamHeaders.Get("Authorization")
	if strings.Contains(authHeader, "tkn_pt_auth") {
		t.Errorf("wrapped token (tkn_pt_auth) was sent to upstream in Authorization header: %q", authHeader)
	}

	// Verify real key was sent
	if !strings.Contains(authHeader, "sk-pt-auth-real") {
		t.Errorf("expected real API key in Authorization header, got %q", authHeader)
	}
}

func hashForTest(h *Handler, token string) string {
	return h.hashToken(token)
}

func TestExtractOpenClawMetadata(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		expected OpenClawMetadata
	}{
		{
			name:     "all_headers_present",
			headers:  map[string]string{
				"X-OpenClaw-Agent-ID":     "agent_123",
				"X-OpenClaw-Agent-Type":   "slack",
				"X-OpenClaw-Team":         "platform",
				"X-OpenClaw-Channel":      "general",
				"X-OpenClaw-Skill":        "search",
				"X-OpenClaw-Environment":  "production",
			},
			expected: OpenClawMetadata{
				AgentID:     "agent_123",
				AgentType:   "slack",
				Team:        "platform",
				Channel:     "general",
				Skill:       "search",
				Environment: "production",
			},
		},
		{
			name:     "partial_headers",
			headers:  map[string]string{
				"X-OpenClaw-Agent-ID": "agent_456",
				"X-OpenClaw-Team":     "ml",
			},
			expected: OpenClawMetadata{
				AgentID: "agent_456",
				Team:    "ml",
			},
		},
		{
			name:     "no_headers",
			headers:  map[string]string{},
			expected: OpenClawMetadata{},
		},
		{
			name:     "empty_header_values",
			headers:  map[string]string{
				"X-OpenClaw-Agent-ID": "",
				"X-OpenClaw-Team":     "",
			},
			expected: OpenClawMetadata{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			result := extractOpenClawMetadata(req)

			if result.AgentID != tt.expected.AgentID {
				t.Errorf("AgentID: expected %q, got %q", tt.expected.AgentID, result.AgentID)
			}
			if result.AgentType != tt.expected.AgentType {
				t.Errorf("AgentType: expected %q, got %q", tt.expected.AgentType, result.AgentType)
			}
			if result.Team != tt.expected.Team {
				t.Errorf("Team: expected %q, got %q", tt.expected.Team, result.Team)
			}
			if result.Channel != tt.expected.Channel {
				t.Errorf("Channel: expected %q, got %q", tt.expected.Channel, result.Channel)
			}
			if result.Skill != tt.expected.Skill {
				t.Errorf("Skill: expected %q, got %q", tt.expected.Skill, result.Skill)
			}
			if result.Environment != tt.expected.Environment {
				t.Errorf("Environment: expected %q, got %q", tt.expected.Environment, result.Environment)
			}
		})
	}
}

func TestOpenClawMetadataToMap(t *testing.T) {
	tests := []struct {
		name     string
		meta     OpenClawMetadata
		expected map[string]string
	}{
		{
			name: "all_fields_populated",
			meta: OpenClawMetadata{
				AgentID:     "agent_123",
				AgentType:   "slack",
				Team:        "platform",
				Channel:     "general",
				Skill:       "search",
				Environment: "production",
			},
			expected: map[string]string{
				"agent_id":    "agent_123",
				"agent_type":  "slack",
				"team":        "platform",
				"channel":     "general",
				"skill":       "search",
				"environment": "production",
			},
		},
		{
			name: "partial_fields",
			meta: OpenClawMetadata{
				AgentID: "agent_456",
				Team:    "ml",
			},
			expected: map[string]string{
				"agent_id": "agent_456",
				"team":     "ml",
			},
		},
		{
			name:     "no_fields",
			meta:     OpenClawMetadata{},
			expected: nil,
		},
		{
			name: "single_field",
			meta: OpenClawMetadata{Channel: "alerts"},
			expected: map[string]string{
				"channel": "alerts",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := openClawMetadataToMap(tt.meta)

			if len(result) != len(tt.expected) {
				t.Errorf("length: expected %d, got %d", len(tt.expected), len(result))
			}

			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("key %q: expected %q, got %q", k, v, result[k])
				}
			}

			// Check no extra keys
			for k := range result {
				if _, exists := tt.expected[k]; !exists {
					t.Errorf("unexpected key in result: %q", k)
				}
			}
		})
	}
}

func TestHandler_OpenClawMetadataInRequest(t *testing.T) {
	t.Setenv("TEST_API_KEY", "sk-test-123")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-test",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "response"}}},
			"usage":   map[string]interface{}{"completion_tokens": 10},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "TEST_API_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_ocm_test"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4","messages":[{"role":"user","content":"test"}]}`))
	req.Header.Set("Authorization", "Bearer tkn_ocm_test")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-OpenClaw-Agent-ID", "agent_test_123")
	req.Header.Set("X-OpenClaw-Team", "ml")
	req.Header.Set("X-OpenClaw-Channel", "alerts")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}
