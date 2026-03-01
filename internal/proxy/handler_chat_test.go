package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rickcrawford/tokenomics/internal/ledger"
	"github.com/rickcrawford/tokenomics/internal/policy"
	"github.com/rickcrawford/tokenomics/internal/session"
	"github.com/rickcrawford/tokenomics/internal/tokencount"
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
		desc string
	}{
		{
			name: "OpenAI completion_tokens",
			body: `{"usage":{"completion_tokens":42}}`,
			want: 42,
			desc: "OpenAI format with completion_tokens",
		},
		{
			name: "Anthropic output_tokens",
			body: `{"usage":{"input_tokens":10,"output_tokens":25}}`,
			want: 25,
			desc: "Anthropic format with output_tokens",
		},
		{
			name: "zero tokens",
			body: `{"usage":{"completion_tokens":0}}`,
			want: 0,
			desc: "Zero completion tokens",
		},
		{
			name: "no usage field",
			body: `{"id":"test"}`,
			want: 0,
			desc: "Response without usage field",
		},
		{
			name: "invalid json",
			body: `{broken}`,
			want: 0,
			desc: "Malformed JSON",
		},
		{
			name: "usage without token fields",
			body: `{"usage":{"prompt_tokens":10,"cached_tokens":5}}`,
			want: 0,
			desc: "Usage object without output/completion tokens",
		},
		{
			name: "Anthropic with null output_tokens",
			body: `{"usage":{"input_tokens":10,"output_tokens":null}}`,
			want: 0,
			desc: "Anthropic format with null output_tokens",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := handler.countResponseTokens([]byte(tt.body), "test-token")
			if got != tt.want {
				t.Errorf("%s: got %d, want %d", tt.desc, got, tt.want)
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
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-xapi",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "hi"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		}); err != nil {
			t.Fatalf("encode upstream response: %v", err)
		}
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "XAPI_KEY"}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
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
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-raw",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "hi"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		}); err != nil {
			t.Fatalf("encode upstream response: %v", err)
		}
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "RAW_KEY"}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
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
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
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
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
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
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
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
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
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

func TestHandler_RetryThenFallbackModel(t *testing.T) {
	t.Setenv("FALLBACK_KEY", "sk-fallback-test")

	var primaryCalls int
	var fallbackCalls int
	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		model, _ := body["model"].(string)

		if model == "gpt-4o" {
			primaryCalls++
			w.WriteHeader(http.StatusTooManyRequests)
			if _, err := w.Write([]byte(`{"error":{"message":"rate limited"}}`)); err != nil {
				t.Fatalf("write rate-limit response: %v", err)
			}
			return
		}

		if model == "gpt-4o-mini" {
			fallbackCalls++
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]interface{}{
				"id":      "chatcmpl-fallback",
				"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "fallback ok"}}},
				"usage":   map[string]interface{}{"completion_tokens": 1},
			}); err != nil {
				t.Fatalf("encode fallback response: %v", err)
			}
			return
		}

		w.WriteHeader(http.StatusBadRequest)
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "FALLBACK_KEY",
		Retry: &policy.RetryConfig{
			MaxRetries: 1,
			Fallbacks:  []string{"gpt-4o-mini"},
			RetryOn:    []int{429},
		},
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	ts.Save(hashForTest(handler, "tkn_fallback"), pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer tkn_fallback")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if primaryCalls != 2 {
		t.Fatalf("expected 2 primary attempts (initial + retry), got %d", primaryCalls)
	}
	if fallbackCalls != 1 {
		t.Fatalf("expected 1 fallback attempt, got %d", fallbackCalls)
	}
	if !strings.Contains(rr.Body.String(), "fallback ok") {
		t.Fatalf("expected fallback response body, got %s", rr.Body.String())
	}
}

