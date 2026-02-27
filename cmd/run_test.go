package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rickcrawford/tokenomics/internal/config"
)

func TestMain(m *testing.M) {
	// Helper path for runRun local-proxy lifecycle tests:
	// when runRun starts "serve", this test binary is invoked with os.Args[1] == "serve".
	if os.Getenv("TOKENOMICS_TEST_HELPER_SERVE") == "1" && len(os.Args) > 1 && os.Args[1] == "serve" {
		mux := http.NewServeMux()
		mux.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		srv := &http.Server{Addr: ":8080", Handler: mux}

		if pidFile := os.Getenv("TOKENOMICS_TEST_HELPER_PIDFILE"); pidFile != "" {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644)
		}

		go func() {
			_ = srv.ListenAndServe()
		}()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		_ = srv.Close()
		os.Exit(0)
	}

	os.Exit(m.Run())
}

func TestDetectProviderFromCLI_Match(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
cli_maps:
  claude: anthropic
  gpt: openai
providers:
  anthropic:
    upstream_url: https://api.anthropic.com
  openai:
    upstream_url: https://api.openai.com
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Verify config loads correctly
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("config load: %v", err)
	}
	if cfg.CLIMaps["claude"] != "anthropic" {
		t.Fatalf("expected claude -> anthropic, got %q", cfg.CLIMaps["claude"])
	}

	result := detectProviderFromCLI("claude", cfgPath)
	if result != "anthropic" {
		t.Errorf("detectProviderFromCLI('claude') = %q, want 'anthropic'", result)
	}
}

func TestDetectProviderFromCLI_NoMatch(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
cli_maps:
  claude: anthropic
providers:
  anthropic:
    upstream_url: https://api.anthropic.com
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	result := detectProviderFromCLI("unknown-cli", cfgPath)
	if result != "" {
		t.Errorf("expected empty string for unknown CLI, got %q", result)
	}
}

func TestDetectProviderFromCLI_NoConfig(t *testing.T) {
	// Even with missing config, hard-coded defaults should work
	result := detectProviderFromCLI("claude", "/nonexistent/config.yaml")
	if result != "anthropic" {
		t.Errorf("expected 'anthropic' from hard-coded defaults, got %q", result)
	}
}

func TestRunCmd_Registration(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "run [flags] COMMAND [ARGS...]" {
			found = true
			break
		}
	}
	if !found {
		t.Error("run command not registered on root")
	}
}

func TestRunCmd_Flags(t *testing.T) {
	flags := []string{"token", "proxy-url", "host", "port", "tls", "insecure", "provider", "env-key", "env-base-url"}
	for _, name := range flags {
		if runCmd.Flags().Lookup(name) == nil {
			t.Errorf("run command missing flag: %s", name)
		}
	}
}

func resetRunGlobals() func() {
	prevRunToken := runToken
	prevRunProxyURL := runProxyURL
	prevRunHost := runHost
	prevRunPort := runPort
	prevRunTLS := runTLS
	prevRunInsecure := runInsecure
	prevRunProvider := runProvider
	prevRunEnvKey := runEnvKey
	prevRunEnvBase := runEnvBase
	prevCfgFile := cfgFile
	prevDBPath := dbPath

	return func() {
		runToken = prevRunToken
		runProxyURL = prevRunProxyURL
		runHost = prevRunHost
		runPort = prevRunPort
		runTLS = prevRunTLS
		runInsecure = prevRunInsecure
		runProvider = prevRunProvider
		runEnvKey = prevRunEnvKey
		runEnvBase = prevRunEnvBase
		cfgFile = prevCfgFile
		dbPath = prevDBPath
	}
}

func TestRunRun_RemoteProxy_InjectsEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh test helper")
	}
	restore := resetRunGlobals()
	defer restore()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer proxy.Close()

	outFile := filepath.Join(t.TempDir(), "env.out")
	script := filepath.Join(t.TempDir(), "capture.sh")
	scriptBody := "#!/bin/sh\nprintf \"%s|%s|%s\" \"$CUSTOM_KEY\" \"$CUSTOM_BASE\" \"$NODE_TLS_REJECT_UNAUTHORIZED\" > \"$1\"\n"
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	runToken = "tkn_test_123"
	runProxyURL = proxy.URL
	runProvider = "generic"
	runEnvKey = "CUSTOM_KEY"
	runEnvBase = "CUSTOM_BASE"
	runInsecure = true
	cfgFile = ""
	dbPath = ""

	if err := runRun(nil, []string{script, outFile}); err != nil {
		t.Fatalf("runRun: %v", err)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	want := fmt.Sprintf("%s|%s|0", runToken, proxy.URL)
	if string(got) != want {
		t.Fatalf("env output = %q, want %q", string(got), want)
	}
}

func TestRunRun_RemoteProxy_PropagatesCommandError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh test helper")
	}
	restore := resetRunGlobals()
	defer restore()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer proxy.Close()

	runToken = "tkn_test_123"
	runProxyURL = proxy.URL
	runProvider = "generic"
	runInsecure = false
	runEnvKey = ""
	runEnvBase = ""
	cfgFile = ""
	dbPath = ""

	err := runRun(nil, []string{"sh", "-c", "exit 7"})
	if err == nil {
		t.Fatal("expected non-nil error from failing user command")
	}
}

func TestRunRun_LocalProxyLifecycleAndCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh and unix signal checks")
	}
	restore := resetRunGlobals()
	defer restore()

	pidFile := filepath.Join(t.TempDir(), "helper.pid")
	outFile := filepath.Join(t.TempDir(), "env.out")
	if err := os.Setenv("TOKENOMICS_TEST_HELPER_SERVE", "1"); err != nil {
		t.Fatalf("set env helper flag: %v", err)
	}
	if err := os.Setenv("TOKENOMICS_TEST_HELPER_PIDFILE", pidFile); err != nil {
		t.Fatalf("set env pid file: %v", err)
	}
	defer os.Unsetenv("TOKENOMICS_TEST_HELPER_SERVE")
	defer os.Unsetenv("TOKENOMICS_TEST_HELPER_PIDFILE")

	runToken = "tkn_local_lifecycle"
	runProxyURL = "" // force local startup path
	runHost = "localhost"
	runPort = 8080
	runTLS = false
	runInsecure = false
	runProvider = "generic"
	runEnvKey = "CUSTOM_KEY"
	runEnvBase = "CUSTOM_BASE"
	cfgFile = ""
	dbPath = ""

	err := runRun(nil, []string{"sh", "-c", fmt.Sprintf("printf \"%%s|%%s\" \"$CUSTOM_KEY\" \"$CUSTOM_BASE\" > %s", outFile)})
	if err != nil {
		t.Fatalf("runRun: %v", err)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	want := "tkn_local_lifecycle|http://localhost:8080"
	if string(got) != want {
		t.Fatalf("env output = %q, want %q", string(got), want)
	}

	pidRaw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidRaw)))
	if err != nil {
		t.Fatalf("parse pid: %v", err)
	}

	// Allow shutdown defer path to complete and process to exit.
	time.Sleep(150 * time.Millisecond)
	if processExists(pid) {
		t.Fatalf("expected helper serve process %d to be stopped", pid)
	}
}

func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}
