package proxy

import (
	"net/http"
	"testing"
)

func TestExtractUpstreamRequestID(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{
			name:    "OpenAI x-request-id",
			headers: map[string]string{"X-Request-Id": "req_abc123"},
			want:    "req_abc123",
		},
		{
			name:    "Anthropic request-id",
			headers: map[string]string{"Request-Id": "req_01T5fCVnYdf9X5ZW6KayReBK"},
			want:    "req_01T5fCVnYdf9X5ZW6KayReBK",
		},
		{
			name:    "Azure apim-request-id",
			headers: map[string]string{"Apim-Request-Id": "4b66a582-bad9-cf2c-1234"},
			want:    "4b66a582-bad9-cf2c-1234",
		},
		{
			name:    "Mistral correlation ID",
			headers: map[string]string{"Mistral-Correlation-Id": "01988a0f-119a-7eac-80cb-e608f2264854"},
			want:    "01988a0f-119a-7eac-80cb-e608f2264854",
		},
		{
			name: "prefers x-request-id over others",
			headers: map[string]string{
				"X-Request-Id": "openai-id",
				"Request-Id":   "anthropic-id",
			},
			want: "openai-id",
		},
		{
			name:    "no tracking headers",
			headers: map[string]string{"Content-Type": "application/json"},
			want:    "",
		},
		{
			name:    "empty headers",
			headers: map[string]string{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := make(http.Header)
			for k, v := range tt.headers {
				h.Set(k, v)
			}
			got := extractUpstreamRequestID(h)
			if got != tt.want {
				t.Errorf("extractUpstreamRequestID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractUpstreamID(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "OpenAI chatcmpl ID",
			body: `{"id":"chatcmpl-B9MHDbslfkBeAs8l4bebGdFOJ6PeG","object":"chat.completion","choices":[]}`,
			want: "chatcmpl-B9MHDbslfkBeAs8l4bebGdFOJ6PeG",
		},
		{
			name: "Anthropic msg ID",
			body: `{"id":"msg_013Zva2CMHLNnXjNJJKqJ2EF","type":"message","role":"assistant"}`,
			want: "msg_013Zva2CMHLNnXjNJJKqJ2EF",
		},
		{
			name: "Mistral cmpl ID",
			body: `{"id":"cmpl-e5cc70bb28c444948073e77776eb30ef","object":"chat.completion"}`,
			want: "cmpl-e5cc70bb28c444948073e77776eb30ef",
		},
		{
			name: "Gemini responseId",
			body: `{"responseId":"mAitaLmkHPPlz7IPvtfUqQ4","candidates":[]}`,
			want: "mAitaLmkHPPlz7IPvtfUqQ4",
		},
		{
			name: "prefers id over responseId",
			body: `{"id":"chatcmpl-123","responseId":"gemini-456"}`,
			want: "chatcmpl-123",
		},
		{
			name: "no id field",
			body: `{"object":"chat.completion","choices":[]}`,
			want: "",
		},
		{
			name: "invalid JSON",
			body: `{not json}`,
			want: "",
		},
		{
			name: "empty body",
			body: ``,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractUpstreamID([]byte(tt.body))
			if got != tt.want {
				t.Errorf("extractUpstreamID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateRequestID(t *testing.T) {
	id1 := generateRequestID()
	id2 := generateRequestID()

	// Should have tkn_ prefix
	if len(id1) < 5 || id1[:4] != "tkn_" {
		t.Errorf("expected tkn_ prefix, got %q", id1)
	}

	// Should be unique
	if id1 == id2 {
		t.Error("expected unique IDs, got identical values")
	}

	// Should be deterministic length: "tkn_" + 32 hex chars = 36
	if len(id1) != 36 {
		t.Errorf("expected length 36, got %d: %q", len(id1), id1)
	}
}

// TestStreamingWriter_NonStreaming verifies first chunk (≤4096 B) captured correctly for JSON
func TestStreamingWriter_NonStreaming(t *testing.T) {
	w := &MockResponseWriter{}
	sw := &streamingResponseWriter{ResponseWriter: w}

	// Simulate a non-streaming JSON response
	jsonData := `{"id":"test-123","object":"text_completion","created":1234567890,"model":"gpt-4","choices":[{"text":"Hello, world!","index":0}]}`
	n, err := sw.Write([]byte(jsonData))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(jsonData) {
		t.Errorf("Expected %d bytes written, got %d", len(jsonData), n)
	}

	content := sw.GetResponseContent()
	if len(content) == 0 {
		t.Error("Expected non-empty content")
	}
	if string(content) != jsonData {
		t.Errorf("Expected %q, got %q", jsonData, string(content))
	}
}

// TestStreamingWriter_SSEAccumulates verifies text deltas are extracted while raw SSE is preserved.
func TestStreamingWriter_SSEAccumulates(t *testing.T) {
	w := &MockResponseWriter{}
	sw := &streamingResponseWriter{ResponseWriter: w}

	// Anthropic streaming format with content_block_delta
	chunk1 := `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}
`
	chunk2 := `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}
`
	chunk3 := `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}
`

	sw.Write([]byte(chunk1))
	sw.Write([]byte(chunk2))
	sw.Write([]byte(chunk3))

	if got := sw.GetAssistantContent(); got != "Hello world" {
		t.Errorf("Expected assistant content 'Hello world', got %q", got)
	}
	raw := string(sw.GetResponseContent())
	if len(raw) < 5 || raw[:5] != "data:" {
		t.Errorf("Expected raw SSE content, got %q", raw)
	}
}

// TestStreamingWriter_SizeCap verifies raw response capture is bounded.
func TestStreamingWriter_SizeCap(t *testing.T) {
	w := &MockResponseWriter{}
	sw := &streamingResponseWriter{ResponseWriter: w}

	largeText := make([]byte, maxResponseBodySize+128)
	for i := range largeText {
		largeText[i] = 'A'
	}

	sw.Write(largeText)
	if got := len(sw.GetResponseContent()); got != maxResponseBodySize {
		t.Errorf("Expected response capture to cap at %d, got %d", maxResponseBodySize, got)
	}
	if !sw.IsTruncated() {
		t.Error("Expected response capture to be marked truncated")
	}
}

// TestStreamingWriter_MultiChunkLine verifies SSE line split across Write calls
func TestStreamingWriter_MultiChunkLine(t *testing.T) {
	w := &MockResponseWriter{}
	sw := &streamingResponseWriter{ResponseWriter: w}

	// Split a complete SSE line across two Write calls
	part1 := `data: {"type":"content_block_delta","delta":{"type":"text_delta",`
	part2 := `"text":"Split line"}}
`

	sw.Write([]byte(part1))
	sw.Write([]byte(part2))

	if got := sw.GetAssistantContent(); got != "Split line" {
		t.Errorf("Expected 'Split line', got %q", got)
	}
}

func TestStreamingWriter_ExtractsUsageFromAnthropicStream(t *testing.T) {
	w := &MockResponseWriter{}
	sw := &streamingResponseWriter{ResponseWriter: w}

	chunks := []string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":11,"output_tokens":1}}}
`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}
`,
		`data: {"type":"message_delta","usage":{"output_tokens":7}}
`,
	}

	for _, chunk := range chunks {
		sw.Write([]byte(chunk))
	}

	in, out := sw.GetTokenCounts()
	if in != 11 {
		t.Fatalf("expected input tokens 11, got %d", in)
	}
	if out != 7 {
		t.Fatalf("expected output tokens 7, got %d", out)
	}
}

// TestStreamingWriter_Flush verifies Flush delegates to underlying http.Flusher
func TestStreamingWriter_Flush(t *testing.T) {
	w := &MockResponseWriter{}
	sw := &streamingResponseWriter{ResponseWriter: w}

	// Should not panic
	sw.Flush()
}

// MockResponseWriter implements http.ResponseWriter for testing
type MockResponseWriter struct {
	headers http.Header
	body    []byte
}

func (m *MockResponseWriter) Header() http.Header {
	if m.headers == nil {
		m.headers = make(http.Header)
	}
	return m.headers
}

func (m *MockResponseWriter) Write(b []byte) (int, error) {
	m.body = append(m.body, b...)
	return len(b), nil
}

func (m *MockResponseWriter) WriteHeader(statusCode int) {}

func (m *MockResponseWriter) Flush() {}
