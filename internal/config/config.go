package config

import (
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Storage  StorageConfig  `mapstructure:"storage"`
	Session  SessionConfig  `mapstructure:"session"`
	Security SecurityConfig `mapstructure:"security"`
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
	HashKeyEnv string `mapstructure:"hash_key_env"`
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

	return &cfg, nil
}
