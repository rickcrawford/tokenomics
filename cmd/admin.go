package cmd

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/go-chi/chi/v5"
	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/ledger"
	"github.com/rickcrawford/tokenomics/internal/policy"
	"github.com/rickcrawford/tokenomics/internal/proxy"
	"github.com/rickcrawford/tokenomics/internal/store"
)

type adminServer struct {
	cfg        *config.Config
	tokenStore store.TokenStore
	stats      *proxy.UsageStats
	dataDir    string
}

type adminTokenSummary struct {
	TokenHash string                 `json:"token_hash"`
	CreatedAt string                 `json:"created_at"`
	ExpiresAt string                 `json:"expires_at,omitempty"`
	Policy    adminPolicySummary     `json:"policy"`
	Providers []adminProviderSummary `json:"providers"`
}

type adminTokenDetail struct {
	adminTokenSummary
	PolicyRaw string `json:"policy_raw"`
}

type adminPolicySummary struct {
	BaseKeyEnv      string `json:"base_key_env,omitempty"`
	DefaultProvider string `json:"default_provider,omitempty"`
	MaxTokens       int64  `json:"max_tokens,omitempty"`
	RuleCount       int    `json:"rule_count"`
}

type adminProviderSummary struct {
	Name        string   `json:"name"`
	PolicyCount int      `json:"policy_count"`
	Models      []string `json:"models,omitempty"`
}

type adminKeyCreateRequest struct {
	Policy  string `json:"policy"`
	Expires string `json:"expires,omitempty"`
}

type adminKeyUpdateRequest struct {
	Policy  string `json:"policy,omitempty"`
	Expires string `json:"expires,omitempty"`
}

type adminMemoryFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

func registerAdminRoutes(r chi.Router, cfg *config.Config, tokenStore store.TokenStore, stats *proxy.UsageStats, dataDir string) {
	if cfg == nil || !cfg.Admin.Enabled {
		return
	}
	admin := &adminServer{
		cfg:        cfg,
		tokenStore: tokenStore,
		stats:      stats,
		dataDir:    dataDir,
	}

	adminMW := []func(http.Handler) http.Handler{
		adminLocalOnlyMiddleware,
	}
	if cfg.Admin.Auth.Username != "" && cfg.Admin.Auth.Password != "" {
		adminMW = append(adminMW, adminBasicAuthMiddleware(cfg.Admin.Auth.Username, cfg.Admin.Auth.Password))
	}

	r.Route("/admin/api", func(ar chi.Router) {
		for _, mw := range adminMW {
			ar.Use(mw)
		}
		ar.Get("/health", admin.handleHealth)
		ar.Get("/analytics/summary", admin.handleAnalyticsSummary)
		ar.Get("/keys", admin.handleKeys)
		ar.Post("/keys", admin.handleKeyCreate)
		ar.Put("/keys/{hash}", admin.handleKeyUpdate)
		ar.Delete("/keys/{hash}", admin.handleKeyDelete)
		ar.Get("/env-vars", admin.handleEnvVars)
		ar.Get("/sessions", admin.handleSessions)
		ar.Get("/sessions/{id}", admin.handleSessionDetail)
		ar.Get("/memory/files", admin.handleMemoryFiles)
		ar.Get("/memory/files/*", admin.handleMemoryFileContent)

		// Backward-compatible aliases
		ar.Get("/tokens", admin.handleTokens)
		ar.Get("/tokens/{hash}", admin.handleTokenDetail)
		ar.Get("/usage/summary", admin.handleUsageSummary)
		ar.Get("/usage/tokens/{hash}", admin.handleUsageByToken)
	})

	registerAdminUIRoutes(r, adminMW...)
}

func (a *adminServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "ok",
		"http_port":   a.cfg.Server.HTTPPort,
		"https_port":  a.cfg.Server.HTTPSPort,
		"tls_enabled": a.cfg.Server.TLS.Enabled,
		"admin": map[string]interface{}{
			"enabled":       a.cfg.Admin.Enabled,
			"auth_required": a.cfg.Admin.Auth.Username != "" && a.cfg.Admin.Auth.Password != "",
		},
	})
}

func (a *adminServer) handleAnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	entries := a.stats.Snapshot()
	tokens := a.stats.SessionSnapshot()
	var totalReqs, totalInput, totalOutput, totalTokens, totalErrors int64
	byProvider := map[string]int64{}
	byModel := map[string]int64{}
	for _, e := range entries {
		totalReqs += e.RequestCount
		totalInput += e.InputTokens
		totalOutput += e.OutputTokens
		totalTokens += e.TotalTokens
		totalErrors += e.ErrorCount
		byProvider[e.BaseKeyEnv] += e.TotalTokens
		byModel[e.Model] += e.TotalTokens
	}

	ledgerTotals := map[string]int64{
		"sessions":      0,
		"request_count": 0,
		"input_tokens":  0,
		"output_tokens": 0,
		"total_tokens":  0,
	}
	if sessions, err := ledger.ReadSessionFiles(a.dataDir); err == nil {
		ledgerTotals["sessions"] = int64(len(sessions))
		for _, s := range sessions {
			ledgerTotals["request_count"] += s.Totals.RequestCount
			ledgerTotals["input_tokens"] += s.Totals.InputTokens
			ledgerTotals["output_tokens"] += s.Totals.OutputTokens
			ledgerTotals["total_tokens"] += s.Totals.TotalTokens
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"totals": map[string]int64{
			"request_count": totalReqs,
			"input_tokens":  totalInput,
			"output_tokens": totalOutput,
			"total_tokens":  totalTokens,
			"error_count":   totalErrors,
		},
		"active_tokens": len(tokens),
		"by_provider":   byProvider,
		"by_model":      byModel,
		"ledger":        ledgerTotals,
	})
}

