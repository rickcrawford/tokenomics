package policy

import (
	"strings"
	"testing"
)

func TestCheckModel(t *testing.T) {
	tests := []struct {
		name       string
		policyJSON string
		model      string
		wantErr    string
	}{
		{
			name:       "no model restriction allows anything",
			policyJSON: `{"base_key_env":"K"}`,
			model:      "gpt-4",
			wantErr:    "",
		},
		{
			name:       "exact model match passes",
			policyJSON: `{"base_key_env":"K","model":"gpt-4"}`,
			model:      "gpt-4",
			wantErr:    "",
		},
		{
			name:       "exact model mismatch fails",
			policyJSON: `{"base_key_env":"K","model":"gpt-4"}`,
			model:      "gpt-3.5-turbo",
			wantErr:    "not allowed",
		},
		{
			name:       "regex match passes",
			policyJSON: `{"base_key_env":"K","model_regex":"^gpt-4"}`,
			model:      "gpt-4-turbo",
			wantErr:    "",
		},
		{
			name:       "regex no match fails",
			policyJSON: `{"base_key_env":"K","model_regex":"^gpt-4"}`,
			model:      "claude-3",
			wantErr:    "does not match policy regex",
		},
		{
			name:       "exact match plus regex both satisfied",
			policyJSON: `{"base_key_env":"K","model":"gpt-4-turbo","model_regex":"^gpt-4"}`,
			model:      "gpt-4-turbo",
			wantErr:    "",
		},
		{
			name:       "exact match fails even if regex would pass",
			policyJSON: `{"base_key_env":"K","model":"gpt-4","model_regex":"^gpt"}`,
			model:      "gpt-4-turbo",
			wantErr:    "not allowed",
		},
		{
			name:       "empty model in request with exact model policy",
			policyJSON: `{"base_key_env":"K","model":"gpt-4"}`,
			model:      "",
			wantErr:    "not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.policyJSON)
			if err != nil {
				t.Fatalf("failed to parse policy: %v", err)
			}

			err = p.CheckModel(tt.model)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCheckModel_ResolvedPolicy(t *testing.T) {
	policyJSON := `{
		"base_key_env": "GLOBAL_KEY",
		"providers": {
			"openai": [{"base_key_env": "OPENAI_KEY", "model": "gpt-4"}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	resolved := p.ResolveForModel("gpt-4")
	if err := resolved.CheckModel("gpt-4"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := resolved.CheckModel("gpt-3.5"); err == nil {
		t.Fatal("expected error for mismatched model")
	}
}

func TestCheckRules(t *testing.T) {
	tests := []struct {
		name       string
		policyJSON string
		content    string
		wantErr    string
	}{
		{
			name:       "no rules allows everything",
			policyJSON: `{"base_key_env":"K"}`,
			content:    "anything goes",
			wantErr:    "",
		},
		{
			name:       "content does not match any rule",
			policyJSON: `{"base_key_env":"K","rules":["secret","password"]}`,
			content:    "hello world",
			wantErr:    "",
		},
		{
			name:       "content matches first rule",
			policyJSON: `{"base_key_env":"K","rules":["secret","password"]}`,
			content:    "tell me the secret",
			wantErr:    "blocked by rule 0",
		},
		{
			name:       "content matches second rule",
			policyJSON: `{"base_key_env":"K","rules":["secret","password"]}`,
			content:    "what is your password",
			wantErr:    "blocked by rule 1",
		},
		{
			name:       "case-insensitive rule match",
			policyJSON: `{"base_key_env":"K","rules":["(?i)forbidden"]}`,
			content:    "This is FORBIDDEN content",
			wantErr:    "blocked by rule 0",
		},
		{
			name:       "regex rule with wildcard",
			policyJSON: `{"base_key_env":"K","rules":["drop\\s+table"]}`,
			content:    "please drop table users",
			wantErr:    "blocked by rule 0",
		},
		{
			name:       "empty content does not match rules",
			policyJSON: `{"base_key_env":"K","rules":["nonempty"]}`,
			content:    "",
			wantErr:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.policyJSON)
			if err != nil {
				t.Fatalf("failed to parse policy: %v", err)
			}

			err = p.CheckRules(tt.content)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCheckRules_ResolvedMergedRules(t *testing.T) {
	policyJSON := `{
		"base_key_env": "GLOBAL_KEY",
		"rules": ["global_secret"],
		"providers": {
			"openai": [{
				"base_key_env": "OPENAI_KEY",
				"model": "gpt-4",
				"rules": ["provider_secret"]
			}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	resolved := p.ResolveForModel("gpt-4")

	// Should block global rule
	if err := resolved.CheckRules("contains global_secret"); err == nil {
		t.Fatal("expected error for global rule match")
	}

	// Should block provider rule
	if err := resolved.CheckRules("contains provider_secret"); err == nil {
		t.Fatal("expected error for provider rule match")
	}

	// Should allow clean content
	if err := resolved.CheckRules("clean content"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInjectPrompts(t *testing.T) {
	tests := []struct {
		name        string
		policyJSON  string
		messages    []map[string]interface{}
		wantLen     int
		wantFirst   string
		wantContent string
	}{
		{
			name:       "no prompts returns original messages",
			policyJSON: `{"base_key_env":"K"}`,
			messages: []map[string]interface{}{
				{"role": "user", "content": "hello"},
			},
			wantLen:   1,
			wantFirst: "user",
		},
		{
			name:       "single prompt injected before messages",
			policyJSON: `{"base_key_env":"K","prompts":[{"role":"system","content":"be helpful"}]}`,
			messages: []map[string]interface{}{
				{"role": "user", "content": "hello"},
			},
			wantLen:     2,
			wantFirst:   "system",
			wantContent: "be helpful",
		},
		{
			name:       "multiple prompts injected in order",
			policyJSON: `{"base_key_env":"K","prompts":[{"role":"system","content":"first"},{"role":"system","content":"second"}]}`,
			messages: []map[string]interface{}{
				{"role": "user", "content": "hello"},
			},
			wantLen:     3,
			wantFirst:   "system",
			wantContent: "first",
		},
		{
			name:       "empty messages with prompts",
			policyJSON: `{"base_key_env":"K","prompts":[{"role":"system","content":"only prompt"}]}`,
			messages:   []map[string]interface{}{},
			wantLen:    1,
			wantFirst:  "system",
		},
		{
			name:       "nil messages with no prompts",
			policyJSON: `{"base_key_env":"K"}`,
			messages:   nil,
			wantLen:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.policyJSON)
			if err != nil {
				t.Fatalf("failed to parse policy: %v", err)
			}

			result := p.InjectPrompts(tt.messages)

			if len(result) != tt.wantLen {
				t.Fatalf("len(result) = %d, want %d", len(result), tt.wantLen)
			}

			if tt.wantLen > 0 && tt.wantFirst != "" {
				if role, ok := result[0]["role"].(string); !ok || role != tt.wantFirst {
					t.Errorf("first message role = %v, want %q", result[0]["role"], tt.wantFirst)
				}
			}

			if tt.wantContent != "" && tt.wantLen > 0 {
				if content, ok := result[0]["content"].(string); !ok || content != tt.wantContent {
					t.Errorf("first message content = %v, want %q", result[0]["content"], tt.wantContent)
				}
			}
		})
	}
}

func TestInjectPrompts_DoesNotMutateOriginal(t *testing.T) {
	p, err := Parse(`{"base_key_env":"K","prompts":[{"role":"system","content":"injected"}]}`)
	if err != nil {
		t.Fatalf("failed to parse policy: %v", err)
	}

	original := []map[string]interface{}{
		{"role": "user", "content": "hello"},
	}

	result := p.InjectPrompts(original)

	if len(original) != 1 {
		t.Fatalf("original was mutated: len = %d, want 1", len(original))
	}
	if len(result) != 2 {
		t.Fatalf("result len = %d, want 2", len(result))
	}
}

func TestInjectPrompts_ResolvedMergedPrompts(t *testing.T) {
	policyJSON := `{
		"base_key_env": "GLOBAL_KEY",
		"prompts": [{"role": "system", "content": "global prompt"}],
		"providers": {
			"openai": [{
				"base_key_env": "OPENAI_KEY",
				"model": "gpt-4",
				"prompts": [{"role": "system", "content": "provider prompt"}]
			}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	resolved := p.ResolveForModel("gpt-4")
	messages := []map[string]interface{}{
		{"role": "user", "content": "hello"},
	}

	result := resolved.InjectPrompts(messages)

	// Provider prompts prepend before global, then user messages
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	if result[0]["content"] != "provider prompt" {
		t.Errorf("first = %q, want %q", result[0]["content"], "provider prompt")
	}
	if result[1]["content"] != "global prompt" {
		t.Errorf("second = %q, want %q", result[1]["content"], "global prompt")
	}
	if result[2]["content"] != "hello" {
		t.Errorf("third = %q, want %q", result[2]["content"], "hello")
	}
}
