package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rickcrawford/tokenomics/internal/policy"
	"github.com/rickcrawford/tokenomics/internal/session"
	"github.com/rickcrawford/tokenomics/internal/store"
	"github.com/rickcrawford/tokenomics/internal/tokencount"
)

type Handler struct {
	store       store.TokenStore
	sessions    session.Store
	stats       *UsageStats
	rateLimiter *RateLimiter
	hashKey     []byte
	upstreamURL string

	memWriterMu    sync.Mutex
	memWriters     map[string]session.MemoryWriter
	redisMemWriter session.MemoryWriter
}

func NewHandler(s store.TokenStore, sess session.Store, hashKey []byte, upstreamURL string) *Handler {
	return &Handler{
		store:       s,
		sessions:    sess,
		stats:       NewUsageStats(),
		rateLimiter: NewRateLimiter(),
		hashKey:     hashKey,
		upstreamURL: upstreamURL,
		memWriters:  make(map[string]session.MemoryWriter),
	}
}

// Stats returns the UsageStats for registering the stats endpoint.
func (h *Handler) Stats() *UsageStats {
	return h.stats
}

// SetRedisMemoryWriter sets the Redis-backed memory writer for session logging.
func (h *Handler) SetRedisMemoryWriter(w session.MemoryWriter) {
	h.redisMemWriter = w
}

// getMemoryWriter returns the appropriate memory writer for the given config.
// Returns nil if memory is disabled.
func (h *Handler) getMemoryWriter(mc policy.MemoryConfig) session.MemoryWriter {
	if !mc.Enabled {
		return nil
	}
	if mc.FilePath != "" {
		h.memWriterMu.Lock()
		defer h.memWriterMu.Unlock()
		if w, ok := h.memWriters[mc.FilePath]; ok {
			return w
		}
		w, err := session.NewFileMemoryWriter(mc.FilePath)
		if err != nil {
			log.Printf("failed to create file memory writer for %q: %v", mc.FilePath, err)
			return nil
		}
		h.memWriters[mc.FilePath] = w
		return w
	}
	if mc.Redis && h.redisMemWriter != nil {
		return h.redisMemWriter
	}
	return nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Extract bearer token
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		httpError(w, http.StatusUnauthorized, "missing or invalid Authorization header")
		logRequest(&RequestLog{
			Timestamp:  start.UTC().Format(time.RFC3339Nano),
			Method:     r.Method,
			Path:       r.URL.Path,
			StatusCode: http.StatusUnauthorized,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      "missing or invalid Authorization header",
			RemoteAddr: r.RemoteAddr,
			UserAgent:  r.UserAgent(),
		})
		return
	}
	rawToken := strings.TrimPrefix(auth, "Bearer ")

	// Hash token
	tokenHash := h.hashToken(rawToken)

	// Lookup policy
	pol, err := h.store.Lookup(tokenHash)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "store lookup error")
		logRequest(&RequestLog{
			Timestamp:  start.UTC().Format(time.RFC3339Nano),
			Method:     r.Method,
			Path:       r.URL.Path,
			TokenHash:  tokenHash[:16],
			StatusCode: http.StatusInternalServerError,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      "store lookup error",
			RemoteAddr: r.RemoteAddr,
			UserAgent:  r.UserAgent(),
		})
		return
	}
	if pol == nil {
		httpError(w, http.StatusUnauthorized, "invalid token")
		logRequest(&RequestLog{
			Timestamp:  start.UTC().Format(time.RFC3339Nano),
			Method:     r.Method,
			Path:       r.URL.Path,
			TokenHash:  tokenHash[:16],
			StatusCode: http.StatusUnauthorized,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      "invalid token",
			RemoteAddr: r.RemoteAddr,
			UserAgent:  r.UserAgent(),
		})
		return
	}

	// Default upstream from global config
	upstream := h.upstreamURL
	if pol.UpstreamURL != "" {
		upstream = pol.UpstreamURL
	}

	// For chat completions, apply policy engine with multi-provider resolution
	if isChatCompletions(r.URL.Path) {
		h.handleChatCompletions(w, r, pol, tokenHash, upstream, start)
		return
	}

	// For all other /v1/* endpoints, passthrough with key swap
	h.passthrough(w, r, pol, tokenHash, upstream, start)
}

