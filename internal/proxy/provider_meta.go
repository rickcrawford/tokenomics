package proxy

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/rickcrawford/tokenomics/internal/ledger"
)

// extractProviderMeta extracts normalized metadata from the provider's
// response headers and body. Supports OpenAI, Anthropic, Azure, Gemini,
// and Mistral response formats.
func extractProviderMeta(headers http.Header, body []byte) *ledger.ProviderMeta {
	meta := &ledger.ProviderMeta{}

	// Rate limit headers (normalized across providers)
	meta.RateLimitRemainingRequests = parseIntHeader(headers,
		"X-Ratelimit-Remaining-Requests",            // OpenAI, Azure, Mistral
		"Anthropic-Ratelimit-Requests-Remaining",     // Anthropic
	)
	meta.RateLimitRemainingTokens = parseIntHeader(headers,
		"X-Ratelimit-Remaining-Tokens",              // OpenAI, Azure, Mistral
		"Anthropic-Ratelimit-Tokens-Remaining",       // Anthropic
	)
	meta.RateLimitReset = firstHeader(headers,
		"X-Ratelimit-Reset-Requests",                // OpenAI, Mistral
		"Anthropic-Ratelimit-Tokens-Reset",           // Anthropic
		"X-Ratelimit-Reset-Tokens",                  // Azure
	)

	// Parse body fields
	if len(body) > 0 {
		extractProviderMetaFromBody(body, meta)
	}

	// Return nil if nothing was populated
	if isEmptyProviderMeta(meta) {
		return nil
	}
	return meta
}

// extractProviderMetaFromBody parses model, finish_reason, and token detail
// from the response body JSON.
func extractProviderMetaFromBody(body []byte, meta *ledger.ProviderMeta) {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return
	}

	// Actual model served
	if model, ok := resp["model"].(string); ok {
		meta.ActualModel = model
	}

	// Finish reason (OpenAI/Mistral: choices[0].finish_reason)
	if choices, ok := resp["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if fr, ok := choice["finish_reason"].(string); ok {
				meta.FinishReason = fr
			}
		}
	}

	// Finish reason (Anthropic: stop_reason)
	if sr, ok := resp["stop_reason"].(string); ok && meta.FinishReason == "" {
		meta.FinishReason = sr
	}

	// Finish reason (Gemini: candidates[0].finishReason)
	if candidates, ok := resp["candidates"].([]interface{}); ok && len(candidates) > 0 && meta.FinishReason == "" {
		if candidate, ok := candidates[0].(map[string]interface{}); ok {
			if fr, ok := candidate["finishReason"].(string); ok {
				meta.FinishReason = fr
			}
		}
	}

	// Token details from usage object
	extractUsageDetails(resp, meta)

	// Gemini uses usageMetadata instead of usage
	extractGeminiUsageDetails(resp, meta)
}

// extractUsageDetails pulls detailed token breakdown from OpenAI/Anthropic usage objects.
func extractUsageDetails(resp map[string]interface{}, meta *ledger.ProviderMeta) {
	usage, ok := resp["usage"].(map[string]interface{})
	if !ok {
		return
	}

	// OpenAI: prompt_tokens_details.cached_tokens
	if ptd, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok {
		if ct, ok := ptd["cached_tokens"].(float64); ok {
			meta.CachedInputTokens = int(ct)
		}
	}

	// OpenAI: completion_tokens_details.reasoning_tokens
	if ctd, ok := usage["completion_tokens_details"].(map[string]interface{}); ok {
		if rt, ok := ctd["reasoning_tokens"].(float64); ok {
			meta.ReasoningTokens = int(rt)
		}
	}

	// Anthropic: cache_read_input_tokens
	if crit, ok := usage["cache_read_input_tokens"].(float64); ok {
		meta.CachedInputTokens = int(crit)
	}

	// Anthropic: cache_creation_input_tokens
	if ccit, ok := usage["cache_creation_input_tokens"].(float64); ok {
		meta.CacheCreationTokens = int(ccit)
	}
}

// extractGeminiUsageDetails pulls token details from Gemini's usageMetadata.
func extractGeminiUsageDetails(resp map[string]interface{}, meta *ledger.ProviderMeta) {
	um, ok := resp["usageMetadata"].(map[string]interface{})
	if !ok {
		return
	}
	if cctc, ok := um["cachedContentTokenCount"].(float64); ok {
		meta.CachedInputTokens = int(cctc)
	}
}

// parseIntHeader returns the integer value of the first non-empty header
// matching any of the given keys.
func parseIntHeader(h http.Header, keys ...string) int {
	for _, key := range keys {
		if v := h.Get(key); v != "" {
			n, err := strconv.Atoi(v)
			if err == nil {
				return n
			}
		}
	}
	return 0
}

// firstHeader returns the value of the first non-empty header matching
// any of the given keys.
func firstHeader(h http.Header, keys ...string) string {
	for _, key := range keys {
		if v := h.Get(key); v != "" {
			return v
		}
	}
	return ""
}

// isEmptyProviderMeta returns true if no fields are populated.
func isEmptyProviderMeta(meta *ledger.ProviderMeta) bool {
	return meta.CachedInputTokens == 0 &&
		meta.CacheCreationTokens == 0 &&
		meta.ReasoningTokens == 0 &&
		meta.ActualModel == "" &&
		meta.FinishReason == "" &&
		meta.RateLimitRemainingRequests == 0 &&
		meta.RateLimitRemainingTokens == 0 &&
		meta.RateLimitReset == ""
}

// extractProviderMetaFromStream extracts provider metadata from streaming
// response state. Called after all SSE chunks have been processed.
func extractProviderMetaFromStream(headers http.Header, lastChunk map[string]interface{}) *ledger.ProviderMeta {
	meta := &ledger.ProviderMeta{}

	// Rate limits from headers (same as buffered)
	meta.RateLimitRemainingRequests = parseIntHeader(headers,
		"X-Ratelimit-Remaining-Requests",
		"Anthropic-Ratelimit-Requests-Remaining",
	)
	meta.RateLimitRemainingTokens = parseIntHeader(headers,
		"X-Ratelimit-Remaining-Tokens",
		"Anthropic-Ratelimit-Tokens-Remaining",
	)
	meta.RateLimitReset = firstHeader(headers,
		"X-Ratelimit-Reset-Requests",
		"Anthropic-Ratelimit-Tokens-Reset",
		"X-Ratelimit-Reset-Tokens",
	)

	// Model and usage from the last/final chunk
	if lastChunk != nil {
		if model, ok := lastChunk["model"].(string); ok {
			meta.ActualModel = model
		}

		// Finish reason from last choice
		if choices, ok := lastChunk["choices"].([]interface{}); ok && len(choices) > 0 {
			if choice, ok := choices[0].(map[string]interface{}); ok {
				if fr, ok := choice["finish_reason"].(string); ok {
					meta.FinishReason = fr
				}
			}
		}

		// Usage details from final chunk (some providers include full usage)
		extractUsageDetails(lastChunk, meta)
	}

	if isEmptyProviderMeta(meta) {
		return nil
	}
	return meta
}
