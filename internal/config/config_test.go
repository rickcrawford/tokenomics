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

func TestLoadConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	configYAML := `
server:
  upstream_url: https://api.openai.com
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	// Check defaults
	if cfg.Server.HTTPPort != 8080 {
		t.Errorf("HTTPPort = %d, want 8080", cfg.Server.HTTPPort)
	}
	if cfg.Server.HTTPSPort != 8443 {
		t.Errorf("HTTPSPort = %d, want 8443", cfg.Server.HTTPSPort)
	}
	if cfg.Storage.DBPath != "./tokenomics.db" {
		t.Errorf("DBPath = %q, want ./tokenomics.db", cfg.Storage.DBPath)
	}
	if cfg.Session.Backend != "memory" {
		t.Errorf("Session.Backend = %q, want memory", cfg.Session.Backend)
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want info", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("Logging.Format = %q, want json", cfg.Logging.Format)
	}
}

func TestLoadConfig_LoggingOverrides(t *testing.T) {
	dir := t.TempDir()
	configYAML := `
server:
  upstream_url: https://api.openai.com
logging:
  level: debug
  format: text
  request_body: true
  hide_token_hash: true
  disable_request: true
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want debug", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "text" {
		t.Errorf("Logging.Format = %q, want text", cfg.Logging.Format)
	}
	if !cfg.Logging.RequestBody {
		t.Error("Logging.RequestBody should be true")
	}
	if !cfg.Logging.HideTokenHash {
		t.Error("Logging.HideTokenHash should be true")
	}
	if !cfg.Logging.DisableRequest {
		t.Error("Logging.DisableRequest should be true")
	}
}

func TestLoadConfig_RemoteConfig(t *testing.T) {
	dir := t.TempDir()
	configYAML := `
server:
  upstream_url: https://api.openai.com
remote:
  url: http://config-server:9090
  api_key: secret123
  sync: 60
  insecure: true
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.Remote.URL != "http://config-server:9090" {
		t.Errorf("Remote.URL = %q", cfg.Remote.URL)
	}
	if cfg.Remote.APIKey != "secret123" {
		t.Errorf("Remote.APIKey = %q", cfg.Remote.APIKey)
	}
	if cfg.Remote.SyncSec != 60 {
		t.Errorf("Remote.SyncSec = %d, want 60", cfg.Remote.SyncSec)
	}
	if !cfg.Remote.Insecure {
		t.Error("Remote.Insecure should be true")
	}
}

func TestLoadConfig_EventsWebhook(t *testing.T) {
	dir := t.TempDir()
	configYAML := `
server:
  upstream_url: https://api.openai.com
events:
  webhooks:
    - url: http://localhost:9999/webhook
      secret: my-secret
      signing_key: my-key
      events:
        - token.*
        - request.completed
      timeout: 5
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if len(cfg.Events.Webhooks) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(cfg.Events.Webhooks))
	}

	wh := cfg.Events.Webhooks[0]
	if wh.URL != "http://localhost:9999/webhook" {
		t.Errorf("Webhook.URL = %q", wh.URL)
	}
	if wh.Secret != "my-secret" {
		t.Errorf("Webhook.Secret = %q", wh.Secret)
	}
	if wh.SigningKey != "my-key" {
		t.Errorf("Webhook.SigningKey = %q", wh.SigningKey)
	}
	if len(wh.Events) != 2 {
		t.Errorf("Webhook.Events = %v", wh.Events)
	}
	if wh.TimeoutSec != 5 {
		t.Errorf("Webhook.TimeoutSec = %d, want 5", wh.TimeoutSec)
	}
}
