package session

import (
	"compress/gzip"
	"io"
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

func TestFileMemoryWriter_SanitizesBinaryContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sanitized.md")

	w, err := NewFileMemoryWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	// Includes NUL, invalid UTF-8 byte, and ESC control byte.
	raw := string([]byte{'h', 'i', 0x00, 0xff, 'x', '\n', 0x1b, '!'})
	if err := w.Append("sess-1234567890abcdef", "assistant", "gpt-4", raw); err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if strings.ContainsRune(string(data), '\x00') {
		t.Error("expected NUL bytes to be removed from memory output")
	}
	if strings.ContainsRune(string(data), '\x1b') {
		t.Error("expected control bytes to be removed from memory output")
	}
	if !strings.Contains(string(data), "hi ?x\n !") {
		t.Errorf("expected sanitized readable content, got: %q", string(data))
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

func TestDirMemoryWriter_PerSessionFiles(t *testing.T) {
	dir := t.TempDir()

	w, err := NewDirMemoryWriter(dir, "{token_hash}.md")
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	// Write to two different sessions
	if err := w.Append("aaaa1111bbbb2222cccc3333", "user", "gpt-4", "Hello from session A"); err != nil {
		t.Fatalf("append session A: %v", err)
	}
	if err := w.Append("dddd4444eeee5555ffff6666", "user", "gpt-4", "Hello from session B"); err != nil {
		t.Fatalf("append session B: %v", err)
	}
	if err := w.Append("aaaa1111bbbb2222cccc3333", "assistant", "gpt-4", "Reply to session A"); err != nil {
		t.Fatalf("append session A reply: %v", err)
	}

	// Session A file should have 2 entries
	dataA, err := os.ReadFile(filepath.Join(dir, "aaaa1111bbbb2222.md"))
	if err != nil {
		t.Fatalf("read session A file: %v", err)
	}
	if !strings.Contains(string(dataA), "Hello from session A") {
		t.Error("session A missing user message")
	}
	if !strings.Contains(string(dataA), "Reply to session A") {
		t.Error("session A missing assistant reply")
	}
	if strings.Contains(string(dataA), "session B") {
		t.Error("session A file should not contain session B content")
	}

	// Session B file should have 1 entry
	dataB, err := os.ReadFile(filepath.Join(dir, "dddd4444eeee5555.md"))
	if err != nil {
		t.Fatalf("read session B file: %v", err)
	}
	if !strings.Contains(string(dataB), "Hello from session B") {
		t.Error("session B missing user message")
	}
}

func TestDirMemoryWriter_DateSubdirectory(t *testing.T) {
	dir := t.TempDir()

	w, err := NewDirMemoryWriter(dir, "{date}/{token_hash}.md")
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	if err := w.Append("aaaa1111bbbb2222cccc3333", "user", "gpt-4", "Hello"); err != nil {
		t.Fatalf("append: %v", err)
	}

	// File should exist in a date-named subdirectory
	resolved := w.ResolvePath("aaaa1111bbbb2222cccc3333")
	if _, err := os.Stat(resolved); os.IsNotExist(err) {
		t.Fatalf("expected file at %s to exist", resolved)
	}
	if !strings.Contains(resolved, dir) {
		t.Errorf("resolved path %q should be under %q", resolved, dir)
	}
}

func TestDirMemoryWriter_SessionIDPlaceholder(t *testing.T) {
	dir := t.TempDir()

	w, err := NewDirMemoryWriter(dir, "{date}_{session_id}.md")
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	sessionID := "sess1234abcdef5678"
	if err := w.Append(sessionID, "user", "gpt-4", "Hello"); err != nil {
		t.Fatalf("append: %v", err)
	}

	resolved := w.ResolvePath(sessionID)
	if _, err := os.Stat(resolved); os.IsNotExist(err) {
		t.Fatalf("expected file at %s to exist", resolved)
	}
	if !strings.Contains(filepath.Base(resolved), "sess1234abcdef56") {
		t.Errorf("expected filename to include truncated session id, got %s", filepath.Base(resolved))
	}
}

func TestDirMemoryWriter_DefaultPattern(t *testing.T) {
	dir := t.TempDir()

	w, err := NewDirMemoryWriter(dir, "")
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	if err := w.Append("aaaa1111bbbb2222cccc3333", "user", "gpt-4", "Hello"); err != nil {
		t.Fatalf("append: %v", err)
	}

	// Default pattern is {token_hash}.md
	expected := filepath.Join(dir, "aaaa1111bbbb2222.md")
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Fatalf("expected default file at %s", expected)
	}
}

func TestDirMemoryWriter_Close(t *testing.T) {
	dir := t.TempDir()

	w, err := NewDirMemoryWriter(dir, "{token_hash}.md")
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	if err := w.Append("aaaa1111bbbb2222cccc3333", "user", "gpt-4", "Hello"); err != nil {
		t.Fatalf("append: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// After close, files map should be empty
	if len(w.files) != 0 {
		t.Errorf("expected 0 open files after close, got %d", len(w.files))
	}
}

func TestDirMemoryWriter_ConcurrentSessions(t *testing.T) {
	dir := t.TempDir()

	w, err := NewDirMemoryWriter(dir, "{token_hash}.md")
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	done := make(chan error, 20)
	sessions := []string{
		"aaaa1111bbbb2222cccc3333",
		"dddd4444eeee5555ffff6666",
	}

	for _, s := range sessions {
		for i := 0; i < 10; i++ {
			s := s
			go func() {
				done <- w.Append(s, "user", "gpt-4", "message")
			}()
		}
	}

	for i := 0; i < 20; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent append failed: %v", err)
		}
	}

	// Each session file should have 10 entries
	for _, s := range sessions {
		prefix := safeSessionPrefix(s, 16)
		data, err := os.ReadFile(filepath.Join(dir, prefix+".md"))
		if err != nil {
			t.Fatalf("read file for %s: %v", prefix, err)
		}
		count := strings.Count(string(data), "---")
		if count != 10 {
			t.Errorf("session %s: expected 10 entries, found %d", prefix, count)
		}
	}
}

func TestSafeSessionPrefix(t *testing.T) {
	if got := safeSessionPrefix("abcdefghijklmnop", 16); got != "abcdefghijklmnop" {
		t.Errorf("expected exact 16 chars, got %q", got)
	}
	if got := safeSessionPrefix("short", 16); got != "short" {
		t.Errorf("expected full string for short input, got %q", got)
	}
	if got := safeSessionPrefix("abcdefghijklmnopqrstuvwxyz", 16); got != "abcdefghijklmnop" {
		t.Errorf("expected truncated to 16, got %q", got)
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

// TestDirMemoryWriter_CloseReleasesHandles verifies all open handles closed on Close()
func TestDirMemoryWriter_CloseReleasesHandles(t *testing.T) {
	dir := t.TempDir()

	w, err := NewDirMemoryWriter(dir, "{token_hash}.md")
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Write to multiple sessions
	sessions := []string{
		"aaaa1111bbbb2222cccc3333",
		"dddd4444eeee5555ffff6666",
		"1111aaaa2222bbbb3333cccc",
	}

	for _, s := range sessions {
		if err := w.Append(s, "user", "gpt-4", "content"); err != nil {
			t.Fatalf("append failed: %v", err)
		}
	}

	// Should have 3 open file handles
	if len(w.files) != 3 {
		t.Errorf("expected 3 open files, got %d", len(w.files))
	}

	// Close the writer
	if err := w.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// All handles should be released
	if len(w.files) != 0 {
		t.Errorf("expected 0 open files after close, got %d", len(w.files))
	}
}

// TestRotatingDirMemoryWriter_NoRotationWhenUnlimited tests unlimited size
func TestRotatingDirMemoryWriter_NoRotationWhenUnlimited(t *testing.T) {
	dir := t.TempDir()

	// Create rotating writer with unlimited size (maxSizeMB = -1 means unlimited)
	w, err := NewRotatingDirMemoryWriter(dir, "{token_hash}.md", -1, false)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	// Write multiple large entries
	sessionID := "aaaa1111bbbb2222cccc3333"
	for i := 0; i < 5; i++ {
		content := strings.Repeat("x", 10000) // 10KB per entry
		if err := w.Append(sessionID, "user", "gpt-4", content); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Should still be a single file (no rotation)
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 file (no rotation), got %d", len(files))
	}
}

// TestRotatingDirMemoryWriter_RotatesOnSizeLimit tests file rotation
func TestRotatingDirMemoryWriter_RotatesOnSizeLimit(t *testing.T) {
	dir := t.TempDir()

	// Create rotating writer with 50 KB max size, no compression
	w, err := NewRotatingDirMemoryWriter(dir, "{token_hash}.md", 0, false)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	// Override maxSizeBytes to 50KB for testing
	w.maxSizeBytes = 50 * 1024

	sessionID := "aaaa1111bbbb2222cccc3333"

	// Write entries totaling ~120 KB (should trigger rotation)
	for i := 0; i < 4; i++ {
		content := strings.Repeat("x", 35000) // 35KB per entry
		if err := w.Append(sessionID, "user", "gpt-4", content); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Should have multiple files (original + rotated)
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(files) < 2 {
		t.Errorf("expected at least 2 files after rotation, got %d", len(files))
	}

	// Check that active file exists
	currentPath := w.ResolvePath(sessionID)
	if _, err := os.Stat(currentPath); os.IsNotExist(err) {
		t.Errorf("current file should exist: %s", currentPath)
	}
}

// TestRotatingDirMemoryWriter_CompressesRotatedFiles tests gzip compression
func TestRotatingDirMemoryWriter_CompressesRotatedFiles(t *testing.T) {
	dir := t.TempDir()

	// Create rotating writer with compression enabled
	w, err := NewRotatingDirMemoryWriter(dir, "{token_hash}.md", 0, true)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	// Override maxSizeBytes to 30KB for testing
	w.maxSizeBytes = 30 * 1024

	sessionID := "aaaa1111bbbb2222cccc3333"

	// Write enough to trigger rotation
	for i := 0; i < 3; i++ {
		content := strings.Repeat("test content ", 5000) // Highly compressible
		if err := w.Append(sessionID, "user", "gpt-4", content); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Look for .gz files (compressed archives)
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}

	hasGzFile := false
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".gz") {
			hasGzFile = true
			// Verify it's a valid gzip file
			path := filepath.Join(dir, f.Name())
			gzFile, err := os.Open(path)
			if err != nil {
				t.Fatalf("open gz file: %v", err)
			}
			defer gzFile.Close()

			gz, err := gzip.NewReader(gzFile)
			if err != nil {
				t.Fatalf("open gzip reader: %v", err)
			}
			defer gz.Close()

			// Try to read decompressed content
			data, err := io.ReadAll(gz)
			if err != nil {
				t.Fatalf("read gzip: %v", err)
			}
			if len(data) == 0 {
				t.Errorf("compressed file is empty")
			}
		}
	}

	if !hasGzFile {
		t.Error("expected at least one .gz file after compression")
	}
}

// TestRotatingDirMemoryWriter_DisableCompression tests no compression
func TestRotatingDirMemoryWriter_DisableCompression(t *testing.T) {
	dir := t.TempDir()

	// Create rotating writer with compression disabled
	w, err := NewRotatingDirMemoryWriter(dir, "{token_hash}.md", 0, false)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	// Override maxSizeBytes to 30KB for testing
	w.maxSizeBytes = 30 * 1024

	sessionID := "aaaa1111bbbb2222cccc3333"

	// Write enough to trigger rotation
	for i := 0; i < 3; i++ {
		content := strings.Repeat("test content ", 5000)
		if err := w.Append(sessionID, "user", "gpt-4", content); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Should have .md files but no .gz files
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}

	hasGzFile := false
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".gz") {
			hasGzFile = true
		}
	}

	if hasGzFile {
		t.Error("expected no .gz files when compression disabled")
	}
}

