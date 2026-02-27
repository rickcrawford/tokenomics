package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickcrawford/tokenomics/internal/events"

	bolt "go.etcd.io/bbolt"
)

type capturingEmitter struct {
	mu     sync.Mutex
	events []events.Event
}

func (c *capturingEmitter) Emit(_ context.Context, event events.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
	return nil
}

func (c *capturingEmitter) Close() error { return nil }

func validPolicyJSON() string {
	return `{"base_key_env":"TEST_KEY"}`
}

func validPolicyJSONWithModel(model string) string {
	return `{"base_key_env":"TEST_KEY","model":"` + model + `"}`
}

func newTestStore(t *testing.T) *BoltStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s := NewBoltStore(dbPath, "")
	if err := s.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	t.Cleanup(func() {
		s.Close()
	})
	return s
}

func newEncryptedTestStore(t *testing.T) *BoltStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "encrypted.db")
	s := NewBoltStore(dbPath, "test-encryption-secret-key")
	if err := s.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}
	t.Cleanup(func() {
		s.Close()
	})
	return s
}

func TestInit(t *testing.T) {
	tests := []struct {
		name    string
		dbPath  string
		wantErr bool
	}{
		{
			name:    "valid path creates database",
			dbPath:  filepath.Join(t.TempDir(), "good.db"),
			wantErr: false,
		},
		{
			name:    "invalid path returns error",
			dbPath:  "/nonexistent/deep/path/bad.db",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewBoltStore(tt.dbPath, "")
			err := s.Init()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			s.Close()
		})
	}
}

func TestCreate(t *testing.T) {
	tests := []struct {
		name       string
		tokenHash  string
		policyJSON string
		wantErr    string
	}{
		{
			name:       "valid token and policy",
			tokenHash:  "hash_abc123",
			policyJSON: validPolicyJSON(),
			wantErr:    "",
		},
		{
			name:       "invalid policy JSON rejected",
			tokenHash:  "hash_bad_policy",
			policyJSON: `{"not_valid": true}`,
			wantErr:    "base_key_env is required",
		},
		{
			name:       "malformed JSON rejected",
			tokenHash:  "hash_malformed",
			policyJSON: `{broken`,
			wantErr:    "invalid policy JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			err := s.Create(tt.tokenHash, tt.policyJSON, "")
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestCreate_DuplicateHash(t *testing.T) {
	s := newTestStore(t)

	if err := s.Create("duplicate_hash", validPolicyJSON(), ""); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	err := s.Create("duplicate_hash", validPolicyJSON(), "")
	if err == nil {
		t.Fatal("expected error on duplicate hash, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected 'already exists' error, got %q", err.Error())
	}
}

func TestLookup(t *testing.T) {
	s := newTestStore(t)

	// Lookup non-existent key returns nil policy, no error
	p, err := s.Lookup("nonexistent")
	if err != nil {
		t.Fatalf("Lookup non-existent: unexpected error: %v", err)
	}
	if p != nil {
		t.Fatalf("expected nil policy for non-existent key, got %+v", p)
	}

	// Create and then look up
	hash := "lookup_test_hash"
	if err := s.Create(hash, validPolicyJSONWithModel("gpt-4"), ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	p, err = s.Lookup(hash)
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil policy after Create")
	}
	if p.BaseKeyEnv != "TEST_KEY" {
		t.Errorf("BaseKeyEnv = %q, want %q", p.BaseKeyEnv, "TEST_KEY")
	}
	if p.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", p.Model, "gpt-4")
	}
}

func TestDelete(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(s *BoltStore)
		deleteKey string
		wantErr   string
	}{
		{
			name: "delete existing token succeeds",
			setup: func(s *BoltStore) {
				if err := s.Create("to_delete", validPolicyJSON(), ""); err != nil {
					t.Fatalf("setup create to_delete: %v", err)
				}
			},
			deleteKey: "to_delete",
			wantErr:   "",
		},
		{
			name:      "delete non-existent token fails",
			setup:     func(s *BoltStore) {},
			deleteKey: "does_not_exist",
			wantErr:   "token not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)
			tt.setup(s)

			err := s.Delete(tt.deleteKey)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify token is gone from cache
			p, err := s.Lookup(tt.deleteKey)
			if err != nil {
				t.Fatalf("Lookup after delete: %v", err)
			}
			if p != nil {
				t.Fatal("expected nil policy after delete")
			}
		})
	}
}

