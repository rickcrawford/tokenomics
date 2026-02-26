package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	entry := fmt.Sprintf("## %s | %s | %s | %s\n\n%s\n\n---\n\n", ts, safeSessionPrefix(sessionID, 16), role, model, content)
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
	entry := fmt.Sprintf("## %s | %s | %s | %s\n\n%s\n\n---\n\n", ts, safeSessionPrefix(sessionID, 16), role, model, content)
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
	entry := fmt.Sprintf("## %s | %s | %s\n\n%s", ts, role, model, content)
	return w.client.RPush(ctx, w.prefix+sessionID, entry).Err()
}

func (w *RedisMemoryWriter) Close() error {
	return nil
}

// NopMemoryWriter is a no-op implementation when memory is disabled.
type NopMemoryWriter struct{}

func (w *NopMemoryWriter) Append(sessionID, role, model, content string) error { return nil }
func (w *NopMemoryWriter) Close() error                                        { return nil }
