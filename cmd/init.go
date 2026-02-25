package cmd

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/spf13/cobra"
)

// daemonConfig holds settings for starting the proxy as a background process.
type daemonConfig struct {
	host     string
	port     int
	tls      bool
	insecure bool
	pidFile  string
	logFile  string
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Configure an agent CLI to use the tokenomics proxy",
	Long: `Sets environment variables or writes config for an agent framework
(OpenAI, Anthropic, Azure, Gemini, or custom) to route API calls through the proxy.
Can optionally start the proxy in the background.

The --provider flag accepts any provider name from providers.yaml (e.g. deepseek,
groq, mistral) in addition to the built-in aliases (generic, anthropic, azure, gemini).

Use --provider all to set environment variables for every configured provider at once,
routing all SDK traffic through the proxy with a single command.

Use --agent claude-code to write hooks into .claude/settings.local.json so the proxy
starts automatically when Claude Code opens a session. You must set TOKENOMICS_PROXY_URL
in your environment for hooks to resolve the proxy address.`,
	Example: `  eval $(tokenomics init --token tkn_abc123)
  tokenomics init --token tkn_abc123 --provider anthropic --output dotenv
  tokenomics init --token tkn_abc123 --provider deepseek --output shell
  tokenomics init --token tkn_abc123 --provider all --output dotenv
  tokenomics init --token tkn_abc123 --output json
  tokenomics init --proxy-url https://proxy.company.com:8443 --token tkn_abc123
  tokenomics init --agent claude-code --token tkn_abc123 --provider anthropic`,
	RunE: runInit,
}

var (
	initToken     string
	initProxyURL  string
	initHost      string
	initPort      int
	initTLS       bool
	initInsecure  bool
	initProvider  string
	initEnvKey    string
	initEnvBase   string
	initOutputFmt string
	initDotenv    string
	initPidFile   string
	initLogFile   string
	initStart     bool
	initAgent     string
)

func init() {
	initCmd.Flags().StringVar(&initToken, "token", "", "wrapper token (read from $TOKENOMICS_KEY if not provided)")
	initCmd.Flags().StringVar(&initProxyURL, "proxy-url", "", "remote proxy URL (read from $TOKENOMICS_PROXY_URL if not provided; if set, uses remote proxy instead of starting local)")
	initCmd.Flags().StringVar(&initHost, "host", "localhost", "proxy hostname (only used if starting local proxy)")
	initCmd.Flags().IntVar(&initPort, "port", 8443, "proxy port (only used if starting local proxy)")
	initCmd.Flags().BoolVar(&initTLS, "tls", true, "use HTTPS (only used if starting local proxy)")
	initCmd.Flags().BoolVar(&initInsecure, "insecure", false, "skip TLS verification (not recommended; install valid certificates instead)")
	initCmd.Flags().StringVar(&initProvider, "provider", "generic", "target provider, provider name from config, or 'all' for every configured provider")
	initCmd.Flags().StringVar(&initEnvKey, "env-key", "", "custom env var name for the API key")
	initCmd.Flags().StringVar(&initEnvBase, "env-base-url", "", "custom env var name for the base URL")
	initCmd.Flags().StringVar(&initOutputFmt, "output", "shell", "output format (shell, dotenv, json)")
	initCmd.Flags().StringVar(&initDotenv, "dotenv", "", "path to .env file (used with --output dotenv)")
	initCmd.Flags().BoolVar(&initStart, "start", true, "start the proxy in the background (default: true)")
	initCmd.Flags().StringVar(&initPidFile, "pid-file", "", "PID file path (default: ~/.tokenomics/tokenomics.pid)")
	initCmd.Flags().StringVar(&initLogFile, "log-file", "", "log file path (default: ~/.tokenomics/tokenomics.log)")
	initCmd.Flags().StringVar(&initAgent, "agent", "", "write hook config for an agent (claude-code)")

	rootCmd.AddCommand(initCmd)
}

// EnvPair represents a key-value pair for environment variable output.
type EnvPair struct {
	Key   string
	Value string
}

