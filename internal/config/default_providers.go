package config

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/spf13/viper"
)

//go:embed providers.embedded.yaml
var embeddedProvidersYAML []byte

// defaultProviders contains built-in provider configurations loaded from an
// embedded providers YAML file.
var defaultProviders = mustLoadEmbeddedDefaultProviders()

func mustLoadEmbeddedDefaultProviders() map[string]ProviderConfig {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(bytes.NewReader(embeddedProvidersYAML)); err != nil {
		panic(fmt.Sprintf("load embedded providers yaml: %v", err))
	}

	var wrapper struct {
		Providers map[string]ProviderConfig `mapstructure:"providers"`
	}
	if err := v.Unmarshal(&wrapper); err != nil {
		panic(fmt.Sprintf("parse embedded providers yaml: %v", err))
	}

	if wrapper.Providers == nil {
		wrapper.Providers = make(map[string]ProviderConfig)
	}

	// Compatibility aliases for existing policy/config usage.
	if openai, ok := wrapper.Providers["openai"]; ok {
		if _, exists := wrapper.Providers["generic"]; !exists {
			alias := openai
			alias.BaseURLEnv = "GENERIC_BASE_URL"
			wrapper.Providers["generic"] = alias
		}
	}
	if azure, ok := wrapper.Providers["azure_openai"]; ok {
		if _, exists := wrapper.Providers["azure"]; !exists {
			wrapper.Providers["azure"] = azure
		}
	}
	if gemini, ok := wrapper.Providers["google_gemini"]; ok {
		if _, exists := wrapper.Providers["gemini"]; !exists {
			alias := gemini
			alias.APIKeyEnv = "GEMINI_API_KEY"
			wrapper.Providers["gemini"] = alias
		}
	}

	return wrapper.Providers
}

// copyDefaultProviders returns a copy of the built-in provider defaults.
func copyDefaultProviders() map[string]ProviderConfig {
	out := make(map[string]ProviderConfig, len(defaultProviders))
	for k, v := range defaultProviders {
		out[k] = v
	}
	return out
}

// DefaultProviders returns a copy of the built-in provider defaults.
func DefaultProviders() map[string]ProviderConfig {
	return copyDefaultProviders()
}
