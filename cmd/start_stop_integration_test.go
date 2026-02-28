package cmd

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestStartStopStart_NoOrphanedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group semantics differ on windows")
	}

	restore := resetStartStopGlobals(t)
	defer restore()

	if err := os.Setenv("TOKENOMICS_TEST_HELPER_SERVE", "1"); err != nil {
		t.Fatalf("set helper env: %v", err)
	}
	defer os.Unsetenv("TOKENOMICS_TEST_HELPER_SERVE")

	port := freeTCPPort(t)
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "tokenomics.pid")
	logFile := filepath.Join(tmp, "tokenomics.log")

	startHost = "localhost"
	startPort = port
	startTLS = false
	startInsecure = false
	startPidFile = pidFile
	startLogFile = logFile
	stopPidFile = pidFile
	cfgFile = ""
	dbPath = ""
	dirOverride = ""

	if err := runStart(nil, nil); err != nil {
		t.Fatalf("first runStart failed: %v", err)
	}
	firstPID, err := readPIDFile(pidFile)
	if err != nil {
		t.Fatalf("read first pid: %v", err)
	}
	if !processAlive(firstPID) {
		t.Fatalf("first started process not alive (pid=%d)", firstPID)
	}

	if err := runStop(nil, nil); err != nil {
		t.Fatalf("runStop failed: %v", err)
	}
	waitForProcessExit(t, firstPID)

	if err := runStart(nil, nil); err != nil {
		t.Fatalf("second runStart failed: %v", err)
	}
	secondPID, err := readPIDFile(pidFile)
	if err != nil {
		t.Fatalf("read second pid: %v", err)
	}
	if !processAlive(secondPID) {
		t.Fatalf("second started process not alive (pid=%d)", secondPID)
	}

	if err := runStop(nil, nil); err != nil {
		t.Fatalf("final runStop failed: %v", err)
	}
	waitForProcessExit(t, secondPID)
}

func resetStartStopGlobals(t *testing.T) func() {
	t.Helper()
	prevStartHost := startHost
	prevStartPort := startPort
	prevStartTLS := startTLS
	prevStartInsecure := startInsecure
	prevStartPID := startPidFile
	prevStartLog := startLogFile
	prevStopPID := stopPidFile
	prevCfgFile := cfgFile
	prevDBPath := dbPath
	prevDirOverride := dirOverride

	return func() {
		startHost = prevStartHost
		startPort = prevStartPort
		startTLS = prevStartTLS
		startInsecure = prevStartInsecure
		startPidFile = prevStartPID
		startLogFile = prevStartLog
		stopPidFile = prevStopPID
		cfgFile = prevCfgFile
		dbPath = prevDBPath
		dirOverride = prevDirOverride
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if !processAlive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process did not exit in time (pid=%d)", pid)
}
