package cmd

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"time"
)

// daemonConfig holds settings for starting the proxy as a background process.
type daemonConfig struct {
	host     string
	port     int
	tls      bool
	insecure bool
	pidFile  string
	logFile  string
}

var (
	daemonRetryAttempts  = 3
	daemonReadinessPolls = 120
	daemonReadinessSleep = 100 * time.Millisecond
	daemonRetrySleep     = 500 * time.Millisecond
	daemonHealthTimeout  = 250 * time.Millisecond
	daemonCommandFactory = func(exePath string, args ...string) *exec.Cmd { return exec.Command(exePath, args...) }
	daemonStopProcess    = stopSpawnedDaemon
	daemonFindServePIDs  = findServePIDs
)

// startDaemon launches the proxy as a background process and waits for it to
// become ready. It returns true when a healthy daemon is already running.
func startDaemon(baseURL string, dc daemonConfig) (bool, error) {
	pidFile, logFile := resolveDaemonPaths(dc.pidFile, dc.logFile)
	scheme := "https"
	if !dc.tls {
		scheme = "http"
	}
	healthURL := fmt.Sprintf("%s://%s:%d/ping", scheme, dc.host, dc.port)

	// Ensure tokenomics directory exists
	pidDir := filepath.Dir(pidFile)
	if err := os.MkdirAll(pidDir, 0o700); err != nil {
		return false, fmt.Errorf("create tokenomics dir: %w", err)
	}

	client := daemonHealthClient(dc)

	// If the target endpoint is already healthy, treat it as already running
	// even if the PID file is stale or missing.
	if resp, err := client.Get(healthURL); err == nil {
		resp.Body.Close()
		if resp.StatusCode == 200 {
			_ = ensurePIDFileForRunningServe(pidFile)
			return true, nil
		}
	}

	// Check if already running
	if existingPid, err := readPIDFile(pidFile); err == nil {
		if processAlive(existingPid) {
			return true, nil
		}
	}
	// Fallback by process scan to avoid spawning a second serve process that
	// would race on BoltDB locks when PID file is stale/missing.
	if pids, err := daemonFindServePIDs(); err == nil && len(pids) > 0 {
		if len(pids) == 1 {
			_ = writePIDFile(pidFile, pids[0])
		}
		return true, nil
	}

	// Open log file for proxy output
	logFd, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, fmt.Errorf("open log file: %w", err)
	}
	defer logFd.Close()

	exePath, err := os.Executable()
	if err != nil {
		exePath = os.Args[0]
	}
	serveArgs := []string{"serve"}
	if cfgFile != "" {
		serveArgs = append(serveArgs, "--config", cfgFile)
	}
	if dbPath != "" {
		serveArgs = append(serveArgs, "--db", dbPath)
	}
	if dirOverride != "" {
		serveArgs = append(serveArgs, "--dir", dirOverride)
	}
	serveEnv := os.Environ()
	if dc.tls {
		serveEnv = append(
			serveEnv,
			"TOKENOMICS_SERVER_TLS_ENABLED=true",
			fmt.Sprintf("TOKENOMICS_SERVER_HTTPS_PORT=%d", dc.port),
		)
	} else {
		serveEnv = append(
			serveEnv,
			"TOKENOMICS_SERVER_TLS_ENABLED=false",
			fmt.Sprintf("TOKENOMICS_SERVER_HTTP_PORT=%d", dc.port),
		)
	}

	// Retry startup to handle transient BoltDB lock contention after stop.
	for attempt := 0; attempt < daemonRetryAttempts; attempt++ {
		serveCmd := daemonCommandFactory(exePath, serveArgs...)
		serveCmd.Env = serveEnv
		serveCmd.Stdout = logFd
		serveCmd.Stderr = logFd
		detachProcess(serveCmd)

		if err := serveCmd.Start(); err != nil {
			return false, fmt.Errorf("start proxy: %w", err)
		}
		if err := writePIDFile(pidFile, serveCmd.Process.Pid); err != nil {
			return false, fmt.Errorf("write PID file: %w", err)
		}

		startedPID := serveCmd.Process.Pid
		ready := false
		// Poll health endpoint for readiness.
		for i := 0; i < daemonReadinessPolls; i++ {
			resp, err := client.Get(healthURL)
			if err == nil && resp.StatusCode == 200 {
				resp.Body.Close()
				ready = true
				return false, nil
			}
			if resp != nil {
				resp.Body.Close()
			}
			// If the process exited before becoming healthy, retry launch.
			if !processAlive(startedPID) {
				_ = os.Remove(pidFile)
				break
			}
			time.Sleep(daemonReadinessSleep)
		}

		// Check one more time in case another process became healthy.
		if resp, err := client.Get(healthURL); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return false, nil
			}
		}

		// Health probe never succeeded for this spawn, clean it up so we do not
		// leave detached orphan processes on repeated failures.
		if !ready && processAlive(startedPID) {
			daemonStopProcess(startedPID)
			_ = os.Remove(pidFile)
		}

		if attempt < daemonRetryAttempts-1 {
			time.Sleep(daemonRetrySleep)
		}
	}

	return false, fmt.Errorf("proxy failed to start within 12 seconds")
}

func stopSpawnedDaemon(pid int) {
	_ = terminateProcessGroup(pid)
	p, err := os.FindProcess(pid)
	if err == nil {
		_ = terminateProcess(p)
	}
	for i := 0; i < 60; i++ {
		if !processAlive(pid) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = killProcessGroup(pid)
	if err == nil {
		_ = killProcess(p)
	}
}

func daemonHealthClient(dc daemonConfig) *http.Client {
	client := &http.Client{Timeout: daemonHealthTimeout}
	if !dc.tls {
		return client
	}

	tlsCfg := &tls.Config{}
	if dc.insecure {
		tlsCfg.InsecureSkipVerify = true
		client.Transport = &http.Transport{TLSClientConfig: tlsCfg}
		return client
	}

	// For startup health checks, skip cert validation since the certs may not exist yet.
	// The serve process will generate them. We trust localhost TLS here since we're
	// checking a local proxy we just spawned.
	tlsCfg.InsecureSkipVerify = true
	client.Transport = &http.Transport{TLSClientConfig: tlsCfg}
	return client
}

// resolveDaemonPaths returns the PID and log file paths, falling back to
// ~/.tokenomics/ defaults when the given paths are empty.
func resolveDaemonPaths(pidFile, logFile string) (string, string) {
	if pidFile != "" && logFile != "" {
		return pidFile, logFile
	}
	u, err := user.Current()
	if err != nil {
		return pidFile, logFile
	}
	dir := filepath.Join(u.HomeDir, ".tokenomics")
	if pidFile == "" {
		pidFile = filepath.Join(dir, "tokenomics.pid")
	}
	if logFile == "" {
		logFile = filepath.Join(dir, "tokenomics.log")
	}
	return pidFile, logFile
}

func readPIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var pid int
	_, err = fmt.Sscanf(string(data), "%d", &pid)
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func writePIDFile(path string, pid int) error {
	return os.WriteFile(path, []byte(fmt.Sprintf("%d", pid)), 0o644)
}

func ensurePIDFileForRunningServe(pidFile string) error {
	if pid, err := readPIDFile(pidFile); err == nil && processAlive(pid) {
		return nil
	}
	pids, err := findServePIDs()
	if err != nil || len(pids) == 0 {
		return err
	}
	if len(pids) == 1 {
		return writePIDFile(pidFile, pids[0])
	}
	return nil
}
