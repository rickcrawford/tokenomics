package main

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"github.com/rickcrawford/tokenomics/cmd"
)

func init() {
	// Load .env files from common locations (don't fail if not found)
	candidates := []string{
		".env",
	}

	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".tokenomics", ".env"))
	}

	for _, envFile := range candidates {
		_ = godotenv.Load(envFile)
	}
}

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
