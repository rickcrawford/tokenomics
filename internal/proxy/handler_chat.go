package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/events"
	"github.com/rickcrawford/tokenomics/internal/ledger"
	"github.com/rickcrawford/tokenomics/internal/policy"
	"github.com/rickcrawford/tokenomics/internal/tokencount"
)

func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request, pol *policy.Policy, tokenHash, upstream string, start time.Time) {
	logEntry := &RequestLog{
		Timestamp:  start.UTC().Format(time.RFC3339Nano),
		Method:     r.Method,
		Path:       r.URL.Path,
		TokenHash:  safePrefix(tokenHash, 16),
		RemoteAddr: r.RemoteAddr,
		UserAgent:  r.UserAgent(),
	}
	defer func() {
		logEntry.DurationMs = time.Since(start).Milliseconds()
		if !h.logging.DisableRequest {
			logRequest(logEntry)
		}
	}()

	// Read body (with size limit to prevent abuse)
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
	if err != nil {
		logEntry.StatusCode = http.StatusBadRequest
		logEntry.Error = "failed to read request body"
		httpError(w, http.StatusBadRequest, logEntry.Error)
		return
	}
	r.Body.Close()

	var reqBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
		logEntry.StatusCode = http.StatusBadRequest
		logEntry.Error = "invalid JSON body"
		httpError(w, http.StatusBadRequest, logEntry.Error)
		return
	}

	// Extract model and resolve multi-provider policy
	model, _ := reqBody["model"].(string)
	logEntry.Model = model

	resolved := pol.ResolveForModel(model)

	// Attach metadata to log entry
	if len(resolved.Metadata) > 0 {
		logEntry.Metadata = resolved.Metadata
	}

	// Resolve provider config for auth, headers, and chat path
	var providerCfg *config.ProviderConfig
	if resolved.ProviderName != "" {
		if pc, ok := h.providers[resolved.ProviderName]; ok {
			providerCfg = &pc
		}
	}

	// Override upstream: policy > provider config > global
	if resolved.UpstreamURL != "" {
		upstream = resolved.UpstreamURL
	} else if providerCfg != nil && providerCfg.UpstreamURL != "" {
		upstream = providerCfg.UpstreamURL
	}

	// Use provider's api_key_env if the resolved policy doesn't specify one
	if resolved.BaseKeyEnv == "" && providerCfg != nil && providerCfg.APIKeyEnv != "" {
		resolved.BaseKeyEnv = providerCfg.APIKeyEnv
	}

	logEntry.BaseKeyEnv = resolved.BaseKeyEnv
	logEntry.UpstreamURL = upstream

	// Rate limit check
	if err := h.rateLimiter.Allow(tokenHash, resolved.RateLimit); err != nil {
		h.emitter.Emit(r.Context(), events.New(events.RateExceeded, map[string]interface{}{
			"token_hash": safePrefix(tokenHash, 16), "model": model, "error": err.Error(),
		}))
		logEntry.StatusCode = http.StatusTooManyRequests
		logEntry.Error = err.Error()
		httpError(w, http.StatusTooManyRequests, err.Error())
		h.stats.Record(tokenHash, model, resolved.BaseKeyEnv, 0, 0, true)
		return
	}

	// Track parallel requests
	h.rateLimiter.Acquire(tokenHash, resolved.RateLimit)
	defer h.rateLimiter.Release(tokenHash, resolved.RateLimit)

	// Model check
	if err := resolved.CheckModel(model); err != nil {
		logEntry.StatusCode = http.StatusForbidden
		logEntry.Error = err.Error()
		httpError(w, http.StatusForbidden, err.Error())
		h.stats.Record(tokenHash, model, resolved.BaseKeyEnv, 0, 0, true)
		return
	}

	// Rules check on user messages (input scope)
	messages, _ := reqBody["messages"].([]interface{})
	for i, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		content, _ := msg["content"].(string)
		if content == "" {
			continue
		}

		// Check rules — fail action returns an error, warn/log just return matches
		matches, err := resolved.CheckRules(content, "input")
		if err != nil {
			// Record the policy violation (fail) matches in structured log
			for _, match := range matches {
				logEntry.RuleMatches = append(logEntry.RuleMatches, RuleMatchLog{
					Name:    match.Name,
					Action:  match.Action,
					Message: match.Message,
				})
				h.emitter.Emit(r.Context(), events.New(events.RuleViolation, map[string]interface{}{
					"token_hash": safePrefix(tokenHash, 16), "model": model,
					"rule_name": match.Name, "message": match.Message,
				}))
			}
			log.Printf("[rule:fail] policy violation: %s (token=%s model=%s)", err.Error(), safePrefix(tokenHash, 16), model)
			logEntry.StatusCode = http.StatusForbidden
			logEntry.Error = err.Error()
			httpError(w, http.StatusForbidden, err.Error())
			h.stats.Record(tokenHash, model, resolved.BaseKeyEnv, 0, 0, true)
			return
		}

		// Record and log any warn/log matches
		for _, match := range matches {
			logEntry.RuleMatches = append(logEntry.RuleMatches, RuleMatchLog{
				Name:    match.Name,
				Action:  match.Action,
				Message: match.Message,
			})
			evtType := events.RuleMatch
			if match.Action == "warn" {
				evtType = events.RuleWarning
			}
			h.emitter.Emit(r.Context(), events.New(evtType, map[string]interface{}{
				"token_hash": safePrefix(tokenHash, 16), "model": model,
				"rule_name": match.Name, "action": match.Action, "message": match.Message,
			}))
			log.Printf("[rule:%s] %s (token=%s model=%s)", match.Action, match.Message, safePrefix(tokenHash, 16), model)
		}

		// Apply mask rules: redact content before forwarding
		masked := resolved.MaskContent(content, "input")
		if masked != content {
			msg["content"] = masked
			messages[i] = msg
			logEntry.RuleMatches = append(logEntry.RuleMatches, RuleMatchLog{
				Action:  "mask",
				Message: "content redacted before forwarding",
			})
			h.emitter.Emit(r.Context(), events.New(events.RuleMask, map[string]interface{}{
				"token_hash": safePrefix(tokenHash, 16), "model": model,
			}))
			log.Printf("[rule:mask] content redacted (token=%s model=%s)", safePrefix(tokenHash, 16), model)
		}
	}

	// Convert messages to typed slice for prompt injection
	typedMessages := make([]map[string]interface{}, 0, len(messages))
	for _, m := range messages {
		if msg, ok := m.(map[string]interface{}); ok {
			typedMessages = append(typedMessages, msg)
		}
	}

	// Inject prompts from resolved policy
	typedMessages = resolved.InjectPrompts(typedMessages)

	// Count input tokens and check budget
	inputTokens, err := tokencount.CountMessages(model, typedMessages)
	if err != nil {
		log.Printf("[tokencount] error for model=%s token=%s: %v", model, safePrefix(tokenHash, 8), err)
		inputTokens = 0
	}
	logEntry.InputTokens = inputTokens

	if resolved.MaxTokens > 0 {
		usage, _ := h.sessions.GetUsage(tokenHash)
		if usage+int64(inputTokens) > resolved.MaxTokens {
			h.emitter.Emit(r.Context(), events.New(events.BudgetExceeded, map[string]interface{}{
				"token_hash": safePrefix(tokenHash, 16), "model": model,
				"used": usage, "input": inputTokens, "limit": resolved.MaxTokens,
			}))
			logEntry.StatusCode = http.StatusTooManyRequests
			logEntry.Error = fmt.Sprintf("budget exceeded: used %d + input %d > limit %d", usage, inputTokens, resolved.MaxTokens)
			httpError(w, http.StatusTooManyRequests, logEntry.Error)
			h.stats.Record(tokenHash, model, resolved.BaseKeyEnv, inputTokens, 0, true)
			return
		}
	}

	// Update request body with injected messages
	interfaceMessages := make([]interface{}, len(typedMessages))
	for i, m := range typedMessages {
		interfaceMessages[i] = m
	}
	reqBody["messages"] = interfaceMessages

	// Check if streaming
	stream, _ := reqBody["stream"].(bool)
	logEntry.Stream = stream

	// Build the model list for retry/fallback: primary model first, then fallbacks
	modelsToTry := []string{model}
	if resolved.Retry != nil && len(resolved.Retry.Fallbacks) > 0 {
		modelsToTry = append(modelsToTry, resolved.Retry.Fallbacks...)
	}

	maxAttempts := 1
	if resolved.Retry != nil && resolved.Retry.MaxRetries > 0 {
		maxAttempts = 1 + resolved.Retry.MaxRetries
	}

	// Determine timeout
	timeout := 30 * time.Second // default
	if resolved.Timeout > 0 {
		timeout = time.Duration(resolved.Timeout) * time.Second
	}

	// Generate a client request ID for upstream correlation
	clientRequestID := generateRequestID()
	logEntry.ClientRequestID = clientRequestID

	// Try each model with retries
	var lastResp *http.Response
	var lastErr error
	var lastBody []byte
	retryCount := 0

	for _, tryModel := range modelsToTry {
		reqBody["model"] = tryModel

		for attempt := 0; attempt < maxAttempts; attempt++ {
			newBody, err := json.Marshal(reqBody)
			if err != nil {
				logEntry.StatusCode = http.StatusInternalServerError
				logEntry.Error = "failed to marshal request"
				httpError(w, http.StatusInternalServerError, logEntry.Error)
				return
			}

			// Resolve the real API key from env
			realKey := os.Getenv(resolved.BaseKeyEnv)
			if realKey == "" {
				logEntry.StatusCode = http.StatusInternalServerError
				logEntry.Error = fmt.Sprintf("base key env %q is not set", resolved.BaseKeyEnv)
				httpError(w, http.StatusInternalServerError, logEntry.Error)
				return
			}

			// Build upstream request with timeout
			upstreamURL, err := url.Parse(upstream)
			if err != nil {
				logEntry.StatusCode = http.StatusInternalServerError
				logEntry.Error = "invalid upstream URL"
				httpError(w, http.StatusInternalServerError, logEntry.Error)
				return
			}

			// Use provider chat path if configured, otherwise use the original request path
			if providerCfg != nil && providerCfg.ChatPath != "" {
				upstreamURL.Path = providerCfg.ChatPath
			} else {
				upstreamURL.Path = r.URL.Path
			}
			upstreamURL.RawQuery = r.URL.RawQuery

			// Apply query-based auth if needed
			authScheme := "bearer"
			if providerCfg != nil && providerCfg.AuthScheme != "" {
				authScheme = providerCfg.AuthScheme
			}
			if authScheme == "query" {
				q := upstreamURL.Query()
				q.Set("key", realKey)
				upstreamURL.RawQuery = q.Encode()
			}

			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			proxyReq, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL.String(), bytes.NewReader(newBody))
			if err != nil {
				cancel()
				logEntry.StatusCode = http.StatusInternalServerError
				logEntry.Error = "failed to create upstream request"
				httpError(w, http.StatusInternalServerError, logEntry.Error)
				return
			}

			// Remove client's Authorization header before copying, since we'll set provider-specific auth
			clientHeaders := r.Header.Clone()
			clientHeaders.Del("Authorization")
			copyHeaders(clientHeaders, proxyReq.Header)

			// Set auth based on provider scheme
			switch authScheme {
			case "header":
				authHeader := "Authorization"
				if providerCfg != nil && providerCfg.AuthHeader != "" {
					authHeader = providerCfg.AuthHeader
				}
				if authHeader != "" {
					proxyReq.Header.Set(authHeader, realKey)
				}
			case "query":
			default: // "bearer"
				proxyReq.Header.Set("Authorization", "Bearer "+realKey)
			}

			// Add provider-specific extra headers
			if providerCfg != nil {
				for k, v := range providerCfg.Headers {
					proxyReq.Header.Set(k, v)
				}
			}

			proxyReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(newBody)))
			proxyReq.Header.Set("X-Client-Request-Id", clientRequestID)

			client := &http.Client{}
			resp, err := client.Do(proxyReq)
			if err != nil {
				cancel()
				lastErr = err
				retryCount++
				continue
			}

			// Check if we should retry based on status code
			if shouldRetry(resolved.Retry, resp.StatusCode) && (attempt < maxAttempts-1 || tryModel != modelsToTry[len(modelsToTry)-1]) {
				respBody, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				cancel()
				lastBody = respBody
				lastResp = resp
				retryCount++
				continue
			}

			// Success or non-retryable error — use this response
			cancel()
			logEntry.RetryCount = retryCount
			if tryModel != model {
				logEntry.FallbackModel = tryModel
			}

			// Capture upstream provider request ID from response headers
			logEntry.UpstreamRequestID = extractUpstreamRequestID(resp.Header)

			h.sessions.AddUsage(tokenHash, int64(inputTokens))
			h.emitter.Emit(r.Context(), events.New(events.BudgetUpdate, map[string]interface{}{
				"token_hash": safePrefix(tokenHash, 16), "model": tryModel, "input_tokens": inputTokens,
			}))

			// Record token usage for rate limiter
			h.rateLimiter.RecordTokens(tokenHash, resolved.RateLimit, inputTokens)

			// Collect user content for memory logging
			var userContent string
			if memWriter := h.getMemoryWriter(resolved.Memory); memWriter != nil {
				var parts []string
				for _, m := range messages {
					msg, ok := m.(map[string]interface{})
					if !ok {
						continue
					}
					role, _ := msg["role"].(string)
					content, _ := msg["content"].(string)
					if content != "" && role == "user" {
						parts = append(parts, content)
					}
				}
				userContent = strings.Join(parts, "\n\n")
			}

			if stream && resp.StatusCode == http.StatusOK {
				outputTokens, assistantContent, streamUpstreamID, streamLastChunk := h.handleStreamingResponse(w, resp, tokenHash, tryModel)
				resp.Body.Close()
				logEntry.StatusCode = resp.StatusCode
				logEntry.OutputTokens = outputTokens
				logEntry.UpstreamID = streamUpstreamID
				h.stats.Record(tokenHash, tryModel, resolved.BaseKeyEnv, inputTokens, outputTokens, false)
				h.rateLimiter.RecordTokens(tokenHash, resolved.RateLimit, outputTokens)
				h.emitter.Emit(r.Context(), events.New(events.RequestCompleted, map[string]interface{}{
					"token_hash": safePrefix(tokenHash, 16), "model": tryModel, "stream": true,
					"status_code": resp.StatusCode, "input_tokens": inputTokens, "output_tokens": outputTokens,
				}))

				if memWriter := h.getMemoryWriter(resolved.Memory); memWriter != nil {
					if userContent != "" {
						memWriter.Append(tokenHash, "user", tryModel, userContent)
					}
					if assistantContent != "" {
						memWriter.Append(tokenHash, "assistant", tryModel, assistantContent)
					}
				}

				// Record to ledger
				h.recordLedgerEntry(logEntry, tokenHash, tryModel, resolved.ProviderName,
					inputTokens, outputTokens, resp.StatusCode, stream, retryCount,
					extractProviderMetaFromStream(resp.Header, streamLastChunk))
				if h.ledger != nil {
					if userContent != "" {
						h.ledger.RecordMemory(tokenHash, "user", tryModel, userContent)
					}
					if assistantContent != "" {
						h.ledger.RecordMemory(tokenHash, "assistant", tryModel, assistantContent)
					}
				}
				return
			}

			// Buffered response
			respBody, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				logEntry.StatusCode = http.StatusBadGateway
				logEntry.Error = "failed to read upstream response"
				httpError(w, http.StatusBadGateway, logEntry.Error)
				h.stats.Record(tokenHash, tryModel, resolved.BaseKeyEnv, inputTokens, 0, true)
				return
			}

			outputTokens := h.countResponseTokens(respBody, tokenHash)
			logEntry.StatusCode = resp.StatusCode
			logEntry.OutputTokens = outputTokens
			logEntry.UpstreamID = extractUpstreamID(respBody)
			h.rateLimiter.RecordTokens(tokenHash, resolved.RateLimit, outputTokens)

			isError := resp.StatusCode >= 400
			h.stats.Record(tokenHash, tryModel, resolved.BaseKeyEnv, inputTokens, outputTokens, isError)
			h.emitter.Emit(r.Context(), events.New(events.RequestCompleted, map[string]interface{}{
				"token_hash": safePrefix(tokenHash, 16), "model": tryModel, "stream": false,
				"status_code": resp.StatusCode, "input_tokens": inputTokens, "output_tokens": outputTokens,
				"error": isError,
			}))

			var assistantContent string
			if !isError {
				if memWriter := h.getMemoryWriter(resolved.Memory); memWriter != nil {
					if userContent != "" {
						memWriter.Append(tokenHash, "user", tryModel, userContent)
					}
					assistantContent = extractAssistantContent(respBody)
					if assistantContent != "" {
						memWriter.Append(tokenHash, "assistant", tryModel, assistantContent)
					}
				}
			}

			// Record to ledger
			h.recordLedgerEntry(logEntry, tokenHash, tryModel, resolved.ProviderName,
				inputTokens, outputTokens, resp.StatusCode, stream, retryCount,
				extractProviderMeta(resp.Header, respBody))
			if h.ledger != nil && !isError {
				if userContent != "" {
					h.ledger.RecordMemory(tokenHash, "user", tryModel, userContent)
				}
				if assistantContent == "" {
					assistantContent = extractAssistantContent(respBody)
				}
				if assistantContent != "" {
					h.ledger.RecordMemory(tokenHash, "assistant", tryModel, assistantContent)
				}
			}

			copyHeaders(resp.Header, w.Header())
			w.WriteHeader(resp.StatusCode)
			w.Write(respBody)
			return
		}
	}

	// All retries/fallbacks exhausted
	logEntry.RetryCount = retryCount
	if lastResp != nil {
		logEntry.StatusCode = lastResp.StatusCode
		logEntry.Error = fmt.Sprintf("all retries exhausted (last status %d)", lastResp.StatusCode)
		h.stats.Record(tokenHash, model, resolved.BaseKeyEnv, inputTokens, 0, true)
		copyHeaders(lastResp.Header, w.Header())
		w.WriteHeader(lastResp.StatusCode)
		w.Write(lastBody)
		return
	}
	logEntry.StatusCode = http.StatusBadGateway
	logEntry.Error = fmt.Sprintf("upstream request failed after %d attempts: %v", retryCount, lastErr)
	httpError(w, http.StatusBadGateway, logEntry.Error)
	h.stats.Record(tokenHash, model, resolved.BaseKeyEnv, inputTokens, 0, true)
}