func TestHandler_BudgetReservationConcurrent(t *testing.T) {
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

	messages := []map[string]interface{}{
		{"role": "user", "content": "hello"},
	}
	inputTokens, err := tokencount.CountMessages("gpt-4o", messages)
	if err != nil {
		t.Fatalf("token count failed: %v", err)
	}

	pol := &policy.Policy{
		BaseKeyEnv: "BUDGET_KEY",
		MaxTokens:  int64(inputTokens),
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	ts.Save(hashForTest(handler, "tkn_budget"), pol)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	var wg sync.WaitGroup
	statuses := make([]int, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer tkn_budget")
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			statuses[idx] = rr.Code
		}(i)
	}
	wg.Wait()

	successes := 0
	rejections := 0
	for _, code := range statuses {
		if code == http.StatusOK {
			successes++
		}
		if code == http.StatusTooManyRequests {
			rejections++
		}
	}
	if successes != 1 || rejections != 1 {
		t.Fatalf("expected one success and one budget rejection, got statuses=%v", statuses)
	}
}

func TestHandler_WarnRuleAllows(t *testing.T) {
	t.Setenv("WARN_KEY", "sk-warn-test")

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-warn",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		}); err != nil {
			t.Fatalf("encode upstream response: %v", err)
		}
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "WARN_KEY",
		Rules: policy.RuleList{
			{Type: "keyword", Keywords: []string{"password"}, Action: "warn"},
		},
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
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
		if _, err := w.Write([]byte(`{"data":[]}`)); err != nil {
			t.Fatalf("write passthrough response: %v", err)
		}
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "PT_KEY"}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
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
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-override",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "from override"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		}); err != nil {
			t.Fatalf("encode override response: %v", err)
		}
	}))
	defer override.Close()

	pol := &policy.Policy{
		BaseKeyEnv:  "OVERRIDE_KEY",
		UpstreamURL: override.URL,
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
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
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-meta",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		}); err != nil {
			t.Fatalf("encode upstream response: %v", err)
		}
	})
	defer upstream.Close()

	pol := &policy.Policy{
		BaseKeyEnv: "META_KEY",
		Metadata:   map[string]string{"team": "engineering", "project": "test"},
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
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
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-clean",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		}); err != nil {
			t.Fatalf("encode upstream response: %v", err)
		}
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "XAPI_CLEAN_KEY"}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
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
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"id":      "chatcmpl-authclean",
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{"content": "ok"}}},
			"usage":   map[string]interface{}{"completion_tokens": 1},
		}); err != nil {
			t.Fatalf("encode upstream response: %v", err)
		}
	})
	defer upstream.Close()

	pol := &policy.Policy{BaseKeyEnv: "AUTH_CLEAN_KEY"}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
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

// TestDecompressResponseBody tests gzip decompression
func TestDecompressResponseBody(t *testing.T) {
	tests := []struct {
		name            string
		body            []byte
		contentEncoding string
		wantDecompressed bool
	}{
		{
			name:            "no encoding",
			body:            []byte("plain text"),
			contentEncoding: "",
			wantDecompressed: false,
		},
		{
			name:            "non-gzip encoding",
			body:            []byte("deflate text"),
			contentEncoding: "deflate",
			wantDecompressed: false,
		},
		{
			name:            "gzip encoding but invalid data",
			body:            []byte("not gzip data"),
			contentEncoding: "gzip",
			wantDecompressed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := decompressResponseBody(tt.body, tt.contentEncoding)
			if tt.wantDecompressed && bytes.Equal(result, tt.body) {
				t.Errorf("expected decompression, but got same body")
			}
		})
	}
}

