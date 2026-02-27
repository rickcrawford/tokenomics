package proxy

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/events"
	"github.com/rickcrawford/tokenomics/internal/ledger"
	"github.com/rickcrawford/tokenomics/internal/policy"
	"github.com/rickcrawford/tokenomics/internal/session"
	"github.com/rickcrawford/tokenomics/internal/store"
)

// maxRequestBodySize limits incoming request body reads (10 MB).
const maxRequestBodySize = 10 * 1024 * 1024

// generateRequestID creates a unique request ID for upstream correlation.
func generateRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("tkn_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("tkn_%x", b)
}

// safePrefix returns the first n characters of s, or all of s if shorter.
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// OpenClawMetadata holds optional metadata from OpenClaw agents
type OpenClawMetadata struct {
	AgentID     string
	AgentType   string
	Team        string
	Channel     string
	Skill       string
	Environment string
}

// extractOpenClawMetadata extracts optional OpenClaw metadata from request headers
// All headers are optional - only populated if present
func extractOpenClawMetadata(r *http.Request) OpenClawMetadata {
	return OpenClawMetadata{
		AgentID:     r.Header.Get("X-OpenClaw-Agent-ID"),
		AgentType:   r.Header.Get("X-OpenClaw-Agent-Type"),
		Team:        r.Header.Get("X-OpenClaw-Team"),
		Channel:     r.Header.Get("X-OpenClaw-Channel"),
		Skill:       r.Header.Get("X-OpenClaw-Skill"),
		Environment: r.Header.Get("X-OpenClaw-Environment"),
	}
}

// openClawMetadataToMap converts OpenClawMetadata to a map[string]string,
// excluding empty values to keep the metadata compact
func openClawMetadataToMap(meta OpenClawMetadata) map[string]string {
	result := make(map[string]string)
	if meta.AgentID != "" {
		result["agent_id"] = meta.AgentID
	}
	if meta.AgentType != "" {
		result["agent_type"] = meta.AgentType
	}
	if meta.Team != "" {
		result["team"] = meta.Team
	}
	if meta.Channel != "" {
		result["channel"] = meta.Channel
	}
	if meta.Skill != "" {
		result["skill"] = meta.Skill
	}
	if meta.Environment != "" {
		result["environment"] = meta.Environment
	}
	// Return nil if no metadata was set to avoid empty maps in JSON
	if len(result) == 0 {
		return nil
	}
	return result
}

type Handler struct {
	store           store.TokenStore
	sessions        session.Store
	stats           *UsageStats
	rateLimiter     *RateLimiter
	emitter         events.Emitter
	hashKey         []byte
	upstreamURL     string
	providers       map[string]config.ProviderConfig
	defaultProvider string // Default provider when policy doesn't specify one
	logging         config.LoggingConfig
	ledger          *ledger.Ledger
	client          *http.Client
	debugLogDir     string // Directory for debug logs (from config)

	memWriterMu    sync.Mutex
	memWriters     map[string]session.MemoryWriter
	redisMemWriter session.MemoryWriter
	budgetMu       sync.Mutex
}