// shouldRetry checks if the status code should trigger a retry.
func shouldRetry(cfg *policy.RetryConfig, statusCode int) bool {
	if cfg == nil || cfg.MaxRetries == 0 {
		return false
	}
	retryOn := cfg.RetryOn
	if len(retryOn) == 0 {
		retryOn = []int{429, 500, 502, 503}
	}
	for _, code := range retryOn {
		if statusCode == code {
			return true
		}
	}
	return false
}

// handleStreamingResponse streams the upstream SSE response to the client,
// counting output tokens and extracting the upstream completion ID.
// Returns (outputTokens, assistantContent, upstreamID, lastChunk).
func (h *Handler) handleStreamingResponse(w http.ResponseWriter, resp *http.Response, tokenHash, model string) (int, string, string, map[string]interface{}) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpError(w, http.StatusInternalServerError, "streaming not supported")
		return 0, "", "", nil
	}

	copyHeaders(resp.Header, w.Header())
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)

	scanner := bufio.NewScanner(resp.Body)
	var totalOutputTokens int
	var contentBuilder strings.Builder
	var upstreamID string
	var lastChunk map[string]interface{}

	for scanner.Scan() {
		line := scanner.Text()

		// Write the line through to the client
		fmt.Fprintf(w, "%s\n", line)
		flusher.Flush()

		// Parse SSE data lines for token counting
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				continue
			}

			var chunk map[string]interface{}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}

			lastChunk = chunk

			// Extract upstream ID from the first chunk
			if upstreamID == "" {
				if id, ok := chunk["id"].(string); ok {
					upstreamID = id
				} else if id, ok := chunk["responseId"].(string); ok {
					upstreamID = id
				}
			}

			// Count content tokens from delta
			if choices, ok := chunk["choices"].([]interface{}); ok {
				for _, c := range choices {
					choice, ok := c.(map[string]interface{})
					if !ok {
						continue
					}
					delta, ok := choice["delta"].(map[string]interface{})
					if !ok {
						continue
					}
					content, _ := delta["content"].(string)
					if content != "" {
						contentBuilder.WriteString(content)
						n, err := tokencount.Count(model, content)
						if err == nil {
							totalOutputTokens += n
						}
					}
				}
			}

			// Also check usage field (some providers include it)
			if usage, ok := chunk["usage"].(map[string]interface{}); ok {
				if completionTokens, ok := usage["completion_tokens"].(float64); ok {
					totalOutputTokens = int(completionTokens)
				}
			}
		}
	}

	if totalOutputTokens > 0 {
		h.sessions.AddUsage(tokenHash, int64(totalOutputTokens))
	}

	return totalOutputTokens, contentBuilder.String(), upstreamID, lastChunk
}

