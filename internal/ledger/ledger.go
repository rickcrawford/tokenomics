package ledger

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rickcrawford/tokenomics/internal/session"
)

// Ledger records per-session token usage to disk in .tokenomics/.
type Ledger struct {
	dir       string
	sessionID string
	startedAt time.Time
	gitInfo   GitInfo
	memory    bool

	memWriter session.MemoryWriter

	mu       sync.Mutex
	requests []RequestEntry
}

// RequestEntry captures a single proxied request.
type RequestEntry struct {
	Timestamp         time.Time         `json:"timestamp"`
	TokenHash         string            `json:"token_hash"`
	Model             string            `json:"model"`
	Provider          string            `json:"provider"`
	InputTokens       int               `json:"input_tokens"`
	OutputTokens      int               `json:"output_tokens"`
	DurationMs        int64             `json:"duration_ms"`
	StatusCode        int               `json:"status_code"`
	Stream            bool              `json:"stream,omitempty"`
	Error             string            `json:"error,omitempty"`
	UpstreamID        string            `json:"upstream_id,omitempty"`
	UpstreamRequestID string            `json:"upstream_request_id,omitempty"`
	RetryCount        int               `json:"retry_count,omitempty"`
	FallbackModel     string            `json:"fallback_model,omitempty"`
	RuleMatches       []RuleMatchEntry  `json:"rule_matches,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	ProviderMeta      *ProviderMeta     `json:"provider_meta,omitempty"`
}

// RuleMatchEntry records a content rule match.
type RuleMatchEntry struct {
	Name    string `json:"name,omitempty"`
	Action  string `json:"action"`
	Message string `json:"message"`
}

// ProviderMeta holds normalized metadata from the provider's response.
type ProviderMeta struct {
	CachedInputTokens  int    `json:"cached_input_tokens,omitempty"`
	CacheCreationTokens int   `json:"cache_creation_tokens,omitempty"`
	ReasoningTokens    int    `json:"reasoning_tokens,omitempty"`
	ActualModel        string `json:"actual_model,omitempty"`
	FinishReason       string `json:"finish_reason,omitempty"`

	RateLimitRemainingRequests int    `json:"rate_limit_remaining_requests,omitempty"`
	RateLimitRemainingTokens   int    `json:"rate_limit_remaining_tokens,omitempty"`
	RateLimitReset             string `json:"rate_limit_reset,omitempty"`
}

// UsageRollup holds aggregated token counts for a grouping dimension.
type UsageRollup struct {
	RequestCount        int64    `json:"request_count"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	TotalTokens         int64    `json:"total_tokens"`
	CachedInputTokens   int64    `json:"cached_input_tokens,omitempty"`
	CacheCreationTokens int64    `json:"cache_creation_tokens,omitempty"`
	ReasoningTokens     int64    `json:"reasoning_tokens,omitempty"`
}

// TokenRollup extends UsageRollup with per-token tracking fields.
type TokenRollup struct {
	UsageRollup
	ModelsUsed []string `json:"models_used"`
	FirstSeen  string   `json:"first_seen"`
	LastSeen   string   `json:"last_seen"`
}

// SessionTotals extends UsageRollup with error and operational counters.
type SessionTotals struct {
	UsageRollup
	ErrorCount         int64 `json:"error_count"`
	RetryCount         int64 `json:"retry_count"`
	RuleViolationCount int64 `json:"rule_violation_count"`
	RateLimitCount     int64 `json:"rate_limit_count"`
}

// SessionSummary is the top-level JSON written to sessions/<date>_<id>.json.
type SessionSummary struct {
	SessionID  string                  `json:"session_id"`
	StartedAt  string                  `json:"started_at"`
	EndedAt    string                  `json:"ended_at"`
	DurationMs int64                   `json:"duration_ms"`
	Git        GitInfo                 `json:"git"`
	Totals     SessionTotals           `json:"totals"`
	ByModel    map[string]*UsageRollup `json:"by_model"`
	ByProvider map[string]*UsageRollup `json:"by_provider"`
	ByToken    map[string]*TokenRollup `json:"by_token"`
	Requests   []RequestEntry          `json:"requests"`
}

// Open creates a new Ledger session. It creates the .tokenomics/sessions/
// and .tokenomics/memory/ directories if they don't exist, snapshots git
// context, and generates a session ID.
func Open(dir string, memory bool) (*Ledger, error) {
	sessDir := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}

	var memWriter session.MemoryWriter
	if memory {
		memDir := filepath.Join(dir, "memory")
		if err := os.MkdirAll(memDir, 0o755); err != nil {
			return nil, fmt.Errorf("create memory dir: %w", err)
		}
		w, err := session.NewDirMemoryWriter(memDir, "{date}_{token_hash}.md")
		if err != nil {
			return nil, fmt.Errorf("create memory writer: %w", err)
		}
		memWriter = w
	}

	id := generateSessionID()
	return &Ledger{
		dir:       dir,
		sessionID: id,
		startedAt: time.Now().UTC(),
		gitInfo:   snapshotGit(),
		memory:    memory,
		memWriter: memWriter,
	}, nil
}

