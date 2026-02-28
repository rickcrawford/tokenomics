package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system configuration and diagnose common issues",
	Long: `Runs a series of checks to verify that tokenomics is correctly configured.
Inspects config files, database access, provider API keys, TLS certificates,
and the running proxy state.`,
	Example: `  tokenomics doctor
  tokenomics doctor --config /etc/tokenomics/config.yaml`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

type checkResult struct {
	name   string
	status string // "ok", "warn", "fail"
	detail string
}

func runDoctor(cmd *cobra.Command, args []string) error {
	var checks []checkResult

	// 1. Config file
	checks = append(checks, checkConfigFile())

	// 2. Load config and run dependent checks
	cfg, err := config.Load(cfgFile)
	if err != nil {
		checks = append(checks, checkResult{"config parse", "fail", fmt.Sprintf("could not parse config: %v", err)})
		printChecks(checks)
		return nil
	}

	checks = append(checks, checkResult{"config parse", "ok", "config loaded successfully"})

	// 3. Database
	checks = append(checks, checkDatabase(cfg))

	// 4. Security keys
	checks = append(checks, checkSecurityKeys(cfg)...)

	// 5. Provider API keys
	checks = append(checks, checkProviderKeys(cfg)...)

	// 6. TLS certificates
	checks = append(checks, checkTLS(cfg)...)

	// 7. Proxy running state
	checks = append(checks, checkProxyRunning())

	// 8. Binary execution policy (macOS)
	checks = append(checks, checkBinaryExecutionPolicy())

	// 9. Remote config
	if cfg.Remote.URL != "" {
		checks = append(checks, checkRemote(cfg))
	}

	printChecks(checks)

	// Summary
	var fails, warns int
	for _, c := range checks {
		switch c.status {
		case "fail":
			fails++
		case "warn":
			warns++
		}
	}

	fmt.Println()
	if fails > 0 {
		fmt.Printf("%d check(s) failed, %d warning(s)\n", fails, warns)
	} else if warns > 0 {
		fmt.Printf("All checks passed with %d warning(s)\n", warns)
	} else {
		fmt.Println("All checks passed")
	}

	return nil
}

func checkConfigFile() checkResult {
	// Check standard locations in order of precedence
	paths := []string{
		filepath.Join(".tokenomics", "config.yaml"),
		"config.yaml",
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".tokenomics", "config.yaml"))
	}
	if cfgFile != "" {
		paths = []string{cfgFile}
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return checkResult{"config file", "ok", fmt.Sprintf("found %s", p)}
		}
	}

	if cfgFile != "" {
		return checkResult{"config file", "fail", fmt.Sprintf("%s not found", cfgFile)}
	}
	return checkResult{"config file", "warn", "no config.yaml found, using defaults"}
}

func checkDatabase(cfg *config.Config) checkResult {
	dbFile := cfg.Storage.DBPath
	if dbPath != "" {
		dbFile = dbPath
	}
	if _, err := os.Stat(dbFile); err == nil {
		return checkResult{"database", "ok", fmt.Sprintf("found %s", dbFile)}
	}

	// Check if directory is writable
	dir := filepath.Dir(dbFile)
	if _, err := os.Stat(dir); err != nil {
		return checkResult{"database", "fail", fmt.Sprintf("directory %s does not exist", dir)}
	}
	return checkResult{"database", "warn", fmt.Sprintf("%s does not exist yet (will be created on first run)", dbFile)}
}

func checkSecurityKeys(cfg *config.Config) []checkResult {
	var results []checkResult

	hashKey := os.Getenv(cfg.Security.HashKeyEnv)
	if hashKey == "" {
		results = append(results, checkResult{
			"hash key", "warn",
			fmt.Sprintf("$%s not set, using default key (not recommended for production)", cfg.Security.HashKeyEnv),
		})
	} else {
		results = append(results, checkResult{"hash key", "ok", fmt.Sprintf("$%s is set", cfg.Security.HashKeyEnv)})
	}

	encKey := os.Getenv(cfg.Security.EncryptionKeyEnv)
	if encKey == "" {
		results = append(results, checkResult{
			"encryption key", "warn",
			fmt.Sprintf("$%s not set, database will not be encrypted at rest", cfg.Security.EncryptionKeyEnv),
		})
	} else {
		results = append(results, checkResult{"encryption key", "ok", fmt.Sprintf("$%s is set", cfg.Security.EncryptionKeyEnv)})
	}

	return results
}

func checkProviderKeys(cfg *config.Config) []checkResult {
	if len(cfg.Providers) == 0 {
		return []checkResult{{"providers", "warn", "no providers configured"}}
	}

	var results []checkResult
	var configured, missing int
	var missingNames []string

	for name, pc := range cfg.Providers {
		if pc.APIKeyEnv == "" {
			configured++ // no key needed (e.g., ollama)
			continue
		}
		if os.Getenv(pc.APIKeyEnv) != "" {
			configured++
		} else {
			missing++
			missingNames = append(missingNames, fmt.Sprintf("%s ($%s)", name, pc.APIKeyEnv))
		}
	}

	results = append(results, checkResult{
		"provider keys",
		statusForCount(configured, missing),
		fmt.Sprintf("%d configured, %d missing API keys", configured, missing),
	})

	if missing > 0 && missing <= 5 {
		for _, m := range missingNames {
			results = append(results, checkResult{"  missing key", "warn", m})
		}
	}

	return results
}

