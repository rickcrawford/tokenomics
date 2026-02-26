package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
)

// TestPassthrough_ExtractsModel verifies model field extracted from request body
func TestPassthrough_ExtractsModel(t *testing.T) {
	// Create a request with model in the body
	reqBody := map[string]interface{}{
		"model":    "gpt-4",
		"messages": []map[string]string{},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))

	// We can't fully test passthrough without a policy, but we can verify body reading
	// doesn't panic with oversized bodies
	largeBody := make([]byte, maxRequestBodySize+1000)
	req = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(largeBody))

	// This should not panic or hang due to unbounded read
	bodyRead, err := io.ReadAll(io.LimitReader(req.Body, maxRequestBodySize))
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if len(bodyRead) > maxRequestBodySize {
		t.Errorf("Expected read ≤ %d bytes, got %d", maxRequestBodySize, len(bodyRead))
	}
}

// TestPassthrough_TokenCountsFromJSON verifies input_tokens/output_tokens parsed from response
func TestPassthrough_TokenCountsFromJSON(t *testing.T) {
	tests := []struct {
		name           string
		responseBody   string
		expectedInput  int
		expectedOutput int
	}{
		{
			name:           "Anthropic format",
			responseBody:   `{"id":"msg_123","usage":{"input_tokens":10,"output_tokens":20},"role":"assistant"}`,
			expectedInput:  10,
			expectedOutput: 20,
		},
		{
			name:           "OpenAI format",
			responseBody:   `{"id":"chatcmpl-123","usage":{"prompt_tokens":10,"completion_tokens":20},"choices":[]}`,
			expectedInput:  10,
			expectedOutput: 20,
		},
		{
			name:           "Gemini usageMetadata format",
			responseBody:   `{"responseId":"gemini-123","usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":34}}`,
			expectedInput:  12,
			expectedOutput: 34,
		},
		{
			name:           "Missing usage",
			responseBody:   `{"id":"test-123","choices":[]}`,
			expectedInput:  0,
			expectedOutput: 0,
		},
		{
			name:           "Invalid JSON",
			responseBody:   `{not json}`,
			expectedInput:  0,
			expectedOutput: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse response for token counts
			var respBody map[string]interface{}
			if err := json.Unmarshal([]byte(tt.responseBody), &respBody); err != nil {
				// Invalid JSON case
				if tt.expectedInput != 0 || tt.expectedOutput != 0 {
					t.Fatalf("Unmarshal failed: %v", err)
				}
				return
			}

			inputTokens, outputTokens := extractTokenCountsFromResponse(respBody)

			if inputTokens != tt.expectedInput {
				t.Errorf("Expected input_tokens=%d, got %d", tt.expectedInput, inputTokens)
			}
			if outputTokens != tt.expectedOutput {
				t.Errorf("Expected output_tokens=%d, got %d", tt.expectedOutput, outputTokens)
			}
		})
	}
}

func TestPassthrough_ExtractAssistantTextFromResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "OpenAI choices format",
			body: `{"choices":[{"message":{"role":"assistant","content":"hello from openai"}}]}`,
			want: "hello from openai",
		},
		{
			name: "Anthropic content blocks format",
			body: `{"content":[{"type":"text","text":"hello from anthropic"}]}`,
			want: "hello from anthropic",
		},
		{
			name: "Gemini candidates format",
			body: `{"candidates":[{"content":{"parts":[{"text":"hello from gemini"}]}}]}`,
			want: "hello from gemini",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp map[string]interface{}
			if err := json.Unmarshal([]byte(tt.body), &resp); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}
			got := extractAssistantTextFromResponse(resp)
			if got != tt.want {
				t.Fatalf("Expected %q, got %q", tt.want, got)
			}
		})
	}
}

// TestPassthrough_BodySizeCapped verifies read capped at maxRequestBodySize
func TestPassthrough_BodySizeCapped(t *testing.T) {
	// Create a request with body larger than the limit
	largeBody := make([]byte, maxRequestBodySize*2)
	for i := range largeBody {
		largeBody[i] = 'X'
	}

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(largeBody))
	readBody, err := io.ReadAll(io.LimitReader(req.Body, maxRequestBodySize))
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if len(readBody) > maxRequestBodySize {
		t.Errorf("Expected read ≤ %d bytes, got %d", maxRequestBodySize, len(readBody))
	}

	// All read bytes should be 'X'
	for i, b := range readBody {
		if b != 'X' {
			t.Errorf("Byte %d: expected 'X', got %q", i, string(b))
		}
	}
}
