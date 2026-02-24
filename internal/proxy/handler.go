package proxy

import (
	"bufio"
	"bytes"
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
	hashKey     []byte
	upstreamURL string
}

func NewHandler(s store.TokenStore, sess session.Store, hashKey []byte, upstreamURL string) *Handler {
	return &Handler{
		store:       s,
		sessions:    sess,
		stats:       NewUsageStats(),
		hashKey:     hashKey,
		upstreamURL: upstreamURL,
	}
}

// Stats returns the UsageStats for registering the stats endpoint.
func (h *Handler) Stats() *UsageStats {
	return h.stats
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

	// Determine upstream URL (policy override > global default)
	upstream := h.upstreamURL
	if pol.UpstreamURL != "" {
		upstream = pol.UpstreamURL
	}

	// For chat completions, apply policy engine
	if isChatCompletions(r.URL.Path) {
		h.handleChatCompletions(w, r, pol, tokenHash, upstream, start)
		return
	}

	// For all other /v1/* endpoints, passthrough with key swap
	h.passthrough(w, r, pol, tokenHash, upstream, start)
}

func (h *Handler) handleChatCompletions(w http.ResponseWriter, r *http.Request, pol *policy.Policy, tokenHash, upstream string, start time.Time) {
	logEntry := &RequestLog{
		Timestamp:   start.UTC().Format(time.RFC3339Nano),
		Method:      r.Method,
		Path:        r.URL.Path,
		TokenHash:   tokenHash[:16],
		BaseKeyEnv:  pol.BaseKeyEnv,
		UpstreamURL: upstream,
		RemoteAddr:  r.RemoteAddr,
		UserAgent:   r.UserAgent(),
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

	// Model check
	model, _ := reqBody["model"].(string)
	logEntry.Model = model
	if err := pol.CheckModel(model); err != nil {
		logEntry.StatusCode = http.StatusForbidden
		logEntry.Error = err.Error()
		httpError(w, http.StatusForbidden, err.Error())
		h.stats.Record(tokenHash, model, pol.BaseKeyEnv, 0, 0, true)
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
		if err := pol.CheckRules(content); err != nil {
			logEntry.StatusCode = http.StatusForbidden
			logEntry.Error = err.Error()
			httpError(w, http.StatusForbidden, err.Error())
			h.stats.Record(tokenHash, model, pol.BaseKeyEnv, 0, 0, true)
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

	// Inject prompts
	typedMessages = pol.InjectPrompts(typedMessages)

	// Count input tokens and check budget
	inputTokens, err := tokencount.CountMessages(model, typedMessages)
	if err != nil {
		log.Printf("token count error: %v", err)
		inputTokens = 0
	}
	logEntry.InputTokens = inputTokens

	if pol.MaxTokens > 0 {
		usage, _ := h.sessions.GetUsage(tokenHash)
		if usage+int64(inputTokens) > pol.MaxTokens {
			logEntry.StatusCode = http.StatusTooManyRequests
			logEntry.Error = fmt.Sprintf("budget exceeded: used %d + input %d > limit %d", usage, inputTokens, pol.MaxTokens)
			httpError(w, http.StatusTooManyRequests, logEntry.Error)
			h.stats.Record(tokenHash, model, pol.BaseKeyEnv, inputTokens, 0, true)
			return
		}
	}

	// Update request body with injected messages
	interfaceMessages := make([]interface{}, len(typedMessages))
	for i, m := range typedMessages {
		interfaceMessages[i] = m
	}
	reqBody["messages"] = interfaceMessages

	newBody, err := json.Marshal(reqBody)
	if err != nil {
		logEntry.StatusCode = http.StatusInternalServerError
		logEntry.Error = "failed to marshal request"
		httpError(w, http.StatusInternalServerError, logEntry.Error)
		return
	}

	// Resolve the real API key from env
	realKey := os.Getenv(pol.BaseKeyEnv)
	if realKey == "" {
		logEntry.StatusCode = http.StatusInternalServerError
		logEntry.Error = fmt.Sprintf("base key env %q is not set", pol.BaseKeyEnv)
		httpError(w, http.StatusInternalServerError, logEntry.Error)
		return
	}

	// Check if streaming
	stream, _ := reqBody["stream"].(bool)
	logEntry.Stream = stream

	// Build upstream request
	upstreamURL, err := url.Parse(upstream)
	if err != nil {
		logEntry.StatusCode = http.StatusInternalServerError
		logEntry.Error = "invalid upstream URL"
		httpError(w, http.StatusInternalServerError, logEntry.Error)
		return
	}
	upstreamURL.Path = r.URL.Path
	upstreamURL.RawQuery = r.URL.RawQuery

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL.String(), bytes.NewReader(newBody))
	if err != nil {
		logEntry.StatusCode = http.StatusInternalServerError
		logEntry.Error = "failed to create upstream request"
		httpError(w, http.StatusInternalServerError, logEntry.Error)
		return
	}

	// Copy headers, replacing auth
	copyHeaders(r.Header, proxyReq.Header)
	proxyReq.Header.Set("Authorization", "Bearer "+realKey)
	proxyReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(newBody)))

	// Send to upstream
	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		logEntry.StatusCode = http.StatusBadGateway
		logEntry.Error = "upstream request failed"
		httpError(w, http.StatusBadGateway, logEntry.Error)
		h.stats.Record(tokenHash, model, pol.BaseKeyEnv, inputTokens, 0, true)
		return
	}
	defer resp.Body.Close()

	// Track input tokens
	h.sessions.AddUsage(tokenHash, int64(inputTokens))

	if stream && resp.StatusCode == http.StatusOK {
		outputTokens := h.handleStreamingResponse(w, resp, tokenHash, model)
		logEntry.StatusCode = resp.StatusCode
		logEntry.OutputTokens = outputTokens
		h.stats.Record(tokenHash, model, pol.BaseKeyEnv, inputTokens, outputTokens, false)
		return
	}

	// Buffered response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logEntry.StatusCode = http.StatusBadGateway
		logEntry.Error = "failed to read upstream response"
		httpError(w, http.StatusBadGateway, logEntry.Error)
		h.stats.Record(tokenHash, model, pol.BaseKeyEnv, inputTokens, 0, true)
		return
	}

	// Count output tokens from response
	outputTokens := h.countResponseTokens(respBody, tokenHash)
	logEntry.StatusCode = resp.StatusCode
	logEntry.OutputTokens = outputTokens

	isError := resp.StatusCode >= 400
	h.stats.Record(tokenHash, model, pol.BaseKeyEnv, inputTokens, outputTokens, isError)

	// Write response
	copyHeaders(resp.Header, w.Header())
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

