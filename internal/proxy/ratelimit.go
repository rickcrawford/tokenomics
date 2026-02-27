package proxy

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rickcrawford/tokenomics/internal/policy"
)

// RateLimiter enforces per-token rate limits using sliding or fixed windows.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	lastGC  time.Time
}

type tokenBucket struct {
	windows     []*windowCounter
	parallel    atomic.Int64
	maxParallel int
	lastSeen    time.Time
}

type windowCounter struct {
	requests []time.Time
	tokens   []tokenEntry
	rule     policy.RateLimitRule
	duration time.Duration

	// Fixed window fields
	fixedStart    time.Time
	fixedRequests int
	fixedTokens   int
}

type tokenEntry struct {
	t     time.Time
	count int
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*tokenBucket),
	}
}

func parseWindow(w string) time.Duration {
	switch w {
	case "1s":
		return time.Second
	case "1m", "":
		return time.Minute
	case "1h":
		return time.Hour
	case "24h":
		return 24 * time.Hour
	default:
		d, err := time.ParseDuration(w)
		if err != nil {
			return time.Minute
		}
		return d
	}
}

func (rl *RateLimiter) getBucket(tokenHash string, cfg *policy.RateLimitConfig) *tokenBucket {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	rl.gcStaleBucketsLocked(now)

	b, ok := rl.buckets[tokenHash]
	if ok {
		b.lastSeen = now
		return b
	}

	b = &tokenBucket{
		maxParallel: cfg.MaxParallel,
		lastSeen:    now,
	}

	for _, rule := range cfg.Rules {
		b.windows = append(b.windows, &windowCounter{
			rule:     rule,
			duration: parseWindow(rule.Window),
		})
	}

	rl.buckets[tokenHash] = b
	return b
}

func (rl *RateLimiter) gcStaleBucketsLocked(now time.Time) {
	// Keep cleanup cheap by running at most once per minute.
	if !rl.lastGC.IsZero() && now.Sub(rl.lastGC) < time.Minute {
		return
	}
	rl.lastGC = now

	const staleAfter = 24 * time.Hour
	for tokenHash, bucket := range rl.buckets {
		if bucket.parallel.Load() > 0 {
			continue
		}
		if !bucket.lastSeen.IsZero() && now.Sub(bucket.lastSeen) > staleAfter {
			delete(rl.buckets, tokenHash)
		}
	}
}

// Allow checks if a request is allowed under the rate limit.
// Returns nil if allowed, or an error describing which limit was hit.
func (rl *RateLimiter) Allow(tokenHash string, cfg *policy.RateLimitConfig) error {
	if cfg == nil || (len(cfg.Rules) == 0 && cfg.MaxParallel == 0) {
		return nil
	}

	b := rl.getBucket(tokenHash, cfg)

	// Check max parallel
	if b.maxParallel > 0 {
		current := b.parallel.Load()
		if current >= int64(b.maxParallel) {
			return fmt.Errorf("rate limit exceeded: %d concurrent requests (max %d)", current, b.maxParallel)
		}
	}

	now := time.Now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	for _, wc := range b.windows {
		if wc.rule.Requests > 0 {
			count := wc.countRequests(now)
			if count >= wc.rule.Requests {
				return fmt.Errorf("rate limit exceeded: %d requests in %s window (max %d)", count, wc.rule.Window, wc.rule.Requests)
			}
		}
		if wc.rule.Tokens > 0 {
			count := wc.countTokens(now)
			if count >= wc.rule.Tokens {
				return fmt.Errorf("rate limit exceeded: %d tokens in %s window (max %d)", count, wc.rule.Window, wc.rule.Tokens)
			}
		}
	}

	// Record the request
	for _, wc := range b.windows {
		if wc.rule.Requests > 0 {
			wc.addRequest(now)
		}
	}

	return nil
}

// RecordTokens records token usage for token-based rate limiting.
func (rl *RateLimiter) RecordTokens(tokenHash string, cfg *policy.RateLimitConfig, tokens int) {
	if cfg == nil || len(cfg.Rules) == 0 || tokens == 0 {
		return
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[tokenHash]
	if !ok {
		return
	}

	now := time.Now()
	for _, wc := range b.windows {
		if wc.rule.Tokens > 0 {
			wc.addTokens(now, tokens)
		}
	}
}

// Acquire increments the parallel request counter. Call Release when done.
func (rl *RateLimiter) Acquire(tokenHash string, cfg *policy.RateLimitConfig) {
	if cfg == nil || cfg.MaxParallel == 0 {
		return
	}
	b := rl.getBucket(tokenHash, cfg)
	b.parallel.Add(1)
}

// Release decrements the parallel request counter.
func (rl *RateLimiter) Release(tokenHash string, cfg *policy.RateLimitConfig) {
	if cfg == nil || cfg.MaxParallel == 0 {
		return
	}

	rl.mu.Lock()
	b, ok := rl.buckets[tokenHash]
	rl.mu.Unlock()

	if ok {
		b.parallel.Add(-1)
	}
}

func (wc *windowCounter) countRequests(now time.Time) int {
	if wc.rule.Strategy == "fixed" {
		return wc.countFixedRequests(now)
	}
	return wc.countSlidingRequests(now)
}

func (wc *windowCounter) countTokens(now time.Time) int {
	if wc.rule.Strategy == "fixed" {
		return wc.countFixedTokens(now)
	}
	return wc.countSlidingTokens(now)
}

func (wc *windowCounter) countSlidingRequests(now time.Time) int {
	cutoff := now.Add(-wc.duration)
	count := 0
	for _, t := range wc.requests {
		if t.After(cutoff) {
			count++
		}
	}
	return count
}

func (wc *windowCounter) countSlidingTokens(now time.Time) int {
	cutoff := now.Add(-wc.duration)
	count := 0
	for _, te := range wc.tokens {
		if te.t.After(cutoff) {
			count += te.count
		}
	}
	return count
}

func (wc *windowCounter) countFixedRequests(now time.Time) int {
	if now.Sub(wc.fixedStart) >= wc.duration {
		wc.fixedStart = now
		wc.fixedRequests = 0
		wc.fixedTokens = 0
	}
	return wc.fixedRequests
}

func (wc *windowCounter) countFixedTokens(now time.Time) int {
	if now.Sub(wc.fixedStart) >= wc.duration {
		wc.fixedStart = now
		wc.fixedRequests = 0
		wc.fixedTokens = 0
	}
	return wc.fixedTokens
}

func (wc *windowCounter) addRequest(now time.Time) {
	if wc.rule.Strategy == "fixed" {
		wc.fixedRequests++
		return
	}
	wc.requests = append(wc.requests, now)
	// Prune old entries
	cutoff := now.Add(-wc.duration)
	pruned := wc.requests[:0]
	for _, t := range wc.requests {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	wc.requests = pruned
}

func (wc *windowCounter) addTokens(now time.Time, count int) {
	if wc.rule.Strategy == "fixed" {
		if now.Sub(wc.fixedStart) >= wc.duration {
			wc.fixedStart = now
			wc.fixedRequests = 0
			wc.fixedTokens = 0
		}
		wc.fixedTokens += count
		return
	}
	wc.tokens = append(wc.tokens, tokenEntry{t: now, count: count})
	// Prune old entries
	cutoff := now.Add(-wc.duration)
	pruned := wc.tokens[:0]
	for _, te := range wc.tokens {
		if te.t.After(cutoff) {
			pruned = append(pruned, te)
		}
	}
	wc.tokens = pruned
}