// TestRotatingDirMemoryWriter_DefaultSizeLimitApplied tests 100MB default
func TestRotatingDirMemoryWriter_DefaultSizeLimitApplied(t *testing.T) {
	dir := t.TempDir()

	// Create rotating writer with maxSizeMB = 0 (use default 100 MB)
	w, err := NewRotatingDirMemoryWriter(dir, "{token_hash}.md", 0, false)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Should have 100 MB default
	expectedDefault := int64(100 * 1024 * 1024)
	if w.maxSizeBytes != expectedDefault {
		t.Errorf("expected default 100 MB (%d bytes), got %d", expectedDefault, w.maxSizeBytes)
	}

	w.Close()
}

// TestRotatingDirMemoryWriter_ConcurrentSessions tests concurrent writes
func TestRotatingDirMemoryWriter_ConcurrentSessions(t *testing.T) {
	dir := t.TempDir()

	w, err := NewRotatingDirMemoryWriter(dir, "{token_hash}.md", 0, false)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	defer w.Close()

	done := make(chan error, 20)
	sessions := []string{
		"aaaa1111bbbb2222cccc3333",
		"dddd4444eeee5555ffff6666",
	}

	for _, s := range sessions {
		for i := 0; i < 10; i++ {
			s := s
			go func() {
				done <- w.Append(s, "user", "gpt-4", "concurrent message")
			}()
		}
	}

	for i := 0; i < 20; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent append failed: %v", err)
		}
	}

	// Verify data integrity
	for _, s := range sessions {
		prefix := safeSessionPrefix(s, 16)
		path := filepath.Join(dir, prefix+".md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read file for %s: %v", prefix, err)
		}
		count := strings.Count(string(data), "concurrent message")
		if count != 10 {
			t.Errorf("session %s: expected 10 messages, found %d", prefix, count)
		}
	}
}