// TestFormatResponseForMemory tests response formatting for memory storage
func TestFormatResponseForMemory(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		contentType string
		wantContent bool
		wantContains string
	}{
		{
			name: "JSON response with assistant content",
			body: `{
				"choices": [
					{
						"message": {
							"content": "Hello, this is the assistant response!"
						}
					}
				]
			}`,
			contentType: "application/json",
			wantContent: true,
			wantContains: "Hello, this is the assistant response!",
		},
		{
			name: "JSON response with error",
			body: `{
				"error": {
					"message": "API key invalid"
				}
			}`,
			contentType: "application/json",
			wantContent: true,
			wantContains: "Error",
		},
		{
			name: "Invalid JSON",
			body: "not valid json",
			contentType: "text/plain",
			wantContent: true,
			wantContains: "not valid json",
		},
		{
			name: "JSON without assistant content",
			body: `{
				"id": "resp_123",
				"usage": {"completion_tokens": 3}
			}`,
			contentType: "application/json",
			wantContent: true,
			wantContains: `"id": "resp_123"`,
		},
		{
			name: "JSON with delta (streaming)",
			body: `{
				"choices": [
					{
						"delta": {
							"content": "Streaming response"
						}
					}
				]
			}`,
			contentType: "application/json",
			wantContent: true,
			wantContains: "Streaming response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatResponseForMemory([]byte(tt.body), tt.contentType)
			if tt.wantContent && result == "" {
				t.Errorf("formatResponseForMemory returned empty string, want non-empty")
			}
			if tt.wantContains != "" && !strings.Contains(result, tt.wantContains) {
				t.Errorf("formatResponseForMemory result doesn't contain %q, got: %s", tt.wantContains, result)
			}
		})
	}
}

func TestFormatResponseForMemory_NonJSONBinaryRaw(t *testing.T) {
	body := []byte{0x00, 0xff, 0x01, 'A'}
	got := formatResponseForMemory(body, "application/octet-stream")
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	if strings.Contains(got, "Non-JSON response") {
		t.Fatalf("expected raw output, got summary %q", got)
	}
}

