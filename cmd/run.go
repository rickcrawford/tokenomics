package cmd

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [flags] COMMAND [ARGS...]",
	Short: "Start proxy and execute a command with proxy environment",
	Long: `Starts the tokenomics proxy in the background and executes the given command
with environment variables configured to use the proxy. The proxy is automatically
shut down when the command exits.

Unlike 'start', which runs a persistent daemon, 'run' creates an ephemeral proxy
that lives only for the duration of the command. It defaults to plain HTTP on
localhost since traffic never leaves the machine.

If --proxy-url or $TOKENOMICS_PROXY_URL is set, uses a remote proxy instead
of starting a local one.

The -- separator is optional. Use it only if your command has flags that conflict
with tokenomics flags.`,
	Example: `  tokenomics run claude "What is AI?"
  tokenomics run --provider anthropic -- python my_script.py
  TOKENOMICS_KEY=tkn_abc123 tokenomics run claude
  TOKENOMICS_PROXY_URL=https://proxy.example.com:8443 tokenomics run claude "test"
  tokenomics run --proxy-url https://proxy.company.com claude "test"`,
	RunE: runRun,
}

var (
	runToken    string
	runProxyURL string
	runHost     string
	runPort     int
	runTLS      bool
	runInsecure bool
	runProvider string
	runEnvKey   string
	runEnvBase  string
)

func init() {
	runCmd.Flags().StringVar(&runToken, "token", "", "wrapper token (read from $TOKENOMICS_KEY if not provided)")
	runCmd.Flags().StringVar(&runProxyURL, "proxy-url", "", "remote proxy URL (read from $TOKENOMICS_PROXY_URL if not provided; if set, uses remote proxy instead of starting local)")
	runCmd.Flags().StringVar(&runHost, "host", "localhost", "proxy hostname (only used if starting local proxy)")
	runCmd.Flags().IntVar(&runPort, "port", 8080, "proxy port (only used if starting local proxy)")
	runCmd.Flags().BoolVar(&runTLS, "tls", false, "use HTTPS (default false for run, traffic is localhost only)")
	runCmd.Flags().BoolVar(&runInsecure, "insecure", false, "skip TLS verification")
	runCmd.Flags().StringVar(&runProvider, "provider", "generic", "target provider (generic, anthropic, azure, gemini, custom)")
	runCmd.Flags().StringVar(&runEnvKey, "env-key", "", "custom env var name for the API key")
	runCmd.Flags().StringVar(&runEnvBase, "env-base-url", "", "custom env var name for the base URL")

	rootCmd.AddCommand(runCmd)
}

// defaultCLIMaps defines hard-coded mappings for common CLIs
var defaultCLIMaps = map[string]string{
	"claude":     "anthropic",
	"anthropic":  "anthropic",
	"python":     "generic",
	"node":       "generic",
	"curl":       "generic",
	"openai":     "generic",
	"openai-cli": "generic",
	"azure":      "azure",
	"gemini":     "gemini",
	"gcloud":     "gemini",
}

// detectProviderFromCLI looks up the provider name for a given CLI from
// hard-coded defaults first, then config file overrides if present
func detectProviderFromCLI(cliName, cfgFile string) string {
	// Check hard-coded defaults first
	if provider, exists := defaultCLIMaps[cliName]; exists {
		return provider
	}

	// If no config file specified, try to find it in standard locations
	if cfgFile == "" {
		// Try .tokenomics/config.yaml first
		if _, err := os.Stat(filepath.Join(".tokenomics", "config.yaml")); err == nil {
			cfgFile = filepath.Join(".tokenomics", "config.yaml")
		} else if _, err := os.Stat("config.yaml"); err == nil {
			cfgFile = "config.yaml"
		} else if _, err := os.Stat("config.yml"); err == nil {
			cfgFile = "config.yml"
		}
		// config.Load will handle home directory defaults if cfgFile is still empty
	}

	// Check config file for overrides
	cfg, err := config.Load(cfgFile)
	if err != nil || cfg == nil {
		return ""
	}

	// Check if there's a mapping override for this CLI name
	if provider, exists := cfg.CLIMaps[cliName]; exists {
		return provider
	}

	return ""
}

