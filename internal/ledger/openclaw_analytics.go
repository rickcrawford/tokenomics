package ledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// OpenClawMetricValue represents an aggregated metric for an OpenClaw dimension.
type OpenClawMetricValue struct {
	Value               string            `json:"value"`
	RequestCount        int64             `json:"request_count"`
	InputTokens         int64             `json:"input_tokens"`
	OutputTokens        int64             `json:"output_tokens"`
	TotalTokens         int64             `json:"total_tokens"`
	CachedInputTokens   int64             `json:"cached_input_tokens,omitempty"`
	CacheCreationTokens int64             `json:"cache_creation_tokens,omitempty"`
	ReasoningTokens     int64             `json:"reasoning_tokens,omitempty"`
	ErrorCount          int64             `json:"error_count"`
	Sessions            int64             `json:"session_count"`
	ModelsUsed          []string          `json:"models_used,omitempty"`
}

// OpenClawAnalytics provides cost attribution and analytics by OpenClaw metadata fields.
type OpenClawAnalytics struct {
	dir string // Directory containing sessions/
}

// NewOpenClawAnalytics creates a new analytics engine for a .tokenomics directory.
func NewOpenClawAnalytics(dir string) *OpenClawAnalytics {
	return &OpenClawAnalytics{dir: dir}
}

// ByMetadataKey aggregates metrics grouped by a specific metadata key.
// key should be one of: agent_id, agent_type, team, channel, skill, environment
// Returns map of metadata value -> aggregated metrics, sorted by total tokens descending.
func (oca *OpenClawAnalytics) ByMetadataKey(key string) (map[string]*OpenClawMetricValue, error) {
	sessions, err := oca.loadSessions()
	if err != nil {
		return nil, err
	}

	metrics := make(map[string]*OpenClawMetricValue)
	seenSessions := make(map[string]map[string]bool) // Track which sessions contributed to each metric
	modelsPerValue := make(map[string]map[string]bool) // Track unique models per value

	for _, session := range sessions {
		for _, req := range session.Requests {
			if req.Metadata == nil {
				continue
			}

			value, ok := req.Metadata[key]
			if !ok || value == "" {
				continue
			}

			if metrics[value] == nil {
				metrics[value] = &OpenClawMetricValue{Value: value}
				seenSessions[value] = make(map[string]bool)
				modelsPerValue[value] = make(map[string]bool)
			}

			m := metrics[value]
			m.RequestCount++
			m.InputTokens += int64(req.InputTokens)
			m.OutputTokens += int64(req.OutputTokens)
			m.TotalTokens += int64(req.InputTokens + req.OutputTokens)

			if req.ProviderMeta != nil {
				m.CachedInputTokens += int64(req.ProviderMeta.CachedInputTokens)
				m.CacheCreationTokens += int64(req.ProviderMeta.CacheCreationTokens)
				m.ReasoningTokens += int64(req.ProviderMeta.ReasoningTokens)
			}

			if req.Error != "" {
				m.ErrorCount++
			}

			seenSessions[value][session.SessionID] = true
			if req.Model != "" {
				modelsPerValue[value][req.Model] = true
			}
		}
	}

	// Set session counts and unique models
	for value, metric := range metrics {
		metric.Sessions = int64(len(seenSessions[value]))
		if len(modelsPerValue[value]) > 0 {
			models := make([]string, 0, len(modelsPerValue[value]))
			for m := range modelsPerValue[value] {
				models = append(models, m)
			}
			sort.Strings(models)
			metric.ModelsUsed = models
		}
	}

	return metrics, nil
}

