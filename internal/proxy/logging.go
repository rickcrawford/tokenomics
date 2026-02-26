package proxy

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// RequestLog is a structured JSON log entry for each proxied request.
type RequestLog struct {
	Timestamp     string            `json:"timestamp"`
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	TokenHash     string            `json:"token_hash"`
	Model         string            `json:"model,omitempty"`
	BaseKeyEnv    string            `json:"base_key_env"`
	UpstreamURL   string            `json:"upstream_url"`
	StatusCode    int               `json:"status_code"`
	DurationMs    int64             `json:"duration_ms"`
	InputTokens   int               `json:"input_tokens,omitempty"`
	OutputTokens  int               `json:"output_tokens,omitempty"`
	Stream        bool              `json:"stream,omitempty"`
	Error         string            `json:"error,omitempty"`
	RemoteAddr    string            `json:"remote_addr"`
	UserAgent     string            `json:"user_agent,omitempty"`
	RetryCount    int               `json:"retry_count,omitempty"`
	FallbackModel string            `json:"fallback_model,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`

	// Rule match tracking
	RuleMatches []RuleMatchLog `json:"rule_matches,omitempty"` // Non-blocking rule matches (warn, log, mask)

	// Upstream provider tracking IDs
	UpstreamRequestID string `json:"upstream_request_id,omitempty"` // Provider's request ID from response header
	UpstreamID        string `json:"upstream_id,omitempty"`         // Provider's response body ID (chatcmpl-*, msg_*, etc.)
	ClientRequestID   string `json:"client_request_id,omitempty"`   // Our outbound tracking ID sent to provider
}

// RuleMatchLog records a non-blocking rule match in the request log.
type RuleMatchLog struct {
	Name    string `json:"name,omitempty"`
	Action  string `json:"action"`
	Message string `json:"message"`
}

// extractUpstreamRequestID pulls the provider's request ID from response headers.
// Supports OpenAI (x-request-id), Anthropic (request-id), Azure (apim-request-id),
// and Mistral (Mistral-Correlation-Id).
func extractUpstreamRequestID(h http.Header) string {
	// Try provider-specific headers in priority order
	for _, key := range []string{
		"X-Request-Id",           // OpenAI, Azure OpenAI
		"Request-Id",             // Anthropic
		"Apim-Request-Id",        // Azure API Management
		"Mistral-Correlation-Id", // Mistral
	} {
		if v := h.Get(key); v != "" {
			return v
		}
	}
	return ""
}

// extractUpstreamID pulls the provider's completion/message ID from the response body.
// Returns the "id" field (e.g., "chatcmpl-...", "msg_...") or "responseId" for Gemini.
func extractUpstreamID(body []byte) string {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	// OpenAI, Anthropic, Cohere, Mistral all use "id"
	if id, ok := resp["id"].(string); ok {
		return id
	}
	// Google Gemini uses "responseId"
	if id, ok := resp["responseId"].(string); ok {
		return id
	}
	return ""
}

func logRequest(entry *RequestLog) {
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[request] marshal error: %v (method=%s path=%s status=%d)", err, entry.Method, entry.Path, entry.StatusCode)
		return
	}
	log.Println(string(data))
}

// loggingResponseWriter captures the status code written to the response.
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func newLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (lw *loggingResponseWriter) WriteHeader(code int) {
	if !lw.written {
		lw.statusCode = code
		lw.written = true
	}
	lw.ResponseWriter.WriteHeader(code)
}

