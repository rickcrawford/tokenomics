package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rickcrawford/tokenomics/internal/config"
)

func TestDetectProviderFromCLI_Match(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
cli_maps:
  claude: anthropic
  gpt: openai
providers:
  anthropic:
    upstream_url: https://api.anthropic.com
  openai:
    upstream_url: https://api.openai.com
`), 0o644)

	// Verify config loads correctly
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if cfg.CLIMaps["claude"] != "anthropic" {
		t.Fatalf("expected claude -> anthropic, got %q", cfg.CLIMaps["claude"])
	}

	result := detectProviderFromCLI("claude", cfgPath)
	if result != "anthropic" {
		t.Errorf("detectProviderFromCLI('claude') = %q, want 'anthropic'", result)
	}
}

func TestDetectProviderFromCLI_NoMatch(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
cli_maps:
  claude: anthropic
providers:
  anthropic:
    upstream_url: https://api.anthropic.com
`), 0o644)

	result := detectProviderFromCLI("unknown-cli", cfgPath)
	if result != "" {
		t.Errorf("expected empty string for unknown CLI, got %q", result)
	}
}

func TestDetectProviderFromCLI_NoConfig(t *testing.T) {
	result := detectProviderFromCLI("claude", "/nonexistent/config.yaml")
	if result != "" {
		t.Errorf("expected empty string for missing config, got %q", result)
	}
}

func TestRunCmd_Registration(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "run [flags] COMMAND [ARGS...]" {
			found = true
			break
		}
	}
	if !found {
		t.Error("run command not registered on root")
	}
}

func TestRunCmd_Flags(t *testing.T) {
	flags := []string{"token", "proxy-url", "host", "port", "tls", "insecure", "provider", "env-key", "env-base-url"}
	for _, name := range flags {
		if runCmd.Flags().Lookup(name) == nil {
			t.Errorf("run command missing flag: %s", name)
		}
	}
}
