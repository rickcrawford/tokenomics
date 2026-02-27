package session

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
)

// MemoryWriter handles writing conversation content to a session file or Redis.
type MemoryWriter interface {
	// Append writes content to the session log.
	Append(sessionID, role, model, content string) error
	// Close cleans up resources.
	Close() error
}

// FileMemoryWriter appends markdown-formatted conversation logs to a file.
type FileMemoryWriter struct {
	mu   sync.Mutex
	file *os.File
	path string
}

// NewFileMemoryWriter opens (or creates) a file for appending session content.
func NewFileMemoryWriter(path string) (*FileMemoryWriter, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open session file: %w", err)
	}
	return &FileMemoryWriter{file: f, path: path}, nil
}

func (w *FileMemoryWriter) Append(sessionID, role, model, content string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	ts := time.Now().UTC().Format(time.RFC3339)
	entry := fmt.Sprintf("## %s | %s | %s | %s\n\n%s\n\n---\n\n", ts, safeSessionPrefix(sessionID, 16), role, model, sanitizeMemoryContent(content))
	_, err := w.file.WriteString(entry)
	return err
}

func (w *FileMemoryWriter) Close() error {
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// DirMemoryWriter writes per-session files into a directory. Each session
// (identified by sessionID) gets its own file based on a configurable name
// pattern. Supported placeholders: {token_hash}, {session_id}, {date}.
type DirMemoryWriter struct {
	mu      sync.Mutex
	dir     string
	pattern string
	files   map[string]*os.File
}

// NewDirMemoryWriter creates a writer that produces per-session files under dir.
// The pattern supports placeholders: {token_hash} (replaced with sessionID),
// {session_id} (replaced with sessionID),
// {date} (replaced with YYYY-MM-DD). Example: "{date}/{token_hash}.md".
func NewDirMemoryWriter(dir, pattern string) (*DirMemoryWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create memory directory: %w", err)
	}
	if pattern == "" {
		pattern = "{token_hash}.md"
	}
	return &DirMemoryWriter{
		dir:     dir,
		pattern: pattern,
		files:   make(map[string]*os.File),
	}, nil
}

// ResolvePath returns the file path for a given session ID without opening the file.
func (w *DirMemoryWriter) ResolvePath(sessionID string) string {
	prefix := safeSessionPrefix(sessionID, 16)
	name := strings.ReplaceAll(w.pattern, "{token_hash}", prefix)
	name = strings.ReplaceAll(name, "{session_id}", prefix)
	name = strings.ReplaceAll(name, "{date}", time.Now().UTC().Format("2006-01-02"))
	return filepath.Join(w.dir, name)
}

func (w *DirMemoryWriter) getFile(sessionID string) (*os.File, error) {
	path := w.ResolvePath(sessionID)

	if f, ok := w.files[path]; ok {
		return f, nil
	}

	// Close stale file handles for this session with different paths (e.g., date rollover)
	sessionHash := safeSessionPrefix(sessionID, 16)
	for existingPath, f := range w.files {
		// Check if this is for the same session but different path
		if strings.Contains(existingPath, sessionHash) && existingPath != path {
			// Different path for same session (e.g., date changed) - close and remove
			f.Close()
			delete(w.files, existingPath)
		}
	}

	// Create subdirectories if the pattern includes them (e.g. "{date}/file.md")
	if dir := filepath.Dir(path); dir != w.dir {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create subdirectory: %w", err)
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open session file %q: %w", path, err)
	}
	w.files[path] = f
	return f, nil
}

