package session

import (
	"context"
	"fmt"
	"os"
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
	entry := fmt.Sprintf("## %s | %s | %s | %s\n\n%s\n\n---\n\n", ts, sessionID[:16], role, model, content)
	_, err := w.file.WriteString(entry)
	return err
}

func (w *FileMemoryWriter) Close() error {
	if w.file != nil {
		return w.file.Close()
	}
	return nil
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
