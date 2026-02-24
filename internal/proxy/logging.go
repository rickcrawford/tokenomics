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
