package session

import (
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkMemoryStore_AddUsage(b *testing.B) {
	store := NewMemoryStore()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.AddUsage("token-hash-1", 100); err != nil {
			b.Fatalf("add usage failed: %v", err)
		}
	}
}

func BenchmarkMemoryStore_GetUsage(b *testing.B) {
	store := NewMemoryStore()
	if _, err := store.AddUsage("token-hash-1", 100); err != nil {
		b.Fatalf("seed add usage failed: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.GetUsage("token-hash-1"); err != nil {
			b.Fatalf("get usage failed: %v", err)
		}
	}
}

func BenchmarkFileMemoryWriter_Append(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "bench.md")
	w, err := NewFileMemoryWriter(path)
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := w.Append("session-1", "user", "gpt-4o", "hello world"); err != nil {
			b.Fatalf("append failed: %v", err)
		}
	}
}

func BenchmarkDirMemoryWriter_Append(b *testing.B) {
	dir := b.TempDir()
	w, err := NewDirMemoryWriter(dir, "{token_hash}.md")
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := w.Append("session-hash-1", "user", "gpt-4o", "hello world"); err != nil {
			b.Fatalf("append failed: %v", err)
		}
	}
}

func BenchmarkDirMemoryWriter_Append_MultipleSessions(b *testing.B) {
	dir := b.TempDir()
	w, err := NewDirMemoryWriter(dir, "{token_hash}.md")
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := w.Append(fmt.Sprintf("session-%d", i%10), "user", "gpt-4o", "hello world"); err != nil {
			b.Fatalf("append failed: %v", err)
		}
	}
}

func BenchmarkSafeSessionPrefix(b *testing.B) {
	s := "abcdef1234567890abcdef1234567890abcdef1234567890"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		safeSessionPrefix(s, 16)
	}
}

func TestDirMemoryWriter_ResolvePath(t *testing.T) {
	dir := t.TempDir()
	w, err := NewDirMemoryWriter(dir, "{token_hash}-{date}.md")
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	path := w.ResolvePath("session-hash-123")
	if path == "" {
		t.Error("expected non-empty path")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %q", path)
	}
}

func TestNopMemoryWriter_Interface(t *testing.T) {
	// Verify NopMemoryWriter satisfies the MemoryWriter interface
	var w NopMemoryWriter
	if err := w.Append("session", "user", "model", "content"); err != nil {
		t.Errorf("Append error: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}
}