func TestList(t *testing.T) {
	s := newTestStore(t)

	// Empty store
	records, err := s.List()
	if err != nil {
		t.Fatalf("List on empty store: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records on empty store, got %d", len(records))
	}

	// Add some tokens
	hashes := []string{"hash_a", "hash_b", "hash_c"}
	for _, h := range hashes {
		if err := s.Create(h, validPolicyJSON(), ""); err != nil {
			t.Fatalf("Create(%s): %v", h, err)
		}
	}

	records, err = s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	// Verify all hashes are present
	found := make(map[string]bool)
	for _, r := range records {
		found[r.TokenHash] = true
		if r.Policy == nil {
			t.Errorf("record %s has nil Policy", r.TokenHash)
		}
		if r.CreatedAt == "" {
			t.Errorf("record %s has empty CreatedAt", r.TokenHash)
		}
	}
	for _, h := range hashes {
		if !found[h] {
			t.Errorf("hash %q not found in List results", h)
		}
	}
}

func TestReload(t *testing.T) {
	s := newTestStore(t)

	// Create a token
	if err := s.Create("reload_test", validPolicyJSON(), ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Manually clear the cache to simulate stale state
	s.mu.Lock()
	s.cache = make(map[string]*cacheEntry)
	s.mu.Unlock()

	// Verify cache is empty
	p, _ := s.Lookup("reload_test")
	if p != nil {
		t.Fatal("expected nil after clearing cache")
	}

	// Reload should restore from DB
	if err := s.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	p, err := s.Lookup("reload_test")
	if err != nil {
		t.Fatalf("Lookup after Reload: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil policy after Reload")
	}
	if p.BaseKeyEnv != "TEST_KEY" {
		t.Errorf("BaseKeyEnv = %q, want %q", p.BaseKeyEnv, "TEST_KEY")
	}
}

func TestClose(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "close_test.db")
	s := NewBoltStore(dbPath, "")
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Operations after close should fail
	err := s.Create("after_close", validPolicyJSON(), "")
	if err == nil {
		t.Fatal("expected error after Close, got nil")
	}
}

// --- Encryption tests ---

func TestEncryption_CreateAndLookup(t *testing.T) {
	s := newEncryptedTestStore(t)

	hash := "enc_test_hash"
	if err := s.Create(hash, validPolicyJSONWithModel("gpt-4o"), ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	p, err := s.Lookup(hash)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil policy")
	}
	if p.BaseKeyEnv != "TEST_KEY" {
		t.Errorf("BaseKeyEnv = %q, want %q", p.BaseKeyEnv, "TEST_KEY")
	}
	if p.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", p.Model, "gpt-4o")
	}
}

func TestEncryption_DataIsEncrypted(t *testing.T) {
	s := newEncryptedTestStore(t)

	hash := "enc_verify_hash"
	if err := s.Create(hash, validPolicyJSON(), ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Read raw data from bolt and verify it doesn't contain plaintext
	var rawValue []byte
	if err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(tokensBucket)
		rawValue = append([]byte{}, b.Get([]byte(hash))...)
		return nil
	}); err != nil {
		t.Fatalf("read raw bolt value: %v", err)
	}

	raw := string(rawValue)
	if strings.Contains(raw, "TEST_KEY") {
		t.Error("raw database value contains plaintext policy — encryption not working")
	}
	if !strings.Contains(raw, "encrypted") {
		t.Error("raw database value missing 'encrypted' field")
	}
}

