package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestResolveDaemonPaths_BothProvided(t *testing.T) {
	pid, log := resolveDaemonPaths("/tmp/my.pid", "/tmp/my.log")
	if pid != "/tmp/my.pid" {
		t.Errorf("pidFile = %q, want /tmp/my.pid", pid)
	}
	if log != "/tmp/my.log" {
		t.Errorf("logFile = %q, want /tmp/my.log", log)
	}
}

func TestResolveDaemonPaths_Defaults(t *testing.T) {
	pid, log := resolveDaemonPaths("", "")
	if !filepath.IsAbs(pid) {
		t.Errorf("expected absolute pid path, got %q", pid)
	}
	if !filepath.IsAbs(log) {
		t.Errorf("expected absolute log path, got %q", log)
	}
	if filepath.Base(pid) != "tokenomics.pid" {
		t.Errorf("expected tokenomics.pid, got %q", filepath.Base(pid))
	}
	if filepath.Base(log) != "tokenomics.log" {
		t.Errorf("expected tokenomics.log, got %q", filepath.Base(log))
	}
}

func TestResolveDaemonPaths_PartialOverride(t *testing.T) {
	pid, log := resolveDaemonPaths("/custom/path.pid", "")
	if pid != "/custom/path.pid" {
		t.Errorf("pidFile = %q, want /custom/path.pid", pid)
	}
	if filepath.Base(log) != "tokenomics.log" {
		t.Errorf("expected default log, got %q", log)
	}
}

func TestReadWritePIDFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	if err := writePIDFile(path, 12345); err != nil {
		t.Fatalf("writePIDFile error: %v", err)
	}

	pid, err := readPIDFile(path)
	if err != nil {
		t.Fatalf("readPIDFile error: %v", err)
	}
	if pid != 12345 {
		t.Errorf("pid = %d, want 12345", pid)
	}
}

func TestReadPIDFile_NotExist(t *testing.T) {
	_, err := readPIDFile("/nonexistent/path/test.pid")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestReadPIDFile_InvalidContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pid")
	if err := os.WriteFile(path, []byte("not-a-number"), 0o644); err != nil {
		t.Fatalf("write bad pid: %v", err)
	}

	_, err := readPIDFile(path)
	if err == nil {
		t.Error("expected error for invalid PID content")
	}
}

func TestStartDaemon_CleansSpawnedProcessOnReadinessFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses shell helper process")
	}

	prevRetries := daemonRetryAttempts
	prevPolls := daemonReadinessPolls
	prevPollSleep := daemonReadinessSleep
	prevRetrySleep := daemonRetrySleep
	prevFactory := daemonCommandFactory
	prevStop := daemonStopProcess
	prevFind := daemonFindServePIDs
	prevTimeout := daemonHealthTimeout
	prevCfgFile := cfgFile
	prevDBPath := dbPath
	prevDirOverride := dirOverride
	t.Cleanup(func() {
		daemonRetryAttempts = prevRetries
		daemonReadinessPolls = prevPolls
		daemonReadinessSleep = prevPollSleep
		daemonRetrySleep = prevRetrySleep
		daemonCommandFactory = prevFactory
		daemonStopProcess = prevStop
		daemonFindServePIDs = prevFind
		daemonHealthTimeout = prevTimeout
		cfgFile = prevCfgFile
		dbPath = prevDBPath
		dirOverride = prevDirOverride
	})

	daemonRetryAttempts = 1
	daemonReadinessPolls = 5
	daemonReadinessSleep = 10 * time.Millisecond
	daemonRetrySleep = 10 * time.Millisecond
	daemonHealthTimeout = 20 * time.Millisecond
	daemonFindServePIDs = func() ([]int, error) { return nil, nil }
	daemonCommandFactory = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command("sh", "-c", "sleep 30")
	}
	stopCalls := 0
	daemonStopProcess = func(pid int) {
		stopCalls++
		stopSpawnedDaemon(pid)
	}
	cfgFile = ""
	dbPath = ""
	dirOverride = ""

	dir := t.TempDir()
	dc := daemonConfig{
		host:    "localhost",
		port:    65530,
		tls:     false,
		pidFile: filepath.Join(dir, "tokenomics.pid"),
		logFile: filepath.Join(dir, "tokenomics.log"),
	}

	_, err := startDaemon("http://localhost:65530", dc)
	if err == nil {
		t.Fatal("expected readiness failure")
	}
	if stopCalls == 0 {
		t.Fatal("expected spawned process cleanup to run on startup failure")
	}
	if _, statErr := os.Stat(dc.pidFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected pid file to be removed on failure, stat err: %v", statErr)
	}
}

func TestStartDaemon_AlreadyRunningByProcessScan(t *testing.T) {
	prevFind := daemonFindServePIDs
	prevFactory := daemonCommandFactory
	prevTimeout := daemonHealthTimeout
	t.Cleanup(func() {
		daemonFindServePIDs = prevFind
		daemonCommandFactory = prevFactory
		daemonHealthTimeout = prevTimeout
	})

	daemonHealthTimeout = 20 * time.Millisecond
	daemonFindServePIDs = func() ([]int, error) { return []int{4242}, nil }
	daemonCommandFactory = func(_ string, _ ...string) *exec.Cmd {
		t.Fatal("did not expect process spawn when serve already running")
		return nil
	}

	dir := t.TempDir()
	dc := daemonConfig{
		host:    "localhost",
		port:    65530,
		tls:     true,
		pidFile: filepath.Join(dir, "tokenomics.pid"),
		logFile: filepath.Join(dir, "tokenomics.log"),
	}
	already, err := startDaemon("https://localhost:65530", dc)
	if err != nil {
		t.Fatalf("startDaemon error: %v", err)
	}
	if !already {
		t.Fatal("expected already running=true from process scan")
	}
}
