package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatusForCount(t *testing.T) {
	tests := []struct {
		ok, bad int
		want    string
	}{
		{3, 0, "ok"},
		{0, 2, "fail"},
		{2, 1, "warn"},
		{1, 1, "warn"},
	}
	for _, tt := range tests {
		got := statusForCount(tt.ok, tt.bad)
		if got != tt.want {
			t.Errorf("statusForCount(%d, %d) = %q, want %q", tt.ok, tt.bad, got, tt.want)
		}
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists.txt")
	if err := os.WriteFile(existing, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	if !fileExists(existing) {
		t.Error("expected true for existing file")
	}
	if fileExists(filepath.Join(dir, "nope.txt")) {
		t.Error("expected false for non-existent file")
	}
}
