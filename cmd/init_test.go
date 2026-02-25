package cmd

import (
	"testing"

	"github.com/rickcrawford/tokenomics/internal/config"
)

func TestResolveEnvPairs(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		token    string
		baseURL  string
		envKey   string
		envBase  string
		wantPairs []EnvPair
	}{
		{
			name:    "generic/openai default",
			provider: "generic",
			token:   "tok_abc123",
			baseURL: "https://localhost:8443",
			envKey:  "",
			envBase: "",
			wantPairs: []EnvPair{
				{"OPENAI_API_KEY", "tok_abc123"},
				{"OPENAI_BASE_URL", "https://localhost:8443/v1"},
			},
		},
		{
			name:    "openai is treated as generic default",
			provider: "openai",
			token:   "tok_abc123",
			baseURL: "https://localhost:8443",
			envKey:  "",
			envBase: "",
			wantPairs: []EnvPair{
				{"OPENAI_API_KEY", "tok_abc123"},
				{"OPENAI_BASE_URL", "https://localhost:8443/v1"},
			},
		},
		{
			name:    "empty cli is treated as generic default",
			provider:     "",
			token:   "my_token",
			baseURL: "https://proxy.example.com:9999",
			envKey:  "",
			envBase: "",
			wantPairs: []EnvPair{
				{"OPENAI_API_KEY", "my_token"},
				{"OPENAI_BASE_URL", "https://proxy.example.com:9999/v1"},
			},
		},
		{
			name:    "anthropic provider",
			provider:     "anthropic",
			token:   "sk-ant-123",
			baseURL: "https://localhost:8443",
			envKey:  "",
			envBase: "",
			wantPairs: []EnvPair{
				{"ANTHROPIC_API_KEY", "sk-ant-123"},
				{"ANTHROPIC_BASE_URL", "https://localhost:8443"},
			},
		},
		{
			name:    "anthropic case insensitive",
			provider:     "Anthropic",
			token:   "sk-ant-123",
			baseURL: "https://localhost:8443",
			envKey:  "",
			envBase: "",
			wantPairs: []EnvPair{
				{"ANTHROPIC_API_KEY", "sk-ant-123"},
				{"ANTHROPIC_BASE_URL", "https://localhost:8443"},
			},
		},
		{
			name:    "azure provider",
			provider:     "azure",
			token:   "azure-key-456",
			baseURL: "https://localhost:8443",
			envKey:  "",
			envBase: "",
			wantPairs: []EnvPair{
				{"AZURE_OPENAI_API_KEY", "azure-key-456"},
				{"AZURE_OPENAI_ENDPOINT", "https://localhost:8443"},
			},
		},
		{
			name:    "gemini provider",
			provider:     "gemini",
			token:   "gemini-key-789",
			baseURL: "https://localhost:8443",
			envKey:  "",
			envBase: "",
			wantPairs: []EnvPair{
				{"GEMINI_API_KEY", "gemini-key-789"},
				{"GEMINI_BASE_URL", "https://localhost:8443"},
			},
		},
		{
			name:    "custom env key and base override cli",
			provider:     "anthropic",
			token:   "custom-tok",
			baseURL: "https://custom.proxy:1234",
			envKey:  "MY_CUSTOM_KEY",
			envBase: "MY_CUSTOM_URL",
			wantPairs: []EnvPair{
				{"MY_CUSTOM_KEY", "custom-tok"},
				{"MY_CUSTOM_URL", "https://custom.proxy:1234"},
			},
		},
		{
			name:    "custom env key and base override generic",
			provider: "generic",
			token:   "tok",
			baseURL: "https://localhost:8443",
			envKey:  "CUSTOM_API_KEY",
			envBase: "CUSTOM_BASE",
			wantPairs: []EnvPair{
				{"CUSTOM_API_KEY", "tok"},
				{"CUSTOM_BASE", "https://localhost:8443"},
			},
		},
		{
			name:    "only envKey set without envBase falls through to cli-based resolution",
			provider:     "anthropic",
			token:   "tok",
			baseURL: "https://localhost:8443",
			envKey:  "ONLY_KEY",
			envBase: "",
			wantPairs: []EnvPair{
				{"ANTHROPIC_API_KEY", "tok"},
				{"ANTHROPIC_BASE_URL", "https://localhost:8443"},
			},
		},
		{
			name:    "only envBase set without envKey falls through to cli-based resolution",
			provider:     "azure",
			token:   "tok",
			baseURL: "https://localhost:8443",
			envKey:  "",
			envBase: "ONLY_BASE",
			wantPairs: []EnvPair{
				{"AZURE_OPENAI_API_KEY", "tok"},
				{"AZURE_OPENAI_ENDPOINT", "https://localhost:8443"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveEnvPairs(tt.provider, tt.token, tt.baseURL, tt.envKey, tt.envBase)

			if len(got) != len(tt.wantPairs) {
				t.Fatalf("len(pairs) = %d, want %d\n  got: %v\n  want: %v",
					len(got), len(tt.wantPairs), got, tt.wantPairs)
			}

			for i, want := range tt.wantPairs {
				if got[i].Key != want.Key {
					t.Errorf("pairs[%d].Key = %q, want %q", i, got[i].Key, want.Key)
				}
				if got[i].Value != want.Value {
					t.Errorf("pairs[%d].Value = %q, want %q", i, got[i].Value, want.Value)
				}
			}
		})
	}
}

