package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current proxy and environment status",
	Long: `Displays the state of the tokenomics proxy, configured providers,
relevant environment variables, and active tokens. Useful for verifying
that init or run has configured the environment correctly.`,
	Example: `  tokenomics status
  tokenomics status --config /etc/tokenomics/config.yaml`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	// Proxy state
	fmt.Println("Proxy")
	u, _ := user.Current()
	var pidFile string
	if u != nil {
		pidFile = filepath.Join(u.HomeDir, ".tokenomics", "tokenomics.pid")
	}

	if pidFile != "" {
		if pid, err := readPIDFile(pidFile); err == nil && processAlive(pid) {
			fmt.Printf("  Status:  running (PID %d)\n", pid)
		} else {
			fmt.Println("  Status:  stopped")
		}
	} else {
		fmt.Println("  Status:  unknown")
	}

	if v := os.Getenv("TOKENOMICS_PROXY_URL"); v != "" {
		fmt.Printf("  Remote:  %s\n", v)
	}
	if v := os.Getenv("TOKENOMICS_KEY"); v != "" {
		fmt.Printf("  Token:   %s...%s\n", v[:min(4, len(v))], v[max(0, len(v)-4):])
	}
	fmt.Println()

	// Load config
	cfg, err := config.Load(cfgFile)
	if err != nil {
		fmt.Printf("Config: error loading (%v)\n", err)
		return nil
	}

	// Provider env status
	if len(cfg.Providers) == 0 {
		fmt.Println("Providers: none configured")
		return nil
	}

	fmt.Println("Environment Variables")

	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		pc := cfg.Providers[name]

		// Check API key
		keyStatus := ""
		if pc.APIKeyEnv == "" {
			keyStatus = "(no key needed)"
		} else if v := os.Getenv(pc.APIKeyEnv); v != "" {
			if isProxyToken(v) {
				keyStatus = "proxy token"
			} else {
				keyStatus = "set (direct key)"
			}
		} else {
			keyStatus = "not set"
		}

		// Check base URL
		urlStatus := ""
		urlEnv := pc.BaseURLEnv
		if urlEnv == "" {
			urlEnv = strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_BASE_URL"
		}
		if v := os.Getenv(urlEnv); v != "" {
			urlStatus = v
		} else {
			urlStatus = "not set (will use provider default)"
		}

		fmt.Printf("  %-16s\n", name)
		if pc.APIKeyEnv != "" {
			fmt.Printf("    %-28s %s\n", pc.APIKeyEnv, keyStatus)
		}
		fmt.Printf("    %-28s %s\n", urlEnv, urlStatus)
	}

	// Token store summary
	fmt.Println()
	fmt.Println("Tokens")
	s, err := initStore(cfg)
	if err != nil {
		fmt.Println("  (could not open database)")
		return nil
	}
	defer s.Close()

	records, err := s.List()
	if err != nil {
		fmt.Println("  (could not list tokens)")
		return nil
	}
	fmt.Printf("  %d token(s) in database\n", len(records))

	return nil
}

// isProxyToken checks if a value looks like a tokenomics wrapper token.
func isProxyToken(v string) bool {
	return strings.HasPrefix(v, "tkn_")
}