func NewHandler(s store.TokenStore, sess session.Store, hashKey []byte, upstreamURL string, providers map[string]config.ProviderConfig, emitter events.Emitter) *Handler {
	if providers == nil {
		providers = make(map[string]config.ProviderConfig)
	}
	if emitter == nil {
		emitter = events.Nop{}
	}
	return &Handler{
		store:       s,
		sessions:    sess,
		stats:       NewUsageStats(),
		rateLimiter: NewRateLimiter(),
		emitter:     emitter,
		hashKey:     hashKey,
		upstreamURL: upstreamURL,
		providers:   providers,
		memWriters:  make(map[string]session.MemoryWriter),
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// SetLogging configures logging behavior from the config.
func (h *Handler) SetLogging(cfg config.LoggingConfig) {
	h.logging = cfg
}

// SetDefaultProvider sets the default provider to use when policies don't specify one.
func (h *Handler) SetDefaultProvider(provider string) {
	h.defaultProvider = provider
}

// SetLedger configures the session ledger for token usage tracking.
func (h *Handler) SetLedger(l *ledger.Ledger) {
	h.ledger = l
}

// SetDebugLogDir sets the directory for debug logs from the configured path.
func (h *Handler) SetDebugLogDir(dir string) {
	h.debugLogDir = dir
}

// Stats returns the UsageStats for registering the stats endpoint.
func (h *Handler) Stats() *UsageStats {
	return h.stats
}

// SetRedisMemoryWriter sets the Redis-backed memory writer for session logging.
func (h *Handler) SetRedisMemoryWriter(w session.MemoryWriter) {
	h.memWriterMu.Lock()
	defer h.memWriterMu.Unlock()
	h.redisMemWriter = w
}

// getMemoryWriter returns the appropriate memory writer for the given config.
// Returns nil if memory is disabled.
// When FileName is set, file_path is treated as a directory and each session
// gets its own file based on the name pattern. Otherwise file_path is a single file.
func (h *Handler) getMemoryWriter(mc policy.MemoryConfig) session.MemoryWriter {
	if !mc.Enabled {
		return nil
	}

	h.memWriterMu.Lock()
	defer h.memWriterMu.Unlock()

	filePath := mc.FilePath
	// Default to ~/.tokenomics/memory if not specified
	if filePath == "" {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			filePath = filepath.Join(homeDir, ".tokenomics", "memory")
		} else {
			filePath = filepath.Join(".tokenomics", "memory")
		}
	}

	if filePath != "" {
		// Per-session files: file_path is a directory, file_name is the pattern
		if mc.FileName != "" {
			key := filePath + ":" + mc.FileName
			if w, ok := h.memWriters[key]; ok {
				return w
			}

			// Use rotating writer if max size is set (or default 100 MB)
			// compressOld defaults to true
			if mc.MaxSizeMB != 0 || mc.CompressOld {
				w, err := session.NewRotatingDirMemoryWriter(filePath, mc.FileName, mc.MaxSizeMB, mc.CompressOld)
				if err != nil {
					log.Printf("[memory] failed to create rotating dir writer for %q: %v", filePath, err)
					return nil
				}
				h.memWriters[key] = w
				return w
			}

			// Use regular dir writer if no rotation configured
			w, err := session.NewDirMemoryWriter(filePath, mc.FileName)
			if err != nil {
				log.Printf("[memory] failed to create dir writer for %q: %v", filePath, err)
				return nil
			}
			h.memWriters[key] = w
			return w
		}

		// Legacy single-file mode
		if w, ok := h.memWriters[filePath]; ok {
			return w
		}
		w, err := session.NewFileMemoryWriter(filePath)
		if err != nil {
			log.Printf("[memory] failed to create file writer for %q: %v", filePath, err)
			return nil
		}
		h.memWriters[filePath] = w
		return w
	}
	if mc.Redis && h.redisMemWriter != nil {
		return h.redisMemWriter
	}
	return nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Extract token from multiple sources (in order of preference):
	// 1. x-api-key header (Anthropic, Gemini style)
	// 2. Authorization: Bearer {token} (OpenAI style)
	// 3. Authorization: {token} (raw token)
	var rawToken string

	if tk := r.Header.Get("x-api-key"); tk != "" {
		rawToken = tk
	} else if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			rawToken = strings.TrimPrefix(auth, "Bearer ")
		} else {
			// Also accept raw token without Bearer prefix
			rawToken = auth
		}
	}

	if rawToken == "" {
		httpError(w, http.StatusUnauthorized, "missing or invalid token (use x-api-key header or Authorization bearer token)")
		logRequest(&RequestLog{
			Timestamp:  start.UTC().Format(time.RFC3339Nano),
			Method:     r.Method,
			Path:       r.URL.Path,
			StatusCode: http.StatusUnauthorized,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      "missing or invalid token",
			RemoteAddr: r.RemoteAddr,
			UserAgent:  r.UserAgent(),
		})
		return
	}

	// Hash token
	tokenHash := h.hashToken(rawToken)

	// Log token extraction (to file)
	debugLog("Token extracted from header")
	debugLog("Token hash: %s", safePrefix(tokenHash, 16))

	// Lookup policy
	pol, err := h.store.Lookup(tokenHash)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "store lookup error")
		logRequest(&RequestLog{
			Timestamp:  start.UTC().Format(time.RFC3339Nano),
			Method:     r.Method,
			Path:       r.URL.Path,
			TokenHash:  safePrefix(tokenHash, 16),
			StatusCode: http.StatusInternalServerError,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      "store lookup error",
			RemoteAddr: r.RemoteAddr,
			UserAgent:  r.UserAgent(),
		})
		return
	}
	if pol == nil {
		httpError(w, http.StatusUnauthorized, "invalid token")
		logRequest(&RequestLog{
			Timestamp:  start.UTC().Format(time.RFC3339Nano),
			Method:     r.Method,
			Path:       r.URL.Path,
			TokenHash:  safePrefix(tokenHash, 16),
			StatusCode: http.StatusUnauthorized,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      "invalid token",
			RemoteAddr: r.RemoteAddr,
			UserAgent:  r.UserAgent(),
		})
		return
	}

	// Default upstream from global config
	upstream := h.upstreamURL
	if pol.UpstreamURL != "" {
		upstream = pol.UpstreamURL
	}

	// Extract OpenClaw metadata from request headers
	openClawMeta := extractOpenClawMetadata(r)

	// For chat completions, apply policy engine with multi-provider resolution
	if isChatCompletions(r.URL.Path) {
		h.handleChatCompletions(w, r, pol, tokenHash, upstream, start, openClawMeta)
		return
	}

	// For all other /v1/* endpoints, passthrough with key swap
	h.passthrough(w, r, pol, tokenHash, upstream, start, openClawMeta)
}

func (h *Handler) hashToken(token string) string {
	mac := hmac.New(sha256.New, h.hashKey)
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

func isChatCompletions(path string) bool {
	return strings.HasSuffix(path, "/chat/completions")
}

func copyHeaders(src, dst http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": msg,
			"type":    "tokenomics_error",
			"code":    code,
		},
	}); err != nil {
		log.Printf("httpError encode failed: %v", err)
	}
}
