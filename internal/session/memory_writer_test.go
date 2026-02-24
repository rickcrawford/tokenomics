package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileMemoryWriter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_session.md")

	w, err := NewFileMemoryWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	if err := w.Append("sess-1234567890abcdef", "user", "gpt-4", "Hello, world!"); err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	if err := w.Append("sess-1234567890abcdef", "assistant", "gpt-4", "Hi there!"); err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "user") {
		t.Error("expected content to contain 'user'")
	}
	if !strings.Contains(content, "assistant") {
		t.Error("expected content to contain 'assistant'")
	}
	if !strings.Contains(content, "gpt-4") {
		t.Error("expected content to contain 'gpt-4'")
	}
	if !strings.Contains(content, "Hello, world!") {
		t.Error("expected content to contain 'Hello, world!'")
	}
	if !strings.Contains(content, "Hi there!") {
		t.Error("expected content to contain 'Hi there!'")
	}
	if !strings.Contains(content, "sess-1234567890a") {
		t.Error("expected content to contain truncated session ID")
	}
}

func TestFileMemoryWriter_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.md")

	// Write initial content
	if err := os.WriteFile(path, []byte("# Existing content\n\n"), 0o644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	w, err := NewFileMemoryWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	if err := w.Append("sess-1234567890abcdef", "user", "gpt-4", "New entry"); err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	w.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "# Existing content") {
		t.Error("expected to preserve existing content")
	}
	if !strings.Contains(content, "New entry") {
		t.Error("expected to contain appended entry")
	}
}

func TestFileMemoryWriter_InvalidPath(t *testing.T) {
	_, err := NewFileMemoryWriter("/nonexistent/dir/file.md")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestNopMemoryWriter(t *testing.T) {
	w := &NopMemoryWriter{}

	if err := w.Append("sess", "user", "model", "content"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFileMemoryWriter_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.md")

	w, err := NewFileMemoryWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			done <- w.Append("sess-1234567890abcdef", "user", "gpt-4", "message")
		}(i)
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent append failed: %v", err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Should have 10 entries, each ending with "---"
	count := strings.Count(string(data), "---")
	if count != 10 {
		t.Errorf("expected 10 entries, found %d separators", count)
	}
}