func (h *Handler) handleStreamingResponse(w http.ResponseWriter, resp *http.Response, tokenHash, model string) int {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpError(w, http.StatusInternalServerError, "streaming not supported")
		return 0
	}

	copyHeaders(resp.Header, w.Header())
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)

	scanner := bufio.NewScanner(resp.Body)
	var totalOutputTokens int

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

	return totalOutputTokens
}

func (h *Handler) passthrough(w http.ResponseWriter, r *http.Request, pol *policy.Policy, tokenHash, upstream string, start time.Time) {
	logEntry := &RequestLog{
		Timestamp:   start.UTC().Format(time.RFC3339Nano),
		Method:      r.Method,
		Path:        r.URL.Path,
		TokenHash:   tokenHash[:16],
		BaseKeyEnv:  pol.BaseKeyEnv,
		UpstreamURL: upstream,
		RemoteAddr:  r.RemoteAddr,
		UserAgent:   r.UserAgent(),
	}
	defer func() {
		logEntry.DurationMs = time.Since(start).Milliseconds()
		logRequest(logEntry)
	}()

	realKey := os.Getenv(pol.BaseKeyEnv)
	if realKey == "" {
		logEntry.StatusCode = http.StatusInternalServerError
		logEntry.Error = fmt.Sprintf("base key env %q is not set", pol.BaseKeyEnv)
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
