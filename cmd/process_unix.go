//go:build !windows

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// detachProcess configures the command to run in a new session, detached from
// the controlling terminal.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// processAlive checks whether a process with the given PID is still running.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; send signal 0 to check liveness.
	if p.Signal(syscall.Signal(0)) != nil {
		return false
	}
	// Treat zombies as not alive for lifecycle and PID management.
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return true
	}
	stat := strings.TrimSpace(string(out))
	return stat != "" && !strings.HasPrefix(stat, "Z")
}

// terminateProcess sends SIGTERM to request graceful shutdown.
func terminateProcess(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}

// killProcess sends SIGKILL to force-stop a process.
func killProcess(p *os.Process) error {
	return p.Signal(syscall.SIGKILL)
}

// terminateProcessGroup sends SIGTERM to the process group rooted at pid.
func terminateProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}

// killProcessGroup sends SIGKILL to the process group rooted at pid.
func killProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}

// interruptProcess sends SIGINT to a process.
func interruptProcess(p *os.Process) error {
	return p.Signal(syscall.SIGINT)
}

// setProcessGroup puts the command in its own process group so it does
// not receive signals (e.g. Ctrl+C) sent to the parent's group.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// findServePIDs returns PIDs for running tokenomics serve processes.
func findServePIDs() ([]int, error) {
	out, err := exec.Command("ps", "-axo", "pid=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	lines := strings.Split(string(out), "\n")
	pids := make([]int, 0)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		pid, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		cmdline := strings.Join(parts[1:], " ")
		if strings.Contains(cmdline, "tokenomics serve") {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}
