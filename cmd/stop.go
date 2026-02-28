package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the background tokenomics proxy",
	Long: `Stops the background proxy process started by 'tokenomics init --start'.
Sends SIGTERM, then SIGKILL if needed.`,
	Example: `  tokenomics stop
  tokenomics stop --pid-file /tmp/tokenomics.pid`,
	RunE: runStop,
}

var (
	stopPidFile string
)

func init() {
	stopCmd.Flags().StringVar(&stopPidFile, "pid-file", "", "PID file path (default: ~/.tokenomics/tokenomics.pid)")
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	// Resolve PID file path
	pidFile := stopPidFile
	if pidFile == "" {
		u, err := user.Current()
		if err != nil {
			return fmt.Errorf("get current user: %w", err)
		}
		pidFile = filepath.Join(u.HomeDir, ".tokenomics", "tokenomics.pid")
	}

	// Read PID from file
	pid, err := readPIDFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			if stopped, stopErr := stopByProcessScan(pidFile); stopErr != nil {
				return stopErr
			} else if stopped {
				fmt.Fprintf(os.Stderr, "Proxy stopped (recovered from stale or missing PID file)\n")
			} else {
				fmt.Fprintf(os.Stderr, "Proxy not running (no PID file at %s)\n", pidFile)
			}
			return nil
		}
		return fmt.Errorf("read PID file: %w", err)
	}

	// Find and signal the process
	p, err := os.FindProcess(pid)
	if err != nil {
		// Process doesn't exist, clean up PID file
		os.Remove(pidFile)
		fmt.Fprintf(os.Stderr, "Proxy not running (PID %d not found)\n", pid)
		return nil
	}

	// Request graceful shutdown
	_ = terminateProcessGroup(pid)
	if err := terminateProcess(p); err != nil {
		// Process might already be dead or PID may be stale.
		if stopped, stopErr := stopByProcessScan(pidFile); stopErr != nil {
			return stopErr
		} else if stopped {
			fmt.Fprintf(os.Stderr, "Proxy stopped (recovered from stale PID %d)\n", pid)
		} else {
			os.Remove(pidFile)
			fmt.Fprintf(os.Stderr, "Proxy not running (signal failed)\n")
		}
		return nil
	}

	// Poll to confirm exit. The serve process may take up to 10 seconds
	// for HTTP graceful shutdown, so we wait up to 12 seconds before
	// resorting to SIGKILL. This prevents force-killing the process
	// while it is still closing the database and flushing state.
	for i := 0; i < 120; i++ {
		if !processAlive(pid) {
			os.Remove(pidFile)
			fmt.Fprintf(os.Stderr, "Proxy stopped (PID %d)\n", pid)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Force kill if still alive
	_ = killProcessGroup(pid)
	if err := killProcess(p); err == nil {
		time.Sleep(100 * time.Millisecond)
	}

	// Remove PID file
	os.Remove(pidFile)
	fmt.Fprintf(os.Stderr, "Proxy killed (PID %d)\n", pid)
	return nil
}

func stopByProcessScan(pidFile string) (bool, error) {
	pids, err := findServePIDs()
	if err != nil {
		return false, fmt.Errorf("scan for running proxy process: %w", err)
	}
	if len(pids) == 0 {
		return false, nil
	}
	stoppedAny := false
	for _, pid := range pids {
		p, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		if err := terminateProcess(p); err != nil {
			continue
		}
		_ = terminateProcessGroup(pid)
		for i := 0; i < 120; i++ {
			if !processAlive(pid) {
				stoppedAny = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if processAlive(pid) {
			_ = killProcessGroup(pid)
			if err := killProcess(p); err == nil {
				time.Sleep(100 * time.Millisecond)
			}
			if !processAlive(pid) {
				stoppedAny = true
			}
		}
	}
	if stoppedAny {
		_ = os.Remove(pidFile)
	}
	return stoppedAny, nil
}
