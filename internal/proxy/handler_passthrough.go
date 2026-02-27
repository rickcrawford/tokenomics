package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/policy"
)

func (h *Handler) passthrough(w http.ResponseWriter, r *http.Request, pol *policy.Policy, tokenHash, upstream string, start time.Time) {
	// Log policy lookup for passthrough
	debugLog("Passthrough - Policy loaded for token %s", safePrefix(tokenHash, 16))
	debugLog("Passthrough - BaseKeyEnv: %s", pol.BaseKeyEnv)

	resolved := pol.ResolveProvider("")

	// Look up provider config
	var providerCfg *config.ProviderConfig
	providerName := resolved.ProviderName

	// If no provider name set, try to use the policy's default provider
	if providerName == "" && pol.DefaultProvider != "" {
		providerName = pol.DefaultProvider
	}
	// If still no provider name, try handler's default provider
	if providerName == "" && h.defaultProvider != "" {
		providerName = h.defaultProvider
	}

	if providerName != "" {
		if pc, ok := h.providers[providerName]; ok {
			providerCfg = &pc
		}
	}

	// Resolve upstream: policy > provider config > global
	if resolved.UpstreamURL != "" {
		upstream = resolved.UpstreamURL
	} else if providerCfg != nil && providerCfg.UpstreamURL != "" {
		upstream = providerCfg.UpstreamURL
	}

	// Use provider's api_key_env if the resolved policy doesn't specify one
	if resolved.BaseKeyEnv == "" && providerCfg != nil && providerCfg.APIKeyEnv != "" {
		resolved.BaseKeyEnv = providerCfg.APIKeyEnv
	}

	logEntry := &RequestLog{
		Timestamp:   start.UTC().Format(time.RFC3339Nano),
		Method:      r.Method,
		Path:        r.URL.Path,
		TokenHash:   safePrefix(tokenHash, 16),
		BaseKeyEnv:  resolved.BaseKeyEnv,
		UpstreamURL: upstream,
		RemoteAddr:  r.RemoteAddr,
		UserAgent:   r.UserAgent(),
	}
	defer func() {
		logEntry.DurationMs = time.Since(start).Milliseconds()
		if !h.logging.DisableRequest {
			logRequest(logEntry)
		}
	}()

	debugLog("Passthrough - Loading env var: %s", resolved.BaseKeyEnv)
	realKey := os.Getenv(resolved.BaseKeyEnv)
	if realKey == "" {
		debugLog("Passthrough - ERROR: Env var %s not set", resolved.BaseKeyEnv)
		logEntry.StatusCode = http.StatusInternalServerError
		logEntry.Error = fmt.Sprintf("base key env %q is not set", resolved.BaseKeyEnv)
		httpError(w, http.StatusInternalServerError, logEntry.Error)
		return
	}

	upstreamURL, err := url.Parse(upstream)
	if err != nil {
		logEntry.StatusCode = http.StatusInternalServerError
		logEntry.Error = "invalid upstream URL"
		httpError(w, http.StatusInternalServerError, logEntry.Error)
		return
	}

	// Extract model and messages from request body for memory recording
	var requestBody []byte
	var reqBody map[string]interface{}
	var model string
	var userContent string
	if r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
		if err == nil {
			requestBody = body
			if err := json.Unmarshal(body, &reqBody); err == nil {
				if m, ok := reqBody["model"].(string); ok {
					model = m
				}
				userContent = extractUserContentFromRequest(reqBody)
			}
			// Restore body for the actual request
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
	}

	lw := newLoggingResponseWriter(w)

	// Streaming handler that extracts tokens without buffering large responses
	streamWriter := &streamingResponseWriter{
		ResponseWriter: lw,
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = upstreamURL.Scheme
			req.URL.Host = upstreamURL.Host
			req.Host = upstreamURL.Host

			// Remove client's auth headers before setting provider-specific auth
			req.Header.Del("Authorization")
			req.Header.Del("x-api-key")

			// Apply auth based on provider scheme
			authScheme := "bearer"
			if providerCfg != nil && providerCfg.AuthScheme != "" {
				authScheme = providerCfg.AuthScheme
			}

			switch authScheme {
			case "header":
				authHeader := "Authorization"
				if providerCfg != nil && providerCfg.AuthHeader != "" {
					authHeader = providerCfg.AuthHeader
				}
				if authHeader != "" {
					req.Header.Set(authHeader, realKey)
				}
			case "query":
				q := req.URL.Query()
				q.Set("key", realKey)
				req.URL.RawQuery = q.Encode()
				req.Header.Del("Authorization")
			default:
				req.Header.Set("Authorization", "Bearer "+realKey)
			}

			// Add provider-specific extra headers
			if providerCfg != nil {
				for k, v := range providerCfg.Headers {
					req.Header.Set(k, v)
				}
			}
		},
	}

	proxy.ServeHTTP(streamWriter, r)
	logEntry.StatusCode = lw.statusCode

	// Extract token counts and response content
	inputTokens, outputTokens := streamWriter.GetTokenCounts()
	assistantContent := streamWriter.GetAssistantContent()

	// Get raw response content (JSON or SSE, depending on endpoint).
	responseData := streamWriter.GetResponseContent()
	if streamWriter.IsTruncated() {
		debugLog("Passthrough response capture truncated at %d bytes", maxResponseBodySize)
	}

	if len(responseData) > 0 {
		// Try to parse as JSON (non-streaming response)
		var respBody map[string]interface{}
		if err := json.Unmarshal(responseData, &respBody); err == nil {
			in, out := extractTokenCountsFromResponse(respBody)
			if in > 0 {
				inputTokens = in
			}
			if out > 0 {
				outputTokens = out
			}
			if parsed := extractAssistantTextFromResponse(respBody); parsed != "" {
				assistantContent = parsed
			}
		}
	}

	// Record to ledger
	debugLog("Passthrough handler: ledger=%v, providerName=%s, statusCode=%d, model=%s, input=%d, output=%d, streaming=%v",
		h.ledger != nil, providerName, lw.statusCode, model, inputTokens, outputTokens, streamWriter.isStreaming)
	if h.ledger != nil {
		debugLog("Passthrough: calling recordLedgerEntry with model=%s", model)
		h.recordLedgerEntry(logEntry, tokenHash, model, providerName, inputTokens, outputTokens, lw.statusCode, streamWriter.isStreaming, 0, nil)
	}

	if model != "" {
		responseForMemory := formatResponseForMemory(responseData, "")
		if memWriter := h.getMemoryWriter(resolved.Memory); memWriter != nil {
			if len(requestBody) > 0 {
				if err := memWriter.Append(tokenHash, "request", model, string(requestBody)); err != nil {
					debugLog("Passthrough: failed to write request memory: %v", err)
				}
			}
			if responseForMemory != "" {
				if err := memWriter.Append(tokenHash, "response", model, responseForMemory); err != nil {
					debugLog("Passthrough: failed to write response memory: %v", err)
				}
			}
			if userContent != "" {
				if err := memWriter.Append(tokenHash, "user", model, userContent); err != nil {
					debugLog("Passthrough: failed to write user memory: %v", err)
				}
			}
			if assistantContent != "" {
				if err := memWriter.Append(tokenHash, "assistant", model, assistantContent); err != nil {
					debugLog("Passthrough: failed to write assistant memory: %v", err)
				}
			}
		}

		if h.ledger != nil {
			if len(requestBody) > 0 {
				h.ledger.RecordMemory(tokenHash, "request", model, string(requestBody))
			}
			if responseForMemory != "" {
				h.ledger.RecordMemory(tokenHash, "response", model, responseForMemory)
			}
			if userContent != "" {
				h.ledger.RecordMemory(tokenHash, "user", model, userContent)
			}
			if assistantContent != "" {
				h.ledger.RecordMemory(tokenHash, "assistant", model, assistantContent)
			}
		}
	}
}

