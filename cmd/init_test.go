package cmd

import (
	"testing"
)

func TestResolveEnvPairs(t *testing.T) {
	tests := []struct {
		name     string
		cli      string
		token    string
		baseURL  string
		envKey   string
		envBase  string
		wantPairs []EnvPair
	}{
		{
			name:    "generic/openai default",
			cli:     "generic",
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
			cli:     "openai",
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
			cli:     "",
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
			cli:     "anthropic",
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
			cli:     "Anthropic",
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
			cli:     "azure",
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
			cli:     "gemini",
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
			cli:     "anthropic",
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
			cli:     "generic",
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
			cli:     "anthropic",
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
			cli:     "azure",
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
			got := ResolveEnvPairs(tt.cli, tt.token, tt.baseURL, tt.envKey, tt.envBase)

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
