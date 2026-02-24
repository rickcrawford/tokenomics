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
