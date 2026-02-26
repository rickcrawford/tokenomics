package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rickcrawford/tokenomics/internal/policy"
	"github.com/rickcrawford/tokenomics/internal/session"
)

func TestShouldRetry(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *policy.RetryConfig
		statusCode int
		want       bool
	}{
		{"nil config", nil, 500, false},
		{"zero retries", &policy.RetryConfig{MaxRetries: 0}, 500, false},
		{"500 with default retry_on", &policy.RetryConfig{MaxRetries: 2}, 500, true},
		{"429 with default retry_on", &policy.RetryConfig{MaxRetries: 2}, 429, true},
		{"502 with default retry_on", &policy.RetryConfig{MaxRetries: 2}, 502, true},
		{"503 with default retry_on", &policy.RetryConfig{MaxRetries: 2}, 503, true},
		{"400 not retryable by default", &policy.RetryConfig{MaxRetries: 2}, 400, false},
		{"200 not retryable", &policy.RetryConfig{MaxRetries: 2}, 200, false},
		{"custom retry_on", &policy.RetryConfig{MaxRetries: 1, RetryOn: []int{418}}, 418, true},
		{"custom retry_on miss", &policy.RetryConfig{MaxRetries: 1, RetryOn: []int{418}}, 500, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRetry(tt.cfg, tt.statusCode)
			if got != tt.want {
				t.Errorf("shouldRetry(%v, %d) = %v, want %v", tt.cfg, tt.statusCode, got, tt.want)
			}
		})
	}
}

