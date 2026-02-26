package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rickcrawford/tokenomics/internal/policy"
	"github.com/rickcrawford/tokenomics/internal/store"
)

// memStore is a minimal in-memory TokenStore for testing.
type memStore struct {
	records map[string]*store.TokenRecord
}

func newMemStore() *memStore {
	return &memStore{records: make(map[string]*store.TokenRecord)}
}

func (m *memStore) Init() error   { return nil }
func (m *memStore) Reload() error { return nil }
func (m *memStore) Close() error  { return nil }

func (m *memStore) Create(tokenHash, policyJSON, expiresAt string) error {
	m.records[tokenHash] = &store.TokenRecord{
		TokenHash: tokenHash,
		PolicyRaw: policyJSON,
		ExpiresAt: expiresAt,
		CreatedAt: "2025-01-01T00:00:00Z",
	}
	return nil
}

func (m *memStore) Get(tokenHash string) (*store.TokenRecord, error) {
	r, ok := m.records[tokenHash]
	if !ok {
		return nil, nil
	}
	return r, nil
}

func (m *memStore) Update(tokenHash, policyJSON, expiresAt string) error {
	r := m.records[tokenHash]
	if policyJSON != "" {
		r.PolicyRaw = policyJSON
	}
	if expiresAt == "clear" {
		r.ExpiresAt = ""
	} else if expiresAt != "" {
		r.ExpiresAt = expiresAt
	}
	return nil
}

func (m *memStore) Delete(tokenHash string) error {
	delete(m.records, tokenHash)
	return nil
}

func (m *memStore) Lookup(tokenHash string) (*policy.Policy, error) {
	r, ok := m.records[tokenHash]
	if !ok {
		return nil, nil
	}
	p, err := policy.Parse(r.PolicyRaw)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (m *memStore) List() ([]store.TokenRecord, error) {
	var list []store.TokenRecord
	for _, r := range m.records {
		list = append(list, *r)
	}
	return list, nil
}

func TestServerListTokens(t *testing.T) {
	ms := newMemStore()
	ms.Create("hash1", `{"base_key_env":"OPENAI_API_KEY"}`, "")
	ms.Create("hash2", `{"base_key_env":"ANTHROPIC_API_KEY"}`, "2026-01-01T00:00:00Z")

	srv := NewServer(ms, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/tokens")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var tokens []TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		t.Fatal(err)
	}

	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}
}

func TestServerGetTokenByHash(t *testing.T) {
	ms := newMemStore()
	ms.Create("abc123", `{"base_key_env":"OPENAI_API_KEY"}`, "")

	srv := NewServer(ms, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/tokens/abc123")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var tok TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		t.Fatal(err)
	}
	if tok.TokenHash != "abc123" {
		t.Fatalf("expected hash abc123, got %s", tok.TokenHash)
	}
}

func TestServerGetTokenNotFound(t *testing.T) {
	ms := newMemStore()
	srv := NewServer(ms, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/tokens/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestServerAuth(t *testing.T) {
	ms := newMemStore()
	srv := NewServer(ms, "secret-key")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// No auth
	resp, _ := http.Get(ts.URL + "/api/v1/tokens")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401 without auth, got %d", resp.StatusCode)
	}

	// Wrong key
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403 with wrong key, got %d", resp.StatusCode)
	}

	// Correct key
	req, _ = http.NewRequest("GET", ts.URL+"/api/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer secret-key")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 with correct key, got %d", resp.StatusCode)
	}
}

