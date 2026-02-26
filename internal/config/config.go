package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// defaultProviders contains built-in configurations for common AI providers.
// External providers.yaml and inline config.yaml entries override these defaults.
var defaultProviders = map[string]ProviderConfig{
	"openai": {
		UpstreamURL: "https://api.openai.com",
		APIKeyEnv:   "OPENAI_API_KEY",
	},
	"generic": {
		UpstreamURL: "https://api.openai.com",
		APIKeyEnv:   "OPENAI_API_KEY",
	},
	"anthropic": {
		UpstreamURL: "https://api.anthropic.com",
		APIKeyEnv:   "ANTHROPIC_API_KEY",
		AuthScheme:  "header",
		AuthHeader:  "x-api-key",
		Headers: map[string]string{
			"anthropic-version": "2023-06-01",
		},
		ChatPath: "/v1/messages",
	},
	"azure": {
		UpstreamURL: "https://my-resource.openai.azure.com",
		APIKeyEnv:   "AZURE_OPENAI_API_KEY",
		AuthScheme:  "header",
		AuthHeader:  "api-key",
		Headers: map[string]string{
			"api-version": "2024-10-21",
		},
	},
	"gemini": {
		UpstreamURL: "https://generativelanguage.googleapis.com",
		APIKeyEnv:   "GEMINI_API_KEY",
		AuthScheme:  "query",
	},
	"groq": {
		UpstreamURL: "https://api.groq.com/openai",
		APIKeyEnv:   "GROQ_API_KEY",
	},
	"mistral": {
		UpstreamURL: "https://api.mistral.ai",
		APIKeyEnv:   "MISTRAL_API_KEY",
	},
	"deepseek": {
		UpstreamURL: "https://api.deepseek.com",
		APIKeyEnv:   "DEEPSEEK_API_KEY",
	},
	"ollama": {
		UpstreamURL: "http://localhost:11434",
		AuthScheme:  "header",
		ChatPath:    "/api/chat",
	},
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

type Config struct {
	Dir              string                    `mapstructure:"dir"`              // base .tokenomics directory (default ".tokenomics")
	Server           ServerConfig              `mapstructure:"server"`
	Storage          StorageConfig             `mapstructure:"storage"`
	Session          SessionConfig             `mapstructure:"session"`
	Security         SecurityConfig            `mapstructure:"security"`
	Logging          LoggingConfig             `mapstructure:"logging"`
	Providers        map[string]ProviderConfig `mapstructure:"providers"`
	Events           EventsConfig              `mapstructure:"events"`
	Remote           RemoteConfig              `mapstructure:"remote"`
	Ledger           LedgerConfig              `mapstructure:"ledger"`
	CLIMaps          map[string]string         `mapstructure:"cli_maps"`           // Map CLI names to providers (e.g. "claude" -> "anthropic")
	DefaultProvider  string                    `mapstructure:"default_provider"`  // Default provider when not specified in policy
}

// LoggingConfig controls request and event logging behavior.
type LoggingConfig struct {
	Level          string `mapstructure:"level"`           // "debug", "info" (default), "warn", "error"
	Format         string `mapstructure:"format"`          // "json" (default), "text"
	RequestBody    bool   `mapstructure:"request_body"`    // Log full request bodies (default false)
	ResponseBody   bool   `mapstructure:"response_body"`   // Log full response bodies (default false)
	HideTokenHash  bool   `mapstructure:"hide_token_hash"` // Mask token hashes in logs (default false)
	DisableRequest bool   `mapstructure:"disable_request"` // Suppress per-request structured logs (default false)
	ProxyLogFile   string `mapstructure:"proxy_log_file"`  // Proxy debug log filename under `dir` (default "proxy.log")
}

// RemoteConfig configures loading tokens and config from a central server.
type RemoteConfig struct {
	URL      string          `mapstructure:"url"`      // Central server URL (e.g. http://config-server:9090)
	APIKey   string          `mapstructure:"api_key"`  // Shared API key for authentication
	SyncSec  int             `mapstructure:"sync"`     // Sync interval in seconds (0 = startup only)
	Insecure bool            `mapstructure:"insecure"` // Skip TLS verification
	Webhook  WebhookReceiver `mapstructure:"webhook"`  // Inbound webhook for push-based sync
}

// WebhookReceiver configures the inbound webhook endpoint on the proxy.
// The central config server pushes token lifecycle events here to trigger
// immediate sync instead of waiting for the poll interval.
type WebhookReceiver struct {
	Enabled      bool   `mapstructure:"enabled"`       // Enable the webhook receiver endpoint
	Path         string `mapstructure:"path"`          // URL path (default: /v1/webhook)
	Secret       string `mapstructure:"secret"`        // Expected X-Webhook-Secret header value
	SigningKey   string `mapstructure:"signing_key"`   // HMAC-SHA256 key for X-Webhook-Signature verification
	AutoRegister bool   `mapstructure:"auto_register"` // Auto-register this proxy's webhook with the central server on startup
	CallbackURL  string `mapstructure:"callback_url"`  // Webhook callback URL the central server will POST to (required if auto_register is true)
	Insecure     bool   `mapstructure:"insecure"`      // Tell the server to skip TLS verification when delivering webhooks to this client
}

// EventsConfig holds webhook and future event emitter configuration.
type EventsConfig struct {
	Webhooks []WebhookEndpoint `mapstructure:"webhooks"`
}

// WebhookEndpoint configures a single webhook destination.
type WebhookEndpoint struct {
	URL        string   `mapstructure:"url"`
	Secret     string   `mapstructure:"secret"`      // Shared secret sent as X-Webhook-Secret
	SigningKey string   `mapstructure:"signing_key"` // HMAC-SHA256 signing key for X-Webhook-Signature
	Events     []string `mapstructure:"events"`      // Event type filter (supports trailing * wildcard); empty = all
	TimeoutSec int      `mapstructure:"timeout"`     // HTTP timeout in seconds (default 10)
	Insecure   bool     `mapstructure:"insecure"`    // Skip TLS certificate verification (for self-signed certs)
}

// ProviderConfig defines a known upstream AI provider.
// Policies reference providers by name instead of repeating connection details.
type ProviderConfig struct {
	UpstreamURL string            `mapstructure:"upstream_url" json:"upstream_url"`
	APIKeyEnv   string            `mapstructure:"api_key_env" json:"api_key_env"`
	BaseURLEnv  string            `mapstructure:"base_url_env" json:"base_url_env,omitempty"` // Env var for base URL override (e.g. "OPENAI_BASE_URL")
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

// LedgerConfig controls per-session token tracking to .tokenomics/.
type LedgerConfig struct {
	Enabled bool   `mapstructure:"enabled"` // Enable session ledger (default false)
	Dir     string `mapstructure:"dir"`     // Output directory (default ".tokenomics")
	Memory  bool   `mapstructure:"memory"`  // Record conversation content (default true)
}

func Load(cfgFile string) (*Config, error) {
	// Use a fresh viper instance to avoid state issues from previous calls
	v := viper.New()

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(filepath.Join(".", ".tokenomics"))  // check .tokenomics/ first
		v.AddConfigPath(".")                                 // then current dir (backward compat)
		v.AddConfigPath("$HOME/.tokenomics")                 // then home dir
	}

	// Defaults
	v.SetDefault("dir", "")                                  // empty = derive to ".tokenomics"
	v.SetDefault("server.http_port", 8080)
	v.SetDefault("server.https_port", 8443)
	v.SetDefault("server.tls.enabled", true)
	v.SetDefault("server.tls.auto_gen", true)
	v.SetDefault("server.tls.cert_dir", "./certs")
	v.SetDefault("server.upstream_url", "https://api.openai.com")
	v.SetDefault("storage.db_path", "")                     // empty = derive from dir at use time
	v.SetDefault("session.backend", "memory")
	v.SetDefault("session.redis.addr", "localhost:6379")
	v.SetDefault("session.redis.db", 0)
	v.SetDefault("security.hash_key_env", "TOKENOMICS_HASH_KEY")
	v.SetDefault("security.encryption_key_env", "TOKENOMICS_ENCRYPTION_KEY")
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
	v.SetDefault("logging.proxy_log_file", "proxy.log")
	v.SetDefault("ledger.enabled", true)                    // Enable session ledger by default
	v.SetDefault("ledger.dir", "")                          // empty = derive from dir at use time
	v.SetDefault("ledger.memory", true)                     // Record conversation content

	v.SetEnvPrefix("TOKENOMICS")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Explicitly bind environment variables that may not have file-based defaults
	v.BindEnv("default_provider", "TOKENOMICS_DEFAULT_PROVIDER")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Resolve derived paths
	if cfg.Dir == "" {
		cfg.Dir = ".tokenomics"
	}
	// Convert to absolute path to avoid working directory issues
	if !filepath.IsAbs(cfg.Dir) {
		absDir, err := filepath.Abs(cfg.Dir)
		if err == nil {
			cfg.Dir = absDir
		}
	}
	// If db_path is empty or still the old default, resolve it to use cfg.Dir
	if cfg.Storage.DBPath == "" || cfg.Storage.DBPath == "./tokenomics.db" {
		cfg.Storage.DBPath = filepath.Join(cfg.Dir, "tokenomics.db")
	}
	if cfg.Ledger.Dir == "" {
		cfg.Ledger.Dir = cfg.Dir
	}

	// Ensure .tokenomics directory and bootstrap files exist
	if err := EnsureDir(cfg.Dir); err != nil {
		return nil, fmt.Errorf("ensure directory: %w", err)
	}

	// Load providers with priority order:
	// 1. LoadProviders() returns defaults + file overrides
	// 2. Inline cfg.Providers (from config.yaml) override everything
	providers, err := LoadProviders("")
	if err == nil && len(providers) > 0 {
		// Initialize map if nil
		if cfg.Providers == nil {
			cfg.Providers = make(map[string]ProviderConfig)
		}
		// Start with defaults + file overrides
		for k, v := range providers {
			if _, exists := cfg.Providers[k]; !exists {
				// Only add if not already in cfg.Providers (inline takes priority)
				cfg.Providers[k] = v
			}
		}
	}
	// If cfg.Providers is still empty, populate with defaults/file
	if len(cfg.Providers) == 0 && len(providers) > 0 {
		cfg.Providers = providers
	}

	return &cfg, nil
}

// LoadProviders loads provider definitions from a providers.yaml file.
// If providersFile is empty, searches standard paths (., $HOME/.tokenomics).
// Returns default built-in providers if no file is found.
// External file entries override defaults.
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

	// Start with defaults
	result := copyDefaultProviders()

	if err := pv.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// No file found - return defaults
			return result, nil
		}
		return nil, err
	}

	// File found - unmarshal and merge over defaults
	var wrapper struct {
		Providers map[string]ProviderConfig `mapstructure:"providers"`
	}
	if err := pv.Unmarshal(&wrapper); err != nil {
		return nil, fmt.Errorf("parse providers config: %w", err)
	}

	// File values override defaults
	for k, v := range wrapper.Providers {
		result[k] = v
	}

	return result, nil
}

