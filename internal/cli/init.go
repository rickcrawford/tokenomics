package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Configure an agent CLI to use the tokenomics proxy",
	Long: `Sets environment variables or writes config for an agent framework
(OpenAI, Anthropic, Azure, Gemini, or custom) to route API calls through the proxy.`,
	RunE: runInit,
}

var (
	initToken     string
	initHost      string
	initPort      int
	initTLS       bool
	initInsecure  bool
	initCLI       string
	initEnvKey    string
	initEnvBase   string
	initOutputFmt string
	initDotenv    string
)

func init() {
	initCmd.Flags().StringVar(&initToken, "token", "", "wrapper token (required)")
	initCmd.MarkFlagRequired("token")
	initCmd.Flags().StringVar(&initHost, "host", "localhost", "proxy hostname")
	initCmd.Flags().IntVar(&initPort, "port", 8443, "proxy port")
	initCmd.Flags().BoolVar(&initTLS, "tls", true, "use HTTPS")
	initCmd.Flags().BoolVar(&initInsecure, "insecure", false, "skip TLS verification")
	initCmd.Flags().StringVar(&initCLI, "cli", "generic", "target CLI/SDK (generic, anthropic, azure, gemini, custom)")
	initCmd.Flags().StringVar(&initEnvKey, "env-key", "", "custom env var name for the API key")
	initCmd.Flags().StringVar(&initEnvBase, "env-base-url", "", "custom env var name for the base URL")
	initCmd.Flags().StringVar(&initOutputFmt, "output", "shell", "output format (shell, dotenv, json)")
	initCmd.Flags().StringVar(&initDotenv, "dotenv", "", "path to .env file (used with --output dotenv)")

	rootCmd.AddCommand(initCmd)
}

type envPair struct {
	key   string
	value string
}

func runInit(cmd *cobra.Command, args []string) error {
	scheme := "https"
	if !initTLS {
		scheme = "http"
	}
	baseURL := fmt.Sprintf("%s://%s:%d", scheme, initHost, initPort)

	pairs := resolveEnvPairs(initCLI, initToken, baseURL)

	if initInsecure {
		pairs = append(pairs, envPair{"NODE_TLS_REJECT_UNAUTHORIZED", "0"})
	}

	switch initOutputFmt {
	case "shell":
		return outputShell(pairs)
	case "dotenv":
		return outputDotenv(pairs, initDotenv)
	case "json":
		return outputJSON(pairs)
	default:
		return fmt.Errorf("unknown output format: %s", initOutputFmt)
	}
}

func resolveEnvPairs(cli, token, baseURL string) []envPair {
	if initEnvKey != "" && initEnvBase != "" {
		return []envPair{
			{initEnvKey, token},
			{initEnvBase, baseURL},
		}
	}

	switch strings.ToLower(cli) {
	case "anthropic":
		return []envPair{
			{"ANTHROPIC_API_KEY", token},
			{"ANTHROPIC_BASE_URL", baseURL},
		}
	case "azure":
		return []envPair{
			{"AZURE_OPENAI_API_KEY", token},
			{"AZURE_OPENAI_ENDPOINT", baseURL},
		}
	case "gemini":
		return []envPair{
			{"GEMINI_API_KEY", token},
			{"GEMINI_BASE_URL", baseURL},
		}
	default: // generic / openai
		return []envPair{
			{"OPENAI_API_KEY", token},
			{"OPENAI_BASE_URL", baseURL + "/v1"},
		}
	}
}

func outputShell(pairs []envPair) error {
	for _, p := range pairs {
		fmt.Printf("export %s=%q\n", p.key, p.value)
	}
	return nil
}

func outputDotenv(pairs []envPair, path string) error {
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
			if strings.HasPrefix(line, p.key+"=") {
				lines[i] = fmt.Sprintf("%s=%q", p.key, p.value)
				setKeys[p.key] = true
			}
		}
	}

	// Append new keys
	for _, p := range pairs {
		if !setKeys[p.key] {
			lines = append(lines, fmt.Sprintf("%s=%q", p.key, p.value))
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

func outputJSON(pairs []envPair) error {
	m := make(map[string]string)
	for _, p := range pairs {
		m[p.key] = p.value
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}
