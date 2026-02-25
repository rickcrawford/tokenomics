package cmd

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/spf13/cobra"
)

var providerCmd = &cobra.Command{
	Use:   "provider",
	Short: "Manage and inspect configured providers",
}

var providerListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured providers and their status",
	Long: `Shows every provider from providers.yaml (or inline config) with its
upstream URL, auth scheme, API key environment variable, and whether
the key is currently set in the environment.`,
	Example: `  tokenomics provider list
  tokenomics provider list --output json`,
	RunE: runProviderList,
}

var providerTestCmd = &cobra.Command{
	Use:   "test [provider...]",
	Short: "Test connectivity to one or more providers",
	Long: `Sends a lightweight request to each provider's upstream URL to verify
reachability and valid credentials. Tests all providers if none specified.`,
	Example: `  tokenomics provider test openai
  tokenomics provider test openai anthropic
  tokenomics provider test`,
	RunE: runProviderTest,
}

var providerModelsCmd = &cobra.Command{
	Use:   "models [provider]",
	Short: "List known models for a provider",
	Long:  `Shows the models configured in providers.yaml for the named provider. Lists all providers and their models if none specified.`,
	Example: `  tokenomics provider models openai
  tokenomics provider models`,
	RunE: runProviderModels,
}

var (
	providerOutputFmt string
	providerInsecure  bool
)

func init() {
	providerListCmd.Flags().StringVar(&providerOutputFmt, "output", "table", "output format (table, json)")
	providerTestCmd.Flags().BoolVar(&providerInsecure, "insecure", false, "skip TLS verification")

	providerCmd.AddCommand(providerListCmd, providerTestCmd, providerModelsCmd)
	rootCmd.AddCommand(providerCmd)
}

type providerStatus struct {
	Name        string `json:"name"`
	UpstreamURL string `json:"upstream_url"`
	AuthScheme  string `json:"auth_scheme"`
	APIKeyEnv   string `json:"api_key_env"`
	KeySet      bool   `json:"key_set"`
	ChatPath    string `json:"chat_path,omitempty"`
	ModelCount  int    `json:"model_count"`
}

func runProviderList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.Providers) == 0 {
		fmt.Println("No providers configured.")
		fmt.Println("Add providers to providers.yaml or under the providers key in config.yaml.")
		return nil
	}

	statuses := buildProviderStatuses(cfg.Providers)

	if providerOutputFmt == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(statuses)
	}

	// Table output
	fmt.Printf("%-16s %-40s %-8s %-28s %-6s %s\n",
		"NAME", "UPSTREAM", "AUTH", "API KEY ENV", "KEY?", "MODELS")
	fmt.Printf("%-16s %-40s %-8s %-28s %-6s %s\n",
		"----", "--------", "----", "-----------", "----", "------")

	for _, s := range statuses {
		keyStatus := "no"
		if s.KeySet {
			keyStatus = "yes"
		}
		if s.APIKeyEnv == "" {
			keyStatus = "n/a"
		}
		upstream := s.UpstreamURL
		if len(upstream) > 40 {
			upstream = upstream[:37] + "..."
		}
		scheme := s.AuthScheme
		if scheme == "" {
			scheme = "bearer"
		}
		fmt.Printf("%-16s %-40s %-8s %-28s %-6s %d\n",
			s.Name, upstream, scheme, s.APIKeyEnv, keyStatus, s.ModelCount)
	}

	return nil
}

func runProviderTest(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.Providers) == 0 {
		fmt.Println("No providers configured.")
		return nil
	}

	// Filter to requested providers or test all
	providers := cfg.Providers
	if len(args) > 0 {
		providers = make(map[string]config.ProviderConfig)
		for _, name := range args {
			pc, ok := cfg.Providers[name]
			if !ok {
				fmt.Printf("%-16s  UNKNOWN (not in config)\n", name)
				continue
			}
			providers[name] = pc
		}
	}

	if len(providers) == 0 {
		return nil
	}

	client := &http.Client{Timeout: 10 * time.Second}
	if providerInsecure {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	names := sortedKeys(providers)

	hasFailure := false
	for _, name := range names {
		pc := providers[name]
		result := testProvider(client, name, pc)
		status := "OK"
		detail := fmt.Sprintf("%dms", result.latencyMs)
		if !result.reachable {
			status = "FAIL"
			detail = result.err
			hasFailure = true
		} else if !result.authValid {
			status = "AUTH"
			detail = "reachable but credentials invalid or missing"
			hasFailure = true
		}
		fmt.Printf("%-16s  %-4s  %s\n", name, status, detail)
	}

	if hasFailure {
		return fmt.Errorf("one or more providers failed connectivity test")
	}
	return nil
}