// ByTeamAndChannel aggregates metrics grouped by team and channel combination.
// Useful for fine-grained cost breakdown for multi-team deployments.
// Returns map of "team/channel" -> metrics, sorted by total tokens descending.
func (oca *OpenClawAnalytics) ByTeamAndChannel() (map[string]*OpenClawMetricValue, error) {
	sessions, err := oca.loadSessions()
	if err != nil {
		return nil, err
	}

	metrics := make(map[string]*OpenClawMetricValue)
	seenSessions := make(map[string]map[string]bool)
	modelsPerValue := make(map[string]map[string]bool)

	for _, session := range sessions {
		for _, req := range session.Requests {
			if req.Metadata == nil {
				continue
			}

			team := req.Metadata["team"]
			channel := req.Metadata["channel"]

			// Skip if either is missing
			if team == "" || channel == "" {
				continue
			}

			key := fmt.Sprintf("%s/%s", team, channel)

			if metrics[key] == nil {
				metrics[key] = &OpenClawMetricValue{Value: key}
				seenSessions[key] = make(map[string]bool)
				modelsPerValue[key] = make(map[string]bool)
			}

			m := metrics[key]
			m.RequestCount++
			m.InputTokens += int64(req.InputTokens)
			m.OutputTokens += int64(req.OutputTokens)
			m.TotalTokens += int64(req.InputTokens + req.OutputTokens)

			if req.ProviderMeta != nil {
				m.CachedInputTokens += int64(req.ProviderMeta.CachedInputTokens)
				m.CacheCreationTokens += int64(req.ProviderMeta.CacheCreationTokens)
				m.ReasoningTokens += int64(req.ProviderMeta.ReasoningTokens)
			}

			if req.Error != "" {
				m.ErrorCount++
			}

			seenSessions[key][session.SessionID] = true
			if req.Model != "" {
				modelsPerValue[key][req.Model] = true
			}
		}
	}

	// Set session counts and unique models
	for key, metric := range metrics {
		metric.Sessions = int64(len(seenSessions[key]))
		if len(modelsPerValue[key]) > 0 {
			models := make([]string, 0, len(modelsPerValue[key]))
			for m := range modelsPerValue[key] {
				models = append(models, m)
			}
			sort.Strings(models)
			metric.ModelsUsed = models
		}
	}

	return metrics, nil
}

// SessionFilter describes criteria for filtering sessions.
type SessionFilter struct {
	StartTime   time.Time // Only sessions that started after this time
	EndTime     time.Time // Only sessions that ended before this time
	MetadataKey string    // Metadata key to filter by (e.g., "team")
	MetadataVal string    // Metadata value to match
	TokenHash   string    // Optional: filter by token hash prefix
}

// FilteredSessions returns sessions matching the provided filter criteria.
// Used for detailed session-level analysis.
func (oca *OpenClawAnalytics) FilteredSessions(filter SessionFilter) ([]*SessionSummary, error) {
	sessions, err := oca.loadSessions()
	if err != nil {
		return nil, err
	}

	var filtered []*SessionSummary

	for _, session := range sessions {
		// Check time range
		startedAt, err := time.Parse(time.RFC3339, session.StartedAt)
		if err != nil {
			continue
		}
		if !filter.StartTime.IsZero() && startedAt.Before(filter.StartTime) {
			continue
		}

		endedAt, err := time.Parse(time.RFC3339, session.EndedAt)
		if err != nil {
			continue
		}
		if !filter.EndTime.IsZero() && endedAt.After(filter.EndTime) {
			continue
		}

		// Check metadata match
		if filter.MetadataKey != "" || filter.MetadataVal != "" {
			found := false
			for _, req := range session.Requests {
				if req.Metadata == nil {
					continue
				}
				if val, ok := req.Metadata[filter.MetadataKey]; ok && val == filter.MetadataVal {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Check token hash match
		if filter.TokenHash != "" {
			found := false
			for _, req := range session.Requests {
				if strings.HasPrefix(req.TokenHash, filter.TokenHash) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		filtered = append(filtered, session)
	}

	return filtered, nil
}

// loadSessions loads all session JSON files from the sessions directory.
func (oca *OpenClawAnalytics) loadSessions() ([]*SessionSummary, error) {
	sessDir := filepath.Join(oca.dir, "sessions")

	// Check if directory exists
	info, err := os.Stat(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No sessions yet
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("sessions path is not a directory: %s", sessDir)
	}

	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return nil, err
	}

	var sessions []*SessionSummary

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(sessDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue // Skip unreadable files
		}

		var session SessionSummary
		if err := json.Unmarshal(data, &session); err != nil {
			continue // Skip malformed JSON
		}

		sessions = append(sessions, &session)
	}

	return sessions, nil
}
