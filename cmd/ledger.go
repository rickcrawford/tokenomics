package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/ledger"
	"github.com/spf13/cobra"
)

var ledgerDir string

var ledgerCmd = &cobra.Command{
	Use:   "ledger",
	Short: "View and manage session ledger data",
	Long:  `Commands for viewing token usage sessions recorded in the .tokenomics/ directory.`,
}

var ledgerSummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Show aggregated token usage across all sessions",
	Example: `  tokenomics ledger summary
  tokenomics ledger summary --dir .tokenomics`,
	RunE: runLedgerSummary,
}

var ledgerSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List recorded sessions",
	Example: `  tokenomics ledger sessions
  tokenomics ledger sessions --json`,
	RunE: runLedgerSessions,
}

var ledgerShowCmd = &cobra.Command{
	Use:   "show [session-id]",
	Short: "Show details for a specific session",
	Args:  cobra.ExactArgs(1),
	Example: `  tokenomics ledger show abc12345
  tokenomics ledger show abc12345 --json`,
	RunE: runLedgerShow,
}

var ledgerJSON bool

func init() {
	rootCmd.AddCommand(ledgerCmd)
	ledgerCmd.PersistentFlags().StringVar(&ledgerDir, "dir", "", "ledger directory (default: from config or .tokenomics)")
	ledgerCmd.PersistentFlags().BoolVar(&ledgerJSON, "json", false, "output as JSON")

	ledgerCmd.AddCommand(ledgerSummaryCmd)
	ledgerCmd.AddCommand(ledgerSessionsCmd)
	ledgerCmd.AddCommand(ledgerShowCmd)
}

func getLedgerDir() string {
	if ledgerDir != "" {
		return ledgerDir
	}
	// Try loading from config
	cfg, err := config.Load(cfgFile)
	if err == nil {
		dir := cfg.Dir
		if dir == "" {
			homeDir, err := os.UserHomeDir()
			if err == nil {
				dir = filepath.Join(homeDir, ".tokenomics")
			}
		}
		if !filepath.IsAbs(dir) {
			if abs, err := filepath.Abs(dir); err == nil {
				dir = abs
			}
		}
		return dir
	}
	// Fallback to home directory
	homeDir, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(homeDir, ".tokenomics")
	}
	return ".tokenomics"
}

