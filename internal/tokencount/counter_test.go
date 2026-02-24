package tokencount

import (
	"strings"
	"testing"
)

// tryCount calls Count and skips the test if the tiktoken encoding
// cannot be fetched (e.g., network is unavailable in CI).
func tryCount(t *testing.T, model, text string) int {
	t.Helper()
	n, err := Count(model, text)
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") ||
			strings.Contains(err.Error(), "no such host") ||
			strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "Get \"https://") {
			t.Skipf("skipping: tiktoken encoding not available (network): %v", err)
		}
		t.Fatalf("Count(%q, %q): %v", model, text, err)
	}
	return n
}

// tryCountMessages calls CountMessages and skips the test if the tiktoken
// encoding cannot be fetched.
func tryCountMessages(t *testing.T, model string, msgs []map[string]interface{}) int {
	t.Helper()
	n, err := CountMessages(model, msgs)
	if err != nil {
		if strings.Contains(err.Error(), "Forbidden") ||
			strings.Contains(err.Error(), "no such host") ||
			strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "Get \"https://") {
			t.Skipf("skipping: tiktoken encoding not available (network): %v", err)
		}
		t.Fatalf("CountMessages(%q, ...): %v", model, err)
	}
	return n
}

func TestCount(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		text    string
		wantMin int // minimum expected tokens (tokenization is model-specific)
		wantMax int // maximum expected tokens
	}{
		{
			name:    "empty string returns zero",
			model:   "gpt-4",
			text:    "",
			wantMin: 0,
			wantMax: 0,
		},
		{
			name:    "single word",
			model:   "gpt-4",
			text:    "hello",
			wantMin: 1,
			wantMax: 1,
		},
		{
			name:    "simple sentence",
			model:   "gpt-4",
			text:    "The quick brown fox jumps over the lazy dog.",
			wantMin: 8,
			wantMax: 12,
		},
		{
			name:    "unknown model falls back to cl100k_base",
			model:   "unknown-model-xyz",
			text:    "hello world",
			wantMin: 1,
			wantMax: 5,
		},
		{
			name:    "whitespace only",
			model:   "gpt-4",
			text:    "   ",
			wantMin: 1,
			wantMax: 3,
		},
		{
			name:    "longer text produces more tokens",
			model:   "gpt-4",
			text:    "This is a longer piece of text that should produce a reasonable number of tokens for testing purposes. It includes multiple sentences and various words.",
			wantMin: 20,
			wantMax: 40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tryCount(t, tt.model, tt.text)
			if got < tt.wantMin || got > tt.wantMax {
				t.Fatalf("Count(%q, %q) = %d, want in range [%d, %d]",
					tt.model, tt.text, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCount_Deterministic(t *testing.T) {
	text := "The same text should always produce the same token count."
	model := "gpt-4"

	count1 := tryCount(t, model, text)
	count2 := tryCount(t, model, text)

	if count1 != count2 {
		t.Fatalf("Count is not deterministic: %d vs %d", count1, count2)
	}
}

func TestCountMessages(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		messages     []map[string]interface{}
		wantMin      int
		wantMax      int
		needsNetwork bool // whether this test case calls Count internally
	}{
		{
			name:         "empty messages list",
			model:        "gpt-4",
			messages:     []map[string]interface{}{},
			wantMin:      3, // reply priming tokens only
			wantMax:      3,
			needsNetwork: false,
		},
		{
			name:  "single user message",
			model: "gpt-4",
			messages: []map[string]interface{}{
				{"role": "user", "content": "hello"},
			},
			wantMin:      5,
			wantMax:      12,
			needsNetwork: true,
		},
		{
			name:  "multiple messages",
			model: "gpt-4",
			messages: []map[string]interface{}{
				{"role": "system", "content": "You are a helpful assistant."},
				{"role": "user", "content": "What is Go?"},
				{"role": "assistant", "content": "Go is a programming language."},
			},
			wantMin:      20,
			wantMax:      40,
			needsNetwork: true,
		},
		{
			name:  "message with non-string values ignored for token counting",
			model: "gpt-4",
			messages: []map[string]interface{}{
				{"role": "user", "content": "hello", "temperature": 0.7},
			},
			wantMin:      5,
			wantMax:      12,
			needsNetwork: true,
		},
		{
			name:         "nil messages",
			model:        "gpt-4",
			messages:     nil,
			wantMin:      3,
			wantMax:      3,
			needsNetwork: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got int
			if tt.needsNetwork {
				got = tryCountMessages(t, tt.model, tt.messages)
			} else {
				var err error
				got, err = CountMessages(tt.model, tt.messages)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if got < tt.wantMin || got > tt.wantMax {
				t.Fatalf("CountMessages(%q, ...) = %d, want in range [%d, %d]",
					tt.model, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCountMessages_IncludesOverhead(t *testing.T) {
	model := "gpt-4"

	textCount := tryCount(t, model, "hello")
	roleCount := tryCount(t, model, "user")

	msgCount := tryCountMessages(t, model, []map[string]interface{}{
		{"role": "user", "content": "hello"},
	})

	// msgCount should be at least textCount + roleCount + overhead (3 per message + 3 reply)
	minExpected := textCount + roleCount + 3 + 3
	if msgCount < minExpected {
		t.Fatalf("CountMessages = %d, expected at least %d (text=%d + role=%d + overhead=6)",
			msgCount, minExpected, textCount, roleCount)
	}
}
