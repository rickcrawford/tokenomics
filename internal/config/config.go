package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig              `mapstructure:"server"`
	Storage   StorageConfig             `mapstructure:"storage"`
	Session   SessionConfig             `mapstructure:"session"`
	Security  SecurityConfig            `mapstructure:"security"`
	Providers map[string]ProviderConfig `mapstructure:"providers"`
	Events    EventsConfig              `mapstructure:"events"`
}

// EventsConfig holds webhook and future event emitter configuration.
type EventsConfig struct {
	Webhooks []WebhookEndpoint `mapstructure:"webhooks"`
}

// WebhookEndpoint configures a single webhook destination.
type WebhookEndpoint struct {
	URL        string   `mapstructure:"url"`
	Secret     string   `mapstructure:"secret"`      // Shared secret sent as X-Webhook-Secret
	SigningKey  string   `mapstructure:"signing_key"`  // HMAC-SHA256 signing key for X-Webhook-Signature
	Events     []string `mapstructure:"events"`       // Event type filter (supports trailing * wildcard); empty = all
	TimeoutSec int      `mapstructure:"timeout"`      // HTTP timeout in seconds (default 10)
}

// ProviderConfig defines a known upstream AI provider.
// Policies reference providers by name instead of repeating connection details.
type ProviderConfig struct {
	UpstreamURL string            `mapstructure:"upstream_url" json:"upstream_url"`
	APIKeyEnv   string            `mapstructure:"api_key_env" json:"api_key_env"`
	AuthHeader  string            `mapstructure:"auth_header" json:"auth_header,omitempty"`   // Custom auth header name (default: "Authorization")
	AuthScheme  string            `mapstructure:"auth_scheme" json:"auth_scheme,omitempty"`   // "bearer" (default), "header" (raw value in auth_header), "query" (appended as ?key=)
	Headers     map[string]string `mapstructure:"headers" json:"headers,omitempty"`           // Extra headers sent with every request
	Models      []string          `mapstructure:"models" json:"models,omitempty"`             // Known model prefixes (informational)
	ChatPath    string            `mapstructure:"chat_path" json:"chat_path,omitempty"`       // Override chat completions path (default: /v1/chat/completions)
}

type ServerConfig struct {
	HTTPPort    int       `mapstructure:"http_port"`
	HTTPSPort   int       `mapstructure:"https_port"`
	TLS         TLSConfig `mapstructure:"tls"`
	UpstreamURL string    `mapstructure:"upstream_url"`
}

type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	CertFile string `mapstructure:"cert_file"`
	KeyFile  string `mapstructure:"key_file"`
	AutoGen  bool   `mapstructure:"auto_gen"`
	CertDir  string `mapstructure:"cert_dir"`
}

type StorageConfig struct {
	DBPath string `mapstructure:"db_path"`
}

type SessionConfig struct {
	Backend string      `mapstructure:"backend"`
	Redis   RedisConfig `mapstructure:"redis"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type SecurityConfig struct {
	HashKeyEnv       string `mapstructure:"hash_key_env"`
	EncryptionKeyEnv string `mapstructure:"encryption_key_env"`
}

func Load(cfgFile string) (*Config, error) {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.tokenomics")
	}

	// Defaults
	viper.SetDefault("server.http_port", 8080)
	viper.SetDefault("server.https_port", 8443)
	viper.SetDefault("server.tls.enabled", true)
	viper.SetDefault("server.tls.auto_gen", true)
	viper.SetDefault("server.tls.cert_dir", "./certs")
	viper.SetDefault("server.upstream_url", "https://api.openai.com")
	viper.SetDefault("storage.db_path", "./tokenomics.db")
	viper.SetDefault("session.backend", "memory")
	viper.SetDefault("session.redis.addr", "localhost:6379")
	viper.SetDefault("session.redis.db", 0)
	viper.SetDefault("security.hash_key_env", "TOKENOMICS_HASH_KEY")
	viper.SetDefault("security.encryption_key_env", "TOKENOMICS_ENCRYPTION_KEY")

	viper.SetEnvPrefix("TOKENOMICS")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Load providers from separate providers.yaml if not inline in config
	if len(cfg.Providers) == 0 {
		providers, err := LoadProviders("")
		if err == nil && len(providers) > 0 {
			cfg.Providers = providers
		}
	}

	return &cfg, nil
}

// LoadProviders loads provider definitions from a providers.yaml file.
// If providersFile is empty, searches standard paths (., $HOME/.tokenomics).
func LoadProviders(providersFile string) (map[string]ProviderConfig, error) {
	pv := viper.New()

	if providersFile != "" {
		pv.SetConfigFile(providersFile)
	} else {
		pv.SetConfigName("providers")
		pv.SetConfigType("yaml")
		pv.AddConfigPath(".")
		pv.AddConfigPath("$HOME/.tokenomics")
	}

	if err := pv.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return nil, nil
		}
		return nil, err
	}

	var wrapper struct {
		Providers map[string]ProviderConfig `mapstructure:"providers"`
	}
	if err := pv.Unmarshal(&wrapper); err != nil {
		return nil, fmt.Errorf("parse providers config: %w", err)
	}

	return wrapper.Providers, nil
}

// GetProvider returns the provider config by name, if it exists.
func (c *Config) GetProvider(name string) (ProviderConfig, bool) {
	if c.Providers == nil {
		return ProviderConfig{}, false
	}
	p, ok := c.Providers[name]
	return p, ok
}