func TestExtractAssistantContent(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "valid response",
			body: `{"choices":[{"message":{"role":"assistant","content":"Hello!"}}]}`,
			want: "Hello!",
		},
		{
			name: "empty choices",
			body: `{"choices":[]}`,
			want: "",
		},
		{
			name: "no choices",
			body: `{"id":"test"}`,
			want: "",
		},
		{
			name: "invalid json",
			body: `{bad json}`,
			want: "",
		},
		{
			name: "no content field",
			body: `{"choices":[{"message":{"role":"assistant"}}]}`,
			want: "",
		},
		{
			name: "no message field",
			body: `{"choices":[{"index":0}]}`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAssistantContent([]byte(tt.body))
			if got != tt.want {
				t.Errorf("extractAssistantContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCountResponseTokens(t *testing.T) {
	handler := NewHandler(newMockTokenStore(), session.NewMemoryStore(), []byte("key"), "http://localhost", nil, nil)

	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "valid usage",
			body: `{"usage":{"completion_tokens":42}}`,
			want: 42,
		},
		{
			name: "zero tokens",
			body: `{"usage":{"completion_tokens":0}}`,
			want: 0,
		},
		{
			name: "no usage field",
			body: `{"id":"test"}`,
			want: 0,
		},
		{
			name: "invalid json",
			body: `{broken}`,
			want: 0,
		},
		{
			name: "usage without completion_tokens",
			body: `{"usage":{"prompt_tokens":10}}`,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.countResponseTokens([]byte(tt.body), "test-token")
			if got != tt.want {
				t.Errorf("countResponseTokens() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSafePrefix(t *testing.T) {
	tests := []struct {
		s    string
		n    int
		want string
	}{
		{"abcdef", 3, "abc"},
		{"ab", 5, "ab"},
		{"", 5, ""},
		{"hello", 5, "hello"},
		{"hello", 0, ""},
	}
	for _, tt := range tests {
		got := safePrefix(tt.s, tt.n)
		if got != tt.want {
			t.Errorf("safePrefix(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}

func TestIsChatCompletions(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/v1/chat/completions", true},
		{"/chat/completions", true},
		{"/v1/models", false},
		{"/v1/completions", false},
		{"/v1/chat/completions/extra", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isChatCompletions(tt.path)
		if got != tt.want {
			t.Errorf("isChatCompletions(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestCopyHeaders(t *testing.T) {
	src := make(http.Header)
	src.Set("Content-Type", "application/json")
	src.Add("X-Custom", "val1")
	src.Add("X-Custom", "val2")

	dst := make(http.Header)
	copyHeaders(src, dst)

	if dst.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", dst.Get("Content-Type"))
	}
	if vals := dst.Values("X-Custom"); len(vals) != 2 {
		t.Errorf("X-Custom values = %v, want 2 values", vals)
	}
}

func TestHttpError(t *testing.T) {
	rr := httptest.NewRecorder()
	httpError(rr, http.StatusForbidden, "test error message")

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("missing error object")
	}
	if errObj["message"] != "test error message" {
		t.Errorf("message = %q, want 'test error message'", errObj["message"])
	}
	if errObj["code"].(float64) != 403 {
		t.Errorf("code = %v, want 403", errObj["code"])
	}
}

func TestHandler_XApiKeyAuth(t *testing.T) {
	t.Setenv("XAPI_KEY", "sk-xapi-test")

	var capturedAuth string
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-xapi",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "hi"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "XAPI_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_xapi"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("x-api-key", "tkn_xapi")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if capturedAuth != "Bearer sk-xapi-test" {
		t.Errorf("expected Bearer auth, got %q", capturedAuth)
	}
}

func TestHandler_RawAuthToken(t *testing.T) {
	t.Setenv("RAW_KEY", "sk-raw-test")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-raw",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "hi"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "RAW_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_raw"), pol)

	// Send Authorization header without Bearer prefix
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "tkn_raw")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandler_InvalidBody(t *testing.T) {
	t.Setenv("BODY_KEY", "sk-body-test")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called for invalid body")
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "BODY_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_body"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{not valid json}`))
	req.Header.Set("Authorization", "Bearer tkn_body")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandler_MissingAPIKey(t *testing.T) {
	// Intentionally don't set the env var
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called when API key is missing")
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "NONEXISTENT_KEY_12345"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_nokey"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer tkn_nokey")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "not set") {
		t.Errorf("expected 'not set' in error, got: %s", rr.Body.String())
	}
}

func TestHandler_StreamingResponse(t *testing.T) {
	t.Setenv("STREAM_KEY", "sk-stream-test")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		// Verify the proxy sent the stream request
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true {
			t.Error("expected stream=true in upstream request")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		lines := []string{
			"data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n",
			"\n",
			"data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n",
			"\n",
			"data: {\"id\":\"chatcmpl-stream\",\"choices\":[],\"usage\":{\"completion_tokens\":5}}\n",
			"\n",
			"data: [DONE]\n",
			"\n",
		}

		for _, line := range lines {
			fmt.Fprint(w, line)
		}
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "STREAM_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_stream"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_stream")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify SSE content type was set
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream content type, got %q", ct)
	}
}

func TestHandler_WarnRuleAllows(t *testing.T) {
	t.Setenv("WARN_KEY", "sk-warn-test")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-warn",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "WARN_KEY",
		Rules: policy.RuleList{
			{Type: "keyword", Keywords: []string{"password"}, Action: "warn"},
		},
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_warn"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"my password is secret"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_warn")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Warn rules should still allow the request
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 (warn should not block), got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandler_PassthroughBearerAuth(t *testing.T) {
	t.Setenv("PT_KEY", "sk-pt-test")

	var capturedAuth string
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[]}`))
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "PT_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_pt"), pol)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer tkn_pt")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	// Passthrough should swap the wrapper token for the real API key
	if capturedAuth != "Bearer sk-pt-test" {
		t.Errorf("expected Bearer auth with real key, got %q", capturedAuth)
	}
}

func TestHandler_UpstreamURLOverride(t *testing.T) {
	t.Setenv("OVERRIDE_KEY", "sk-override")

	handler, ts, _ := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("default upstream should not be called")
	})

	// Create a separate upstream for the policy override
	override := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-override",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "from override"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	}))
	defer override.Close()

	pol := &policy.Policy{
		BaseKeyEnv:  "OVERRIDE_KEY",
		UpstreamURL: override.URL,
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_override"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_override")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandler_PolicyWithMetadata(t *testing.T) {
	t.Setenv("META_KEY", "sk-meta-test")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-meta",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "META_KEY",
		Metadata:   map[string]string{"team": "engineering", "project": "test"},
	}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_meta"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_meta")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandler_XApiKeyHeaderCleaned(t *testing.T) {
	t.Setenv("XAPI_CLEAN_KEY", "sk-real-api-key")

	var upstreamHeaders http.Header
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-clean",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "XAPI_CLEAN_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_clean"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
	))
	// Send wrapped token via x-api-key header
	req.Header.Set("x-api-key", "tkn_clean")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify wrapped token was NOT sent to upstream
	if upstreamHeaders.Get("x-api-key") == "tkn_clean" {
		t.Error("wrapped token (tkn_clean) was sent to upstream - should have been cleaned")
	}

	// Verify real key was sent (via Authorization header for bearer auth)
	authHeader := upstreamHeaders.Get("Authorization")
	if !strings.Contains(authHeader, "sk-real-api-key") {
		t.Errorf("expected real API key in Authorization header, got %q", authHeader)
	}
}

func TestHandler_AuthorizationHeaderCleaned(t *testing.T) {
	t.Setenv("AUTH_CLEAN_KEY", "sk-auth-real-key")

	var upstreamHeaders http.Header
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-authclean",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		})
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "AUTH_CLEAN_KEY"}
	pol.Validate()
	ts.Save(hashForTest(handler, "tkn_authclean"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
	))
	// Send wrapped token via Authorization header
	req.Header.Set("Authorization", "Bearer tkn_authclean")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify wrapped token was NOT sent to upstream
	authHeader := upstreamHeaders.Get("Authorization")
	if strings.Contains(authHeader, "tkn_authclean") {
		t.Errorf("wrapped token (tkn_authclean) was sent to upstream in Authorization header: %q", authHeader)
	}

	// Verify real key was sent
	if !strings.Contains(authHeader, "sk-auth-real-key") {
		t.Errorf("expected real API key in Authorization header, got %q", authHeader)
	}
}