type testResult struct {
	reachable bool
	authValid bool
	latencyMs int64
	err       string
}

func testProvider(client *http.Client, name string, pc config.ProviderConfig) testResult {
	// Build a minimal request to the provider's base URL
	testURL := strings.TrimRight(pc.UpstreamURL, "/")

	// Skip providers with placeholder URLs
	if strings.Contains(testURL, "{") {
		return testResult{err: "skipped (URL contains placeholders)"}
	}

	// Try the models endpoint for OpenAI-compatible APIs, or just HEAD the base URL
	modelsURL := testURL + "/v1/models"
	if pc.ChatPath != "" && !strings.Contains(pc.ChatPath, "/v1/") {
		// Non-standard API, just check the base URL
		modelsURL = testURL
	}

	req, err := http.NewRequest("GET", modelsURL, nil)
	if err != nil {
		return testResult{err: fmt.Sprintf("bad URL: %v", err)}
	}

	// Apply auth
	realKey := os.Getenv(pc.APIKeyEnv)
	if realKey != "" {
		scheme := pc.AuthScheme
		if scheme == "" {
			scheme = "bearer"
		}
		switch scheme {
		case "header":
			header := pc.AuthHeader
			if header == "" {
				header = "Authorization"
			}
			req.Header.Set(header, realKey)
		case "query":
			q := req.URL.Query()
			q.Set("key", realKey)
			req.URL.RawQuery = q.Encode()
		default:
			req.Header.Set("Authorization", "Bearer "+realKey)
		}
	}

	// Add provider headers
	for k, v := range pc.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return testResult{err: fmt.Sprintf("unreachable: %v", err), latencyMs: latency}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return testResult{reachable: true, authValid: false, latencyMs: latency}
	}

	return testResult{reachable: true, authValid: true, latencyMs: latency}
}

func runProviderModels(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.Providers) == 0 {
		fmt.Println("No providers configured.")
		return nil
	}

	if len(args) > 0 {
		// Show models for specific provider
		name := args[0]
		pc, ok := cfg.Providers[name]
		if !ok {
			return fmt.Errorf("provider %q not found in config", name)
		}
		if len(pc.Models) == 0 {
			fmt.Printf("%s: no models configured\n", name)
			return nil
		}
		for _, m := range pc.Models {
			fmt.Println(m)
		}
		return nil
	}

	// Show all providers and their models
	names := sortedKeys(cfg.Providers)
	for _, name := range names {
		pc := cfg.Providers[name]
		if len(pc.Models) == 0 {
			fmt.Printf("%s: (none)\n", name)
			continue
		}
		fmt.Printf("%s:\n", name)
		for _, m := range pc.Models {
			fmt.Printf("  %s\n", m)
		}
	}

	return nil
}

func buildProviderStatuses(providers map[string]config.ProviderConfig) []providerStatus {
	names := sortedKeys(providers)
	statuses := make([]providerStatus, 0, len(names))
	for _, name := range names {
		pc := providers[name]
		keySet := false
		if pc.APIKeyEnv != "" {
			keySet = os.Getenv(pc.APIKeyEnv) != ""
		}
		statuses = append(statuses, providerStatus{
			Name:        name,
			UpstreamURL: pc.UpstreamURL,
			AuthScheme:  pc.AuthScheme,
			APIKeyEnv:   pc.APIKeyEnv,
			KeySet:      keySet,
			ChatPath:    pc.ChatPath,
			ModelCount:  len(pc.Models),
		})
	}
	return statuses
}

func sortedKeys(m map[string]config.ProviderConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