func (a *adminServer) handleKeys(w http.ResponseWriter, r *http.Request) {
	a.handleTokens(w, r)
}

func (a *adminServer) handleKeyCreate(w http.ResponseWriter, r *http.Request) {
	var req adminKeyCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if req.Policy == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "policy is required"})
		return
	}
	if _, err := policy.Parse(req.Policy); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid policy: %v", err)})
		return
	}
	expiresAt, err := parseExpires(req.Expires)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	rawToken := "tkn_" + uuid.New().String()
	hashKey := getHashKey(a.cfg.Security.HashKeyEnv)
	tokenHash := hashToken(rawToken, hashKey)
	if err := a.tokenStore.Create(tokenHash, req.Policy, expiresAt); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("create key failed: %v", err)})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"token":   rawToken,
		"hash":    tokenHash,
		"expires": expiresAt,
		"message": "store this token securely, it is only returned once",
	})
}

func (a *adminServer) handleKeyUpdate(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	var req adminKeyUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
		return
	}
	if req.Policy == "" && req.Expires == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "policy and/or expires required"})
		return
	}
	if req.Policy != "" {
		if _, err := policy.Parse(req.Policy); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid policy: %v", err)})
			return
		}
	}
	expiresAt, err := parseExpires(req.Expires)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := a.tokenStore.Update(hash, req.Policy, expiresAt); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("update key failed: %v", err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (a *adminServer) handleKeyDelete(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	if err := a.tokenStore.Delete(hash); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("delete key failed: %v", err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (a *adminServer) handleEnvVars(w http.ResponseWriter, r *http.Request) {
	names := make([]string, 0)
	seen := map[string]bool{}
	for _, entry := range os.Environ() {
		i := strings.Index(entry, "=")
		if i <= 0 {
			continue
		}
		key := entry[:i]
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, key)
	}
	sort.Strings(names)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"env_vars": names,
	})
}

func (a *adminServer) handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := ledger.ReadSessionFiles(a.dataDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "read sessions failed"})
		return
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt > sessions[j].StartedAt
	})
	agg := map[string]int64{
		"sessions":      int64(len(sessions)),
		"request_count": 0,
		"input_tokens":  0,
		"output_tokens": 0,
		"total_tokens":  0,
	}
	for _, s := range sessions {
		agg["request_count"] += s.Totals.RequestCount
		agg["input_tokens"] += s.Totals.InputTokens
		agg["output_tokens"] += s.Totals.OutputTokens
		agg["total_tokens"] += s.Totals.TotalTokens
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"summary":  agg,
		"sessions": sessions,
	})
}

func (a *adminServer) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sessions, err := ledger.ReadSessionFiles(a.dataDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "read sessions failed"})
		return
	}
	for _, s := range sessions {
		if s.SessionID == id || strings.HasPrefix(s.SessionID, id) {
			writeJSON(w, http.StatusOK, s)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
}

func (a *adminServer) handleMemoryFiles(w http.ResponseWriter, r *http.Request) {
	memDir := filepath.Join(a.dataDir, "memory")
	files := make([]adminMemoryFile, 0)
	_ = filepath.WalkDir(memDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(memDir, p)
		if relErr != nil {
			return nil
		}
		files = append(files, adminMemoryFile{
			Path:    filepath.ToSlash(rel),
			Size:    info.Size(),
			ModTime: info.ModTime().UTC().Format(time.RFC3339),
		})
		return nil
	})
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime > files[j].ModTime
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count": len(files),
		"files": files,
	})
}

func (a *adminServer) handleMemoryFileContent(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
	if rest == "" || strings.Contains(rest, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid file path"})
		return
	}
	memDir := filepath.Join(a.dataDir, "memory")
	target := filepath.Clean(filepath.Join(memDir, rest))
	if !strings.HasPrefix(target, memDir) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid file path"})
		return
	}
	data, err := os.ReadFile(target)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":    filepath.ToSlash(rest),
		"content": string(data),
	})
}

func (a *adminServer) handleTokens(w http.ResponseWriter, r *http.Request) {
	records, err := a.tokenStore.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list tokens failed"})
		return
	}
	out := make([]adminTokenSummary, 0, len(records))
	for _, rec := range records {
		out = append(out, toTokenSummary(rec))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count":  len(out),
		"tokens": out,
	})
}

