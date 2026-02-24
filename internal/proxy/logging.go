package proxy

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
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

	// Upstream provider tracking IDs
	UpstreamRequestID string `json:"upstream_request_id,omitempty"` // Provider's request ID from response header
	UpstreamID        string `json:"upstream_id,omitempty"`         // Provider's response body ID (chatcmpl-*, msg_*, etc.)
	ClientRequestID   string `json:"client_request_id,omitempty"`   // Our outbound tracking ID sent to provider
}

// extractUpstreamRequestID pulls the provider's request ID from response headers.
// Supports OpenAI (x-request-id), Anthropic (request-id), Azure (apim-request-id),
// and Mistral (Mistral-Correlation-Id).
func extractUpstreamRequestID(h http.Header) string {
	// Try provider-specific headers in priority order
	for _, key := range []string{
		"X-Request-Id",            // OpenAI, Azure OpenAI
		"Request-Id",              // Anthropic
		"Apim-Request-Id",         // Azure API Management
		"Mistral-Correlation-Id",  // Mistral
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
		log.Printf("error marshaling log entry: %v", err)
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

// requestStartTime returns the current time for latency tracking.
func requestStartTime() time.Time {
	return time.Now()
}
