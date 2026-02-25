package cmd

import (
	"os"
	"path/filepath"
	"testing"
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
	os.WriteFile(path, []byte("not-a-number"), 0o644)

	_, err := readPIDFile(path)
	if err == nil {
		t.Error("expected error for invalid PID content")
	}
}