func (a *adminServer) handleTokenDetail(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	rec, err := a.tokenStore.Get(hash)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "get token failed"})
		return
	}
	if rec == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "token not found"})
		return
	}
	summary := toTokenSummary(*rec)
	writeJSON(w, http.StatusOK, adminTokenDetail{
		adminTokenSummary: summary,
		PolicyRaw:         rec.PolicyRaw,
	})
}

func (a *adminServer) handleUsageSummary(w http.ResponseWriter, r *http.Request) {
	entries := a.stats.Snapshot()
	tokenEntries := a.stats.SessionSnapshot()
	var totalReqs, totalInput, totalOutput, totalTokens int64
	for _, e := range entries {
		totalReqs += e.RequestCount
		totalInput += e.InputTokens
		totalOutput += e.OutputTokens
		totalTokens += e.TotalTokens
	}

	ledgerTotals := map[string]int64{
		"sessions":      0,
		"request_count": 0,
		"input_tokens":  0,
		"output_tokens": 0,
		"total_tokens":  0,
	}
	sessions, err := ledger.ReadSessionFiles(a.dataDir)
	if err == nil {
		ledgerTotals["sessions"] = int64(len(sessions))
		for _, s := range sessions {
			ledgerTotals["request_count"] += s.Totals.RequestCount
			ledgerTotals["input_tokens"] += s.Totals.InputTokens
			ledgerTotals["output_tokens"] += s.Totals.OutputTokens
			ledgerTotals["total_tokens"] += s.Totals.TotalTokens
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"realtime": map[string]interface{}{
			"totals": map[string]int64{
				"request_count": totalReqs,
				"input_tokens":  totalInput,
				"output_tokens": totalOutput,
				"total_tokens":  totalTokens,
			},
			"by_model_and_key": entries,
			"by_token":         tokenEntries,
		},
		"ledger": ledgerTotals,
	})
}

func (a *adminServer) handleUsageByToken(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	realtime := make([]proxy.SessionEntry, 0)
	for _, e := range a.stats.SessionSnapshot() {
		if strings.HasPrefix(hash, e.TokenHash) || strings.HasPrefix(e.TokenHash, hash) {
			realtime = append(realtime, e)
		}
	}

	ledgerRollup := &ledger.TokenRollup{}
	sessions, err := ledger.ReadSessionFiles(a.dataDir)
	if err == nil {
		for _, s := range sessions {
			if tr, ok := s.ByToken[hash]; ok {
				ledgerRollup.RequestCount += tr.RequestCount
				ledgerRollup.InputTokens += tr.InputTokens
				ledgerRollup.OutputTokens += tr.OutputTokens
				ledgerRollup.TotalTokens += tr.TotalTokens
				if ledgerRollup.FirstSeen == "" || tr.FirstSeen < ledgerRollup.FirstSeen {
					ledgerRollup.FirstSeen = tr.FirstSeen
				}
				if tr.LastSeen > ledgerRollup.LastSeen {
					ledgerRollup.LastSeen = tr.LastSeen
				}
				for _, model := range tr.ModelsUsed {
					if !containsModel(ledgerRollup.ModelsUsed, model) {
						ledgerRollup.ModelsUsed = append(ledgerRollup.ModelsUsed, model)
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token_hash": hash,
		"realtime":   realtime,
		"ledger":     ledgerRollup,
	})
}

func toTokenSummary(rec store.TokenRecord) adminTokenSummary {
	p := rec.Policy
	if p == nil {
		p = &policy.Policy{}
	}
	providers := make([]adminProviderSummary, 0, len(p.Providers))
	for name, policies := range p.Providers {
		models := make([]string, 0)
		for _, pp := range policies {
			if pp.Model != "" {
				models = append(models, pp.Model)
			}
			if pp.ModelRegex != "" {
				models = append(models, "regex:"+pp.ModelRegex)
			}
		}
		sort.Strings(models)
		providers = append(providers, adminProviderSummary{
			Name:        name,
			PolicyCount: len(policies),
			Models:      models,
		})
	}
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].Name < providers[j].Name
	})

	return adminTokenSummary{
		TokenHash: rec.TokenHash,
		CreatedAt: rec.CreatedAt,
		ExpiresAt: rec.ExpiresAt,
		Policy: adminPolicySummary{
			BaseKeyEnv:      p.BaseKeyEnv,
			DefaultProvider: p.DefaultProvider,
			MaxTokens:       p.MaxTokens,
			RuleCount:       len(p.Rules),
		},
		Providers: providers,
	}
}

func adminLocalOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin UI/API is local-only"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func adminBasicAuthMiddleware(username, password string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok || u != username || p != password {
				w.Header().Set("WWW-Authenticate", `Basic realm="tokenomics-admin"`)
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func containsModel(models []string, candidate string) bool {
	for _, m := range models {
		if m == candidate {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