func (h *Handler) countResponseTokens(body []byte, tokenHash string) int {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0
	}
	usage, ok := resp["usage"].(map[string]interface{})
	if !ok {
		return 0
	}
	completionTokens, ok := usage["completion_tokens"].(float64)
	if !ok {
		return 0
	}
	if completionTokens > 0 {
		h.sessions.AddUsage(tokenHash, int64(completionTokens))
	}
	return int(completionTokens)
}

// recordLedgerEntry records a request to the session ledger if configured.
func (h *Handler) recordLedgerEntry(logEntry *RequestLog, tokenHash, model, provider string,
	inputTokens, outputTokens, statusCode int, stream bool, retryCount int, providerMeta *ledger.ProviderMeta) {
	if h.ledger == nil {
		return
	}

	entry := ledger.RequestEntry{
		Timestamp:         time.Now().UTC(),
		TokenHash:         tokenHash,
		Model:             model,
		Provider:          provider,
		InputTokens:       inputTokens,
		OutputTokens:      outputTokens,
		DurationMs:        logEntry.DurationMs,
		StatusCode:        statusCode,
		Stream:            stream,
		Error:             logEntry.Error,
		UpstreamID:        logEntry.UpstreamID,
		UpstreamRequestID: logEntry.UpstreamRequestID,
		RetryCount:        retryCount,
		FallbackModel:     logEntry.FallbackModel,
		Metadata:          logEntry.Metadata,
		ProviderMeta:      providerMeta,
	}

	// Convert rule matches
	for _, rm := range logEntry.RuleMatches {
		entry.RuleMatches = append(entry.RuleMatches, ledger.RuleMatchEntry{
			Name:    rm.Name,
			Action:  rm.Action,
			Message: rm.Message,
		})
	}

	h.ledger.RecordRequest(entry)
}

func extractAssistantContent(body []byte) string {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	choices, ok := resp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return ""
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return ""
	}
	msg, ok := choice["message"].(map[string]interface{})
	if !ok {
		return ""
	}
	content, _ := msg["content"].(string)
	return content
}
