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
		f.Seek(0, 0)
		f.Truncate(0)
		OutputShell(pairs, f)
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
		parseExpires("24h")
	}
}

func BenchmarkParseExpires_Days(b *testing.B) {
	for i := 0; i < b.N; i++ {
		parseExpires("30d")
	}
}

func BenchmarkParseExpires_RFC3339(b *testing.B) {
	for i := 0; i < b.N; i++ {
		parseExpires("2025-12-31T23:59:59Z")
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
