package cmd

import (
	"os"
	"testing"

	"github.com/rickcrawford/tokenomics/internal/config"
)

func BenchmarkResolveEnvPairs_Generic(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ResolveEnvPairs("generic", "tkn_abc123", "https://localhost:8443", "", "")
	}
}

func BenchmarkResolveEnvPairs_Anthropic(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ResolveEnvPairs("anthropic", "tkn_abc123", "https://localhost:8443", "", "")
	}
}

func BenchmarkResolveEnvPairsWithConfig(b *testing.B) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"openai":    {APIKeyEnv: "OPENAI_API_KEY", BaseURLEnv: "OPENAI_BASE_URL"},
			"anthropic": {APIKeyEnv: "ANTHROPIC_API_KEY", BaseURLEnv: "ANTHROPIC_BASE_URL"},
			"groq":      {APIKeyEnv: "GROQ_API_KEY", BaseURLEnv: "GROQ_BASE_URL"},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolveEnvPairsWithConfig(cfg, "anthropic", "tkn_test", "https://localhost:8443", "", "")
	}
}

func BenchmarkNeedsV1Suffix(b *testing.B) {
	providers := []string{"openai", "anthropic", "groq", "deepseek", "xai", "gemini"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		needsV1Suffix(providers[i%len(providers)])
	}
}

func BenchmarkOutputShell(b *testing.B) {
	pairs := []EnvPair{
		{"OPENAI_API_KEY", "tkn_abc123"},
		{"OPENAI_BASE_URL", "https://localhost:8443/v1"},
		{"ANTHROPIC_API_KEY", "tkn_abc123"},
		{"ANTHROPIC_BASE_URL", "https://localhost:8443"},
	}
	f, _ := os.CreateTemp(b.TempDir(), "bench-*")
	defer f.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := f.Seek(0, 0); err != nil {
			b.Fatalf("seek failed: %v", err)
		}
		if err := f.Truncate(0); err != nil {
			b.Fatalf("truncate failed: %v", err)
		}
		if err := OutputShell(pairs, f); err != nil {
			b.Fatalf("output shell failed: %v", err)
		}
	}
}

func BenchmarkHashToken(b *testing.B) {
	key := []byte("test-hash-key-for-benchmarking")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hashToken("tkn_abc123-test-token", key)
	}
}

func BenchmarkParseExpires_Duration(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := parseExpires("24h"); err != nil {
			b.Fatalf("parse expires failed: %v", err)
		}
	}
}

func BenchmarkParseExpires_Days(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := parseExpires("30d"); err != nil {
			b.Fatalf("parse expires failed: %v", err)
		}
	}
}

func BenchmarkParseExpires_RFC3339(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := parseExpires("2025-12-31T23:59:59Z"); err != nil {
			b.Fatalf("parse expires failed: %v", err)
		}
	}
}

func BenchmarkResolveDaemonPaths(b *testing.B) {
	for i := 0; i < b.N; i++ {
		resolveDaemonPaths("", "")
	}
}

func BenchmarkResolveAllProviderPairs(b *testing.B) {
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"openai":    {APIKeyEnv: "OPENAI_API_KEY", BaseURLEnv: "OPENAI_BASE_URL"},
			"anthropic": {APIKeyEnv: "ANTHROPIC_API_KEY", BaseURLEnv: "ANTHROPIC_BASE_URL"},
			"groq":      {APIKeyEnv: "GROQ_API_KEY", BaseURLEnv: "GROQ_BASE_URL"},
			"deepseek":  {APIKeyEnv: "DEEPSEEK_API_KEY", BaseURLEnv: "DEEPSEEK_BASE_URL"},
			"xai":       {APIKeyEnv: "XAI_API_KEY", BaseURLEnv: "XAI_BASE_URL"},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resolveAllProviderPairs(cfg, "tkn_test", "https://localhost:8443")
	}
}
