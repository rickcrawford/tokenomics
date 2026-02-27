package cmd

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/policy"
	"github.com/rickcrawford/tokenomics/internal/store"
	"github.com/spf13/cobra"
)

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage wrapper tokens",
	Long:  `Create, inspect, update, and delete wrapper tokens and their associated policies.`,
}

var tokenCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new wrapper token",
	Example: `  tokenomics token create --policy '{"base_key_env":"OPENAI_API_KEY","max_tokens":100000}'
  tokenomics token create --policy '{"base_key_env":"ANTHROPIC_API_KEY"}' --expires 30d
  tokenomics token create --policy @policy.json --expires 2025-12-31T00:00:00Z`,
	RunE: runTokenCreate,
}

var tokenGetCmd = &cobra.Command{
	Use:     "get",
	Short:   "Get a token's details by hash",
	Example: `  tokenomics token get --hash 9f86d0818...`,
	RunE:    runTokenGet,
}

var tokenUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update a token's policy or expiration",
	Example: `  tokenomics token update --hash 9f86d0818... --expires 7d
  tokenomics token update --hash 9f86d0818... --policy '{"max_tokens":200000}'
  tokenomics token update --hash 9f86d0818... --expires clear`,
	RunE: runTokenUpdate,
}

var tokenDeleteCmd = &cobra.Command{
	Use:     "delete",
	Short:   "Delete a wrapper token",
	Example: `  tokenomics token delete --hash 9f86d0818...`,
	RunE:    runTokenDelete,
}

var tokenListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all wrapper tokens and their policies",
	Example: `  tokenomics token list`,
	RunE:    runTokenList,
}

var (
	policyFlag  string
	deleteHash  string
	getHash     string
	updateHash  string
	expiresFlag string
)

func init() {
	tokenCreateCmd.Flags().StringVar(&policyFlag, "policy", "", "policy JSON (required)")
	if err := tokenCreateCmd.MarkFlagRequired("policy"); err != nil {
		panic(err)
	}
	tokenCreateCmd.Flags().StringVar(&expiresFlag, "expires", "", "expiration duration (e.g., 24h, 7d, 30d) or RFC3339 timestamp")

	tokenGetCmd.Flags().StringVar(&getHash, "hash", "", "token hash to retrieve (required)")
	if err := tokenGetCmd.MarkFlagRequired("hash"); err != nil {
		panic(err)
	}

	tokenUpdateCmd.Flags().StringVar(&updateHash, "hash", "", "token hash to update (required)")
	if err := tokenUpdateCmd.MarkFlagRequired("hash"); err != nil {
		panic(err)
	}
	tokenUpdateCmd.Flags().StringVar(&policyFlag, "policy", "", "new policy JSON")
	tokenUpdateCmd.Flags().StringVar(&expiresFlag, "expires", "", "new expiration (duration, RFC3339, or 'clear' to remove)")

	tokenDeleteCmd.Flags().StringVar(&deleteHash, "hash", "", "token hash to delete (required)")
	if err := tokenDeleteCmd.MarkFlagRequired("hash"); err != nil {
		panic(err)
	}

	tokenCmd.AddCommand(tokenCreateCmd, tokenGetCmd, tokenUpdateCmd, tokenDeleteCmd, tokenListCmd)
	rootCmd.AddCommand(tokenCmd)
}

// parseExpires converts a human-friendly expiration string to an RFC3339 timestamp.
// Supports: "24h", "7d", "30d", "1y", Go durations, or raw RFC3339.
func parseExpires(s string) (string, error) {
	if s == "" || s == "clear" {
		return s, nil
	}

	// Try RFC3339 first
	if _, err := time.Parse(time.RFC3339, s); err == nil {
		return s, nil
	}

	// Handle day and year suffixes
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		var n int
		if _, err := fmt.Sscanf(days, "%d", &n); err == nil && n > 0 {
			return time.Now().UTC().Add(time.Duration(n) * 24 * time.Hour).Format(time.RFC3339), nil
		}
	}
	if strings.HasSuffix(s, "y") {
		years := strings.TrimSuffix(s, "y")
		var n int
		if _, err := fmt.Sscanf(years, "%d", &n); err == nil && n > 0 {
			return time.Now().UTC().AddDate(n, 0, 0).Format(time.RFC3339), nil
		}
	}

	// Try Go duration (e.g., "24h", "168h")
	d, err := time.ParseDuration(s)
	if err != nil {
		return "", fmt.Errorf("invalid expiration %q: use a duration (24h, 7d, 30d, 1y), RFC3339 timestamp, or 'clear'", s)
	}
	if d <= 0 {
		return "", fmt.Errorf("expiration must be positive, got %s", s)
	}
	return time.Now().UTC().Add(d).Format(time.RFC3339), nil
}

func initStore(cfg *config.Config) (*store.BoltStore, error) {
	dbFile := cfg.Storage.DBPath
	if dbPath != "" {
		dbFile = dbPath
	}

	encKey := os.Getenv(cfg.Security.EncryptionKeyEnv)

	s := store.NewBoltStore(dbFile, encKey)
	if err := s.Init(); err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}
	return s, nil
}