func (w *DirMemoryWriter) Append(sessionID, role, model, content string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := w.getFile(sessionID)
	if err != nil {
		return err
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	entry := fmt.Sprintf("## %s | %s | %s | %s\n\n%s\n\n---\n\n", ts, safeSessionPrefix(sessionID, 16), role, model, sanitizeMemoryContent(content))
	_, err = f.WriteString(entry)
	return err
}

func (w *DirMemoryWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	var firstErr error
	for path, f := range w.files {
		if err := f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(w.files, path)
	}
	return firstErr
}

// safeSessionPrefix returns up to n characters of s, or the full string if shorter.
func safeSessionPrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// RotatingDirMemoryWriter writes per-session files with automatic rotation when size limit is reached.
// It optionally gzips rotated files to save space.
type RotatingDirMemoryWriter struct {
	mu           sync.Mutex
	dir          string
	pattern      string
	maxSizeBytes int64
	compressOld  bool
	files        map[string]*rotatingFile
}

type rotatingFile struct {
	path    string
	file    *os.File
	written int64
}

// NewRotatingDirMemoryWriter creates a writer with size-based rotation and optional compression.
// maxSizeMB: max file size before rotation (0 = unlimited, defaults to 100 MB)
// compressOld: if true, gzips rotated files
func NewRotatingDirMemoryWriter(dir, pattern string, maxSizeMB int, compressOld bool) (*RotatingDirMemoryWriter, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create memory directory: %w", err)
	}
	if pattern == "" {
		pattern = "{token_hash}.md"
	}

	// Default to 100 MB if not specified (0)
	// Negative value means unlimited
	maxSizeBytes := int64(100 * 1024 * 1024)
	if maxSizeMB > 0 {
		maxSizeBytes = int64(maxSizeMB * 1024 * 1024)
	} else if maxSizeMB < 0 {
		maxSizeBytes = 0 // unlimited
	}

	return &RotatingDirMemoryWriter{
		dir:          dir,
		pattern:      pattern,
		maxSizeBytes: maxSizeBytes,
		compressOld:  compressOld,
		files:        make(map[string]*rotatingFile),
	}, nil
}

// ResolvePath returns the file path for a given session ID without opening the file.
func (w *RotatingDirMemoryWriter) ResolvePath(sessionID string) string {
	prefix := safeSessionPrefix(sessionID, 16)
	name := strings.ReplaceAll(w.pattern, "{token_hash}", prefix)
	name = strings.ReplaceAll(name, "{session_id}", prefix)
	name = strings.ReplaceAll(name, "{date}", time.Now().UTC().Format("2006-01-02"))
	return filepath.Join(w.dir, name)
}

func (w *RotatingDirMemoryWriter) getFile(sessionID string) (*os.File, error) {
	path := w.ResolvePath(sessionID)

	if rf, ok := w.files[path]; ok {
		return rf.file, nil
	}

	// Close stale file handles for this session with different paths (e.g., date rollover)
	sessionHash := safeSessionPrefix(sessionID, 16)
	for existingPath, rf := range w.files {
		if strings.Contains(existingPath, sessionHash) && existingPath != path {
			rf.file.Close()
			delete(w.files, existingPath)
		}
	}

	// Create subdirectories if the pattern includes them
	if dir := filepath.Dir(path); dir != w.dir {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create subdirectory: %w", err)
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open session file %q: %w", path, err)
	}

	// Get current file size
	stat, err := f.Stat()
	written := int64(0)
	if err == nil {
		written = stat.Size()
	}

	w.files[path] = &rotatingFile{
		path:    path,
		file:    f,
		written: written,
	}
	return f, nil
}

// rotate archives the current file and opens a new one if needed
func (w *RotatingDirMemoryWriter) rotate(rf *rotatingFile) error {
	if rf.file != nil {
		rf.file.Close()
	}

	// Generate archive name with timestamp
	ext := filepath.Ext(rf.path)
	base := rf.path[:len(rf.path)-len(ext)]
	timestamp := time.Now().UTC().Format("20060102-150405")
	archivePath := base + "." + timestamp + ext

	// Rename current file to archive name
	if err := os.Rename(rf.path, archivePath); err != nil {
		return fmt.Errorf("rotate file: %w", err)
	}

	// Optionally compress the archive
	if w.compressOld {
		if err := w.compressFile(archivePath); err != nil {
			// Log but don't fail - continue with new file
			fmt.Fprintf(os.Stderr, "Warning: failed to compress archived file %q: %v\n", archivePath, err)
		}
	}

	// Open new file
	newFile, err := os.OpenFile(rf.path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open new file after rotation: %w", err)
	}

	rf.file = newFile
	rf.written = 0
	return nil
}

