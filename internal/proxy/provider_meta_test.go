package proxy

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestExtractProviderMetaOpenAI(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Ratelimit-Remaining-Requests", "100")
	headers.Set("X-Ratelimit-Remaining-Tokens", "50000")
	headers.Set("X-Ratelimit-Reset-Requests", "2024-01-01T00:00:00Z")

	body := mustJSON(map[string]interface{}{
		"model": "gpt-4-0613",
		"choices": []interface{}{
			map[string]interface{}{
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     100,
			"completion_tokens": 50,
			"prompt_tokens_details": map[string]interface{}{
				"cached_tokens": float64(30),
			},
			"completion_tokens_details": map[string]interface{}{
				"reasoning_tokens": float64(10),
			},
		},
	})

	meta := extractProviderMeta(headers, body)
	if meta == nil {
		t.Fatal("expected non-nil provider meta")
	}

	if meta.ActualModel != "gpt-4-0613" {
		t.Errorf("actual_model: got %q, want %q", meta.ActualModel, "gpt-4-0613")
	}
	if meta.FinishReason != "stop" {
		t.Errorf("finish_reason: got %q, want %q", meta.FinishReason, "stop")
	}
	if meta.CachedInputTokens != 30 {
		t.Errorf("cached_input_tokens: got %d, want 30", meta.CachedInputTokens)
	}
	if meta.ReasoningTokens != 10 {
		t.Errorf("reasoning_tokens: got %d, want 10", meta.ReasoningTokens)
	}
	if meta.RateLimitRemainingRequests != 100 {
		t.Errorf("rate_limit_remaining_requests: got %d, want 100", meta.RateLimitRemainingRequests)
	}
	if meta.RateLimitRemainingTokens != 50000 {
		t.Errorf("rate_limit_remaining_tokens: got %d, want 50000", meta.RateLimitRemainingTokens)
	}
	if meta.RateLimitReset != "2024-01-01T00:00:00Z" {
		t.Errorf("rate_limit_reset: got %q, want %q", meta.RateLimitReset, "2024-01-01T00:00:00Z")
	}
}

func TestExtractProviderMetaAnthropic(t *testing.T) {
	headers := http.Header{}
	headers.Set("Anthropic-Ratelimit-Requests-Remaining", "200")
	headers.Set("Anthropic-Ratelimit-Tokens-Remaining", "80000")
	headers.Set("Anthropic-Ratelimit-Tokens-Reset", "2024-01-01T00:01:00Z")

	body := mustJSON(map[string]interface{}{
		"model":       "claude-3-opus-20240229",
		"stop_reason": "end_turn",
		"usage": map[string]interface{}{
			"input_tokens":              100,
			"output_tokens":             50,
			"cache_read_input_tokens":   float64(40),
			"cache_creation_input_tokens": float64(60),
		},
	})

	meta := extractProviderMeta(headers, body)
	if meta == nil {
		t.Fatal("expected non-nil provider meta")
	}

	if meta.ActualModel != "claude-3-opus-20240229" {
		t.Errorf("actual_model: got %q", meta.ActualModel)
	}
	if meta.FinishReason != "end_turn" {
		t.Errorf("finish_reason: got %q, want %q", meta.FinishReason, "end_turn")
	}
	if meta.CachedInputTokens != 40 {
		t.Errorf("cached_input_tokens: got %d, want 40", meta.CachedInputTokens)
	}
	if meta.CacheCreationTokens != 60 {
		t.Errorf("cache_creation_tokens: got %d, want 60", meta.CacheCreationTokens)
	}
	if meta.RateLimitRemainingRequests != 200 {
		t.Errorf("rate_limit_remaining_requests: got %d, want 200", meta.RateLimitRemainingRequests)
	}
}

func TestExtractProviderMetaGemini(t *testing.T) {
	headers := http.Header{}

	body := mustJSON(map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{
				"finishReason": "STOP",
			},
		},
		"usageMetadata": map[string]interface{}{
			"promptTokenCount":          float64(100),
			"candidatesTokenCount":      float64(50),
			"cachedContentTokenCount":   float64(25),
		},
	})

	meta := extractProviderMeta(headers, body)
	if meta == nil {
		t.Fatal("expected non-nil provider meta")
	}

	if meta.FinishReason != "STOP" {
		t.Errorf("finish_reason: got %q, want %q", meta.FinishReason, "STOP")
	}
	if meta.CachedInputTokens != 25 {
		t.Errorf("cached_input_tokens: got %d, want 25", meta.CachedInputTokens)
	}
}

func TestExtractProviderMetaEmpty(t *testing.T) {
	headers := http.Header{}
	body := []byte(`{}`)

	meta := extractProviderMeta(headers, body)
	if meta != nil {
		t.Errorf("expected nil for empty response, got %+v", meta)
	}
}

func TestExtractProviderMetaFromStream(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Ratelimit-Remaining-Requests", "99")

	lastChunk := map[string]interface{}{
		"model": "gpt-4-turbo",
		"choices": []interface{}{
			map[string]interface{}{
				"finish_reason": "stop",
				"delta":         map[string]interface{}{},
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     float64(200),
			"completion_tokens": float64(100),
			"prompt_tokens_details": map[string]interface{}{
				"cached_tokens": float64(50),
			},
		},
	}

	meta := extractProviderMetaFromStream(headers, lastChunk)
	if meta == nil {
		t.Fatal("expected non-nil provider meta from stream")
	}

	if meta.ActualModel != "gpt-4-turbo" {
		t.Errorf("actual_model: got %q, want %q", meta.ActualModel, "gpt-4-turbo")
	}
	if meta.FinishReason != "stop" {
		t.Errorf("finish_reason: got %q, want %q", meta.FinishReason, "stop")
	}
	if meta.CachedInputTokens != 50 {
		t.Errorf("cached_input_tokens: got %d, want 50", meta.CachedInputTokens)
	}
	if meta.RateLimitRemainingRequests != 99 {
		t.Errorf("rate_limit_remaining_requests: got %d, want 99", meta.RateLimitRemainingRequests)
	}
}

func TestExtractProviderMetaFromStreamNilChunk(t *testing.T) {
	headers := http.Header{}
	meta := extractProviderMetaFromStream(headers, nil)
	if meta != nil {
		t.Errorf("expected nil for empty stream, got %+v", meta)
	}
}

func TestParseIntHeader(t *testing.T) {
	h := http.Header{}
	h.Set("X-Rate-Limit", "42")
	h.Set("Bad-Value", "not-a-number")

	if got := parseIntHeader(h, "Missing", "X-Rate-Limit"); got != 42 {
		t.Errorf("expected 42, got %d", got)
	}
	if got := parseIntHeader(h, "Bad-Value"); got != 0 {
		t.Errorf("expected 0 for bad value, got %d", got)
	}
	if got := parseIntHeader(h, "Missing"); got != 0 {
		t.Errorf("expected 0 for missing, got %d", got)
	}
}

func TestFirstHeader(t *testing.T) {
	h := http.Header{}
	h.Set("X-Reset", "2024-01-01T00:00:00Z")

	if got := firstHeader(h, "Missing", "X-Reset"); got != "2024-01-01T00:00:00Z" {
		t.Errorf("expected reset time, got %q", got)
	}
	if got := firstHeader(h, "Missing"); got != "" {
		t.Errorf("expected empty for missing, got %q", got)
	}
}

func mustJSON(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
