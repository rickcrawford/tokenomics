package proxy

import (
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"
)

// UsageStats tracks token usage by model/key and by token hash.
type UsageStats struct {
	mu             sync.RWMutex
	entries        map[statsKey]*UsageEntry
	sessionEntries map[string]*SessionEntry
}

type statsKey struct {
	Model      string
	BaseKeyEnv string
}

// UsageEntry holds aggregated usage for a model/key combination.
type UsageEntry struct {
	Model        string `json:"model"`
	BaseKeyEnv   string `json:"base_key_env"`
	RequestCount int64  `json:"request_count"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
	ErrorCount   int64  `json:"error_count"`
}

// SessionEntry holds usage for a specific wrapper token (session).
type SessionEntry struct {
	TokenHash    string    `json:"token_hash"`
	RequestCount int64     `json:"request_count"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	TotalTokens  int64     `json:"total_tokens"`
	ErrorCount   int64     `json:"error_count"`
	LastModel    string    `json:"last_model"`
	BaseKeyEnv   string    `json:"base_key_env"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
}

func NewUsageStats() *UsageStats {
	return &UsageStats{
		entries:        make(map[statsKey]*UsageEntry),
		sessionEntries: make(map[string]*SessionEntry),
	}
}

// Record adds a request's usage to both aggregated stats and per-token session stats.
func (u *UsageStats) Record(tokenHash, model, baseKeyEnv string, inputTokens, outputTokens int, isError bool) {
	u.mu.Lock()
	defer u.mu.Unlock()

	now := time.Now().UTC()

	// Aggregate by model + key
	key := statsKey{Model: model, BaseKeyEnv: baseKeyEnv}
	entry, ok := u.entries[key]
	if !ok {
		entry = &UsageEntry{
			Model:      model,
			BaseKeyEnv: baseKeyEnv,
		}
		u.entries[key] = entry
	}

	entry.RequestCount++
	entry.InputTokens += int64(inputTokens)
	entry.OutputTokens += int64(outputTokens)
	entry.TotalTokens += int64(inputTokens + outputTokens)
	if isError {
		entry.ErrorCount++
	}

	// Track per-token session (use truncated hash for display)
	displayHash := tokenHash
	if len(displayHash) > 16 {
		displayHash = displayHash[:16]
	}
	sess, ok := u.sessionEntries[displayHash]
	if !ok {
		sess = &SessionEntry{
			TokenHash:  displayHash,
			BaseKeyEnv: baseKeyEnv,
			FirstSeen:  now,
		}
		u.sessionEntries[displayHash] = sess
	}

	sess.RequestCount++
	sess.InputTokens += int64(inputTokens)
	sess.OutputTokens += int64(outputTokens)
	sess.TotalTokens += int64(inputTokens + outputTokens)
	sess.LastModel = model
	sess.LastSeen = now
	if isError {
		sess.ErrorCount++
	}
}

// Snapshot returns a copy of all current usage entries.
func (u *UsageStats) Snapshot() []UsageEntry {
	u.mu.RLock()
	defer u.mu.RUnlock()

	result := make([]UsageEntry, 0, len(u.entries))
	for _, e := range u.entries {
		result = append(result, *e)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalTokens > result[j].TotalTokens
	})

	return result
}

// SessionSnapshot returns a copy of all per-token session entries.
func (u *UsageStats) SessionSnapshot() []SessionEntry {
	u.mu.RLock()
	defer u.mu.RUnlock()

	result := make([]SessionEntry, 0, len(u.sessionEntries))
	for _, e := range u.sessionEntries {
		result = append(result, *e)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalTokens > result[j].TotalTokens
	})

	return result
}

// StatsHandler serves the /stats endpoint.
func (u *UsageStats) StatsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	entries := u.Snapshot()
	sessions := u.SessionSnapshot()

	// Compute totals
	var totalReqs, totalInput, totalOutput, totalAll, totalErrors int64
	for _, e := range entries {
		totalReqs += e.RequestCount
		totalInput += e.InputTokens
		totalOutput += e.OutputTokens
		totalAll += e.TotalTokens
		totalErrors += e.ErrorCount
	}

	resp := map[string]interface{}{
		"totals": map[string]int64{
			"request_count": totalReqs,
			"input_tokens":  totalInput,
			"output_tokens": totalOutput,
			"total_tokens":  totalAll,
			"error_count":   totalErrors,
		},
		"by_model_and_key": entries,
		"by_token":         sessions,
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(resp); err != nil {
		http.Error(w, `{"error":"encode stats response"}`, http.StatusInternalServerError)
	}
}
