package cmd

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/policy"
	"github.com/rickcrawford/tokenomics/internal/store"
	"github.com/spf13/cobra"
)

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage wrapper tokens",
}

var tokenCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new wrapper token",
	RunE:  runTokenCreate,
}

var tokenDeleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a wrapper token",
	RunE:  runTokenDelete,
}

var tokenListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all wrapper tokens and their policies",
	RunE:  runTokenList,
}

var (
	policyFlag string
	deleteHash string
)

func init() {
	tokenCreateCmd.Flags().StringVar(&policyFlag, "policy", "", "policy JSON (required)")
	tokenCreateCmd.MarkFlagRequired("policy")

	tokenDeleteCmd.Flags().StringVar(&deleteHash, "hash", "", "token hash to delete (required)")
	tokenDeleteCmd.MarkFlagRequired("hash")

	tokenCmd.AddCommand(tokenCreateCmd, tokenDeleteCmd, tokenListCmd)
	rootCmd.AddCommand(tokenCmd)
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

	dbFile := cfg.Storage.DBPath
	if dbPath != "" {
		dbFile = dbPath
	}

	s := store.NewBoltStore(dbFile)
	if err := s.Init(); err != nil {
		return fmt.Errorf("init store: %w", err)
	}
	defer s.Close()

	// Generate UUID-based token
	rawToken := "tkn_" + uuid.New().String()

	// Hash it
	hashKey := getHashKey(cfg.Security.HashKeyEnv)
	tokenHash := hashToken(rawToken, hashKey)

	// Store
	if err := s.Create(tokenHash, policyFlag); err != nil {
		return fmt.Errorf("create token: %w", err)
	}

	fmt.Println("Token created successfully.")
	fmt.Println("WARNING: This token will only be shown once. Store it securely.")
	fmt.Println()
	fmt.Printf("  Token: %s\n", rawToken)
	fmt.Printf("  Hash:  %s\n", tokenHash)
	fmt.Println()

	return nil
}

func runTokenDelete(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	dbFile := cfg.Storage.DBPath
	if dbPath != "" {
		dbFile = dbPath
	}

	s := store.NewBoltStore(dbFile)
	if err := s.Init(); err != nil {
		return fmt.Errorf("init store: %w", err)
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

	dbFile := cfg.Storage.DBPath
	if dbPath != "" {
		dbFile = dbPath
	}

	s := store.NewBoltStore(dbFile)
	if err := s.Init(); err != nil {
		return fmt.Errorf("init store: %w", err)
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
