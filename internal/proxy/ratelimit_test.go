package proxy

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickcrawford/tokenomics/internal/policy"
)

func TestRateLimiter_NilConfig(t *testing.T) {
	rl := NewRateLimiter()
	if err := rl.Allow("token1", nil); err != nil {
		t.Fatalf("expected nil config to allow: %v", err)
	}
}

func TestRateLimiter_EmptyConfig(t *testing.T) {
	rl := NewRateLimiter()
	cfg := &policy.RateLimitConfig{}
	if err := rl.Allow("token1", cfg); err != nil {
		t.Fatalf("expected empty config to allow: %v", err)
	}
}

func TestRateLimiter_RequestLimit_Sliding(t *testing.T) {
	rl := NewRateLimiter()
	cfg := &policy.RateLimitConfig{
		Rules: []policy.RateLimitRule{
			{Requests: 3, Window: "1m", Strategy: "sliding"},
		},
	}

	for i := 0; i < 3; i++ {
		if err := rl.Allow("token1", cfg); err != nil {
			t.Fatalf("request %d should be allowed: %v", i, err)
		}
	}

	// 4th request should be blocked
	if err := rl.Allow("token1", cfg); err == nil {
		t.Fatal("expected 4th request to be rate limited")
	} else if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Fatalf("expected rate limit error, got: %v", err)
	}
}

func TestRateLimiter_RequestLimit_Fixed(t *testing.T) {
	rl := NewRateLimiter()
	cfg := &policy.RateLimitConfig{
		Rules: []policy.RateLimitRule{
			{Requests: 2, Window: "1m", Strategy: "fixed"},
		},
	}

	for i := 0; i < 2; i++ {
		if err := rl.Allow("token1", cfg); err != nil {
			t.Fatalf("request %d should be allowed: %v", i, err)
		}
	}

	if err := rl.Allow("token1", cfg); err == nil {
		t.Fatal("expected 3rd request to be rate limited")
	}
}

func TestRateLimiter_TokenLimit(t *testing.T) {
	rl := NewRateLimiter()
	cfg := &policy.RateLimitConfig{
		Rules: []policy.RateLimitRule{
			{Tokens: 100, Window: "1m"},
		},
	}

	// Allow request (no token limit on requests themselves)
	if err := rl.Allow("token1", cfg); err != nil {
		t.Fatalf("should be allowed: %v", err)
	}

	// Record tokens near the limit
	rl.RecordTokens("token1", cfg, 90)

	// Still under
	if err := rl.Allow("token1", cfg); err != nil {
		t.Fatalf("should be allowed: %v", err)
	}

	// Push over
	rl.RecordTokens("token1", cfg, 20)

	// Should be blocked now
	if err := rl.Allow("token1", cfg); err == nil {
		t.Fatal("expected token limit to block request")
	}
}

func TestRateLimiter_MaxParallel(t *testing.T) {
	rl := NewRateLimiter()
	cfg := &policy.RateLimitConfig{
		MaxParallel: 2,
	}

	rl.Acquire("token1", cfg)
	rl.Acquire("token1", cfg)

	// Third should be blocked
	if err := rl.Allow("token1", cfg); err == nil {
		t.Fatal("expected parallel limit to block")
	} else if !strings.Contains(err.Error(), "concurrent requests") {
		t.Fatalf("expected concurrent error, got: %v", err)
	}

	// Release one
	rl.Release("token1", cfg)

	// Should now be allowed
	if err := rl.Allow("token1", cfg); err != nil {
		t.Fatalf("should be allowed after release: %v", err)
	}
}

func TestRateLimiter_MultipleRules(t *testing.T) {
	rl := NewRateLimiter()
	cfg := &policy.RateLimitConfig{
		Rules: []policy.RateLimitRule{
			{Requests: 10, Window: "1m"}, // lenient per-minute
			{Requests: 2, Window: "1s"},  // strict per-second
		},
	}

	// First 2 requests in 1s window should be fine
	for i := 0; i < 2; i++ {
		if err := rl.Allow("token1", cfg); err != nil {
			t.Fatalf("request %d should be allowed: %v", i, err)
		}
	}

	// Third should be blocked by the 1s window even though 1m is fine
	if err := rl.Allow("token1", cfg); err == nil {
		t.Fatal("expected 1s rate limit to block")
	}
}

func TestRateLimiter_DifferentTokens(t *testing.T) {
	rl := NewRateLimiter()
	cfg := &policy.RateLimitConfig{
		Rules: []policy.RateLimitRule{
			{Requests: 1, Window: "1m"},
		},
	}

	if err := rl.Allow("token1", cfg); err != nil {
		t.Fatalf("token1 should be allowed: %v", err)
	}

	// token2 has its own bucket
	if err := rl.Allow("token2", cfg); err != nil {
		t.Fatalf("token2 should be allowed: %v", err)
	}

	// token1 is exhausted
	if err := rl.Allow("token1", cfg); err == nil {
		t.Fatal("token1 should be rate limited")
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter()
	cfg := &policy.RateLimitConfig{
		Rules: []policy.RateLimitRule{
			{Requests: 100, Window: "1m"},
		},
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := rl.Allow("token1", cfg); err != nil {
				t.Errorf("allow failed: %v", err)
			}
			rl.RecordTokens("token1", cfg, 10)
		}()
	}
	wg.Wait()
}

func TestParseWindow(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"1s", time.Second},
		{"1m", time.Minute},
		{"", time.Minute},
		{"1h", time.Hour},
		{"24h", 24 * time.Hour},
		{"5m", 5 * time.Minute},
		{"invalid", time.Minute},
	}

	for _, tt := range tests {
		got := parseWindow(tt.input)
		if got != tt.want {
			t.Errorf("parseWindow(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestRateLimiter_GCStaleBuckets(t *testing.T) {
	rl := NewRateLimiter()
	cfg := &policy.RateLimitConfig{
		Rules: []policy.RateLimitRule{
			{Requests: 1, Window: "1m"},
		},
	}

	// Seed one stale bucket and force cleanup to run.
	rl.buckets["stale-token"] = &tokenBucket{
		lastSeen: time.Now().Add(-25 * time.Hour),
	}
	rl.lastGC = time.Now().Add(-2 * time.Minute)

	_ = rl.getBucket("fresh-token", cfg)

	if _, ok := rl.buckets["stale-token"]; ok {
		t.Fatal("expected stale bucket to be garbage-collected")
	}
	if _, ok := rl.buckets["fresh-token"]; !ok {
		t.Fatal("expected fresh bucket to exist")
	}
}

func TestRateLimiter_DoesNotGCActiveParallelBucket(t *testing.T) {
	rl := NewRateLimiter()
	cfg := &policy.RateLimitConfig{
		MaxParallel: 1,
	}

	active := &tokenBucket{
		lastSeen: time.Now().Add(-25 * time.Hour),
	}
	active.parallel.Add(1)
	rl.buckets["active-token"] = active
	rl.lastGC = time.Now().Add(-2 * time.Minute)

	_ = rl.getBucket("fresh-token", cfg)

	if _, ok := rl.buckets["active-token"]; !ok {
		t.Fatal("expected active bucket to be retained during GC")
	}
}