// compressFile gzips a file and removes the original
func (w *RotatingDirMemoryWriter) compressFile(filePath string) error {
	src, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer src.Close()

	gzPath := filePath + ".gz"
	dst, err := os.Create(gzPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	gz := gzip.NewWriter(dst)
	defer gz.Close()

	if _, err := io.Copy(gz, src); err != nil {
		os.Remove(gzPath)
		return err
	}

	// Only remove original after successful compression
	if err := gz.Close(); err != nil {
		os.Remove(gzPath)
		return err
	}

	return os.Remove(filePath)
}

func (w *RotatingDirMemoryWriter) Append(sessionID, role, model, content string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	f, err := w.getFile(sessionID)
	if err != nil {
		return err
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	entry := fmt.Sprintf("## %s | %s | %s | %s\n\n%s\n\n---\n\n", ts, safeSessionPrefix(sessionID, 16), role, model, sanitizeMemoryContent(content))

	// Check if we need to rotate before writing
	if w.maxSizeBytes > 0 {
		path := w.ResolvePath(sessionID)
		rf := w.files[path]
		if rf != nil && rf.written+int64(len(entry)) > w.maxSizeBytes {
			if err := w.rotate(rf); err != nil {
				return fmt.Errorf("rotate file: %w", err)
			}
			// Get the new file after rotation
			f, err = w.getFile(sessionID)
			if err != nil {
				return err
			}
		}
	}

	n, err := f.WriteString(entry)
	if err == nil && w.maxSizeBytes > 0 {
		path := w.ResolvePath(sessionID)
		if rf, ok := w.files[path]; ok {
			rf.written += int64(n)
		}
	}
	return err
}

func (w *RotatingDirMemoryWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	var firstErr error
	for path, rf := range w.files {
		if rf.file != nil {
			if err := rf.file.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		delete(w.files, path)
	}
	return firstErr
}

// RedisMemoryWriter pushes conversation entries to a Redis list keyed by session.
type RedisMemoryWriter struct {
	client *redis.Client
	prefix string
}

// NewRedisMemoryWriter creates a writer that pushes to Redis lists.
func NewRedisMemoryWriter(client *redis.Client) *RedisMemoryWriter {
	return &RedisMemoryWriter{
		client: client,
		prefix: "tokenomics:memory:",
	}
}

func (w *RedisMemoryWriter) Append(sessionID, role, model, content string) error {
	ctx := context.Background()
	ts := time.Now().UTC().Format(time.RFC3339)
	entry := fmt.Sprintf("## %s | %s | %s\n\n%s", ts, role, model, sanitizeMemoryContent(content))
	return w.client.RPush(ctx, w.prefix+sessionID, entry).Err()
}

func (w *RedisMemoryWriter) Close() error {
	return nil
}

// NopMemoryWriter is a no-op implementation when memory is disabled.
type NopMemoryWriter struct{}

func (w *NopMemoryWriter) Append(sessionID, role, model, content string) error { return nil }
func (w *NopMemoryWriter) Close() error                                        { return nil }

// sanitizeMemoryContent makes memory log entries readable by replacing invalid UTF-8
// bytes and normalizing non-whitespace control bytes to spaces.
func sanitizeMemoryContent(content string) string {
	if content == "" {
		return ""
	}
	if !needsSanitization(content) {
		return content
	}

	var b strings.Builder
	b.Grow(len(content))

	for i := 0; i < len(content); {
		r, size := utf8.DecodeRuneInString(content[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteByte('?')
			i++
			continue
		}
		if isDisallowedControlRune(r) {
			b.WriteByte(' ')
			i += size
			continue
		}
		b.WriteRune(r)
		i += size
	}

	return b.String()
}

func needsSanitization(content string) bool {
	for i := 0; i < len(content); {
		r, size := utf8.DecodeRuneInString(content[i:])
		if r == utf8.RuneError && size == 1 {
			return true
		}
		if isDisallowedControlRune(r) {
			return true
		}
		i += size
	}
	return false
}

func isDisallowedControlRune(r rune) bool {
	switch r {
	case '\n', '\r', '\t':
		return false
	}
	return (r >= 0 && r < 0x20) || r == 0x7f
}
