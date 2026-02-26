package policy

import (
	"testing"
)

// TestResolveForModel_MultiProviderRouting verifies that the proxy routes
// to the correct provider based on the requested model name.
func TestResolveForModel_MultiProviderRouting(t *testing.T) {
	policyJSON := `{
		"base_key_env": "DEFAULT_KEY",
		"upstream_url": "https://default.api",
		"max_tokens": 100000,
		"providers": {
			"openai": [
				{"base_key_env": "OPENAI_KEY", "model": "gpt-4o", "max_tokens": 50000},
				{"base_key_env": "OPENAI_KEY", "model_regex": "^gpt-3\\.5", "max_tokens": 200000}
			],
			"anthropic": [
				{"base_key_env": "ANTHROPIC_KEY", "upstream_url": "https://api.anthropic.com", "model_regex": "^claude"}
			],
			"groq": [
				{"base_key_env": "GROQ_KEY", "model_regex": "^llama"}
			]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	tests := []struct {
		name             string
		model            string
		wantProvider     string
		wantBaseKeyEnv   string
		wantUpstreamURL  string
		wantMaxTokens    int64
	}{
		{
			name:           "exact model match routes to openai",
			model:          "gpt-4o",
			wantProvider:   "openai",
			wantBaseKeyEnv: "OPENAI_KEY",
			wantMaxTokens:  50000,
		},
		{
			name:           "regex match routes to openai gpt-3.5",
			model:          "gpt-3.5-turbo",
			wantProvider:   "openai",
			wantBaseKeyEnv: "OPENAI_KEY",
			wantMaxTokens:  200000,
		},
		{
			name:            "regex match routes to anthropic",
			model:           "claude-3-opus",
			wantProvider:    "anthropic",
			wantBaseKeyEnv:  "ANTHROPIC_KEY",
			wantUpstreamURL: "https://api.anthropic.com",
		},
		{
			name:           "regex match routes to anthropic sonnet",
			model:          "claude-3-sonnet",
			wantProvider:   "anthropic",
			wantBaseKeyEnv: "ANTHROPIC_KEY",
		},
		{
			name:           "regex match routes to groq",
			model:          "llama-3-70b",
			wantProvider:   "groq",
			wantBaseKeyEnv: "GROQ_KEY",
		},
		{
			name:           "unmatched model falls back to global",
			model:          "mistral-large",
			wantProvider:   "",
			wantBaseKeyEnv: "DEFAULT_KEY",
			wantMaxTokens:  100000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := p.ResolveForModel(tt.model)

			if resolved.ProviderName != tt.wantProvider {
				t.Errorf("ProviderName = %q, want %q", resolved.ProviderName, tt.wantProvider)
			}
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

// TestResolveForModel_WildcardProvider verifies that a provider policy
// with no model constraint matches any model.
func TestResolveForModel_WildcardProvider(t *testing.T) {
	policyJSON := `{
		"base_key_env": "DEFAULT_KEY",
		"providers": {
			"openai": [
				{"base_key_env": "OPENAI_KEY", "model": "gpt-4o"},
				{"base_key_env": "OPENAI_FALLBACK_KEY"}
			]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Exact match takes the first policy
	resolved := p.ResolveForModel("gpt-4o")
	if resolved.BaseKeyEnv != "OPENAI_KEY" {
		t.Errorf("gpt-4o: BaseKeyEnv = %q, want OPENAI_KEY", resolved.BaseKeyEnv)
	}

	// Non-matching model hits the wildcard (second policy)
	resolved = p.ResolveForModel("any-model")
	if resolved.BaseKeyEnv != "OPENAI_FALLBACK_KEY" {
		t.Errorf("any-model: BaseKeyEnv = %q, want OPENAI_FALLBACK_KEY", resolved.BaseKeyEnv)
	}
}

// TestResolveForModel_FirstMatchWins verifies that the first matching
// provider policy is used when multiple could match.
func TestResolveForModel_FirstMatchWins(t *testing.T) {
	policyJSON := `{
		"base_key_env": "DEFAULT_KEY",
		"providers": {
			"openai": [
				{"base_key_env": "SPECIFIC_KEY", "model": "gpt-4o"},
				{"base_key_env": "BROAD_KEY", "model_regex": "^gpt-"}
			]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// gpt-4o matches both policies, but first (exact) wins
	resolved := p.ResolveForModel("gpt-4o")
	if resolved.BaseKeyEnv != "SPECIFIC_KEY" {
		t.Errorf("BaseKeyEnv = %q, want SPECIFIC_KEY", resolved.BaseKeyEnv)
	}

	// gpt-4-turbo only matches the regex policy
	resolved = p.ResolveForModel("gpt-4-turbo")
	if resolved.BaseKeyEnv != "BROAD_KEY" {
		t.Errorf("BaseKeyEnv = %q, want BROAD_KEY", resolved.BaseKeyEnv)
	}
}

// TestResolveForModel_ProviderPromptsPrepend verifies that provider prompts
// are prepended before global prompts.
func TestResolveForModel_ProviderPromptsPrepend(t *testing.T) {
	policyJSON := `{
		"base_key_env": "KEY",
		"prompts": [
			{"role": "system", "content": "Global instruction A"},
			{"role": "system", "content": "Global instruction B"}
		],
		"providers": {
			"anthropic": [{
				"base_key_env": "ANT_KEY",
				"model_regex": "^claude",
				"prompts": [
					{"role": "system", "content": "Provider instruction 1"},
					{"role": "system", "content": "Provider instruction 2"}
				]
			}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	resolved := p.ResolveForModel("claude-3-opus")

	if len(resolved.Prompts) != 4 {
		t.Fatalf("expected 4 prompts, got %d", len(resolved.Prompts))
	}

	expected := []string{
		"Provider instruction 1",
		"Provider instruction 2",
		"Global instruction A",
		"Global instruction B",
	}
	for i, want := range expected {
		if resolved.Prompts[i].Content != want {
			t.Errorf("Prompts[%d].Content = %q, want %q", i, resolved.Prompts[i].Content, want)
		}
	}
}

// TestResolveForModel_ProviderRulesAppend verifies that provider rules
// are appended after global rules.
func TestResolveForModel_ProviderRulesAppend(t *testing.T) {
	policyJSON := `{
		"base_key_env": "KEY",
		"rules": [
			{"type": "regex", "pattern": "global_pattern", "action": "fail"}
		],
		"providers": {
			"openai": [{
				"base_key_env": "OAI_KEY",
				"model": "gpt-4o",
				"rules": [
					{"type": "keyword", "keywords": ["provider_keyword"], "action": "warn"}
				]
			}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	resolved := p.ResolveForModel("gpt-4o")

	if len(resolved.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(resolved.Rules))
	}

	// Global rule first
	if resolved.Rules[0].Pattern != "global_pattern" {
		t.Errorf("Rules[0].Pattern = %q, want global_pattern", resolved.Rules[0].Pattern)
	}
	if resolved.Rules[0].Action != "fail" {
		t.Errorf("Rules[0].Action = %q, want fail", resolved.Rules[0].Action)
	}

	// Provider rule second
	if resolved.Rules[1].Type != "keyword" {
		t.Errorf("Rules[1].Type = %q, want keyword", resolved.Rules[1].Type)
	}
	if resolved.Rules[1].Action != "warn" {
		t.Errorf("Rules[1].Action = %q, want warn", resolved.Rules[1].Action)
	}
}

// TestResolveForModel_TimeoutOverride verifies provider timeout overrides global.
func TestResolveForModel_TimeoutOverride(t *testing.T) {
	policyJSON := `{
		"base_key_env": "KEY",
		"timeout": 30,
		"providers": {
			"openai": [
				{"base_key_env": "OAI_KEY", "model": "gpt-4o", "timeout": 120},
				{"base_key_env": "OAI_KEY", "model_regex": "^gpt-4o-mini", "timeout": 60}
			]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	tests := []struct {
		model       string
		wantTimeout int
	}{
		{"gpt-4o", 120},
		{"gpt-4o-mini", 60},
		{"unknown-model", 30}, // Falls back to global
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			resolved := p.ResolveForModel(tt.model)
			if resolved.Timeout != tt.wantTimeout {
				t.Errorf("Timeout = %d, want %d", resolved.Timeout, tt.wantTimeout)
			}
		})
	}
}

// TestResolveForModel_GlobalFieldsInherited verifies that rate_limit, retry,
// metadata, and memory are inherited from global and not overridden by providers.
func TestResolveForModel_GlobalFieldsInherited(t *testing.T) {
	policyJSON := `{
		"base_key_env": "KEY",
		"rate_limit": {
			"rules": [{"requests": 60, "window": "1m"}],
			"max_parallel": 5
		},
		"retry": {
			"max_retries": 2,
			"fallbacks": ["gpt-4o-mini"]
		},
		"metadata": {"team": "engineering"},
		"memory": {"enabled": true, "file_path": "/tmp/mem"},
		"providers": {
			"openai": [{
				"base_key_env": "OAI_KEY",
				"model": "gpt-4o",
				"max_tokens": 50000
			}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	resolved := p.ResolveForModel("gpt-4o")

	if resolved.RateLimit == nil {
		t.Fatal("expected rate_limit to be inherited")
	}
	if resolved.RateLimit.MaxParallel != 5 {
		t.Errorf("MaxParallel = %d, want 5", resolved.RateLimit.MaxParallel)
	}

	if resolved.Retry == nil {
		t.Fatal("expected retry to be inherited")
	}
	if resolved.Retry.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", resolved.Retry.MaxRetries)
	}

	if resolved.Metadata["team"] != "engineering" {
		t.Errorf("Metadata[team] = %q, want engineering", resolved.Metadata["team"])
	}

	if !resolved.Memory.Enabled {
		t.Error("expected memory to be inherited and enabled")
	}
}

// TestResolveForModel_ProviderOnlyPolicy verifies that a policy with only
// providers (no global base_key_env) works correctly.
func TestResolveForModel_ProviderOnlyPolicy(t *testing.T) {
	policyJSON := `{
		"providers": {
			"openai": [
				{"base_key_env": "OPENAI_KEY", "model_regex": "^gpt-"}
			],
			"anthropic": [
				{"base_key_env": "ANTHROPIC_KEY", "model_regex": "^claude"}
			]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	resolved := p.ResolveForModel("gpt-4o")
	if resolved.BaseKeyEnv != "OPENAI_KEY" {
		t.Errorf("gpt-4o: BaseKeyEnv = %q, want OPENAI_KEY", resolved.BaseKeyEnv)
	}

	resolved = p.ResolveForModel("claude-3-opus")
	if resolved.BaseKeyEnv != "ANTHROPIC_KEY" {
		t.Errorf("claude-3-opus: BaseKeyEnv = %q, want ANTHROPIC_KEY", resolved.BaseKeyEnv)
	}

	// Unmatched model falls back to empty global
	resolved = p.ResolveForModel("unknown-model")
	if resolved.BaseKeyEnv != "" {
		t.Errorf("unknown: BaseKeyEnv = %q, want empty", resolved.BaseKeyEnv)
	}
}

// TestResolveForModel_UpstreamURLPriority verifies the resolution priority
// for upstream URL: policy > provider config > global.
func TestResolveForModel_UpstreamURLPriority(t *testing.T) {
	// Provider policy with upstream_url overrides global
	policyJSON := `{
		"base_key_env": "KEY",
		"upstream_url": "https://global.api",
		"providers": {
			"custom": [{
				"base_key_env": "CUSTOM_KEY",
				"model": "custom-model",
				"upstream_url": "https://custom.api"
			}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	// Matched provider policy has upstream_url
	resolved := p.ResolveForModel("custom-model")
	if resolved.UpstreamURL != "https://custom.api" {
		t.Errorf("custom-model UpstreamURL = %q, want https://custom.api", resolved.UpstreamURL)
	}

	// Unmatched model gets global upstream_url
	resolved = p.ResolveForModel("other-model")
	if resolved.UpstreamURL != "https://global.api" {
		t.Errorf("other-model UpstreamURL = %q, want https://global.api", resolved.UpstreamURL)
	}
}

// TestResolveForModel_NoPromptsOrRulesDoesNotMutate verifies that when
// the provider policy has no prompts or rules, the global ones are preserved unchanged.
func TestResolveForModel_NoPromptsOrRulesDoesNotMutate(t *testing.T) {
	policyJSON := `{
		"base_key_env": "KEY",
		"prompts": [{"role": "system", "content": "global"}],
		"rules": [{"type": "regex", "pattern": "blocked", "action": "fail"}],
		"providers": {
			"openai": [{
				"base_key_env": "OAI_KEY",
				"model": "gpt-4o"
			}]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	resolved := p.ResolveForModel("gpt-4o")

	if len(resolved.Prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(resolved.Prompts))
	}
	if resolved.Prompts[0].Content != "global" {
		t.Errorf("Prompts[0].Content = %q, want global", resolved.Prompts[0].Content)
	}

	if len(resolved.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(resolved.Rules))
	}
	if resolved.Rules[0].Pattern != "blocked" {
		t.Errorf("Rules[0].Pattern = %q, want blocked", resolved.Rules[0].Pattern)
	}
}

// TestMatchesModel verifies the ProviderPolicy.matchesModel logic.
func TestMatchesModel(t *testing.T) {
	tests := []struct {
		name       string
		pp         *ProviderPolicy
		model      string
		wantMatch  bool
	}{
		{
			name:      "exact match",
			pp:        &ProviderPolicy{BaseKeyEnv: "K", Model: "gpt-4o"},
			model:     "gpt-4o",
			wantMatch: true,
		},
		{
			name:      "exact mismatch",
			pp:        &ProviderPolicy{BaseKeyEnv: "K", Model: "gpt-4o"},
			model:     "gpt-4-turbo",
			wantMatch: false,
		},
		{
			name:      "regex match",
			pp:        &ProviderPolicy{BaseKeyEnv: "K", ModelRegex: "^gpt-4"},
			model:     "gpt-4-turbo",
			wantMatch: true,
		},
		{
			name:      "regex no match",
			pp:        &ProviderPolicy{BaseKeyEnv: "K", ModelRegex: "^gpt-4"},
			model:     "claude-3",
			wantMatch: false,
		},
		{
			name:      "wildcard matches anything",
			pp:        &ProviderPolicy{BaseKeyEnv: "K"},
			model:     "anything",
			wantMatch: true,
		},
		{
			name:      "wildcard matches empty",
			pp:        &ProviderPolicy{BaseKeyEnv: "K"},
			model:     "",
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compile if regex is set
			if tt.pp.ModelRegex != "" {
				if err := tt.pp.compile(); err != nil {
					t.Fatalf("compile failed: %v", err)
				}
			}

			got := tt.pp.matchesModel(tt.model)
			if got != tt.wantMatch {
				t.Errorf("matchesModel(%q) = %v, want %v", tt.model, got, tt.wantMatch)
			}
		})
	}
}

// TestResolveForModel_RetryFallbackResolution verifies that retry config
// is preserved through model resolution for fallback handling.
func TestResolveForModel_RetryFallbackResolution(t *testing.T) {
	policyJSON := `{
		"base_key_env": "KEY",
		"retry": {
			"max_retries": 3,
			"fallbacks": ["gpt-4o-mini", "gpt-3.5-turbo"],
			"retry_on": [429, 500, 502, 503]
		},
		"providers": {
			"openai": [
				{"base_key_env": "OAI_KEY", "model_regex": "^gpt-"}
			]
		}
	}`
	p, err := Parse(policyJSON)
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	resolved := p.ResolveForModel("gpt-4o")

	if resolved.Retry == nil {
		t.Fatal("expected retry config to be present")
	}
	if resolved.Retry.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", resolved.Retry.MaxRetries)
	}
	if len(resolved.Retry.Fallbacks) != 2 {
		t.Fatalf("expected 2 fallbacks, got %d", len(resolved.Retry.Fallbacks))
	}
	if resolved.Retry.Fallbacks[0] != "gpt-4o-mini" {
		t.Errorf("Fallbacks[0] = %q, want gpt-4o-mini", resolved.Retry.Fallbacks[0])
	}
	if resolved.Retry.Fallbacks[1] != "gpt-3.5-turbo" {
		t.Errorf("Fallbacks[1] = %q, want gpt-3.5-turbo", resolved.Retry.Fallbacks[1])
	}
	if len(resolved.Retry.RetryOn) != 4 {
		t.Errorf("expected 4 retry_on codes, got %d", len(resolved.Retry.RetryOn))
	}
}
