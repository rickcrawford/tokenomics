package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/proxy"
	"github.com/rickcrawford/tokenomics/internal/store"
)

func TestAdminRoutes_HealthTokensUsageAndUI(t *testing.T) {
	cfg, tokenStore, stats := newAdminTestFixture(t, false)
	dataDir := t.TempDir()
	writeTestSessionAndMemory(t, dataDir)
	r := chi.NewRouter()
	registerAdminRoutes(r, cfg, tokenStore, stats, dataDir)

	healthReq := httptest.NewRequest(http.MethodGet, "/admin/api/health", nil)
	healthReq.RemoteAddr = "127.0.0.1:12345"
	healthRec := httptest.NewRecorder()
	r.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", healthRec.Code)
	}

	tokensReq := httptest.NewRequest(http.MethodGet, "/admin/api/tokens", nil)
	tokensReq.RemoteAddr = "127.0.0.1:12345"
	tokensRec := httptest.NewRecorder()
	r.ServeHTTP(tokensRec, tokensReq)
	if tokensRec.Code != http.StatusOK {
		t.Fatalf("tokens status = %d, want 200", tokensRec.Code)
	}
	var tokenResp map[string]interface{}
	if err := json.Unmarshal(tokensRec.Body.Bytes(), &tokenResp); err != nil {
		t.Fatalf("parse tokens response: %v", err)
	}
	if tokenResp["count"].(float64) < 1 {
		t.Fatalf("expected at least one token in response")
	}

	usageReq := httptest.NewRequest(http.MethodGet, "/admin/api/usage/summary", nil)
	usageReq.RemoteAddr = "127.0.0.1:12345"
	usageRec := httptest.NewRecorder()
	r.ServeHTTP(usageRec, usageReq)
	if usageRec.Code != http.StatusOK {
		t.Fatalf("usage status = %d, want 200", usageRec.Code)
	}

	analyticsReq := httptest.NewRequest(http.MethodGet, "/admin/api/analytics/summary", nil)
	analyticsReq.RemoteAddr = "127.0.0.1:12345"
	analyticsRec := httptest.NewRecorder()
	r.ServeHTTP(analyticsRec, analyticsReq)
	if analyticsRec.Code != http.StatusOK {
		t.Fatalf("analytics status = %d, want 200", analyticsRec.Code)
	}

	sessionsReq := httptest.NewRequest(http.MethodGet, "/admin/api/sessions", nil)
	sessionsReq.RemoteAddr = "127.0.0.1:12345"
	sessionsRec := httptest.NewRecorder()
	r.ServeHTTP(sessionsRec, sessionsReq)
	if sessionsRec.Code != http.StatusOK {
		t.Fatalf("sessions status = %d, want 200", sessionsRec.Code)
	}

	memoryReq := httptest.NewRequest(http.MethodGet, "/admin/api/memory/files", nil)
	memoryReq.RemoteAddr = "127.0.0.1:12345"
	memoryRec := httptest.NewRecorder()
	r.ServeHTTP(memoryRec, memoryReq)
	if memoryRec.Code != http.StatusOK {
		t.Fatalf("memory status = %d, want 200", memoryRec.Code)
	}

	uiReq := httptest.NewRequest(http.MethodGet, "/", nil)
	uiReq.RemoteAddr = "127.0.0.1:12345"
	uiRec := httptest.NewRecorder()
	r.ServeHTTP(uiRec, uiReq)
	if uiRec.Code != http.StatusOK {
		t.Fatalf("ui status = %d, want 200", uiRec.Code)
	}
	if ct := uiRec.Header().Get("Content-Type"); ct == "" {
		t.Fatalf("expected content type for ui response")
	}
}

