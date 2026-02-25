package cmd

import (
	"testing"
)

func TestVersionVarsHaveDefaults(t *testing.T) {
	// When not set by ldflags, these should have sensible defaults
	if Version == "" {
		t.Error("Version should not be empty")
	}
	if Commit == "" {
		t.Error("Commit should not be empty")
	}
	if BuildDate == "" {
		t.Error("BuildDate should not be empty")
	}
}
