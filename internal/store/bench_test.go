package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkDeriveKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		deriveKey("my-secret-encryption-key")
	}
}

func BenchmarkEncrypt(b *testing.B) {
	key := deriveKey("benchmark-key")
	data := []byte(`{"base_key_env":"OPENAI_API_KEY","max_tokens":100000,"model_regex":"^gpt"}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encrypt(data, key)
	}
}

func BenchmarkDecrypt(b *testing.B) {
	key := deriveKey("benchmark-key")
	data := []byte(`{"base_key_env":"OPENAI_API_KEY","max_tokens":100000,"model_regex":"^gpt"}`)
	encrypted, _ := encrypt(data, key)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decrypt(encrypted, key)
	}
}

func BenchmarkBoltStore_Lookup(b *testing.B) {
	dir := b.TempDir()
	dbPath := filepath.Join(dir, "bench.db")

	s := NewBoltStore(dbPath, "bench-secret")
	if err := s.Init(); err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	// Create a token
	polJSON := `{"base_key_env":"OPENAI_API_KEY","max_tokens":100000}`
	if err := s.Create("benchhash123", polJSON, ""); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Lookup("benchhash123")
	}
}

func BenchmarkBoltStore_Lookup_Miss(b *testing.B) {
	dir := b.TempDir()
	dbPath := filepath.Join(dir, "bench.db")

	s := NewBoltStore(dbPath, "bench-secret")
	if err := s.Init(); err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Lookup("nonexistent-hash")
	}
}

func BenchmarkBoltStore_Create(b *testing.B) {
	dir := b.TempDir()
	dbPath := filepath.Join(dir, "bench.db")

	s := NewBoltStore(dbPath, "bench-secret")
	if err := s.Init(); err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	polJSON := `{"base_key_env":"OPENAI_API_KEY","max_tokens":100000}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Create(fmt.Sprintf("hash-%d", i), polJSON, "")
	}
}

func BenchmarkBoltStore_Reload(b *testing.B) {
	dir := b.TempDir()
	dbPath := filepath.Join(dir, "bench.db")

	s := NewBoltStore(dbPath, "bench-secret")
	if err := s.Init(); err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	// Seed with tokens
	for i := 0; i < 100; i++ {
		s.Create(fmt.Sprintf("hash-%d", i), `{"base_key_env":"KEY"}`, "")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Reload()
	}
}

func TestBoltStore_SetEmitter(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "emitter.db")

	s := NewBoltStore(dbPath, "test-secret")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// SetEmitter with nil should not panic
	s.SetEmitter(nil)

	// emit with nil emitter should not panic
	s.emit("test.event", map[string]interface{}{"key": "value"})
}

func TestBoltStore_ListEmpty(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "empty.db")

	s := NewBoltStore(dbPath, "test-secret")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	records, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

func TestBoltStore_ListMultiple(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "list.db")

	s := NewBoltStore(dbPath, "test-secret")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for i := 0; i < 5; i++ {
		if err := s.Create(fmt.Sprintf("hash-%d", i), `{"base_key_env":"KEY"}`, ""); err != nil {
			t.Fatal(err)
		}
	}

	records, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 5 {
		t.Errorf("expected 5 records, got %d", len(records))
	}
}

func TestBoltStore_NoEncryption(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "noenc.db")

	// Empty secret means no encryption
	s := NewBoltStore(dbPath, "")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	polJSON := `{"base_key_env":"OPENAI_API_KEY"}`
	if err := s.Create("hash-noenc", polJSON, ""); err != nil {
		t.Fatal(err)
	}

	pol, err := s.Lookup("hash-noenc")
	if err != nil {
		t.Fatal(err)
	}
	if pol == nil {
		t.Fatal("expected policy, got nil")
	}
	if pol.BaseKeyEnv != "OPENAI_API_KEY" {
		t.Errorf("BaseKeyEnv = %q, want OPENAI_API_KEY", pol.BaseKeyEnv)
	}
}

func TestBoltStore_Reload(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "reload.db")

	s := NewBoltStore(dbPath, "test-secret")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.Create("hash-1", `{"base_key_env":"KEY1"}`, "")
	s.Create("hash-2", `{"base_key_env":"KEY2"}`, "")

	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}

	pol, _ := s.Lookup("hash-1")
	if pol == nil {
		t.Error("expected policy for hash-1 after reload")
	}
	pol, _ = s.Lookup("hash-2")
	if pol == nil {
		t.Error("expected policy for hash-2 after reload")
	}
}

func TestBoltStore_CloseIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "close.db")

	s := NewBoltStore(dbPath, "test-secret")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}

	// Close should not error
	if err := s.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
}

func TestBoltStore_DBFileCreated(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "create.db")

	s := NewBoltStore(dbPath, "test-secret")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("expected database file to exist")
	}
}