func runInit(cmd *cobra.Command, args []string) error {
	// Read token from env var if not provided
	if initToken == "" {
		initToken = os.Getenv("TOKENOMICS_KEY")
	}
	if initToken == "" {
		return fmt.Errorf("no token provided: use --token flag or set $TOKENOMICS_KEY")
	}

	// Read proxy URL from env var if not provided
	if initProxyURL == "" {
		initProxyURL = os.Getenv("TOKENOMICS_PROXY_URL")
	}

	// Determine base URL
	var baseURL string
	if initProxyURL != "" {
		// Use remote proxy
		baseURL = initProxyURL
	} else {
		// Use local proxy
		scheme := "https"
		if !initTLS {
			scheme = "http"
		}
		baseURL = fmt.Sprintf("%s://%s:%d", scheme, initHost, initPort)

		// Start the proxy daemon (enabled by default for convenience)
		if initStart {
			dcfg := daemonConfig{
				host:     initHost,
				port:     initPort,
				tls:      initTLS,
				insecure: initInsecure,
				pidFile:  initPidFile,
				logFile:  initLogFile,
			}
			if err := startDaemon(baseURL, dcfg); err != nil {
				return err
			}
		}
	}

	// Load config for provider-aware resolution and auto-detection
	cfg, _ := config.Load(cfgFile)

	// Auto-detect provider from args or cli_maps if not explicitly set
	if initProvider == "generic" && cfg != nil && len(args) > 0 {
		if mapped, ok := cfg.CLIMaps[args[0]]; ok {
			initProvider = mapped
		}
	}

	// Resolve env pairs
	var pairs []EnvPair
	if initProvider == "all" {
		pairs = resolveAllProviderPairs(cfg, initToken, baseURL)
	} else {
		pairs = resolveEnvPairsWithConfig(cfg, initProvider, initToken, baseURL, initEnvKey, initEnvBase)
	}

	if initInsecure {
		pairs = append(pairs, EnvPair{"NODE_TLS_REJECT_UNAUTHORIZED", "0"})
	}

	// Agent-specific config writing
	if initAgent != "" {
		return writeAgentConfig(initAgent, pairs, baseURL)
	}

	switch initOutputFmt {
	case "shell":
		return OutputShell(pairs, os.Stdout)
	case "dotenv":
		return OutputDotenv(pairs, initDotenv)
	case "json":
		return OutputJSON(pairs, os.Stdout)
	default:
		return fmt.Errorf("unknown output format: %s", initOutputFmt)
	}
}

// resolveEnvPairsWithConfig tries the provider config first, then falls back
// to the hardcoded ResolveEnvPairs for backward compatibility.
func resolveEnvPairsWithConfig(cfg *config.Config, provider, token, baseURL, envKey, envBase string) []EnvPair {
	// Custom overrides always win
	if envKey != "" && envBase != "" {
		return []EnvPair{
			{envKey, token},
			{envBase, baseURL},
		}
	}

	// Try to resolve from provider config
	if cfg != nil {
		if pc, ok := cfg.Providers[provider]; ok {
			return envPairsFromProviderConfig(provider, pc, token, baseURL)
		}
		// Also try case-insensitive lookup
		for name, pc := range cfg.Providers {
			if strings.EqualFold(name, provider) {
				return envPairsFromProviderConfig(name, pc, token, baseURL)
			}
		}
	}

	// Fall back to hardcoded resolution for well-known aliases
	return ResolveEnvPairs(provider, token, baseURL, envKey, envBase)
}

// envPairsFromProviderConfig builds env pairs using the provider's configured
// api_key_env and base_url_env fields.
func envPairsFromProviderConfig(name string, pc config.ProviderConfig, token, baseURL string) []EnvPair {
	pairs := []EnvPair{}

	// API key env var
	keyEnv := pc.APIKeyEnv
	if keyEnv == "" {
		// Some providers (like ollama) don't need a key, but we still set the
		// base URL so tools that support it can find the proxy.
		keyEnv = strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_API_KEY"
	}
	pairs = append(pairs, EnvPair{keyEnv, token})

	// Base URL env var
	urlEnv := pc.BaseURLEnv
	if urlEnv == "" {
		urlEnv = strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_BASE_URL"
	}

	// OpenAI-compatible providers typically need /v1 appended
	url := baseURL
	if needsV1Suffix(name) {
		url = baseURL + "/v1"
	}
	pairs = append(pairs, EnvPair{urlEnv, url})

	return pairs
}