func TestEncryption_WrongKeyFails(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wrong_key.db")

	// Create with one key
	s1 := NewBoltStore(dbPath, "secret-key-1")
	if err := s1.Init(); err != nil {
		t.Fatalf("Init s1: %v", err)
	}
	if err := s1.Create("wk_hash", validPolicyJSON(), ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	s1.Close()

	// Open with different key — Reload should warn and skip
	s2 := NewBoltStore(dbPath, "wrong-key-2")
	if err := s2.Init(); err != nil {
		t.Fatalf("Init s2: %v", err)
	}
	defer s2.Close()

	// Token should not be in cache since decrypt fails
	p, err := s2.Lookup("wk_hash")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p != nil {
		t.Error("expected nil policy when decrypting with wrong key")
	}
}

func TestEncryption_List(t *testing.T) {
	s := newEncryptedTestStore(t)

	for _, h := range []string{"el_a", "el_b"} {
		if err := s.Create(h, validPolicyJSON(), ""); err != nil {
			t.Fatalf("Create(%s): %v", h, err)
		}
	}

	records, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	for _, r := range records {
		if r.Policy == nil {
			t.Errorf("record %s has nil policy", r.TokenHash)
		}
	}
}

func TestEncryption_Get(t *testing.T) {
	s := newEncryptedTestStore(t)

	hash := "enc_get_hash"
	if err := s.Create(hash, validPolicyJSONWithModel("claude-3"), ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rec, err := s.Get(hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec == nil {
		t.Fatal("expected non-nil record")
	}
	if rec.Policy.Model != "claude-3" {
		t.Errorf("Model = %q, want %q", rec.Policy.Model, "claude-3")
	}
}

func TestEncryption_Update(t *testing.T) {
	s := newEncryptedTestStore(t)

	hash := "enc_update_hash"
	if err := s.Create(hash, validPolicyJSONWithModel("gpt-4"), ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Update(hash, validPolicyJSONWithModel("gpt-4o"), ""); err != nil {
		t.Fatalf("Update: %v", err)
	}

	p, err := s.Lookup(hash)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil policy")
	}
	if p.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q after update", p.Model, "gpt-4o")
	}
}

// --- Expiration tests ---

func TestExpiration_CreateWithExpiry(t *testing.T) {
	s := newTestStore(t)

	future := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)
	if err := s.Create("exp_future", validPolicyJSON(), future); err != nil {
		t.Fatalf("Create with future expiry: %v", err)
	}

	p, err := s.Lookup("exp_future")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil policy for non-expired token")
	}
}

func TestExpiration_ExpiredTokenReturnsNil(t *testing.T) {
	s := newTestStore(t)

	past := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	if err := s.Create("exp_past", validPolicyJSON(), past); err != nil {
		t.Fatalf("Create with past expiry: %v", err)
	}

	p, err := s.Lookup("exp_past")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p != nil {
		t.Error("expected nil policy for expired token")
	}
}

func TestExpiration_ExpiredTokenEmitsOnceAndRemovesCacheEntry(t *testing.T) {
	s := newTestStore(t)
	em := &capturingEmitter{}
	s.SetEmitter(em)

	past := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	if err := s.Create("exp_emit_once", validPolicyJSON(), past); err != nil {
		t.Fatalf("Create with past expiry: %v", err)
	}

	// First lookup should emit token.expired and remove from cache.
	p, err := s.Lookup("exp_emit_once")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p != nil {
		t.Fatal("expected nil policy for expired token")
	}

	// Second lookup should not emit again because cache entry is removed.
	p, err = s.Lookup("exp_emit_once")
	if err != nil {
		t.Fatalf("Lookup second time: %v", err)
	}
	if p != nil {
		t.Fatal("expected nil policy for expired token on second lookup")
	}

	em.mu.Lock()
	defer em.mu.Unlock()
	expiredEvents := 0
	for _, evt := range em.events {
		if evt.Type == events.TokenExpired {
			expiredEvents++
		}
	}
	if expiredEvents != 1 {
		t.Fatalf("expected exactly one token.expired event, got %d", expiredEvents)
	}
}

func TestExpiration_NoExpiry(t *testing.T) {
	s := newTestStore(t)

	if err := s.Create("exp_none", validPolicyJSON(), ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	p, err := s.Lookup("exp_none")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil policy for token without expiration")
	}
}

func TestExpiration_InvalidFormat(t *testing.T) {
	s := newTestStore(t)

	err := s.Create("exp_invalid", validPolicyJSON(), "not-a-date")
	if err == nil {
		t.Fatal("expected error for invalid expiration format")
	}
	if !strings.Contains(err.Error(), "invalid expires_at") {
		t.Errorf("expected 'invalid expires_at' error, got %q", err.Error())
	}
}

func TestExpiration_ListShowsExpiry(t *testing.T) {
	s := newTestStore(t)

	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	if err := s.Create("exp_list", validPolicyJSON(), future); err != nil {
		t.Fatalf("Create: %v", err)
	}

	records, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].ExpiresAt == "" {
		t.Error("expected non-empty ExpiresAt in list record")
	}
}

