//go:build !windows

package cmd

import (
	"os"
	"os/exec"
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
	return p.Signal(syscall.Signal(0)) == nil
}

// terminateProcess sends SIGTERM to request graceful shutdown.
func terminateProcess(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}

// killProcess sends SIGKILL to force-stop a process.
func killProcess(p *os.Process) error {
	return p.Signal(syscall.SIGKILL)
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
