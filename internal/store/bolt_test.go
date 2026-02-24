package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickcrawford/tokenomics/internal/policy"
)

func validPolicyJSON() string {
	return `{"base_key_env":"TEST_KEY"}`
}

func validPolicyJSONWithModel(model string) string {
	return `{"base_key_env":"TEST_KEY","model":"` + model + `"}`
}

func newTestStore(t *testing.T) *BoltStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s := NewBoltStore(dbPath)
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
			s := NewBoltStore(tt.dbPath)
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
			err := s.Create(tt.tokenHash, tt.policyJSON)
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

	if err := s.Create("duplicate_hash", validPolicyJSON()); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	err := s.Create("duplicate_hash", validPolicyJSON())
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
	if err := s.Create(hash, validPolicyJSONWithModel("gpt-4")); err != nil {
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
		setup     func(s *BoltStore) // create tokens before delete
		deleteKey string
		wantErr   string
	}{
		{
			name: "delete existing token succeeds",
			setup: func(s *BoltStore) {
				s.Create("to_delete", validPolicyJSON())
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
		if err := s.Create(h, validPolicyJSON()); err != nil {
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
	if err := s.Create("reload_test", validPolicyJSON()); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Manually clear the cache to simulate stale state
	s.mu.Lock()
	s.cache = make(map[string]*policy.Policy)
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
	s := NewBoltStore(dbPath)
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Operations after close should fail
	err := s.Create("after_close", validPolicyJSON())
	if err == nil {
		t.Fatal("expected error after Close, got nil")
	}
}