func TestExpiration_GetShowsExpiry(t *testing.T) {
	s := newTestStore(t)

	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	if err := s.Create("exp_get", validPolicyJSON(), future); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rec, err := s.Get("exp_get")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec == nil {
		t.Fatal("expected non-nil record")
	}
	if rec.ExpiresAt != future {
		t.Errorf("ExpiresAt = %q, want %q", rec.ExpiresAt, future)
	}
}

// --- Get and Update tests ---

func TestGet_NotFound(t *testing.T) {
	s := newTestStore(t)

	rec, err := s.Get("nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec != nil {
		t.Error("expected nil record for nonexistent hash")
	}
}

func TestGet_ReturnsFullRecord(t *testing.T) {
	s := newTestStore(t)

	hash := "get_full"
	if err := s.Create(hash, validPolicyJSONWithModel("gpt-4"), ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rec, err := s.Get(hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec == nil {
		t.Fatal("expected non-nil record")
	}
	if rec.TokenHash != hash {
		t.Errorf("TokenHash = %q, want %q", rec.TokenHash, hash)
	}
	if rec.CreatedAt == "" {
		t.Error("expected non-empty CreatedAt")
	}
	if rec.Policy.Model != "gpt-4" {
		t.Errorf("Policy.Model = %q, want %q", rec.Policy.Model, "gpt-4")
	}
	if rec.PolicyRaw == "" {
		t.Error("expected non-empty PolicyRaw")
	}
}

func TestUpdate_Policy(t *testing.T) {
	s := newTestStore(t)

	hash := "update_pol"
	if err := s.Create(hash, validPolicyJSONWithModel("gpt-3"), ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Update(hash, validPolicyJSONWithModel("gpt-4o"), ""); err != nil {
		t.Fatalf("Update: %v", err)
	}

	p, err := s.Lookup(hash)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil policy after update")
	}
	if p.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", p.Model, "gpt-4o")
	}
}

func TestUpdate_Expiration(t *testing.T) {
	s := newTestStore(t)

	hash := "update_exp"
	if err := s.Create(hash, validPolicyJSON(), ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	if err := s.Update(hash, "", future); err != nil {
		t.Fatalf("Update expiration: %v", err)
	}

	rec, err := s.Get(hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.ExpiresAt != future {
		t.Errorf("ExpiresAt = %q, want %q", rec.ExpiresAt, future)
	}
}

func TestUpdate_ClearExpiration(t *testing.T) {
	s := newTestStore(t)

	hash := "update_clear_exp"
	future := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	if err := s.Create(hash, validPolicyJSON(), future); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.Update(hash, "", "clear"); err != nil {
		t.Fatalf("Update to clear expiration: %v", err)
	}

	rec, err := s.Get(hash)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.ExpiresAt != "" {
		t.Errorf("ExpiresAt = %q, want empty after clear", rec.ExpiresAt)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	s := newTestStore(t)

	err := s.Update("nonexistent", validPolicyJSON(), "")
	if err == nil {
		t.Fatal("expected error for nonexistent token")
	}
	if !strings.Contains(err.Error(), "token not found") {
		t.Errorf("expected 'token not found' error, got %q", err.Error())
	}
}

// --- Crypto unit tests ---

func TestEncryptDecrypt_Roundtrip(t *testing.T) {
	key := deriveKey("my-test-secret")
	plaintext := []byte(`{"base_key_env":"OPENAI_API_KEY","model":"gpt-4o"}`)

	encrypted, err := encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if encrypted == string(plaintext) {
		t.Error("encrypted output is same as plaintext")
	}

	decrypted, err := decrypt(encrypted, key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("decrypted = %q, want %q", string(decrypted), string(plaintext))
	}
}

func TestEncryptDecrypt_DifferentCiphertext(t *testing.T) {
	key := deriveKey("my-test-secret")
	plaintext := []byte("same input")

	enc1, _ := encrypt(plaintext, key)
	enc2, _ := encrypt(plaintext, key)

	if enc1 == enc2 {
		t.Error("two encryptions of same plaintext produced identical ciphertext — nonce may not be random")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1 := deriveKey("key-one")
	key2 := deriveKey("key-two")

	encrypted, err := encrypt([]byte("secret data"), key1)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = decrypt(encrypted, key2)
	if err == nil {
		t.Error("expected error decrypting with wrong key")
	}
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	key := deriveKey("any-key")
	_, err := decrypt("!!!not-base64!!!", key)
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestDeriveKey_Deterministic(t *testing.T) {
	k1 := deriveKey("same-secret")
	k2 := deriveKey("same-secret")

	if string(k1) != string(k2) {
		t.Error("deriveKey not deterministic for same input")
	}
	if len(k1) != 32 {
		t.Errorf("expected 32-byte key, got %d", len(k1))
	}
}

func TestDeriveKey_DifferentSecrets(t *testing.T) {
	k1 := deriveKey("secret-one")
	k2 := deriveKey("secret-two")

	if string(k1) == string(k2) {
		t.Error("different secrets produced same key")
	}
}

func TestClose_IdempotentWithFileWatcher(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "watch.db")
	s := NewBoltStore(dbPath, "")
	if err := s.Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	s.StartFileWatch(1 * time.Millisecond)

	if err := s.Close(); err != nil {
		t.Fatalf("first Close() error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close() error: %v", err)
	}
}

func TestReload_SkipsCorruptRecord(t *testing.T) {
	s := newTestStore(t)

	// Insert corrupt raw JSON directly into Bolt bucket.
	if err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(tokensBucket)
		return b.Put([]byte("corrupt-key"), []byte("{not-json"))
	}); err != nil {
		t.Fatalf("insert corrupt record: %v", err)
	}

	if err := s.Reload(); err != nil {
		t.Fatalf("Reload() should skip corrupt records, got error: %v", err)
	}

	// Corrupt entry should not appear in cache/List.
	p, err := s.Lookup("corrupt-key")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if p != nil {
		t.Fatal("expected corrupt record to be skipped during reload")
	}
}

func TestCreate_LargePolicy(t *testing.T) {
	s := newTestStore(t)

	large := strings.Repeat("x", 2*1024*1024) // 2 MB payload
	policyJSON := `{"base_key_env":"TEST_KEY","prompts":[{"role":"system","content":"` + large + `"}]}`

	if err := s.Create("large-policy-hash", policyJSON, ""); err != nil {
		t.Fatalf("Create large policy: %v", err)
	}

	p, err := s.Lookup("large-policy-hash")
	if err != nil {
		t.Fatalf("Lookup large policy: %v", err)
	}
	if p == nil || len(p.Prompts) != 1 {
		t.Fatal("expected large policy to be retrievable")
	}
}

func TestInit_DBPathPermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission mode semantics differ on Windows")
	}

	parent := filepath.Join(t.TempDir(), "readonly")
	if err := os.MkdirAll(parent, 0o555); err != nil {
		t.Fatalf("mkdir readonly dir: %v", err)
	}

	dbPath := filepath.Join(parent, "denied.db")
	s := NewBoltStore(dbPath, "")
	err := s.Init()
	if err == nil {
		_ = s.Close()
		t.Fatal("expected permission error when opening DB in readonly directory")
	}
}

func TestConcurrentCreateAndLookup(t *testing.T) {
	s := newTestStore(t)

	const n = 50
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			hash := fmt.Sprintf("concurrent-hash-%d", i)
			_ = s.Create(hash, validPolicyJSON(), "")
			_, _ = s.Lookup(hash)
		}()
	}
	wg.Wait()

	records, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one created record in concurrent test")
	}
}

func TestFileWatch_ReloadsExternalChanges(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "watch-reload.db")
	s1 := NewBoltStore(dbPath, "")
	if err := s1.Init(); err != nil {
		t.Fatalf("Init s1: %v", err)
	}
	defer s1.Close()
	s1.StartFileWatch(10 * time.Millisecond)

	// Write directly to BoltDB to bypass cache updates, then rely on file watcher reload.
	rec := boltRecord{
		PolicyJSON: validPolicyJSON(),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal test record: %v", err)
	}
	if err := s1.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(tokensBucket)
		return b.Put([]byte("watched-hash"), data)
	}); err != nil {
		t.Fatalf("direct bolt write: %v", err)
	}

	deadline := time.Now().Add(750 * time.Millisecond)
	for time.Now().Before(deadline) {
		p, err := s1.Lookup("watched-hash")
		if err != nil {
			t.Fatalf("Lookup from s1: %v", err)
		}
		if p != nil {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("expected file watcher to reload token created by external writer")
}
