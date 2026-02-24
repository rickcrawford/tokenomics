package cli

import (
	"github.com/spf13/cobra"
)

var cfgFile string
var dbPath string

var rootCmd = &cobra.Command{
	Use:   "tokenomics",
	Short: "OpenAI-compatible reverse proxy with token management and policy enforcement",
	Long: `Tokenomics is a reverse proxy that sits in front of OpenAI (and compatible) APIs.
It issues wrapper tokens mapped to policies that control model access, token budgets,
prompt injection, and content rules.`,
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./config.yaml)")
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", "", "database path (overrides config)")
}