func (lw *loggingResponseWriter) Flush() {
	if f, ok := lw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// streamingResponseWriter captures SSE content and usage info from streaming responses.
// For non-streaming responses, captures the first chunk (full body).
// For streaming (SSE), parses text_delta messages and accumulates response content.
type streamingResponseWriter struct {
	http.ResponseWriter
	sseBuffer      bytes.Buffer // Accumulate incomplete SSE lines
	rawResponse    bytes.Buffer // Raw response body (JSON or SSE), capped at maxResponseBodySize
	assistantText  bytes.Buffer // Extracted assistant text from streaming deltas
	chunkCount     int
	isStreaming    bool
	contentSize    int64 // Track captured raw response size
	isTruncated    bool  // True when raw response exceeds maxResponseBodySize
	inputTokens    int
	outputTokens   int
}

func (sw *streamingResponseWriter) Write(b []byte) (int, error) {
	// Check if this looks like SSE (has "data:" prefix)
	if sw.chunkCount == 0 && len(b) > 0 {
		sw.isStreaming = bytes.Contains(b, []byte("data:"))
	}

	// Capture raw response body with a hard cap to bound memory.
	if sw.contentSize < maxResponseBodySize {
		remaining := int64(maxResponseBodySize) - sw.contentSize
		toWrite := b
		if int64(len(toWrite)) > remaining {
			toWrite = toWrite[:remaining]
			sw.isTruncated = true
		}
		sw.rawResponse.Write(toWrite)
		sw.contentSize += int64(len(toWrite))
	} else {
		sw.isTruncated = true
	}

	// For streaming responses, parse SSE and extract assistant text and usage.
	if sw.isStreaming {
		sw.parseAndAccumulateSSE(b)
	}

	sw.chunkCount++
	return sw.ResponseWriter.Write(b)
}

// parseAndAccumulateSSE parses Server-Sent Events and accumulates assistant text.
func (sw *streamingResponseWriter) parseAndAccumulateSSE(data []byte) {
	sw.sseBuffer.Write(data)

	for {
		line, err := sw.sseBuffer.ReadBytes('\n')
		if err != nil {
			// If no complete line, put it back for next batch
			if len(line) > 0 {
				sw.sseBuffer.Write(line)
			}
			break
		}

		// Process complete line
		lineStr := strings.TrimSpace(string(line))
		if !strings.HasPrefix(lineStr, "data:") {
			continue
		}

		jsonStr := strings.TrimPrefix(lineStr, "data:")
		jsonStr = strings.TrimSpace(jsonStr)

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &event); err != nil {
			continue
		}

		// Generic usage extraction from OpenAI/Anthropic stream chunks.
		in, out := extractTokenCountsFromResponse(event)
		if in > 0 {
			sw.inputTokens = in
		}
		if out > 0 {
			sw.outputTokens = out
		}

		// Extract text deltas from OpenAI-compatible stream chunks.
		if choices, ok := event["choices"].([]interface{}); ok {
			for _, rawChoice := range choices {
				choice, ok := rawChoice.(map[string]interface{})
				if !ok {
					continue
				}
				delta, ok := choice["delta"].(map[string]interface{})
				if !ok {
					continue
				}
				if text, _ := delta["content"].(string); text != "" {
					sw.assistantText.WriteString(text)
				}
			}
		}

		// Extract text deltas (Anthropic format).
		if eventType, ok := event["type"].(string); ok {
			switch eventType {
			case "content_block_delta":
				if delta, ok := event["delta"].(map[string]interface{}); ok {
					if deltaType, ok := delta["type"].(string); ok && deltaType == "text_delta" {
						if text, ok := delta["text"].(string); ok {
							sw.assistantText.WriteString(text)
						}
					}
				}
			case "message_delta":
				// Anthropic may emit usage only on message_delta events.
				if usage, ok := event["usage"].(map[string]interface{}); ok {
					if out, ok := readTokenCount(usage, "output_tokens", "completion_tokens"); ok && out > 0 {
						sw.outputTokens = out
					}
				}
			case "message_start":
				// Anthropic includes input token usage in message_start.message.usage.
				if msg, ok := event["message"].(map[string]interface{}); ok {
					if usage, ok := msg["usage"].(map[string]interface{}); ok {
						if in, ok := readTokenCount(usage, "input_tokens", "prompt_tokens"); ok && in > 0 {
							sw.inputTokens = in
						}
						if out, ok := readTokenCount(usage, "output_tokens", "completion_tokens"); ok && out > 0 {
							sw.outputTokens = out
						}
					}
				}
			}
		}
	}
}

func (sw *streamingResponseWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// GetResponseContent returns the response content (either full response or accumulated SSE content)
func (sw *streamingResponseWriter) GetResponseContent() []byte {
	return sw.rawResponse.Bytes()
}

// GetTokenCounts returns extracted input and output token counts from stream/non-stream responses.
func (sw *streamingResponseWriter) GetTokenCounts() (int, int) {
	return sw.inputTokens, sw.outputTokens
}

// GetAssistantContent returns extracted assistant text from streaming deltas.
func (sw *streamingResponseWriter) GetAssistantContent() string {
	return sw.assistantText.String()
}

// IsTruncated reports whether response capture exceeded maxResponseBodySize.
func (sw *streamingResponseWriter) IsTruncated() bool {
	return sw.isTruncated
}