func TestResolveEnvPairs_AlwaysReturnsTwoPairs(t *testing.T) {
	providers := []string{"generic", "anthropic", "azure", "gemini", "unknown"}
	for _, p := range providers {
		t.Run(p, func(t *testing.T) {
			pairs := ResolveEnvPairs(p, "tok", "https://localhost:8443", "", "")
			if len(pairs) != 2 {
				t.Fatalf("expected 2 pairs for provider %q, got %d: %v", p, len(pairs), pairs)
			}
		})
	}
}

func TestResolveEnvPairs_GenericAppends_v1(t *testing.T) {
	// Verify that the generic provider appends /v1 to the base URL
	pairs := ResolveEnvPairs("generic", "tok", "https://proxy:8443", "", "")

	baseURLPair := pairs[1]
	want := "https://proxy:8443/v1"
	if baseURLPair.Value != want {
		t.Errorf("generic base URL = %q, want %q", baseURLPair.Value, want)
	}
}

func TestResolveEnvPairs_NonGenericDoesNotAppend_v1(t *testing.T) {
	// Verify that non-generic providers do NOT append /v1
	providers := []string{"anthropic", "azure", "gemini"}
	for _, p := range providers {
		t.Run(p, func(t *testing.T) {
			pairs := ResolveEnvPairs(p, "tok", "https://proxy:8443", "", "")
			baseURLPair := pairs[1]
			if baseURLPair.Value == "https://proxy:8443/v1" {
				t.Errorf("provider %q should not append /v1 to base URL", p)
			}
			if baseURLPair.Value != "https://proxy:8443" {
				t.Errorf("provider %q base URL = %q, want %q", p, baseURLPair.Value, "https://proxy:8443")
			}
		})
	}
}

// --- Tests for config-aware resolution ---

func TestResolveEnvPairsWithConfig_UsesProviderConfig(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"deepseek": {
				APIKeyEnv:  "DEEPSEEK_API_KEY",
				BaseURLEnv: "DEEPSEEK_BASE_URL",
			},
			"mistral": {
				APIKeyEnv:  "MISTRAL_API_KEY",
				BaseURLEnv: "MISTRAL_BASE_URL",
			},
		},
	}

	pairs := resolveEnvPairsWithConfig(cfg, "deepseek", "tok", "https://proxy:8443", "", "")
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d: %v", len(pairs), pairs)
	}
	if pairs[0].Key != "DEEPSEEK_API_KEY" {
		t.Errorf("key env = %q, want DEEPSEEK_API_KEY", pairs[0].Key)
	}
	if pairs[1].Key != "DEEPSEEK_BASE_URL" {
		t.Errorf("base url env = %q, want DEEPSEEK_BASE_URL", pairs[1].Key)
	}
	// DeepSeek is OpenAI-compatible so should get /v1
	if pairs[1].Value != "https://proxy:8443/v1" {
		t.Errorf("base url = %q, want https://proxy:8443/v1", pairs[1].Value)
	}
}

func TestResolveEnvPairsWithConfig_CustomOverride(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"deepseek": {APIKeyEnv: "DEEPSEEK_API_KEY"},
		},
	}

	// Custom env vars should still take precedence over config
	pairs := resolveEnvPairsWithConfig(cfg, "deepseek", "tok", "https://proxy:8443", "MY_KEY", "MY_URL")
	if pairs[0].Key != "MY_KEY" || pairs[1].Key != "MY_URL" {
		t.Errorf("custom overrides should win, got %v", pairs)
	}
}

func TestResolveEnvPairsWithConfig_FallsBackToHardcoded(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{},
	}

	// Provider not in config should fall back to hardcoded
	pairs := resolveEnvPairsWithConfig(cfg, "anthropic", "tok", "https://proxy:8443", "", "")
	if pairs[0].Key != "ANTHROPIC_API_KEY" {
		t.Errorf("should fall back to hardcoded, got %v", pairs)
	}
}