// RecordRequest appends a request entry to the session. Thread-safe.
func (l *Ledger) RecordRequest(entry RequestEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = append(l.requests, entry)
}

// RecordMemory writes conversation content to the memory log.
func (l *Ledger) RecordMemory(tokenHash, role, model, content string) error {
	if l.memWriter == nil {
		return nil
	}
	return l.memWriter.Append(tokenHash, role, model, content)
}

// SessionID returns the current session's ID.
func (l *Ledger) SessionID() string {
	return l.sessionID
}

// Close finalizes the session: snapshots git end state, computes rollups,
// writes the session JSON, and closes the memory writer.
func (l *Ledger) Close() error {
	l.mu.Lock()
	requests := make([]RequestEntry, len(l.requests))
	copy(requests, l.requests)
	l.mu.Unlock()

	endedAt := time.Now().UTC()
	l.gitInfo.CommitEnd = snapshotGitEnd()

	summary := l.buildSummary(requests, endedAt)

	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	filename := fmt.Sprintf("%s_%s.json", l.startedAt.Format("2006-01-02"), l.sessionID)
	path := filepath.Join(l.dir, "sessions", filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write session file: %w", err)
	}

	if l.memWriter != nil {
		return l.memWriter.Close()
	}
	return nil
}

func (l *Ledger) buildSummary(requests []RequestEntry, endedAt time.Time) *SessionSummary {
	summary := &SessionSummary{
		SessionID:  l.sessionID,
		StartedAt:  l.startedAt.Format(time.RFC3339),
		EndedAt:    endedAt.Format(time.RFC3339),
		DurationMs: endedAt.Sub(l.startedAt).Milliseconds(),
		Git:        l.gitInfo,
		ByModel:    make(map[string]*UsageRollup),
		ByProvider: make(map[string]*UsageRollup),
		ByToken:    make(map[string]*TokenRollup),
		Requests:   requests,
	}

	for _, req := range requests {
		in := int64(req.InputTokens)
		out := int64(req.OutputTokens)
		total := in + out

		var cached, cacheCreate, reasoning int64
		if req.ProviderMeta != nil {
			cached = int64(req.ProviderMeta.CachedInputTokens)
			cacheCreate = int64(req.ProviderMeta.CacheCreationTokens)
			reasoning = int64(req.ProviderMeta.ReasoningTokens)
		}

		// Totals
		summary.Totals.RequestCount++
		summary.Totals.InputTokens += in
		summary.Totals.OutputTokens += out
		summary.Totals.TotalTokens += total
		summary.Totals.CachedInputTokens += cached
		summary.Totals.CacheCreationTokens += cacheCreate
		summary.Totals.ReasoningTokens += reasoning
		summary.Totals.RetryCount += int64(req.RetryCount)

		if req.StatusCode >= 400 {
			summary.Totals.ErrorCount++
		}
		if req.StatusCode == 429 {
			summary.Totals.RateLimitCount++
		}
		for _, rm := range req.RuleMatches {
			if rm.Action == "fail" {
				summary.Totals.RuleViolationCount++
			}
		}

		// By model
		addToRollup(summary.ByModel, req.Model, in, out, cached, cacheCreate, reasoning)

		// By provider
		if req.Provider != "" {
			addToRollup(summary.ByProvider, req.Provider, in, out, cached, cacheCreate, reasoning)
		}

		// By token
		tr, ok := summary.ByToken[req.TokenHash]
		if !ok {
			tr = &TokenRollup{
				FirstSeen: req.Timestamp.Format(time.RFC3339),
			}
			summary.ByToken[req.TokenHash] = tr
		}
		tr.RequestCount++
		tr.InputTokens += in
		tr.OutputTokens += out
		tr.TotalTokens += total
		tr.LastSeen = req.Timestamp.Format(time.RFC3339)
		if !containsStr(tr.ModelsUsed, req.Model) {
			tr.ModelsUsed = append(tr.ModelsUsed, req.Model)
		}
	}

	return summary
}

func addToRollup(m map[string]*UsageRollup, key string, in, out, cached, cacheCreate, reasoning int64) {
	r, ok := m[key]
	if !ok {
		r = &UsageRollup{}
		m[key] = r
	}
	r.RequestCount++
	r.InputTokens += in
	r.OutputTokens += out
	r.TotalTokens += in + out
	r.CachedInputTokens += cached
	r.CacheCreationTokens += cacheCreate
	r.ReasoningTokens += reasoning
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func generateSessionID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	return hex.EncodeToString(b)
}

// ReadSessionFiles reads all session summary JSON files from the given directory.
func ReadSessionFiles(dir string) ([]*SessionSummary, error) {
	sessDir := filepath.Join(dir, "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	var sessions []*SessionSummary
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sessDir, entry.Name()))
		if err != nil {
			continue
		}
		var s SessionSummary
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		sessions = append(sessions, &s)
	}
	return sessions, nil
}