// GetProvider returns the provider config by name, if it exists.
func (c *Config) GetProvider(name string) (ProviderConfig, bool) {
	if c.Providers == nil {
		return ProviderConfig{}, false
	}
	p, ok := c.Providers[name]
	return p, ok
}

// EnsureDir creates the .tokenomics directory and bootstrap files if they don't exist.
func EnsureDir(dir string) error {
	// Create directory
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Create .gitignore if missing
	gitignorePath := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		gitignoreContent := "# tokenomics database contains encrypted tokens - do not commit\ntokenomics.db\n"
		if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0o644); err != nil {
			return fmt.Errorf("write .gitignore: %w", err)
		}
	}

	// Create default config.yaml if no config exists in that directory
	// and no explicit config was provided (we only create if the directory was just made)
	configPath := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Only create default config if this is a fresh directory (no other config found elsewhere)
		// This is safe because Load() only calls EnsureDir after successful config load
		defaultConfig := `# Tokenomics configuration
# Full reference: https://github.com/rickcrawford/tokenomics/docs/CONFIGURATION.md

logging:
  level: info
  format: json
  proxy_log_file: proxy.log
`
		if err := os.WriteFile(configPath, []byte(defaultConfig), 0o644); err != nil {
			return fmt.Errorf("write default config: %w", err)
		}
	}

	return nil
}
