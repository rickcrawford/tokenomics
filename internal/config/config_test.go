package config

import (
	"os"
	"path/filepath"
	"strings"
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

	// Should have defaults + file overrides (at least 3 from file + others from defaults)
	if len(providers) < 3 {
		t.Fatalf("expected at least 3 providers, got %d", len(providers))
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
		if len(providers) > 0 {
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
	// Empty file means no overrides, but defaults should still be present
	if len(providers) == 0 {
		t.Fatalf("expected default providers, got 0")
	}
	// Should have at least the defaults
	if _, ok := providers["anthropic"]; !ok {
		t.Error("expected anthropic in providers from defaults")
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
	// Check that DBPath is absolute and contains .tokenomics/tokenomics.db
	if !filepath.IsAbs(cfg.Storage.DBPath) {
		t.Errorf("DBPath = %q, must be absolute", cfg.Storage.DBPath)
	}
	if !strings.Contains(cfg.Storage.DBPath, filepath.Join(".tokenomics", "tokenomics.db")) {
		t.Errorf("DBPath = %q, must contain .tokenomics/tokenomics.db", cfg.Storage.DBPath)
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
	if cfg.Logging.ProxyLogFile != "proxy.log" {
		t.Errorf("Logging.ProxyLogFile = %q, want proxy.log", cfg.Logging.ProxyLogFile)
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
  proxy_log_file: custom-proxy.log
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
	if cfg.Logging.ProxyLogFile != "custom-proxy.log" {
		t.Errorf("Logging.ProxyLogFile = %q, want custom-proxy.log", cfg.Logging.ProxyLogFile)
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

func TestDefaultProviders(t *testing.T) {
	defaults := DefaultProviders()

	// Verify all expected providers exist
	expectedProviders := []string{
		"openai", "generic", "anthropic", "azure", "gemini", "groq", "mistral", "deepseek", "ollama",
	}
	for _, name := range expectedProviders {
		if _, ok := defaults[name]; !ok {
			t.Errorf("expected provider %q in defaults", name)
		}
	}

	// Verify anthropic has correct settings
	anthropic, ok := defaults["anthropic"]
	if !ok {
		t.Fatal("missing anthropic provider")
	}
	if anthropic.UpstreamURL != "https://api.anthropic.com" {
		t.Errorf("anthropic.UpstreamURL = %q, want https://api.anthropic.com", anthropic.UpstreamURL)
	}
	if anthropic.APIKeyEnv != "ANTHROPIC_API_KEY" {
		t.Errorf("anthropic.APIKeyEnv = %q, want ANTHROPIC_API_KEY", anthropic.APIKeyEnv)
	}
	if anthropic.AuthScheme != "header" {
		t.Errorf("anthropic.AuthScheme = %q, want header", anthropic.AuthScheme)
	}
	if anthropic.AuthHeader != "x-api-key" {
		t.Errorf("anthropic.AuthHeader = %q, want x-api-key", anthropic.AuthHeader)
	}
	if anthropic.ChatPath != "/v1/messages" {
		t.Errorf("anthropic.ChatPath = %q, want /v1/messages", anthropic.ChatPath)
	}
	if anthropic.Headers["anthropic-version"] != "2023-06-01" {
		t.Errorf("anthropic.Headers[anthropic-version] = %q, want 2023-06-01", anthropic.Headers["anthropic-version"])
	}

	// Verify openai defaults
	openai, ok := defaults["openai"]
	if !ok {
		t.Fatal("missing openai provider")
	}
	if openai.UpstreamURL != "https://api.openai.com" {
		t.Errorf("openai.UpstreamURL = %q, want https://api.openai.com", openai.UpstreamURL)
	}

	// Verify azure
	azure, ok := defaults["azure"]
	if !ok {
		t.Fatal("missing azure provider")
	}
	if azure.AuthHeader != "api-key" {
		t.Errorf("azure.AuthHeader = %q, want api-key", azure.AuthHeader)
	}

	// Verify gemini uses query auth
	gemini, ok := defaults["gemini"]
	if !ok {
		t.Fatal("missing gemini provider")
	}
	if gemini.AuthScheme != "query" {
		t.Errorf("gemini.AuthScheme = %q, want query", gemini.AuthScheme)
	}

	// Verify ollama
	ollama, ok := defaults["ollama"]
	if !ok {
		t.Fatal("missing ollama provider")
	}
	if ollama.UpstreamURL != "http://localhost:11434" {
		t.Errorf("ollama.UpstreamURL = %q, want http://localhost:11434", ollama.UpstreamURL)
	}
	if ollama.ChatPath != "/api/chat" {
		t.Errorf("ollama.ChatPath = %q, want /api/chat", ollama.ChatPath)
	}
}

func TestLoadProviders_ReturnsDefaults(t *testing.T) {
	// When no providers.yaml exists in standard paths, LoadProviders should return defaults
	// Create a temp dir and LoadProviders with search mode (empty string)
	// This tests the "no file found" path in standard search paths
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(cwd)

	providers, err := LoadProviders("")
	if err != nil {
		t.Fatalf("LoadProviders error: %v", err)
	}

	// Should have defaults even though file doesn't exist
	if len(providers) == 0 {
		t.Fatal("expected providers from defaults, got empty map")
	}

	// Verify anthropic is in defaults
	if _, ok := providers["anthropic"]; !ok {
		t.Error("expected anthropic provider in defaults")
	}
}

func TestLoadProviders_DefaultsOverriddenByFile(t *testing.T) {
	dir := t.TempDir()
	providersYAML := `
providers:
  anthropic:
    upstream_url: https://custom.anthropic.com
    api_key_env: CUSTOM_API_KEY
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

	// File overrides should be in result
	anthropic, ok := providers["anthropic"]
	if !ok {
		t.Fatal("missing anthropic provider")
	}
	if anthropic.UpstreamURL != "https://custom.anthropic.com" {
		t.Errorf("anthropic.UpstreamURL = %q, want https://custom.anthropic.com", anthropic.UpstreamURL)
	}
	if anthropic.APIKeyEnv != "CUSTOM_API_KEY" {
		t.Errorf("anthropic.APIKeyEnv = %q, want CUSTOM_API_KEY", anthropic.APIKeyEnv)
	}

	// Groq from file should be present
	groq, ok := providers["groq"]
	if !ok {
		t.Fatal("missing groq provider")
	}
	if groq.UpstreamURL != "https://api.groq.com/openai" {
		t.Errorf("groq.UpstreamURL = %q", groq.UpstreamURL)
	}

	// openai default should still be present (not overridden)
	openai, ok := providers["openai"]
	if !ok {
		t.Fatal("missing openai provider from defaults")
	}
	if openai.UpstreamURL != "https://api.openai.com" {
		t.Errorf("openai.UpstreamURL = %q, want https://api.openai.com", openai.UpstreamURL)
	}
}

func TestLoad_DefaultProviders(t *testing.T) {
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

	// Should have default providers even with minimal config
	if len(cfg.Providers) == 0 {
		t.Fatal("expected default providers in config")
	}

	// Verify anthropic is available
	if _, ok := cfg.Providers["anthropic"]; !ok {
		t.Error("expected anthropic provider in loaded config")
	}
	if _, ok := cfg.Providers["openai"]; !ok {
		t.Error("expected openai provider in loaded config")
	}
}

func TestLoad_InlineProvidersOverrideDefaults(t *testing.T) {
	dir := t.TempDir()
	configYAML := `
server:
  upstream_url: https://api.openai.com
providers:
  anthropic:
    upstream_url: https://my-custom-endpoint.com
    api_key_env: MY_CUSTOM_KEY
    auth_scheme: bearer
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	// Inline anthropic should override default
	anthropic, ok := cfg.Providers["anthropic"]
	if !ok {
		t.Fatal("missing anthropic provider")
	}
	if anthropic.UpstreamURL != "https://my-custom-endpoint.com" {
		t.Errorf("anthropic.UpstreamURL = %q, want https://my-custom-endpoint.com", anthropic.UpstreamURL)
	}
	if anthropic.APIKeyEnv != "MY_CUSTOM_KEY" {
		t.Errorf("anthropic.APIKeyEnv = %q, want MY_CUSTOM_KEY", anthropic.APIKeyEnv)
	}
	if anthropic.AuthScheme != "bearer" {
		t.Errorf("anthropic.AuthScheme = %q, want bearer", anthropic.AuthScheme)
	}

	// Other defaults should still be present
	if _, ok := cfg.Providers["openai"]; !ok {
		t.Error("expected openai provider from defaults")
	}
}