func runRun(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command to execute\n\nUsage: tokenomics run [flags] COMMAND [ARGS...]\nExample: tokenomics run claude \"your prompt\"")
	}

	// Read token from env var if not provided
	if runToken == "" {
		runToken = os.Getenv("TOKENOMICS_KEY")
	}
	if runToken == "" {
		return fmt.Errorf("no token provided: use --token flag or set $TOKENOMICS_KEY")
	}

	// Read proxy URL from env var if not provided
	if runProxyURL == "" {
		runProxyURL = os.Getenv("TOKENOMICS_PROXY_URL")
	}

	// Auto-detect provider from CLI name if not explicitly set
	if runProvider == "generic" {
		cliName := args[0]
		if mappedProvider := detectProviderFromCLI(cliName, cfgFile); mappedProvider != "" {
			runProvider = mappedProvider
		}
	}

	var serveCmd *exec.Cmd

	// Determine scheme and base URL
	scheme := "http"
	proxyPort := runPort
	if runTLS {
		scheme = "https"
		// Use port 8443 for HTTPS if default port 8080 is used
		if proxyPort == 8080 {
			proxyPort = 8443
		}
	}

	// If proxy URL is provided, use remote proxy; otherwise start local proxy
	baseURL := runProxyURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("%s://%s:%d", scheme, runHost, proxyPort)

		// Build serve args. Override the port via environment so we control
		// exactly which port the ephemeral proxy binds to.
		serveArgs := []string{"serve", "--config", cfgFile, "--db", dbPath}
		serveCmd = exec.Command(os.Args[0], serveArgs...)
		serveCmd.Stdout = os.Stderr
		serveCmd.Stderr = os.Stderr
		// Put serve subprocess in its own process group so it does not
		// receive Ctrl+C (SIGINT) from the terminal. We send signals
		// explicitly during cleanup to avoid a double-signal that would
		// bypass Go's graceful shutdown handler.
		setProcessGroup(serveCmd)
		// Tell serve which port to bind, overriding config defaults.
		// Always enable both HTTP and HTTPS for ephemeral proxy
		// Preserve all parent environment variables to ensure API key env vars are available
		serveEnv := append(os.Environ(),
			fmt.Sprintf("TOKENOMICS_SERVER_HTTP_PORT=%d", 8080),
			fmt.Sprintf("TOKENOMICS_DEFAULT_PROVIDER=%s", runProvider),
		)
		// Enable debug output if requested
		if os.Getenv("TOKENOMICS_DEBUG_ENV") == "1" {
			serveEnv = append(serveEnv, "TOKENOMICS_DEBUG_ENV=1")
		}
		serveCmd.Env = serveEnv

		if err := serveCmd.Start(); err != nil {
			return fmt.Errorf("start proxy: %w", err)
		}
	}

	// shutdownServe sends SIGTERM to the serve subprocess and waits for
	// it to exit gracefully. If the process does not exit within 12
	// seconds, it is force-killed.
	shutdownServe := func() {
		if serveCmd == nil || serveCmd.Process == nil {
			return
		}
		// Use SIGTERM so the serve process runs its graceful shutdown
		// path (drain HTTP servers, close DB, etc.).
		if err := terminateProcess(serveCmd.Process); err != nil {
			// Process may already be dead.
			_ = serveCmd.Wait()
			return
		}
		// Wait for the process in a goroutine so we can enforce a timeout.
		done := make(chan struct{})
		go func() {
			_ = serveCmd.Wait()
			close(done)
		}()
		select {
		case <-done:
			// Exited cleanly.
		case <-time.After(12 * time.Second):
			// Force kill after timeout.
			_ = killProcess(serveCmd.Process)
			<-done
		}
	}

	// Intercept signals so we can tear down the serve subprocess
	// ourselves instead of relying on the OS to propagate the signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Cleanup: ensure the local proxy is shut down when we return.
	defer shutdownServe()

	healthURL := fmt.Sprintf("%s/ping", baseURL)

	// Create HTTP client with optional TLS skip
	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	if runTLS && runInsecure {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	// Poll for readiness (30 attempts, ~3 seconds)
	readyErr := fmt.Errorf("proxy failed to start within 3 seconds")
	for i := 0; i < 30; i++ {
		resp, err := client.Get(healthURL)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			readyErr = nil
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}

	if readyErr != nil {
		return readyErr
	}

	// Build environment variables for the proxy
	pairs := ResolveEnvPairs(runProvider, runToken, baseURL, runEnvKey, runEnvBase)

	if runInsecure {
		pairs = append(pairs, EnvPair{"NODE_TLS_REJECT_UNAUTHORIZED", "0"})
	}


	// Prepare environment: inherit current env but remove any vars we're about to override
	keysToSet := make(map[string]bool)
	for _, p := range pairs {
		keysToSet[p.Key] = true
	}

	env := []string{}
	for _, e := range os.Environ() {
		// Parse "KEY=VALUE" format
		if idx := strings.Index(e, "="); idx > 0 {
			key := e[:idx]
			if !keysToSet[key] {
				env = append(env, e)
			}
		}
	}

	// Add proxy config (overriding any inherited values)
	for _, p := range pairs {
		env = append(env, fmt.Sprintf("%s=%s", p.Key, p.Value))
	}

	// Execute user command with proxy environment
	userCmd := exec.Command(args[0], args[1:]...)
	userCmd.Env = env
	userCmd.Stdin = os.Stdin
	userCmd.Stdout = os.Stdout
	userCmd.Stderr = os.Stderr

	if err := userCmd.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	// Wait for either the user command to finish or a signal.
	cmdDone := make(chan error, 1)
	go func() {
		cmdDone <- userCmd.Wait()
	}()

	select {
	case err := <-cmdDone:
		// User command finished normally (or with its own error).
		// Proxy cleanup happens in defer (shutdownServe).
		return err
	case <-sigCh:
		// We caught Ctrl+C / SIGTERM. The user command (in our process
		// group) also received the signal. Wait briefly for it to exit.
		select {
		case err := <-cmdDone:
			return err
		case <-time.After(5 * time.Second):
			_ = userCmd.Process.Kill()
			return fmt.Errorf("command did not exit after signal")
		}
	}
}
