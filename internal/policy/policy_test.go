package policy

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "valid minimal policy",
			input:   `{"base_key_env":"MY_KEY"}`,
			wantErr: "",
		},
		{
			name:    "valid policy with all fields",
			input:   `{"base_key_env":"MY_KEY","upstream_url":"https://api.example.com","max_tokens":1000,"model":"gpt-4","prompts":[{"role":"system","content":"be helpful"}]}`,
			wantErr: "",
		},
		{
			name:    "valid policy with model_regex",
			input:   `{"base_key_env":"MY_KEY","model_regex":"^gpt-4.*$"}`,
			wantErr: "",
		},
		{
			name:    "valid policy with rules",
			input:   `{"base_key_env":"MY_KEY","rules":["(?i)secret","badword"]}`,
			wantErr: "",
		},
		{
			name:    "valid with providers instead of global base_key_env",
			input:   `{"providers":{"openai":[{"base_key_env":"OPENAI_KEY","model":"gpt-4"}]}}`,
			wantErr: "",
		},
		{
			name:    "invalid JSON",
			input:   `{not json}`,
			wantErr: "invalid policy JSON",
		},
		{
			name:    "empty JSON object missing base_key_env and providers",
			input:   `{}`,
			wantErr: "base_key_env is required",
		},
		{
			name:    "missing base_key_env with other fields but no providers",
			input:   `{"model":"gpt-4"}`,
			wantErr: "base_key_env is required",
		},
		{
			name:    "bad model_regex",
			input:   `{"base_key_env":"K","model_regex":"[invalid"}`,
			wantErr: "invalid model_regex",
		},
		{
			name:    "bad rule regex",
			input:   `{"base_key_env":"K","rules":["good","[bad"]}`,
			wantErr: "invalid rule regex",
		},
		{
			name:    "empty string input",
			input:   ``,
			wantErr: "invalid policy JSON",
		},
		{
			name:    "provider policy missing base_key_env",
			input:   `{"providers":{"openai":[{"model":"gpt-4"}]}}`,
			wantErr: "base_key_env is required",
		},
		{
			name:    "provider policy with bad model_regex",
			input:   `{"providers":{"openai":[{"base_key_env":"K","model_regex":"[bad"}]}}`,
			wantErr: "invalid model_regex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				if p != nil {
					t.Fatalf("expected nil policy on error, got %+v", p)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p == nil {
				t.Fatal("expected non-nil policy")
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		policy  Policy
		wantErr string
	}{
		{
			name:    "valid policy with global base_key_env",
			policy:  Policy{BaseKeyEnv: "MY_KEY"},
			wantErr: "",
		},
		{
			name: "valid policy with providers only",
			policy: Policy{
				Providers: map[string][]*ProviderPolicy{
					"openai": {{BaseKeyEnv: "OPENAI_KEY"}},
				},
			},
			wantErr: "",
		},
		{
			name:    "missing both base_key_env and providers",
			policy:  Policy{},
			wantErr: "base_key_env is required",
		},
		{
			name:    "valid model regex compiles",
			policy:  Policy{BaseKeyEnv: "K", ModelRegex: "^gpt-[34]"},
			wantErr: "",
		},
		{
			name:    "invalid model regex",
			policy:  Policy{BaseKeyEnv: "K", ModelRegex: "(unclosed"},
			wantErr: "invalid model_regex",
		},
		{
			name:    "valid rules compile",
			policy:  Policy{BaseKeyEnv: "K", Rules: []string{"foo", "bar.*baz"}},
			wantErr: "",
		},
		{
			name:    "invalid rule in list",
			policy:  Policy{BaseKeyEnv: "K", Rules: []string{"good", "[bad"}},
			wantErr: "invalid rule regex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
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

func TestValidate_CompilesRegexes(t *testing.T) {
	p := Policy{
		BaseKeyEnv: "K",
		ModelRegex: "^gpt-4.*$",
		Rules:      []string{"secret", "password"},
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.CompiledModelRegex() == nil {
		t.Fatal("expected compiledModelRegex to be set after Validate")
	}
	if !p.CompiledModelRegex().MatchString("gpt-4-turbo") {
		t.Fatal("expected compiledModelRegex to match gpt-4-turbo")
	}

	rules := p.CompiledRules()
	if len(rules) != 2 {
		t.Fatalf("expected 2 compiled rules, got %d", len(rules))
	}
	if !rules[0].MatchString("this is a secret") {
		t.Fatal("expected first rule to match 'this is a secret'")
	}
}

func TestJSON(t *testing.T) {
	p := Policy{
		BaseKeyEnv: "MY_KEY",
		Model:      "gpt-4",
		MaxTokens:  500,
	}
	j := p.JSON()
	if !strings.Contains(j, `"base_key_env":"MY_KEY"`) {
		t.Fatalf("JSON output missing base_key_env: %s", j)
	}
	if !strings.Contains(j, `"model":"gpt-4"`) {
		t.Fatalf("JSON output missing model: %s", j)
	}
	if !strings.Contains(j, `"max_tokens":500`) {
		t.Fatalf("JSON output missing max_tokens: %s", j)
	}
}

func TestParse_PreservesFields(t *testing.T) {
	input := `{"base_key_env":"KEY","upstream_url":"https://api.test.com","max_tokens":2000,"model":"claude-3","model_regex":"^claude","prompts":[{"role":"system","content":"hello"}],"rules":["blocked"]}`
	p, err := Parse(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.BaseKeyEnv != "KEY" {
		t.Errorf("BaseKeyEnv = %q, want %q", p.BaseKeyEnv, "KEY")
	}
	if p.UpstreamURL != "https://api.test.com" {
		t.Errorf("UpstreamURL = %q, want %q", p.UpstreamURL, "https://api.test.com")
	}
	if p.MaxTokens != 2000 {
		t.Errorf("MaxTokens = %d, want %d", p.MaxTokens, 2000)
	}
	if p.Model != "claude-3" {
		t.Errorf("Model = %q, want %q", p.Model, "claude-3")
	}
	if p.ModelRegex != "^claude" {
		t.Errorf("ModelRegex = %q, want %q", p.ModelRegex, "^claude")
	}
	if len(p.Prompts) != 1 {
		t.Fatalf("len(Prompts) = %d, want 1", len(p.Prompts))
	}
	if p.Prompts[0].Role != "system" || p.Prompts[0].Content != "hello" {
		t.Errorf("Prompts[0] = %+v, want role=system content=hello", p.Prompts[0])
	}
	if len(p.Rules) != 1 || p.Rules[0] != "blocked" {
		t.Errorf("Rules = %v, want [blocked]", p.Rules)
	}
}

func TestResolveForModel(t *testing.T) {
	tests := []struct {
		name            string
		policyJSON      string
		model           string
		wantBaseKeyEnv  string
		wantUpstreamURL string
		wantMaxTokens   int64
	}{
		{
			name:           "global only, no providers",
			policyJSON:     `{"base_key_env":"GLOBAL_KEY","upstream_url":"https://global.api","max_tokens":1000}`,
			model:          "gpt-4",
			wantBaseKeyEnv: "GLOBAL_KEY",
			wantMaxTokens:  1000,
		},
		{
			name: "provider overrides global base_key_env",
			policyJSON: `{
				"base_key_env": "GLOBAL_KEY",
				"providers": {
					"openai": [{"base_key_env": "OPENAI_KEY", "model": "gpt-4"}]
				}
			}`,
			model:          "gpt-4",
			wantBaseKeyEnv: "OPENAI_KEY",
		},
		{
			name: "provider overrides global upstream_url",
			policyJSON: `{
				"base_key_env": "GLOBAL_KEY",
				"upstream_url": "https://global.api",
				"providers": {
					"openai": [{"base_key_env": "OPENAI_KEY", "model": "gpt-4", "upstream_url": "https://openai.api"}]
				}
			}`,
			model:           "gpt-4",
			wantBaseKeyEnv:  "OPENAI_KEY",
			wantUpstreamURL: "https://openai.api",
		},
		{
			name: "model regex matching",
			policyJSON: `{
				"base_key_env": "GLOBAL_KEY",
				"providers": {
					"openai": [{"base_key_env": "OPENAI_KEY", "model_regex": "^gpt-4"}]
				}
			}`,
			model:          "gpt-4-turbo",
			wantBaseKeyEnv: "OPENAI_KEY",
		},
		{
			name: "no provider matches, falls back to global",
			policyJSON: `{
				"base_key_env": "GLOBAL_KEY",
				"providers": {
					"openai": [{"base_key_env": "OPENAI_KEY", "model": "gpt-4"}]
				}
			}`,
			model:          "claude-3",
			wantBaseKeyEnv: "GLOBAL_KEY",
		},
		{
			name: "multiple providers, second provider matches",
			policyJSON: `{
				"base_key_env": "GLOBAL_KEY",
				"providers": {
					"openai": [{"base_key_env": "OPENAI_KEY", "model": "gpt-4"}],
					"anthropic": [{"base_key_env": "ANTHROPIC_KEY", "model": "claude-3"}]
				}
			}`,
			model:          "claude-3",
			wantBaseKeyEnv: "ANTHROPIC_KEY",
		},
		{
			name: "multiple policies per provider, second matches",
			policyJSON: `{
				"base_key_env": "GLOBAL_KEY",
				"providers": {
					"openai": [
						{"base_key_env": "OPENAI_KEY_GPT4", "model": "gpt-4"},
						{"base_key_env": "OPENAI_KEY_GPT3", "model_regex": "^gpt-3"}
					]
				}
			}`,
			model:          "gpt-3.5-turbo",
			wantBaseKeyEnv: "OPENAI_KEY_GPT3",
		},
		{
			name: "provider with no model constraint matches everything",
			policyJSON: `{
				"base_key_env": "GLOBAL_KEY",
				"providers": {
					"openai": [{"base_key_env": "OPENAI_KEY"}]
				}
			}`,
			model:          "any-model",
			wantBaseKeyEnv: "OPENAI_KEY",
		},
		{
			name: "provider max_tokens overrides global",
			policyJSON: `{
				"base_key_env": "GLOBAL_KEY",
				"max_tokens": 5000,
				"providers": {
					"openai": [{"base_key_env": "OPENAI_KEY", "model": "gpt-4", "max_tokens": 1000}]
				}
			}`,
			model:          "gpt-4",
			wantBaseKeyEnv: "OPENAI_KEY",
			wantMaxTokens:  1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Parse(tt.policyJSON)
			if err != nil {
				t.Fatalf("failed to parse policy: %v", err)
			}

			resolved := p.ResolveForModel(tt.model)
			if resolved.BaseKeyEnv != tt.wantBaseKeyEnv {
				t.Errorf("BaseKeyEnv = %q, want %q", resolved.BaseKeyEnv, tt.wantBaseKeyEnv)
			}
			if tt.wantUpstreamURL != "" && resolved.UpstreamURL != tt.wantUpstreamURL {
				t.Errorf("UpstreamURL = %q, want %q", resolved.UpstreamURL, tt.wantUpstreamURL)
			}
			if tt.wantMaxTokens > 0 && resolved.MaxTokens != tt.wantMaxTokens {
				t.Errorf("MaxTokens = %d, want %d", resolved.MaxTokens, tt.wantMaxTokens)
			}
		})
	}
}

func TestResolveForModel_PromptMerging(t *testing.T) {
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

	if len(resolved.Prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(resolved.Prompts))
	}
	// Provider prompts come first
	if resolved.Prompts[0].Content != "provider prompt" {
		t.Errorf("first prompt = %q, want %q", resolved.Prompts[0].Content, "provider prompt")
	}
	if resolved.Prompts[1].Content != "global prompt" {
		t.Errorf("second prompt = %q, want %q", resolved.Prompts[1].Content, "global prompt")
	}
}

func TestResolveForModel_RuleMerging(t *testing.T) {
	policyJSON := `{
		"base_key_env": "GLOBAL_KEY",
		"rules": ["global_blocked"],
		"providers": {
			"openai": [{
				"base_key_env": "OPENAI_KEY",
				"model": "gpt-4",
				"rules": ["provider_blocked"]
			}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	resolved := p.ResolveForModel("gpt-4")

	// Global + provider rules
	if len(resolved.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d: %v", len(resolved.Rules), resolved.Rules)
	}
	if resolved.Rules[0] != "global_blocked" {
		t.Errorf("first rule = %q, want %q", resolved.Rules[0], "global_blocked")
	}
	if resolved.Rules[1] != "provider_blocked" {
		t.Errorf("second rule = %q, want %q", resolved.Rules[1], "provider_blocked")
	}
}

func TestResolveProvider(t *testing.T) {
	policyJSON := `{
		"base_key_env": "GLOBAL_KEY",
		"upstream_url": "https://global.api",
		"providers": {
			"openai": [{"base_key_env": "OPENAI_KEY", "upstream_url": "https://openai.api"}],
			"anthropic": [{"base_key_env": "ANTHROPIC_KEY"}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	t.Run("empty provider returns global", func(t *testing.T) {
		resolved := p.ResolveProvider("")
		if resolved.BaseKeyEnv != "GLOBAL_KEY" {
			t.Errorf("BaseKeyEnv = %q, want %q", resolved.BaseKeyEnv, "GLOBAL_KEY")
		}
	})

	t.Run("named provider overrides", func(t *testing.T) {
		resolved := p.ResolveProvider("openai")
		if resolved.BaseKeyEnv != "OPENAI_KEY" {
			t.Errorf("BaseKeyEnv = %q, want %q", resolved.BaseKeyEnv, "OPENAI_KEY")
		}
		if resolved.UpstreamURL != "https://openai.api" {
			t.Errorf("UpstreamURL = %q, want %q", resolved.UpstreamURL, "https://openai.api")
		}
	})

	t.Run("unknown provider returns global", func(t *testing.T) {
		resolved := p.ResolveProvider("gemini")
		if resolved.BaseKeyEnv != "GLOBAL_KEY" {
			t.Errorf("BaseKeyEnv = %q, want %q", resolved.BaseKeyEnv, "GLOBAL_KEY")
		}
	})
}

func TestProviderNames(t *testing.T) {
	policyJSON := `{
		"base_key_env": "K",
		"providers": {
			"openai": [{"base_key_env": "OK"}],
			"anthropic": [{"base_key_env": "AK"}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	names := p.ProviderNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(names))
	}

	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["openai"] {
		t.Error("missing provider: openai")
	}
	if !nameSet["anthropic"] {
		t.Error("missing provider: anthropic")
	}
}

func TestMemoryConfig(t *testing.T) {
	policyJSON := `{
		"base_key_env": "K",
		"memory": {"enabled": true, "file_path": "/tmp/sessions.md"}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if !p.Memory.Enabled {
		t.Error("expected memory to be enabled")
	}
	if p.Memory.FilePath != "/tmp/sessions.md" {
		t.Errorf("FilePath = %q, want %q", p.Memory.FilePath, "/tmp/sessions.md")
	}

	// Verify memory config propagates through resolution
	resolved := p.ResolveForModel("any-model")
	if !resolved.Memory.Enabled {
		t.Error("expected resolved memory to be enabled")
	}
	if resolved.Memory.FilePath != "/tmp/sessions.md" {
		t.Errorf("resolved FilePath = %q, want %q", resolved.Memory.FilePath, "/tmp/sessions.md")
	}
}

func TestMemoryConfig_Redis(t *testing.T) {
	policyJSON := `{
		"base_key_env": "K",
		"memory": {"enabled": true, "redis": true}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if !p.Memory.Enabled {
		t.Error("expected memory to be enabled")
	}
	if !p.Memory.Redis {
		t.Error("expected memory redis to be true")
	}
}
