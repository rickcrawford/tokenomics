package cmd

import (
	"os"
	"testing"

	"github.com/rickcrawford/tokenomics/internal/config"
)

func TestBuildProviderStatuses(t *testing.T) {
	providers := map[string]config.ProviderConfig{
		"openai": {
			UpstreamURL: "https://api.openai.com",
			APIKeyEnv:   "OPENAI_API_KEY",
			Models:      []string{"gpt-4o", "gpt-4o-mini"},
		},
		"ollama": {
			UpstreamURL: "http://localhost:11434",
			APIKeyEnv:   "",
			Models:      []string{"llama3.1"},
		},
	}

	statuses := buildProviderStatuses(providers)

	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	// Should be sorted alphabetically
	if statuses[0].Name != "ollama" {
		t.Errorf("expected first provider to be ollama, got %s", statuses[0].Name)
	}
	if statuses[1].Name != "openai" {
		t.Errorf("expected second provider to be openai, got %s", statuses[1].Name)
	}

	// ollama has no key env, so KeySet should be false
	if statuses[0].KeySet {
		t.Error("ollama should not have KeySet=true")
	}
	if statuses[0].ModelCount != 1 {
		t.Errorf("ollama model count = %d, want 1", statuses[0].ModelCount)
	}

	// openai without env var set
	if statuses[1].KeySet {
		t.Error("openai should not have KeySet=true when env is not set")
	}
	if statuses[1].ModelCount != 2 {
		t.Errorf("openai model count = %d, want 2", statuses[1].ModelCount)
	}
}

func TestBuildProviderStatuses_KeySet(t *testing.T) {
	t.Setenv("TEST_PROVIDER_KEY", "sk-test-123")

	providers := map[string]config.ProviderConfig{
		"test_provider": {
			UpstreamURL: "https://api.test.com",
			APIKeyEnv:   "TEST_PROVIDER_KEY",
			Models:      []string{"model-1"},
		},
	}

	statuses := buildProviderStatuses(providers)

	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if !statuses[0].KeySet {
		t.Error("expected KeySet=true when env var is set")
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]config.ProviderConfig{
		"zebra":    {},
		"alpha":    {},
		"middle":   {},
	}

	keys := sortedKeys(m)
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	if keys[0] != "alpha" || keys[1] != "middle" || keys[2] != "zebra" {
		t.Errorf("keys not sorted: %v", keys)
	}
}

func TestBuildProviderStatuses_Empty(t *testing.T) {
	statuses := buildProviderStatuses(map[string]config.ProviderConfig{})
	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses for empty providers, got %d", len(statuses))
	}
}

func TestBuildProviderStatuses_AuthSchemePreserved(t *testing.T) {
	providers := map[string]config.ProviderConfig{
		"anthropic": {
			UpstreamURL: "https://api.anthropic.com",
			APIKeyEnv:   "ANTHROPIC_API_KEY",
			AuthScheme:  "header",
			AuthHeader:  "x-api-key",
		},
		"gemini": {
			UpstreamURL: "https://generativelanguage.googleapis.com",
			APIKeyEnv:   "GOOGLE_API_KEY",
			AuthScheme:  "query",
		},
	}

	// Ensure we don't leak env state
	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("GOOGLE_API_KEY")

	statuses := buildProviderStatuses(providers)
	for _, s := range statuses {
		switch s.Name {
		case "anthropic":
			if s.AuthScheme != "header" {
				t.Errorf("anthropic auth scheme = %q, want header", s.AuthScheme)
			}
		case "gemini":
			if s.AuthScheme != "query" {
				t.Errorf("gemini auth scheme = %q, want query", s.AuthScheme)
			}
		}
	}
}