func TestServerHealth(t *testing.T) {
	ms := newMemStore()
	srv := NewServer(ms, "secret-key")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Health should not require auth
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestServerMethodNotAllowed(t *testing.T) {
	ms := newMemStore()
	srv := NewServer(ms, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, _ := http.Post(ts.URL+"/api/v1/tokens", "application/json", nil)
	if resp.StatusCode != 405 {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestClientFetchTokens(t *testing.T) {
	ms := newMemStore()
	ms.Create("hash1", `{"base_key_env":"OPENAI_API_KEY"}`, "")

	srv := NewServer(ms, "my-key")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewClient(ts.URL, "my-key", nil)
	tokens, err := client.FetchTokens()
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	if tokens[0].TokenHash != "hash1" {
		t.Fatalf("expected hash1, got %s", tokens[0].TokenHash)
	}
}

func TestClientFetchUnauthorized(t *testing.T) {
	ms := newMemStore()
	srv := NewServer(ms, "correct-key")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewClient(ts.URL, "wrong-key", nil)
	_, err := client.FetchTokens()
	if err == nil {
		t.Fatal("expected error for wrong key")
	}
	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", httpErr.StatusCode)
	}
}

func TestClientSyncTo(t *testing.T) {
	// Remote store has 2 tokens
	remoteStore := newMemStore()
	remoteStore.Create("hash-a", `{"base_key_env":"OPENAI_API_KEY"}`, "")
	remoteStore.Create("hash-b", `{"base_key_env":"ANTHROPIC_API_KEY"}`, "2026-06-01T00:00:00Z")

	srv := NewServer(remoteStore, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Local store is empty
	localStore := newMemStore()

	client := NewClient(ts.URL, "", nil)
	n, err := client.SyncTo(localStore)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 synced, got %d", n)
	}

	// Verify tokens are in local store
	rec, _ := localStore.Get("hash-a")
	if rec == nil {
		t.Fatal("hash-a not found in local store")
	}
	if rec.PolicyRaw != `{"base_key_env":"OPENAI_API_KEY"}` {
		t.Fatalf("unexpected policy: %s", rec.PolicyRaw)
	}

	// Second sync should be no-op
	n, err = client.SyncTo(localStore)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected 0 synced on second call, got %d", n)
	}
}

func TestClientSyncToUpdatesChanged(t *testing.T) {
	remoteStore := newMemStore()
	remoteStore.Create("hash-x", `{"base_key_env":"OPENAI_API_KEY"}`, "")

	srv := NewServer(remoteStore, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	localStore := newMemStore()
	localStore.Create("hash-x", `{"base_key_env":"OLD_KEY"}`, "")

	client := NewClient(ts.URL, "", nil)
	n, err := client.SyncTo(localStore)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 updated, got %d", n)
	}

	rec, _ := localStore.Get("hash-x")
	if rec.PolicyRaw != `{"base_key_env":"OPENAI_API_KEY"}` {
		t.Fatalf("policy not updated: %s", rec.PolicyRaw)
	}
}

func TestClientSyncToPreservesLocalOnly(t *testing.T) {
	// Remote has 1 token, local has 2 (one is local-only)
	remoteStore := newMemStore()
	remoteStore.Create("hash-remote", `{"base_key_env":"OPENAI_API_KEY"}`, "")

	srv := NewServer(remoteStore, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	localStore := newMemStore()
	localStore.Create("hash-local", `{"base_key_env":"LOCAL_KEY"}`, "")

	client := NewClient(ts.URL, "", nil)
	n, err := client.SyncTo(localStore)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 synced, got %d", n)
	}

	// Local-only token should still exist
	rec, _ := localStore.Get("hash-local")
	if rec == nil {
		t.Fatal("local-only token was removed during sync")
	}

	// Remote token should now exist locally
	rec, _ = localStore.Get("hash-remote")
	if rec == nil {
		t.Fatal("remote token not synced to local")
	}
}

func TestHTTPError(t *testing.T) {
	e := &HTTPError{StatusCode: 503}
	if e.Error() != "remote server returned Service Unavailable" {
		t.Fatalf("unexpected error message: %s", e.Error())
	}
}

func TestClientRegisterWebhook(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	srv := NewServer(ms, "my-key", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewClient(ts.URL, "my-key", nil)
	id, err := client.RegisterWebhook(ClientRegistration{
		URL:       "https://proxy.example.com/v1/webhook",
		Secret:    "shared-secret",
		SigningKey: "hmac-key",
		Events:    []string{"token.*"},
		Insecure:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected non-empty client ID")
	}

	// Verify registration was stored
	clients, _ := cr.List()
	if len(clients) != 1 {
		t.Fatalf("expected 1 registered client, got %d", len(clients))
	}
	if clients[0].URL != "https://proxy.example.com/v1/webhook" {
		t.Fatalf("unexpected URL: %s", clients[0].URL)
	}
	if !clients[0].Insecure {
		t.Fatal("expected insecure=true")
	}
}

func TestClientRegisterWebhookUnauthorized(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	srv := NewServer(ms, "correct-key", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewClient(ts.URL, "wrong-key", nil)
	_, err = client.RegisterWebhook(ClientRegistration{URL: "https://proxy.example.com/hook"})
	if err == nil {
		t.Fatal("expected error for wrong API key")
	}
	httpErr, ok := err.(*HTTPError)
	if !ok {
		t.Fatalf("expected HTTPError, got %T", err)
	}
	if httpErr.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", httpErr.StatusCode)
	}
}

func TestClientUnregisterWebhook(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	srv := NewServer(ms, "", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewClient(ts.URL, "", nil)
	id, err := client.RegisterWebhook(ClientRegistration{URL: "https://proxy.example.com/hook"})
	if err != nil {
		t.Fatal(err)
	}

	// Unregister
	err = client.UnregisterWebhook(id)
	if err != nil {
		t.Fatal(err)
	}

	// Verify it's gone
	clients, _ := cr.List()
	if len(clients) != 0 {
		t.Fatalf("expected 0 clients after unregister, got %d", len(clients))
	}
}

func TestClientUnregisterWebhookNotFound(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	srv := NewServer(ms, "", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	client := NewClient(ts.URL, "", nil)
	err = client.UnregisterWebhook("cl_nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent client")
	}
}