func TestResolveEnvPairsWithConfig_NilConfig(t *testing.T) {
	// Nil config should work (falls back to hardcoded)
	pairs := resolveEnvPairsWithConfig(nil, "generic", "tok", "https://proxy:8443", "", "")
	if pairs[0].Key != "OPENAI_API_KEY" {
		t.Errorf("nil config should fall back, got %v", pairs)
	}
}

func TestResolveAllProviderPairs(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"openai": {
				APIKeyEnv:  "OPENAI_API_KEY",
				BaseURLEnv: "OPENAI_BASE_URL",
			},
			"anthropic": {
				APIKeyEnv:  "ANTHROPIC_API_KEY",
				BaseURLEnv: "ANTHROPIC_BASE_URL",
			},
		},
	}

	pairs := resolveAllProviderPairs(cfg, "tok", "https://proxy:8443")

	// Should have 4 pairs (2 per provider)
	if len(pairs) != 4 {
		t.Fatalf("expected 4 pairs, got %d: %v", len(pairs), pairs)
	}

	// Should be sorted alphabetically by provider name
	keys := make(map[string]string)
	for _, p := range pairs {
		keys[p.Key] = p.Value
	}

	if keys["ANTHROPIC_API_KEY"] != "tok" {
		t.Error("missing ANTHROPIC_API_KEY")
	}
	if keys["OPENAI_API_KEY"] != "tok" {
		t.Error("missing OPENAI_API_KEY")
	}
	if keys["OPENAI_BASE_URL"] != "https://proxy:8443/v1" {
		t.Errorf("OPENAI_BASE_URL = %q, want /v1 suffix", keys["OPENAI_BASE_URL"])
	}
	if keys["ANTHROPIC_BASE_URL"] != "https://proxy:8443" {
		t.Errorf("ANTHROPIC_BASE_URL = %q, want no /v1 suffix", keys["ANTHROPIC_BASE_URL"])
	}
}

func TestResolveAllProviderPairs_NilConfig(t *testing.T) {
	pairs := resolveAllProviderPairs(nil, "tok", "https://proxy:8443")
	if len(pairs) != 2 {
		t.Fatalf("nil config should return generic pairs, got %d", len(pairs))
	}
	if pairs[0].Key != "OPENAI_API_KEY" {
		t.Errorf("expected OPENAI_API_KEY, got %q", pairs[0].Key)
	}
}

func TestResolveAllProviderPairs_Deduplicates(t *testing.T) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"openai": {
				APIKeyEnv:  "OPENAI_API_KEY",
				BaseURLEnv: "OPENAI_BASE_URL",
			},
			// Another provider that happens to use the same env var name
			"openai_compat": {
				APIKeyEnv:  "OPENAI_API_KEY",
				BaseURLEnv: "OPENAI_BASE_URL",
			},
		},
	}

	pairs := resolveAllProviderPairs(cfg, "tok", "https://proxy:8443")

	keyCount := make(map[string]int)
	for _, p := range pairs {
		keyCount[p.Key]++
	}
	for k, c := range keyCount {
		if c > 1 {
			t.Errorf("duplicate env var %q appeared %d times", k, c)
		}
	}
}

func TestNeedsV1Suffix(t *testing.T) {
	v1Providers := []string{"openai", "generic", "groq", "deepseek", "xai", "openrouter"}
	for _, p := range v1Providers {
		if !needsV1Suffix(p) {
			t.Errorf("expected needsV1Suffix(%q) = true", p)
		}
	}

	noV1Providers := []string{"anthropic", "azure_openai", "google_gemini", "cohere", "ollama"}
	for _, p := range noV1Providers {
		if needsV1Suffix(p) {
			t.Errorf("expected needsV1Suffix(%q) = false", p)
		}
	}
}

func TestIsProxyToken(t *testing.T) {
	if !isProxyToken("tkn_abc123") {
		t.Error("should detect tkn_ prefix")
	}
	if isProxyToken("sk-proj-abc123") {
		t.Error("should not match sk-proj- prefix")
	}
	if isProxyToken("") {
		t.Error("should not match empty string")
	}
}

func TestEnvPairsFromProviderConfig_GeneratesDefaultEnvNames(t *testing.T) {
	// Provider with no base_url_env should generate one from the name
	pc := config.ProviderConfig{
		APIKeyEnv: "CUSTOM_KEY",
	}
	pairs := envPairsFromProviderConfig("my-provider", pc, "tok", "https://proxy:8443")
	if pairs[0].Key != "CUSTOM_KEY" {
		t.Errorf("key env = %q, want CUSTOM_KEY", pairs[0].Key)
	}
	if pairs[1].Key != "MY_PROVIDER_BASE_URL" {
		t.Errorf("base url env = %q, want MY_PROVIDER_BASE_URL", pairs[1].Key)
	}
}