func runLedgerSummary(cmd *cobra.Command, args []string) error {
	dir := getLedgerDir()
	sessions, err := ledger.ReadSessionFiles(dir)
	if err != nil {
		return fmt.Errorf("read sessions: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	// Aggregate across all sessions
	totals := ledger.SessionTotals{}
	byModel := make(map[string]*ledger.UsageRollup)
	byProvider := make(map[string]*ledger.UsageRollup)
	byToken := make(map[string]*ledger.UsageRollup)

	for _, s := range sessions {
		totals.RequestCount += s.Totals.RequestCount
		totals.InputTokens += s.Totals.InputTokens
		totals.OutputTokens += s.Totals.OutputTokens
		totals.TotalTokens += s.Totals.TotalTokens
		totals.CachedInputTokens += s.Totals.CachedInputTokens
		totals.CacheCreationTokens += s.Totals.CacheCreationTokens
		totals.ReasoningTokens += s.Totals.ReasoningTokens
		totals.ErrorCount += s.Totals.ErrorCount
		totals.RetryCount += s.Totals.RetryCount
		totals.RuleViolationCount += s.Totals.RuleViolationCount
		totals.RateLimitCount += s.Totals.RateLimitCount

		mergeRollups(byModel, s.ByModel)
		mergeRollups(byProvider, s.ByProvider)
		for k, v := range s.ByToken {
			mergeRollup(byToken, k, &v.UsageRollup)
		}
	}

	if ledgerJSON {
		out := map[string]interface{}{
			"sessions":    len(sessions),
			"totals":      totals,
			"by_model":    byModel,
			"by_provider": byProvider,
			"by_token":    byToken,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	fmt.Printf("Sessions: %d\n\n", len(sessions))

	fmt.Println("Totals:")
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  Requests\t%d\n", totals.RequestCount)
	fmt.Fprintf(tw, "  Input tokens\t%d\n", totals.InputTokens)
	fmt.Fprintf(tw, "  Output tokens\t%d\n", totals.OutputTokens)
	fmt.Fprintf(tw, "  Total tokens\t%d\n", totals.TotalTokens)
	if totals.CachedInputTokens > 0 {
		fmt.Fprintf(tw, "  Cached input\t%d\n", totals.CachedInputTokens)
	}
	if totals.CacheCreationTokens > 0 {
		fmt.Fprintf(tw, "  Cache creation\t%d\n", totals.CacheCreationTokens)
	}
	if totals.ReasoningTokens > 0 {
		fmt.Fprintf(tw, "  Reasoning\t%d\n", totals.ReasoningTokens)
	}
	if totals.ErrorCount > 0 {
		fmt.Fprintf(tw, "  Errors\t%d\n", totals.ErrorCount)
	}
	if totals.RetryCount > 0 {
		fmt.Fprintf(tw, "  Retries\t%d\n", totals.RetryCount)
	}
	tw.Flush()

	if len(byModel) > 0 {
		fmt.Println("\nBy Model:")
		printRollupTable(byModel)
	}
	if len(byProvider) > 0 {
		fmt.Println("\nBy Provider:")
		printRollupTable(byProvider)
	}

	return nil
}

func runLedgerSessions(cmd *cobra.Command, args []string) error {
	dir := getLedgerDir()
	sessions, err := ledger.ReadSessionFiles(dir)
	if err != nil {
		return fmt.Errorf("read sessions: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	// Sort by start time
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt < sessions[j].StartedAt
	})

	if ledgerJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(sessions)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "SESSION\tSTARTED\tDURATION\tREQUESTS\tTOKENS\tBRANCH\n")
	for _, s := range sessions {
		duration := fmt.Sprintf("%ds", s.DurationMs/1000)
		if s.DurationMs > 60000 {
			duration = fmt.Sprintf("%dm%ds", s.DurationMs/60000, (s.DurationMs%60000)/1000)
		}
		branch := s.Git.Branch
		if len(branch) > 30 {
			branch = branch[:27] + "..."
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\n",
			s.SessionID,
			formatTime(s.StartedAt),
			duration,
			s.Totals.RequestCount,
			s.Totals.TotalTokens,
			branch,
		)
	}
	tw.Flush()

	return nil
}

func runLedgerShow(cmd *cobra.Command, args []string) error {
	dir := getLedgerDir()
	sessions, err := ledger.ReadSessionFiles(dir)
	if err != nil {
		return fmt.Errorf("read sessions: %w", err)
	}

	sessionID := args[0]
	var found *ledger.SessionSummary
	for _, s := range sessions {
		if s.SessionID == sessionID || strings.HasPrefix(s.SessionID, sessionID) {
			found = s
			break
		}
	}
	if found == nil {
		return fmt.Errorf("session %q not found", sessionID)
	}

	if ledgerJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(found)
	}

	fmt.Printf("Session: %s\n", found.SessionID)
	fmt.Printf("Started: %s\n", found.StartedAt)
	fmt.Printf("Ended:   %s\n", found.EndedAt)
	duration := fmt.Sprintf("%ds", found.DurationMs/1000)
	if found.DurationMs > 60000 {
		duration = fmt.Sprintf("%dm%ds", found.DurationMs/60000, (found.DurationMs%60000)/1000)
	}
	fmt.Printf("Duration: %s\n", duration)

	if found.Git.Branch != "" {
		fmt.Printf("\nGit:\n")
		fmt.Printf("  Branch: %s\n", found.Git.Branch)
		if found.Git.CommitStart != "" {
			fmt.Printf("  Start:  %s\n", found.Git.CommitStart)
		}
		if found.Git.CommitEnd != "" {
			fmt.Printf("  End:    %s\n", found.Git.CommitEnd)
		}
	}

	fmt.Printf("\nTotals:\n")
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  Requests\t%d\n", found.Totals.RequestCount)
	fmt.Fprintf(tw, "  Input tokens\t%d\n", found.Totals.InputTokens)
	fmt.Fprintf(tw, "  Output tokens\t%d\n", found.Totals.OutputTokens)
	fmt.Fprintf(tw, "  Total tokens\t%d\n", found.Totals.TotalTokens)
	if found.Totals.CachedInputTokens > 0 {
		fmt.Fprintf(tw, "  Cached input\t%d\n", found.Totals.CachedInputTokens)
	}
	if found.Totals.ReasoningTokens > 0 {
		fmt.Fprintf(tw, "  Reasoning\t%d\n", found.Totals.ReasoningTokens)
	}
	if found.Totals.ErrorCount > 0 {
		fmt.Fprintf(tw, "  Errors\t%d\n", found.Totals.ErrorCount)
	}
	tw.Flush()

	if len(found.ByModel) > 0 {
		fmt.Println("\nBy Model:")
		printRollupTable(found.ByModel)
	}
	if len(found.ByProvider) > 0 {
		fmt.Println("\nBy Provider:")
		printRollupTable(found.ByProvider)
	}

	return nil
}

func printRollupTable(m map[string]*ledger.UsageRollup) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  NAME\tREQUESTS\tINPUT\tOUTPUT\tTOTAL\n")

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		r := m[k]
		fmt.Fprintf(tw, "  %s\t%d\t%d\t%d\t%d\n", k, r.RequestCount, r.InputTokens, r.OutputTokens, r.TotalTokens)
	}
	tw.Flush()
}

func mergeRollups(dst map[string]*ledger.UsageRollup, src map[string]*ledger.UsageRollup) {
	for k, v := range src {
		mergeRollup(dst, k, v)
	}
}

func mergeRollup(dst map[string]*ledger.UsageRollup, key string, src *ledger.UsageRollup) {
	r, ok := dst[key]
	if !ok {
		r = &ledger.UsageRollup{}
		dst[key] = r
	}
	r.RequestCount += src.RequestCount
	r.InputTokens += src.InputTokens
	r.OutputTokens += src.OutputTokens
	r.TotalTokens += src.TotalTokens
	r.CachedInputTokens += src.CachedInputTokens
	r.CacheCreationTokens += src.CacheCreationTokens
	r.ReasoningTokens += src.ReasoningTokens
}

func formatTime(rfc3339 string) string {
	// Return just the date+time portion for compact display
	if len(rfc3339) >= 19 {
		return rfc3339[:19]
	}
	return rfc3339
}
