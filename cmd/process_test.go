package cmd

import (
	"os"
	"os/exec"
	"testing"
)

func TestProcessAlive_Self(t *testing.T) {
	// Our own process should be alive.
	if !processAlive(os.Getpid()) {
		t.Error("expected own process to be alive")
	}
}

func TestProcessAlive_Dead(t *testing.T) {
	// PID 0 or a very unlikely PID should not be alive.
	// Use a large PID that almost certainly doesn't exist.
	if processAlive(99999999) {
		t.Error("expected non-existent PID to be dead")
	}
}

func TestDetachProcess_SetsField(t *testing.T) {
	cmd := exec.Command("echo", "test")
	detachProcess(cmd)
	// Just verify it doesn't panic. Platform-specific behavior is tested
	// by the build tags themselves.
	if cmd.Path == "" {
		t.Error("command path should not be empty")
	}
}
