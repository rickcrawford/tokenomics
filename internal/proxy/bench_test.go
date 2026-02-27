package proxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rickcrawford/tokenomics/internal/policy"
	"github.com/rickcrawford/tokenomics/internal/session"
)

func BenchmarkHashToken(b *testing.B) {
	handler := NewHandler(newMockTokenStore(), session.NewMemoryStore(), []byte("benchmark-key"), "http://localhost", nil, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.hashToken("tkn_abc123-test-token-value")
	}
}

func BenchmarkHashTokenDirect(b *testing.B) {
	key := []byte("benchmark-key")
	token := []byte("tkn_abc123-test-token-value")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mac := hmac.New(sha256.New, key)
		mac.Write(token)
		hex.EncodeToString(mac.Sum(nil))
	}
}

func BenchmarkGenerateRequestID(b *testing.B) {
	for i := 0; i < b.N; i++ {
		generateRequestID()
	}
}

func BenchmarkSafePrefix(b *testing.B) {
	s := "abcdef1234567890abcdef1234567890abcdef1234567890"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		safePrefix(s, 16)
	}
}

func BenchmarkRateLimiter_Allow(b *testing.B) {
	rl := NewRateLimiter()
	cfg := &policy.RateLimitConfig{
		Rules: []policy.RateLimitRule{
			{Requests: 1000000, Window: "1m"},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := rl.Allow("token1", cfg); err != nil {
			b.Fatalf("allow failed: %v", err)
		}
	}
}

func BenchmarkRateLimiter_Allow_MultiRule(b *testing.B) {
	rl := NewRateLimiter()
	cfg := &policy.RateLimitConfig{
		Rules: []policy.RateLimitRule{
			{Requests: 1000000, Window: "1s"},
			{Requests: 10000000, Window: "1m"},
			{Tokens: 100000000, Window: "1m"},
		},
		MaxParallel: 100,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.Acquire("token1", cfg)
		if err := rl.Allow("token1", cfg); err != nil {
			b.Fatalf("allow failed: %v", err)
		}
		rl.Release("token1", cfg)
	}
}

func BenchmarkRateLimiter_RecordTokens(b *testing.B) {
	rl := NewRateLimiter()
	cfg := &policy.RateLimitConfig{
		Rules: []policy.RateLimitRule{
			{Tokens: 100000000, Window: "1m"},
		},
	}
	// Initialize bucket
	if err := rl.Allow("token1", cfg); err != nil {
		b.Fatalf("allow failed: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rl.RecordTokens("token1", cfg, 100)
	}
}

func BenchmarkUsageStats_Record(b *testing.B) {
	stats := NewUsageStats()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stats.Record("abcdef1234567890", "gpt-4o", "OPENAI_API_KEY", 100, 50, false)
	}
}

func BenchmarkUsageStats_Snapshot(b *testing.B) {
	stats := NewUsageStats()
	// Seed with data
	for i := 0; i < 20; i++ {
		stats.Record("token1", "gpt-4o", "KEY1", 100, 50, false)
		stats.Record("token2", "claude-3", "KEY2", 200, 100, false)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stats.Snapshot()
	}
}

func BenchmarkUsageStats_SessionSnapshot(b *testing.B) {
	stats := NewUsageStats()
	for i := 0; i < 50; i++ {
		stats.Record(strings.Repeat("a", 16)+string(rune('a'+i%26)), "gpt-4o", "KEY", 100, 50, false)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stats.SessionSnapshot()
	}
}

func BenchmarkHandler_ServeHTTP_ChatCompletions(b *testing.B) {
	b.Setenv("BENCH_KEY", "sk-bench-test")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"id":"chatcmpl-bench","choices":[{"message":{"content":"hi"}}],"usage":{"completion_tokens":1}}`)); err != nil {
			b.Fatalf("write response failed: %v", err)
		}
	}))
	defer upstream.Close()

	ts := newMockTokenStore()
	handler := NewHandler(ts, session.NewMemoryStore(), []byte("benchkey"), upstream.URL, nil, nil)

	pol := &policy.Policy{BaseKeyEnv: "BENCH_KEY"}
	if err := pol.Validate(); err != nil {
		b.Fatalf("policy validate failed: %v", err)
	}
	ts.Save(handler.hashToken("tkn_bench"), pol)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer tkn_bench")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkHandler_ServeHTTP_Passthrough(b *testing.B) {
	b.Setenv("BENCH_PT_KEY", "sk-bench-pt")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"data":[]}`)); err != nil {
			b.Fatalf("write response failed: %v", err)
		}
	}))
	defer upstream.Close()

	ts := newMockTokenStore()
	handler := NewHandler(ts, session.NewMemoryStore(), []byte("benchkey"), upstream.URL, nil, nil)

	pol := &policy.Policy{BaseKeyEnv: "BENCH_PT_KEY"}
	if err := pol.Validate(); err != nil {
		b.Fatalf("policy validate failed: %v", err)
	}
	ts.Save(handler.hashToken("tkn_bench_pt"), pol)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/v1/models", nil)
		req.Header.Set("Authorization", "Bearer tkn_bench_pt")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

func BenchmarkExtractUpstreamID(b *testing.B) {
	body := []byte(`{"id":"chatcmpl-B9MHDbslfkBeAs8l4bebGdFOJ6PeG","object":"chat.completion","choices":[{"message":{"content":"hello"}}],"usage":{"completion_tokens":5}}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractUpstreamID(body)
	}
}

func BenchmarkExtractAssistantContent(b *testing.B) {
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"Hello! How can I help you today?"}}],"usage":{"completion_tokens":10}}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		extractAssistantContent(body)
	}
}

func BenchmarkShouldRetry(b *testing.B) {
	cfg := &policy.RetryConfig{MaxRetries: 3, RetryOn: []int{429, 500, 502, 503}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		shouldRetry(cfg, 500)
	}
}
