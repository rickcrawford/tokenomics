package cmd

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/exec"
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

// detectProviderFromCLI looks up the provider name for a given CLI from config
func detectProviderFromCLI(cliName, cfgFile string) string {
	cfg, err := config.Load(cfgFile)
	if err != nil || cfg == nil {
		return ""
	}

	// Check if there's a mapping for this CLI name
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
	if runTLS {
		scheme = "https"
	}

	// If proxy URL is provided, use remote proxy; otherwise start local proxy
	baseURL := runProxyURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("%s://%s:%d", scheme, runHost, runPort)

		// Build serve args. Override the port via environment so we control
		// exactly which port the ephemeral proxy binds to.
		serveArgs := []string{"serve", "--config", cfgFile, "--db", dbPath}
		serveCmd = exec.Command(os.Args[0], serveArgs...)
		serveCmd.Stdout = os.Stderr
		serveCmd.Stderr = os.Stderr
		// Tell serve which port to bind, overriding config defaults.
		serveCmd.Env = append(os.Environ(),
			fmt.Sprintf("TOKENOMICS_SERVER_HTTP_PORT=%d", runPort),
		)
		if !runTLS {
			// Disable TLS for local-only traffic.
			serveCmd.Env = append(serveCmd.Env, "TOKENOMICS_SERVER_TLS_ENABLED=false")
		}

		if err := serveCmd.Start(); err != nil {
			return fmt.Errorf("start proxy: %w", err)
		}
	}

	// Cleanup function to ensure local proxy is shut down (no-op if using remote)
	defer func() {
		if serveCmd != nil && serveCmd.Process != nil {
			interruptProcess(serveCmd.Process)
			serveCmd.Wait()
		}
	}()

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

	// Prepare environment: inherit current env and add proxy config
	env := os.Environ()
	for _, p := range pairs {
		env = append(env, fmt.Sprintf("%s=%s", p.Key, p.Value))
	}

	// Execute user command with proxy environment
	userCmd := exec.Command(args[0], args[1:]...)
	userCmd.Env = env
	userCmd.Stdin = os.Stdin
	userCmd.Stdout = os.Stdout
	userCmd.Stderr = os.Stderr

	err := userCmd.Run()

	// Proxy cleanup happens in defer

	return err
}