func runTokenCreate(cmd *cobra.Command, args []string) error {
	// Validate policy
	if _, err := policy.Parse(policyFlag); err != nil {
		return fmt.Errorf("invalid policy: %w", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	s, err := initStore(cfg)
	if err != nil {
		return err
	}
	defer s.Close()

	// Parse expiration
	expiresAt, err := parseExpires(expiresFlag)
	if err != nil {
		return err
	}

	// Generate UUID-based token
	rawToken := "tkn_" + uuid.New().String()

	// Hash it
	hashKey := getHashKey(cfg.Security.HashKeyEnv)
	tokenHash := hashToken(rawToken, hashKey)

	// Store
	if err := s.Create(tokenHash, policyFlag, expiresAt); err != nil {
		return fmt.Errorf("create token: %w", err)
	}

	fmt.Println("Token created successfully.")
	fmt.Println("WARNING: This token will only be shown once. Store it securely.")
	fmt.Println()
	fmt.Printf("  Token:   %s\n", rawToken)
	fmt.Printf("  Hash:    %s\n", tokenHash)
	if expiresAt != "" {
		fmt.Printf("  Expires: %s\n", expiresAt)
	}
	fmt.Println()

	return nil
}

func runTokenGet(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	s, err := initStore(cfg)
	if err != nil {
		return err
	}
	defer s.Close()

	record, err := s.Get(getHash)
	if err != nil {
		return fmt.Errorf("get token: %w", err)
	}
	if record == nil {
		fmt.Println("Token not found.")
		return nil
	}

	fmt.Printf("Hash:       %s\n", record.TokenHash)
	fmt.Printf("Created:    %s\n", record.CreatedAt)
	if record.ExpiresAt != "" {
		exp, _ := time.Parse(time.RFC3339, record.ExpiresAt)
		status := "active"
		if time.Now().After(exp) {
			status = "EXPIRED"
		}
		fmt.Printf("Expires:    %s (%s)\n", record.ExpiresAt, status)
	} else {
		fmt.Printf("Expires:    never\n")
	}

	// Pretty-print the policy
	var pretty json.RawMessage
	if err := json.Unmarshal([]byte(record.PolicyRaw), &pretty); err == nil {
		indented, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Printf("Policy:\n%s\n", string(indented))
	} else {
		fmt.Printf("Policy:     %s\n", record.PolicyRaw)
	}

	return nil
}

func runTokenUpdate(cmd *cobra.Command, args []string) error {
	if policyFlag == "" && expiresFlag == "" {
		return fmt.Errorf("must provide --policy and/or --expires")
	}

	// Validate policy if provided
	if policyFlag != "" {
		if _, err := policy.Parse(policyFlag); err != nil {
			return fmt.Errorf("invalid policy: %w", err)
		}
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	s, err := initStore(cfg)
	if err != nil {
		return err
	}
	defer s.Close()

	// Parse expiration
	expiresAt, err := parseExpires(expiresFlag)
	if err != nil {
		return err
	}

	if err := s.Update(updateHash, policyFlag, expiresAt); err != nil {
		return fmt.Errorf("update token: %w", err)
	}

	fmt.Println("Token updated.")
	return nil
}

func runTokenDelete(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	s, err := initStore(cfg)
	if err != nil {
		return err
	}
	defer s.Close()

	if err := s.Delete(deleteHash); err != nil {
		return fmt.Errorf("delete token: %w", err)
	}

	fmt.Println("Token deleted.")
	return nil
}

func runTokenList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	s, err := initStore(cfg)
	if err != nil {
		return err
	}
	defer s.Close()

	records, err := s.List()
	if err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}

	if len(records) == 0 {
		fmt.Println("No tokens found.")
		return nil
	}

	for _, r := range records {
		fmt.Printf("Hash:       %s\n", r.TokenHash)
		fmt.Printf("Created:    %s\n", r.CreatedAt)
		if r.ExpiresAt != "" {
			exp, _ := time.Parse(time.RFC3339, r.ExpiresAt)
			status := "active"
			if time.Now().After(exp) {
				status = "EXPIRED"
			}
			fmt.Printf("Expires:    %s (%s)\n", r.ExpiresAt, status)
		}
		fmt.Printf("Key Env:    %s\n", r.Policy.BaseKeyEnv)
		if r.Policy.UpstreamURL != "" {
			fmt.Printf("Upstream:   %s\n", r.Policy.UpstreamURL)
		}
		if r.Policy.Model != "" {
			fmt.Printf("Model:      %s\n", r.Policy.Model)
		}
		if r.Policy.ModelRegex != "" {
			fmt.Printf("Model Regex: %s\n", r.Policy.ModelRegex)
		}
		if r.Policy.MaxTokens > 0 {
			fmt.Printf("Max Tokens: %d\n", r.Policy.MaxTokens)
		}
		fmt.Printf("Prompts:    %d\n", len(r.Policy.Prompts))
		fmt.Printf("Rules:      %d\n", len(r.Policy.Rules))
		fmt.Println("---")
	}

	return nil
}

func getHashKey(envName string) []byte {
	key := os.Getenv(envName)
	if key == "" {
		key = "tokenomics-default-key-change-me"
	}
	return []byte(key)
}

func hashToken(token string, key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}