func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request, pol *policy.Policy, tokenHash, upstream string, start time.Time) {
	logEntry := &RequestLog{
		Timestamp:  start.UTC().Format(time.RFC3339Nano),
		Method:     r.Method,
		Path:       r.URL.Path,
		TokenHash:  tokenHash[:16],
		RemoteAddr: r.RemoteAddr,
		UserAgent:  r.UserAgent(),
	}
	defer func() {
		logEntry.DurationMs = time.Since(start).Milliseconds()
		logRequest(logEntry)
	}()

	// Read body
	bodyBytes, err := io.ReadAll(r.Body)
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

	// Override upstream with resolved policy if set
	if resolved.UpstreamURL != "" {
		upstream = resolved.UpstreamURL
	}
	logEntry.BaseKeyEnv = resolved.BaseKeyEnv
	logEntry.UpstreamURL = upstream

	// Rate limit check
	if err := h.rateLimiter.Allow(tokenHash, resolved.RateLimit); err != nil {
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

	// Rules check on user messages
	messages, _ := reqBody["messages"].([]interface{})
	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		content, _ := msg["content"].(string)
		if content == "" {
			continue
		}
		if err := resolved.CheckRules(content); err != nil {
			logEntry.StatusCode = http.StatusForbidden
			logEntry.Error = err.Error()
			httpError(w, http.StatusForbidden, err.Error())
			h.stats.Record(tokenHash, model, resolved.BaseKeyEnv, 0, 0, true)
			return
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
		log.Printf("token count error: %v", err)
		inputTokens = 0
	}
	logEntry.InputTokens = inputTokens

	if resolved.MaxTokens > 0 {
		usage, _ := h.sessions.GetUsage(tokenHash)
		if usage+int64(inputTokens) > resolved.MaxTokens {
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
			upstreamURL.Path = r.URL.Path
			upstreamURL.RawQuery = r.URL.RawQuery

			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			proxyReq, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL.String(), bytes.NewReader(newBody))
			if err != nil {
				cancel()
				logEntry.StatusCode = http.StatusInternalServerError
				logEntry.Error = "failed to create upstream request"
				httpError(w, http.StatusInternalServerError, logEntry.Error)
				return
			}

			copyHeaders(r.Header, proxyReq.Header)
			proxyReq.Header.Set("Authorization", "Bearer "+realKey)
			proxyReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(newBody)))

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

			h.sessions.AddUsage(tokenHash, int64(inputTokens))

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
				outputTokens, assistantContent := h.handleStreamingResponse(w, resp, tokenHash, tryModel)
				resp.Body.Close()
				logEntry.StatusCode = resp.StatusCode
				logEntry.OutputTokens = outputTokens
				h.stats.Record(tokenHash, tryModel, resolved.BaseKeyEnv, inputTokens, outputTokens, false)
				h.rateLimiter.RecordTokens(tokenHash, resolved.RateLimit, outputTokens)

				if memWriter := h.getMemoryWriter(resolved.Memory); memWriter != nil {
					if userContent != "" {
						memWriter.Append(tokenHash, "user", tryModel, userContent)
					}
					if assistantContent != "" {
						memWriter.Append(tokenHash, "assistant", tryModel, assistantContent)
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
			h.rateLimiter.RecordTokens(tokenHash, resolved.RateLimit, outputTokens)

			isError := resp.StatusCode >= 400
			h.stats.Record(tokenHash, tryModel, resolved.BaseKeyEnv, inputTokens, outputTokens, isError)

			if !isError {
				if memWriter := h.getMemoryWriter(resolved.Memory); memWriter != nil {
					if userContent != "" {
						memWriter.Append(tokenHash, "user", tryModel, userContent)
					}
					assistantContent := extractAssistantContent(respBody)
					if assistantContent != "" {
						memWriter.Append(tokenHash, "assistant", tryModel, assistantContent)
					}
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

func (h *Handler) handleStreamingResponse(w http.ResponseWriter, resp *http.Response, tokenHash, model string) (int, string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpError(w, http.StatusInternalServerError, "streaming not supported")
		return 0, ""
	}

	copyHeaders(resp.Header, w.Header())
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)

	scanner := bufio.NewScanner(resp.Body)
	var totalOutputTokens int
	var contentBuilder strings.Builder

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

	return totalOutputTokens, contentBuilder.String()
}

func (h *Handler) passthrough(w http.ResponseWriter, r *http.Request, pol *policy.Policy, tokenHash, upstream string, start time.Time) {
	resolved := pol.ResolveProvider("")

	logEntry := &RequestLog{
		Timestamp:   start.UTC().Format(time.RFC3339Nano),
		Method:      r.Method,
		Path:        r.URL.Path,
		TokenHash:   tokenHash[:16],
		BaseKeyEnv:  resolved.BaseKeyEnv,
		UpstreamURL: upstream,
		RemoteAddr:  r.RemoteAddr,
		UserAgent:   r.UserAgent(),
	}
	defer func() {
		logEntry.DurationMs = time.Since(start).Milliseconds()
		logRequest(logEntry)
	}()

	realKey := os.Getenv(resolved.BaseKeyEnv)
	if realKey == "" {
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

	lw := newLoggingResponseWriter(w)

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = upstreamURL.Scheme
			req.URL.Host = upstreamURL.Host
			req.Host = upstreamURL.Host
			req.Header.Set("Authorization", "Bearer "+realKey)
		},
	}

	proxy.ServeHTTP(lw, r)
	logEntry.StatusCode = lw.statusCode
}

func (h *Handler) hashToken(token string) string {
	mac := hmac.New(sha256.New, h.hashKey)
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
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

func isChatCompletions(path string) bool {
	return strings.HasSuffix(path, "/chat/completions")
}

func copyHeaders(src, dst http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": msg,
			"type":    "tokenomics_error",
			"code":    code,
		},
	})
}
