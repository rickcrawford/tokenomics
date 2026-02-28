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

func TestEvaluateBinaryExecutionPolicy(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		exePath  string
		xattrs   string
		xattrErr bool
		want     string
	}{
		{
			name:    "non-darwin not applicable",
			goos:    "linux",
			exePath: "/tmp/tokenomics",
			want:    "ok",
		},
		{
			name:    "darwin outside var tmp",
			goos:    "darwin",
			exePath: "/Users/rick/bin/tokenomics",
			want:    "ok",
		},
		{
			name:    "darwin var tmp with provenance warns",
			goos:    "darwin",
			exePath: "/var/tmp/test/tokenomics",
			xattrs:  "com.apple.provenance: 1",
			want:    "warn",
		},
		{
			name:     "darwin var tmp xattr inspect error warns",
			goos:     "darwin",
			exePath:  "/private/var/tmp/test/tokenomics",
			xattrs:   "",
			xattrErr: true,
			want:     "warn",
		},
		{
			name:    "darwin var tmp no blocking xattr ok",
			goos:    "darwin",
			exePath: "/private/var/tmp/test/tokenomics",
			xattrs:  "com.apple.lastuseddate#PS: 1",
			want:    "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.xattrErr {
				err = os.ErrPermission
			}
			got := evaluateBinaryExecutionPolicy(tt.goos, tt.exePath, tt.xattrs, err)
			if got.status != tt.want {
				t.Fatalf("status = %q, want %q (detail: %s)", got.status, tt.want, got.detail)
			}
		})
	}
}