func TestHandler_StreamingMemoryDoesNotStoreRawSSE(t *testing.T) {
	t.Setenv("STREAM_MEM_KEY", "sk-stream-mem-test")
	memDir := t.TempDir()

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		lines := []string{
			"data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n",
			"\n",
			"data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n",
			"\n",
			"data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"delta\":{\"content\":\" world\"}}],\"usage\":{\"completion_tokens\":2}}\n",
			"\n",
			"data: [DONE]\n",
			"\n",
		}
		for _, line := range lines {
			fmt.Fprint(w, line)
		}
	})
	defer upstream.Close()

	token := "tkn_stream_mem"
	tokenHash := hashForTest(handler, token)
	pol := &policy.Policy{
		BaseKeyEnv: "STREAM_MEM_KEY",
		Memory: policy.MemoryConfig{
			Enabled:  true,
			FilePath: memDir,
			FileName: "{token_hash}.md",
		},
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	ts.Save(tokenHash, pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	memoryPath := filepath.Join(memDir, tokenHash[:16]+".md")
	data, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatalf("failed to read memory file: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "Hello world") {
		t.Fatalf("expected assistant text in memory, got: %s", content)
	}
	if !strings.Contains(content, "[streaming sse]") || !strings.Contains(content, "SSE events:") {
		t.Fatalf("expected parsed SSE section in memory, got: %s", content)
	}
	if !strings.Contains(content, "[DONE]") {
		t.Fatalf("expected [DONE] marker in parsed SSE memory, got: %s", content)
	}
	if strings.Contains(content, "data: {") || strings.Contains(content, "data: [DONE]") {
		t.Fatalf("expected no raw SSE frames in memory, got: %s", content)
	}
}

func TestHandler_StreamingResponseLongSSELine(t *testing.T) {
	t.Setenv("STREAM_LONG_KEY", "sk-stream-long")
	memDir := t.TempDir()

	longContent := strings.Repeat("a", 100000)
	payload, err := json.Marshal(map[string]interface{}{
		"id": "chatcmpl-long",
		"choices": []interface{}{
			map[string]interface{}{
				"delta": map[string]interface{}{
					"content": longContent,
				},
			},
		},
		"usage": map[string]interface{}{
			"completion_tokens": 3,
		},
	})
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: %s\n\n", string(payload))
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer upstream.Close()

	token := "tkn_stream_long"
	tokenHash := hashForTest(handler, token)
	pol := &policy.Policy{
		BaseKeyEnv: "STREAM_LONG_KEY",
		Memory: policy.MemoryConfig{
			Enabled:  true,
			FilePath: memDir,
			FileName: "{token_hash}.md",
		},
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	ts.Save(tokenHash, pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	data, err := os.ReadFile(filepath.Join(memDir, tokenHash[:16]+".md"))
	if err != nil {
		t.Fatalf("failed to read memory file: %v", err)
	}
	if !strings.Contains(string(data), "assistant") {
		t.Fatalf("expected assistant entry in memory file")
	}
	if strings.Contains(string(data), "data: {") {
		t.Fatalf("expected no raw SSE JSON frames in memory file")
	}
}

func TestHandler_StreamingLedgerEvents_ChunkOrder(t *testing.T) {
	t.Setenv("STREAM_EVT_KEY", "sk-stream-event")
	ledgerDir := t.TempDir()

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"delta\":{\"content\":\"A\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"id\":\"chatcmpl-stream\",\"choices\":[{\"delta\":{\"content\":\"B\"}}],\"usage\":{\"completion_tokens\":2}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer upstream.Close()

	l, err := ledger.Open(ledgerDir, false, true)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	handler.SetLedger(l)

	token := "tkn_stream_evt"
	tokenHash := hashForTest(handler, token)
	pol := &policy.Policy{BaseKeyEnv: "STREAM_EVT_KEY"}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	ts.Save(tokenHash, pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	if err := l.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}
	sessions, err := ledger.ReadSessionFiles(ledgerDir)
	if err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	var chunkBodies []string
	for _, ev := range sessions[0].CommunicationEvents {
		if ev.Type == ledger.CommunicationEventResponseChunk {
			chunkBodies = append(chunkBodies, ev.Body)
		}
	}
	if len(chunkBodies) < 2 {
		t.Fatalf("expected at least 2 chunk events, got %d", len(chunkBodies))
	}
	if chunkBodies[0] != `{"id":"chatcmpl-stream","choices":[{"delta":{"content":"A"}}]}` {
		t.Fatalf("unexpected first chunk body: %q", chunkBodies[0])
	}
	if chunkBodies[1] != `{"id":"chatcmpl-stream","choices":[{"delta":{"content":"B"}}],"usage":{"completion_tokens":2}}` {
		t.Fatalf("unexpected second chunk body: %q", chunkBodies[1])
	}
}

func TestHandler_RetryLedgerEvents_Sequence(t *testing.T) {
	t.Setenv("RETRY_EVT_KEY", "sk-retry-event")
	ledgerDir := t.TempDir()
	var callCount int

	handler, ts, upstream := setupTestHandler(t, nil, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error":{"message":"temporary"}}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"chatcmpl-ok","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"completion_tokens":1}}`)
	})
	defer upstream.Close()

	l, err := ledger.Open(ledgerDir, false, true)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	handler.SetLedger(l)

	token := "tkn_retry_evt"
	tokenHash := hashForTest(handler, token)
	pol := &policy.Policy{
		BaseKeyEnv: "RETRY_EVT_KEY",
		Retry: &policy.RetryConfig{
			MaxRetries: 1,
			RetryOn:    []int{500},
		},
	}
	if err := pol.Validate(); err != nil {
		t.Fatalf("validate policy: %v", err)
	}
	ts.Save(tokenHash, pol)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(
		`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close ledger: %v", err)
	}

	sessions, err := ledger.ReadSessionFiles(ledgerDir)
	if err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	var startedCount, bodyCount, doneCount int
	var lastDone ledger.CommunicationEvent
	for _, ev := range sessions[0].CommunicationEvents {
		if ev.Type == ledger.CommunicationEventResponseStarted {
			startedCount++
		}
		if ev.Type == ledger.CommunicationEventResponseBody {
			bodyCount++
		}
		if ev.Type == ledger.CommunicationEventResponseDone {
			doneCount++
			lastDone = ev
		}
	}
	if startedCount != 1 {
		t.Fatalf("expected exactly 1 response.started event for final attempt, got %d", startedCount)
	}
	if bodyCount != 1 {
		t.Fatalf("expected exactly 1 response.body event for final attempt, got %d", bodyCount)
	}
	if doneCount == 0 {
		t.Fatal("expected a response.completed event")
	}
	if lastDone.RetryCount != 1 {
		t.Fatalf("expected completed event retry_count=1, got %d", lastDone.RetryCount)
	}
}