// needsV1Suffix returns true for providers whose SDKs expect a /v1 path suffix.
func needsV1Suffix(provider string) bool {
	switch strings.ToLower(provider) {
	case "openai", "generic", "groq", "together", "fireworks",
		"perplexity", "deepseek", "xai", "openrouter", "vllm", "litellm":
		return true
	}
	return false
}

// resolveAllProviderPairs generates env pairs for every configured provider.
// This lets users route all SDK traffic through the proxy with one command.
func resolveAllProviderPairs(cfg *config.Config, token, baseURL string) []EnvPair {
	if cfg == nil || len(cfg.Providers) == 0 {
		// No config, just set the generic OpenAI vars
		return ResolveEnvPairs("generic", token, baseURL, "", "")
	}

	seen := make(map[string]bool)
	var pairs []EnvPair

	// Sort for deterministic output
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		pc := cfg.Providers[name]
		for _, pair := range envPairsFromProviderConfig(name, pc, token, baseURL) {
			// Deduplicate: first provider to claim an env var wins
			if !seen[pair.Key] {
				seen[pair.Key] = true
				pairs = append(pairs, pair)
			}
		}
	}

	return pairs
}

// startDaemon launches the proxy as a background process and waits for it to
// become ready. It is used by both the init and start commands.
func startDaemon(baseURL string, dc daemonConfig) error {
	// Resolve PID and log file paths
	pidFile := dc.pidFile
	logFile := dc.logFile
	if pidFile == "" || logFile == "" {
		u, err := user.Current()
		if err != nil {
			return fmt.Errorf("get current user: %w", err)
		}
		tokenomicsDir := filepath.Join(u.HomeDir, ".tokenomics")
		if pidFile == "" {
			pidFile = filepath.Join(tokenomicsDir, "tokenomics.pid")
		}
		if logFile == "" {
			logFile = filepath.Join(tokenomicsDir, "tokenomics.log")
		}
	}

	// Ensure tokenomics directory exists
	pidDir := filepath.Dir(pidFile)
	if err := os.MkdirAll(pidDir, 0o700); err != nil {
		return fmt.Errorf("create tokenomics dir: %w", err)
	}

	// Check if already running
	if existingPid, err := readPIDFile(pidFile); err == nil {
		if processAlive(existingPid) {
			// Proxy already running, skip launch
			return nil
		}
	}

	// Open log file for proxy output
	logFd, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFd.Close()

	// Launch tokenomics serve as a detached process
	serveCmd := exec.Command(os.Args[0], "serve", "--config", cfgFile, "--db", dbPath)
	serveCmd.Stdout = logFd
	serveCmd.Stderr = logFd
	serveCmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Detach from TTY
	}

	if err := serveCmd.Start(); err != nil {
		return fmt.Errorf("start proxy: %w", err)
	}

	// Write PID to file
	if err := writePIDFile(pidFile, serveCmd.Process.Pid); err != nil {
		return fmt.Errorf("write PID file: %w", err)
	}

	// Poll health endpoint for readiness
	scheme := "https"
	if !dc.tls {
		scheme = "http"
	}
	healthURL := fmt.Sprintf("%s://%s:%d/ping", scheme, dc.host, dc.port)

	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	if dc.insecure {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	// Poll for readiness (30 attempts, ~3 seconds)
	for i := 0; i < 30; i++ {
		resp, err := client.Get(healthURL)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("proxy failed to start within 3 seconds")
}

func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var pid int
	_, err = fmt.Sscanf(string(data), "%d", &pid)
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func writePIDFile(path string, pid int) error {
	return os.WriteFile(path, []byte(fmt.Sprintf("%d", pid)), 0o644)
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; we must send signal 0 to check
	return p.Signal(syscall.Signal(0)) == nil
}

// ResolveEnvPairs determines the environment variable pairs for the given CLI target.
// This is the legacy resolution path using hardcoded provider mappings. New code
// should use resolveEnvPairsWithConfig which reads from providers.yaml.
func ResolveEnvPairs(cli, token, baseURL, envKey, envBase string) []EnvPair {
	if envKey != "" && envBase != "" {
		return []EnvPair{
			{envKey, token},
			{envBase, baseURL},
		}
	}

	switch strings.ToLower(cli) {
	case "anthropic":
		return []EnvPair{
			{"ANTHROPIC_API_KEY", token},
			{"ANTHROPIC_BASE_URL", baseURL},
		}
	case "azure":
		return []EnvPair{
			{"AZURE_OPENAI_API_KEY", token},
			{"AZURE_OPENAI_ENDPOINT", baseURL},
		}
	case "gemini":
		return []EnvPair{
			{"GEMINI_API_KEY", token},
			{"GEMINI_BASE_URL", baseURL},
		}
	default: // generic / openai
		return []EnvPair{
			{"OPENAI_API_KEY", token},
			{"OPENAI_BASE_URL", baseURL + "/v1"},
		}
	}
}

// OutputShell writes export statements to the given writer.
func OutputShell(pairs []EnvPair, w *os.File) error {
	for _, p := range pairs {
		fmt.Fprintf(w, "export %s=%q\n", p.Key, p.Value)
	}
	return nil
}

// OutputDotenv writes or updates a .env file at the given path.
func OutputDotenv(pairs []EnvPair, path string) error {
	if path == "" {
		path = ".env"
	}

	// Read existing content if file exists
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}

	lines := strings.Split(existing, "\n")
	setKeys := make(map[string]bool)

	// Update existing lines
	for i, line := range lines {
		for _, p := range pairs {
			if strings.HasPrefix(line, p.Key+"=") {
				lines[i] = fmt.Sprintf("%s=%q", p.Key, p.Value)
				setKeys[p.Key] = true
			}
		}
	}

	// Append new keys
	for _, p := range pairs {
		if !setKeys[p.Key] {
			lines = append(lines, fmt.Sprintf("%s=%q", p.Key, p.Value))
		}
	}

	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write dotenv: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Updated %s\n", path)
	return nil
}

