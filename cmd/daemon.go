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

// startDaemon launches the proxy as a background process and waits for it to
// become ready. It is used by both the init and start commands.
func startDaemon(baseURL string, dc daemonConfig) error {
	pidFile, logFile := resolveDaemonPaths(dc.pidFile, dc.logFile)

	// Ensure tokenomics directory exists
	pidDir := filepath.Dir(pidFile)
	if err := os.MkdirAll(pidDir, 0o700); err != nil {
		return fmt.Errorf("create tokenomics dir: %w", err)
	}

	// Check if already running
	if existingPid, err := readPIDFile(pidFile); err == nil {
		if processAlive(existingPid) {
			return nil
		}
	}

	// Open log file for proxy output
	logFd, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFd.Close()

	// Launch tokenomics serve as a detached process
	serveCmd := exec.Command(os.Args[0], "serve", "--config", cfgFile, "--db", dbPath)
	serveCmd.Stdout = logFd
	serveCmd.Stderr = logFd
	detachProcess(serveCmd)

	if err := serveCmd.Start(); err != nil {
		return fmt.Errorf("start proxy: %w", err)
	}

	if err := writePIDFile(pidFile, serveCmd.Process.Pid); err != nil {
		return fmt.Errorf("write PID file: %w", err)
	}

	// Poll health endpoint for readiness
	scheme := "https"
	if !dc.tls {
		scheme = "http"
	}
	healthURL := fmt.Sprintf("%s://%s:%d/ping", scheme, dc.host, dc.port)

	client := &http.Client{Timeout: 5 * time.Second}
	if dc.insecure {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	for i := 0; i < 30; i++ {
		resp, err := client.Get(healthURL)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("proxy failed to start within 3 seconds")
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