func extractUserContentFromRequest(reqBody map[string]interface{}) string {
	var parts []string

	// OpenAI / Anthropic style message arrays.
	if msgs, ok := reqBody["messages"].([]interface{}); ok {
		for _, rawMsg := range msgs {
			msg, ok := rawMsg.(map[string]interface{})
			if !ok {
				continue
			}
			role, _ := msg["role"].(string)
			if role != "user" {
				continue
			}
			parts = append(parts, extractTextFromContent(msg["content"]))
		}
	}

	// Gemini style request body.
	if contents, ok := reqBody["contents"].([]interface{}); ok {
		for _, rawContent := range contents {
			content, ok := rawContent.(map[string]interface{})
			if !ok {
				continue
			}
			role, _ := content["role"].(string)
			if role != "" && role != "user" {
				continue
			}
			if cparts, ok := content["parts"].([]interface{}); ok {
				for _, rawPart := range cparts {
					part, ok := rawPart.(map[string]interface{})
					if !ok {
						continue
					}
					if text, _ := part["text"].(string); text != "" {
						parts = append(parts, text)
					}
				}
			}
		}
	}

	return strings.Join(parts, "\n\n")
}

func extractAssistantTextFromResponse(respBody map[string]interface{}) string {
	// OpenAI style response.
	if choices, ok := respBody["choices"].([]interface{}); ok && len(choices) > 0 {
		if first, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := first["message"].(map[string]interface{}); ok {
				if content := extractTextFromContent(msg["content"]); content != "" {
					return content
				}
			}
			if delta, ok := first["delta"].(map[string]interface{}); ok {
				if content := extractTextFromContent(delta["content"]); content != "" {
					return content
				}
			}
		}
	}

	// Anthropic style response.
	if content := extractTextFromContent(respBody["content"]); content != "" {
		return content
	}

	// Gemini style response.
	if candidates, ok := respBody["candidates"].([]interface{}); ok && len(candidates) > 0 {
		if first, ok := candidates[0].(map[string]interface{}); ok {
			if candidateContent, ok := first["content"].(map[string]interface{}); ok {
				if parts, ok := candidateContent["parts"].([]interface{}); ok {
					var texts []string
					for _, rawPart := range parts {
						part, ok := rawPart.(map[string]interface{})
						if !ok {
							continue
						}
						if text, _ := part["text"].(string); text != "" {
							texts = append(texts, text)
						}
					}
					return strings.Join(texts, "\n")
				}
			}
		}
	}

	return ""
}

func extractTextFromContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, raw := range v {
			item, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if text, _ := item["text"].(string); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