func TestAdminRoutes_KeyCRUDAndEnvVars(t *testing.T) {
	cfg, tokenStore, stats := newAdminTestFixture(t, false)
	dataDir := t.TempDir()
	r := chi.NewRouter()
	registerAdminRoutes(r, cfg, tokenStore, stats, dataDir)
	t.Setenv("TOKENOMICS_TEST_ENV_LIST", "1")

	createBody := `{"policy":"{\"base_key_env\":\"OPENAI_API_KEY\",\"max_tokens\":500}","expires":"7d"}`
	createReq := httptest.NewRequest(http.MethodPost, "/admin/api/keys", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.RemoteAddr = "127.0.0.1:12345"
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("key create status = %d, want 201", createRec.Code)
	}
	var createResp map[string]string
	_ = json.Unmarshal(createRec.Body.Bytes(), &createResp)
	if createResp["hash"] == "" || createResp["token"] == "" {
		t.Fatalf("expected hash and token in create response")
	}

	updateBody := `{"expires":"clear"}`
	updateReq := httptest.NewRequest(http.MethodPut, "/admin/api/keys/"+createResp["hash"], strings.NewReader(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.RemoteAddr = "127.0.0.1:12345"
	updateRec := httptest.NewRecorder()
	r.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("key update status = %d, want 200", updateRec.Code)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/admin/api/keys/"+createResp["hash"], nil)
	deleteReq.RemoteAddr = "127.0.0.1:12345"
	deleteRec := httptest.NewRecorder()
	r.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("key delete status = %d, want 200", deleteRec.Code)
	}

	envReq := httptest.NewRequest(http.MethodGet, "/admin/api/env-vars", nil)
	envReq.RemoteAddr = "127.0.0.1:12345"
	envRec := httptest.NewRecorder()
	r.ServeHTTP(envRec, envReq)
	if envRec.Code != http.StatusOK {
		t.Fatalf("env-vars status = %d, want 200", envRec.Code)
	}
	var envResp map[string][]string
	if err := json.Unmarshal(envRec.Body.Bytes(), &envResp); err != nil {
		t.Fatalf("parse env-vars response: %v", err)
	}
	names := envResp["env_vars"]
	found := false
	for _, name := range names {
		if name == "TOKENOMICS_TEST_ENV_LIST" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected TOKENOMICS_TEST_ENV_LIST in env_vars response")
	}
}

func TestAdminRoutes_AuthRequiredWhenConfigured(t *testing.T) {
	cfg, tokenStore, stats := newAdminTestFixture(t, true)
	r := chi.NewRouter()
	registerAdminRoutes(r, cfg, tokenStore, stats, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/admin/api/health", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status without auth = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/api/health", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.SetBasicAuth("admin", "secret")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status with auth = %d, want 200", rec.Code)
	}
}

func newAdminTestFixture(t *testing.T, auth bool) (*config.Config, *store.BoltStore, *proxy.UsageStats) {
	t.Helper()
	cfg := &config.Config{
		Admin: config.AdminConfig{
			Enabled: true,
		},
		Server: config.ServerConfig{
			HTTPPort:  8080,
			TLS:       config.TLSConfig{Enabled: true},
			HTTPSPort: 8443,
		},
	}
	if auth {
		cfg.Admin.Auth.Username = "admin"
		cfg.Admin.Auth.Password = "secret"
	}

	dbPath := filepath.Join(t.TempDir(), "tokenomics.db")
	tokenStore := store.NewBoltStore(dbPath, "")
	if err := tokenStore.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}
	t.Cleanup(func() { _ = tokenStore.Close() })

	policyJSON := `{"base_key_env":"OPENAI_API_KEY","max_tokens":1000,"default_provider":"openai","providers":{"openai":[{"base_key_env":"OPENAI_API_KEY","model":"gpt-4o"}]}}`
	if err := tokenStore.Create("tok_hash_123", policyJSON, ""); err != nil {
		t.Fatalf("create token: %v", err)
	}

	stats := proxy.NewUsageStats()
	stats.Record("tok_hash_123", "gpt-4o", "OPENAI_API_KEY", 100, 25, false)
	return cfg, tokenStore, stats
}

func writeTestSessionAndMemory(t *testing.T, dataDir string) {
	t.Helper()
	sessionsDir := filepath.Join(dataDir, "sessions")
	memoryDir := filepath.Join(dataDir, "memory")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	s := map[string]interface{}{
		"session_id":  "sess1234",
		"started_at":  time.Now().UTC().Format(time.RFC3339),
		"ended_at":    time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
		"duration_ms": 60000,
		"git":         map[string]interface{}{"branch": "main"},
		"totals": map[string]interface{}{
			"request_count": 2,
			"input_tokens":  100,
			"output_tokens": 40,
			"total_tokens":  140,
		},
		"by_model":    map[string]interface{}{},
		"by_provider": map[string]interface{}{},
		"by_token":    map[string]interface{}{},
		"requests":    []interface{}{},
	}
	b, _ := json.Marshal(s)
	if err := os.WriteFile(filepath.Join(sessionsDir, "2026-02-27_sess1234.json"), b, 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryDir, "sess1234.md"), []byte("hello memory"), 0o644); err != nil {
		t.Fatalf("write memory: %v", err)
	}
}
