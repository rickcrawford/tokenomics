package proxy

import (
	"encoding/json"
)

// extractTokenCountsFromResponse extracts provider token usage from known response shapes.
// Supports OpenAI, Anthropic, and Gemini usage fields.
func extractTokenCountsFromResponse(resp map[string]interface{}) (int, int) {
	// OpenAI / Anthropic style: usage object
	if usage, ok := resp["usage"].(map[string]interface{}); ok {
		in, _ := readTokenCount(usage, "input_tokens", "prompt_tokens")
		out, _ := readTokenCount(usage, "output_tokens", "completion_tokens", "candidates_token_count")
		return in, out
	}

	// Anthropic message_start streams include usage under message.usage.
	if msg, ok := resp["message"].(map[string]interface{}); ok {
		if usage, ok := msg["usage"].(map[string]interface{}); ok {
			in, _ := readTokenCount(usage, "input_tokens", "prompt_tokens")
			out, _ := readTokenCount(usage, "output_tokens", "completion_tokens")
			return in, out
		}
	}

	// Gemini style: usageMetadata object
	if usageMeta, ok := resp["usageMetadata"].(map[string]interface{}); ok {
		in, _ := readTokenCount(usageMeta, "promptTokenCount")
		out, _ := readTokenCount(usageMeta, "candidatesTokenCount")
		return in, out
	}

	return 0, 0
}

// readTokenCount reads the first matching token key from a usage map.
func readTokenCount(usage map[string]interface{}, keys ...string) (int, bool) {
	for _, key := range keys {
		v, ok := usage[key]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case float64:
			return int(n), true
		case int:
			return n, true
		case int32:
			return int(n), true
		case int64:
			return int(n), true
		case json.Number:
			if iv, err := n.Int64(); err == nil {
				return int(iv), true
			}
		}
	}
	return 0, false
}

