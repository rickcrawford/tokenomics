package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvPair represents a key-value pair for environment variable output.
type EnvPair struct {
	Key   string
	Value string
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

// writeClaudeCodeConfig writes env vars into .claude/settings.local.json.
// Merges with existing settings to preserve user configuration.
func writeClaudeCodeConfig(pairs []EnvPair, proxyURL string) error {
	settingsDir := ".claude"
	settingsPath := filepath.Join(settingsDir, "settings.local.json")

	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		return fmt.Errorf("create %s directory: %w", settingsDir, err)
	}

	// Load existing settings to merge
	settings := make(map[string]interface{})
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			settings = make(map[string]interface{})
		}
	}

	// Build env map from pairs
	envMap := make(map[string]string)
	for _, p := range pairs {
		envMap[p.Key] = p.Value
	}
	settings["env"] = envMap

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	out = append(out, '\n')

	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", settingsPath, err)
	}

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
