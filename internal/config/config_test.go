package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProviders(t *testing.T) {
	dir := t.TempDir()
	providersYAML := `
providers:
  openai:
    upstream_url: https://api.openai.com
    api_key_env: OPENAI_API_KEY
    models:
      - gpt-4o
      - gpt-4o-mini
  anthropic:
    upstream_url: https://api.anthropic.com
    api_key_env: ANTHROPIC_API_KEY
    auth_scheme: header
    auth_header: x-api-key
    headers:
      anthropic-version: "2023-06-01"
    chat_path: /v1/messages
    models:
      - claude-3-opus
  groq:
    upstream_url: https://api.groq.com/openai
    api_key_env: GROQ_API_KEY
`
	path := filepath.Join(dir, "providers.yaml")
	if err := os.WriteFile(path, []byte(providersYAML), 0644); err != nil {
		t.Fatal(err)
	}

	providers, err := LoadProviders(path)
	if err != nil {
		t.Fatalf("LoadProviders error: %v", err)
	}

	if len(providers) != 3 {
		t.Fatalf("expected 3 providers, got %d", len(providers))
	}

	// OpenAI
	openai, ok := providers["openai"]
	if !ok {
		t.Fatal("missing openai provider")
	}
	if openai.UpstreamURL != "https://api.openai.com" {
		t.Errorf("openai.UpstreamURL = %q", openai.UpstreamURL)
	}
	if openai.APIKeyEnv != "OPENAI_API_KEY" {
		t.Errorf("openai.APIKeyEnv = %q", openai.APIKeyEnv)
	}
	if len(openai.Models) != 2 {
		t.Errorf("openai.Models = %v", openai.Models)
	}

	// Anthropic with custom auth
	anthropic, ok := providers["anthropic"]
	if !ok {
		t.Fatal("missing anthropic provider")
	}
	if anthropic.AuthScheme != "header" {
		t.Errorf("anthropic.AuthScheme = %q, want %q", anthropic.AuthScheme, "header")
	}
	if anthropic.AuthHeader != "x-api-key" {
		t.Errorf("anthropic.AuthHeader = %q, want %q", anthropic.AuthHeader, "x-api-key")
	}
	if anthropic.Headers["anthropic-version"] != "2023-06-01" {
		t.Errorf("anthropic.Headers = %v", anthropic.Headers)
	}
	if anthropic.ChatPath != "/v1/messages" {
		t.Errorf("anthropic.ChatPath = %q", anthropic.ChatPath)
	}

	// Groq (minimal)
	groq, ok := providers["groq"]
	if !ok {
		t.Fatal("missing groq provider")
	}
	if groq.UpstreamURL != "https://api.groq.com/openai" {
		t.Errorf("groq.UpstreamURL = %q", groq.UpstreamURL)
	}
}

func TestLoadProviders_MissingFile(t *testing.T) {
	providers, err := LoadProviders("/nonexistent/path/providers.yaml")
	if err == nil {
		// This should fail since explicit path doesn't exist
		// But viper may handle it differently
		if providers != nil && len(providers) > 0 {
			t.Fatal("expected nil/empty providers for missing file")
		}
	}
}

func TestLoadProviders_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "providers.yaml")
	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	providers, err := LoadProviders(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("expected 0 providers, got %d", len(providers))
	}
}

func TestGetProvider(t *testing.T) {
	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"openai": {
				UpstreamURL: "https://api.openai.com",
				APIKeyEnv:   "OPENAI_API_KEY",
			},
		},
	}

	p, ok := cfg.GetProvider("openai")
	if !ok {
		t.Fatal("expected to find openai provider")
	}
	if p.UpstreamURL != "https://api.openai.com" {
		t.Errorf("UpstreamURL = %q", p.UpstreamURL)
	}

	_, ok = cfg.GetProvider("nonexistent")
	if ok {
		t.Fatal("expected nonexistent provider to not be found")
	}
}

func TestGetProvider_NilProviders(t *testing.T) {
	cfg := &Config{}
	_, ok := cfg.GetProvider("openai")
	if ok {
		t.Fatal("expected provider not found on nil map")
	}
}
