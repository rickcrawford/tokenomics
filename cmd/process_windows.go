//go:build windows

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// detachProcess is a no-op on Windows. Background processes naturally outlive
// their parent when not waited on.
func detachProcess(cmd *exec.Cmd) {}

// processAlive checks whether a process with the given PID is still running.
// On Windows, os.FindProcess always succeeds, so we use tasklist to verify.
func processAlive(pid int) bool {
	out, err := exec.Command("tasklist", "/FI",
		fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), strconv.Itoa(pid))
}

// terminateProcess on Windows calls TerminateProcess since there is no
// SIGTERM equivalent.
func terminateProcess(p *os.Process) error {
	return p.Kill()
}

// killProcess force-stops the process via TerminateProcess.
func killProcess(p *os.Process) error {
	return p.Kill()
}

// terminateProcessGroup is unsupported on Windows in this implementation.
func terminateProcessGroup(pid int) error { return nil }

// killProcessGroup is unsupported on Windows in this implementation.
func killProcessGroup(pid int) error { return nil }

// interruptProcess on Windows falls back to Kill since os.Interrupt is not
// reliably supported for arbitrary processes.
func interruptProcess(p *os.Process) error {
	return p.Kill()
}

// setProcessGroup is a no-op on Windows. Child processes do not share
// the parent's console signal group by default.
func setProcessGroup(cmd *exec.Cmd) {}

// findServePIDs returns PIDs for running tokenomics serve processes.
// Windows implementation is best-effort and returns no matches for now.
func findServePIDs() ([]int, error) {
	return nil, nil
}