func checkTLS(cfg *config.Config) []checkResult {
	if !cfg.Server.TLS.Enabled {
		return []checkResult{{"tls", "warn", "TLS is disabled"}}
	}

	if cfg.Server.TLS.CertFile != "" && cfg.Server.TLS.KeyFile != "" {
		certOK := fileExists(cfg.Server.TLS.CertFile)
		keyOK := fileExists(cfg.Server.TLS.KeyFile)
		if certOK && keyOK {
			return []checkResult{{"tls", "ok", fmt.Sprintf("cert=%s key=%s", cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile)}}
		}
		var missing []string
		if !certOK {
			missing = append(missing, "cert: "+cfg.Server.TLS.CertFile)
		}
		if !keyOK {
			missing = append(missing, "key: "+cfg.Server.TLS.KeyFile)
		}
		return []checkResult{{"tls", "fail", "missing files: " + strings.Join(missing, ", ")}}
	}

	if cfg.Server.TLS.AutoGen {
		certDir := cfg.Server.TLS.CertDir
		if certDir == "" {
			certDir = "./certs"
		}
		if fileExists(filepath.Join(certDir, "server.crt")) {
			return []checkResult{{"tls", "ok", fmt.Sprintf("auto-generated certs in %s", certDir)}}
		}
		return []checkResult{{"tls", "ok", fmt.Sprintf("auto_gen enabled, certs will be created in %s on first serve", certDir)}}
	}

	return []checkResult{{"tls", "fail", "TLS enabled but no certs configured and auto_gen is off"}}
}

func checkProxyRunning() checkResult {
	u, err := user.Current()
	if err != nil {
		return checkResult{"proxy status", "warn", "could not determine home directory"}
	}
	pidFile := filepath.Join(u.HomeDir, ".tokenomics", "tokenomics.pid")

	pid, err := readPIDFile(pidFile)
	if err != nil {
		return checkResult{"proxy status", "ok", "no background proxy running"}
	}

	if processAlive(pid) {
		return checkResult{"proxy status", "ok", fmt.Sprintf("background proxy running (PID %d)", pid)}
	}
	return checkResult{"proxy status", "warn", fmt.Sprintf("stale PID file at %s (PID %d not running)", pidFile, pid)}
}

func checkRemote(cfg *config.Config) checkResult {
	if cfg.Remote.APIKey == "" {
		return checkResult{"remote sync", "warn", fmt.Sprintf("remote URL=%s but no api_key set", cfg.Remote.URL)}
	}
	return checkResult{"remote sync", "ok", fmt.Sprintf("configured for %s (sync every %ds)", cfg.Remote.URL, cfg.Remote.SyncSec)}
}

func checkBinaryExecutionPolicy() checkResult {
	exePath, err := os.Executable()
	if err != nil {
		return checkResult{"binary policy", "warn", "could not determine executable path"}
	}
	xattrs, xattrErr := listXattrs(exePath)
	return evaluateBinaryExecutionPolicy(runtime.GOOS, exePath, xattrs, xattrErr)
}

func evaluateBinaryExecutionPolicy(goos, exePath, xattrs string, xattrErr error) checkResult {
	if goos != "darwin" {
		return checkResult{"binary policy", "ok", "not applicable on this OS"}
	}
	if !strings.HasPrefix(exePath, "/var/tmp/") && !strings.HasPrefix(exePath, "/private/var/tmp/") {
		return checkResult{"binary policy", "ok", "executable path is outside /var/tmp"}
	}
	if xattrErr != nil {
		return checkResult{"binary policy", "warn", fmt.Sprintf("running from %s. Could not inspect xattrs: %v", exePath, xattrErr)}
	}
	if strings.Contains(xattrs, "com.apple.provenance") || strings.Contains(xattrs, "com.apple.quarantine") {
		return checkResult{
			"binary policy",
			"warn",
			fmt.Sprintf("running from %s with macOS xattrs (provenance/quarantine) can trigger 'killed'. Rebuild or move binary, then retry", exePath),
		}
	}
	return checkResult{"binary policy", "ok", fmt.Sprintf("running from %s with no blocking xattrs detected", exePath)}
}

var listXattrs = func(path string) (string, error) {
	out, err := exec.Command("xattr", "-l", path).CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func printChecks(checks []checkResult) {
	for _, c := range checks {
		marker := "[ok]  "
		switch c.status {
		case "warn":
			marker = "[!!]  "
		case "fail":
			marker = "[FAIL]"
		}
		fmt.Printf("%s %-18s %s\n", marker, c.name, c.detail)
	}
}

func statusForCount(ok, bad int) string {
	if bad == 0 {
		return "ok"
	}
	if ok == 0 {
		return "fail"
	}
	return "warn"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