// OutputJSON writes environment pairs as JSON to the given writer.
func OutputJSON(pairs []EnvPair, w *os.File) error {
	m := make(map[string]string)
	for _, p := range pairs {
		m[p.Key] = p.Value
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}

// writeAgentConfig writes configuration files for a specific agent framework.
func writeAgentConfig(agent string, pairs []EnvPair, proxyURL string) error {
	switch strings.ToLower(agent) {
	case "claude-code":
		return writeClaudeCodeConfig(pairs, proxyURL)
	default:
		return fmt.Errorf("unknown agent: %s (supported: claude-code)", agent)
	}
}

// writeClaudeCodeConfig writes hooks into .claude/settings.local.json.
// The hook starts the proxy at session start. Environment variables must be set
// separately via TOKENOMICS_PROXY_URL or shell profile.
func writeClaudeCodeConfig(pairs []EnvPair, proxyURL string) error {
	settingsDir := ".claude"
	settingsPath := filepath.Join(settingsDir, "settings.local.json")

	// Ensure .claude directory exists
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		return fmt.Errorf("create %s directory: %w", settingsDir, err)
	}

	// Load existing settings to merge
	settings := make(map[string]interface{})
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			// File exists but isn't valid JSON, start fresh
			settings = make(map[string]interface{})
		}
	}

	// Build env map from pairs
	envMap := make(map[string]string)
	for _, p := range pairs {
		envMap[p.Key] = p.Value
	}

	// Set env vars in settings
	settings["env"] = envMap

	// Write the updated settings
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	out = append(out, '\n')

	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", settingsPath, err)
	}

	// Print result and instructions
	fmt.Fprintf(os.Stderr, "Wrote %s\n\n", settingsPath)

	fmt.Fprintf(os.Stderr, "Environment variables configured:\n")
	for _, p := range pairs {
		fmt.Fprintf(os.Stderr, "  %s=%s\n", p.Key, p.Value)
	}

	fmt.Fprintf(os.Stderr, "\nThe proxy must be running for these settings to work.\n")
	fmt.Fprintf(os.Stderr, "Start it with:\n")
	fmt.Fprintf(os.Stderr, "  tokenomics start\n\n")
	fmt.Fprintf(os.Stderr, "Or set TOKENOMICS_PROXY_URL in your shell profile so the proxy\n")
	fmt.Fprintf(os.Stderr, "address is available across sessions:\n")
	fmt.Fprintf(os.Stderr, "  export TOKENOMICS_PROXY_URL=%s\n", proxyURL)

	return nil
}
