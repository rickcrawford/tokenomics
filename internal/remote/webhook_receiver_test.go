package remote

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rickcrawford/tokenomics/internal/config"
)

// trackingStore wraps memStore and counts Reload calls.
type trackingStore struct {
	*memStore
	reloadCount atomic.Int64
}

func newTrackingStore() *trackingStore {
	return &trackingStore{memStore: newMemStore()}
}

func (ts *trackingStore) Reload() error {
	ts.reloadCount.Add(1)
	return nil
}

func TestWebhookReceiver_AcceptsTokenEvent(t *testing.T) {
	store := newTrackingStore()
	receiver := NewWebhookReceiver(config.WebhookReceiver{Enabled: true}, store, nil)

	body := `{"type":"token.created","id":"evt_123","timestamp":"2025-01-01T00:00:00Z"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/webhook", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	receiver.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != `{"status":"accepted"}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}

	// Wait for async reload
	time.Sleep(50 * time.Millisecond)
	if store.reloadCount.Load() != 1 {
		t.Fatalf("expected 1 reload, got %d", store.reloadCount.Load())
	}
}

func TestWebhookReceiver_IgnoresNonTokenEvent(t *testing.T) {
	store := newTrackingStore()
	receiver := NewWebhookReceiver(config.WebhookReceiver{Enabled: true}, store, nil)

	body := `{"type":"request.completed","id":"evt_456"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/webhook", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	receiver.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != `{"status":"ignored","reason":"not a token event"}` {
		t.Fatalf("unexpected body: %s", w.Body.String())
	}

	time.Sleep(50 * time.Millisecond)
	if store.reloadCount.Load() != 0 {
		t.Fatalf("expected 0 reloads, got %d", store.reloadCount.Load())
	}
}

func TestWebhookReceiver_RejectsGet(t *testing.T) {
	store := newTrackingStore()
	receiver := NewWebhookReceiver(config.WebhookReceiver{Enabled: true}, store, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/webhook", nil)
	w := httptest.NewRecorder()

	receiver.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestWebhookReceiver_SecretAuth(t *testing.T) {
	store := newTrackingStore()
	receiver := NewWebhookReceiver(config.WebhookReceiver{
		Enabled: true,
		Secret:  "my-secret",
	}, store, nil)

	body := `{"type":"token.updated"}`

	// Missing secret
	req := httptest.NewRequest(http.MethodPost, "/v1/webhook", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	receiver.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without secret, got %d", w.Code)
	}

	// Wrong secret
	req = httptest.NewRequest(http.MethodPost, "/v1/webhook", bytes.NewBufferString(body))
	req.Header.Set("X-Webhook-Secret", "wrong")
	w = httptest.NewRecorder()
	receiver.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong secret, got %d", w.Code)
	}

	// Correct secret - reset debounce first
	receiver.mu.Lock()
	receiver.lastSync = time.Time{}
	receiver.mu.Unlock()

	req = httptest.NewRequest(http.MethodPost, "/v1/webhook", bytes.NewBufferString(body))
	req.Header.Set("X-Webhook-Secret", "my-secret")
	w = httptest.NewRecorder()
	receiver.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with correct secret, got %d", w.Code)
	}
}

func TestWebhookReceiver_HMACSignature(t *testing.T) {
	store := newTrackingStore()
	signingKey := "hmac-test-key"
	receiver := NewWebhookReceiver(config.WebhookReceiver{
		Enabled:    true,
		SigningKey: signingKey,
	}, store, nil)

	body := []byte(`{"type":"token.deleted"}`)

	// Missing signature
	req := httptest.NewRequest(http.MethodPost, "/v1/webhook", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	receiver.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without signature, got %d", w.Code)
	}

	// Invalid signature
	req = httptest.NewRequest(http.MethodPost, "/v1/webhook", bytes.NewBuffer(body))
	req.Header.Set("X-Webhook-Signature", "sha256=0000000000000000")
	w = httptest.NewRecorder()
	receiver.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with wrong signature, got %d", w.Code)
	}

	// Valid signature - reset debounce
	receiver.mu.Lock()
	receiver.lastSync = time.Time{}
	receiver.mu.Unlock()

	mac := hmac.New(sha256.New, []byte(signingKey))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req = httptest.NewRequest(http.MethodPost, "/v1/webhook", bytes.NewBuffer(body))
	req.Header.Set("X-Webhook-Signature", sig)
	w = httptest.NewRecorder()
	receiver.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid signature, got %d", w.Code)
	}
}

func TestWebhookReceiver_Debounce(t *testing.T) {
	store := newTrackingStore()
	receiver := NewWebhookReceiver(config.WebhookReceiver{Enabled: true}, store, nil)

	body := `{"type":"token.created"}`

	// First request accepted
	req := httptest.NewRequest(http.MethodPost, "/v1/webhook", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	receiver.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != `{"status":"accepted"}` {
		t.Fatalf("first request should be accepted: %s", w.Body.String())
	}

	// Immediate second request debounced
	req = httptest.NewRequest(http.MethodPost, "/v1/webhook", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	receiver.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != `{"status":"debounced"}` {
		t.Fatalf("second request should be debounced: %s", w.Body.String())
	}
}

func TestWebhookReceiver_InvalidJSON(t *testing.T) {
	store := newTrackingStore()
	receiver := NewWebhookReceiver(config.WebhookReceiver{Enabled: true}, store, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/webhook", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()
	receiver.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestWebhookReceiver_CustomPath(t *testing.T) {
	receiver := NewWebhookReceiver(config.WebhookReceiver{
		Enabled: true,
		Path:    "/hooks/sync",
	}, newTrackingStore(), nil)

	if receiver.Path() != "/hooks/sync" {
		t.Fatalf("expected /hooks/sync, got %s", receiver.Path())
	}
}

func TestWebhookReceiver_DefaultPath(t *testing.T) {
	receiver := NewWebhookReceiver(config.WebhookReceiver{Enabled: true}, newTrackingStore(), nil)
	if receiver.Path() != "/v1/webhook" {
		t.Fatalf("expected /v1/webhook, got %s", receiver.Path())
	}
}

func TestWebhookReceiver_RemoteSyncOnEvent(t *testing.T) {
	// Set up a remote server with a token
	remoteStore := newMemStore()
	remoteStore.Create("hash-remote", `{"base_key_env":"OPENAI_API_KEY"}`, "")
	srv := NewServer(remoteStore, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Local store is empty
	localStore := newTrackingStore()
	client := NewClient(ts.URL, "", nil)

	receiver := NewWebhookReceiver(config.WebhookReceiver{Enabled: true}, localStore, client)

	body := `{"type":"token.created","id":"evt_789"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/webhook", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	receiver.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Wait for async sync
	time.Sleep(200 * time.Millisecond)

	// The remote sync should have created the token locally
	rec, err := localStore.Get("hash-remote")
	if err != nil {
		t.Fatal(err)
	}
	if rec == nil {
		t.Fatal("expected remote token to be synced to local store")
	}
}

func TestWebhookReceiver_AllTokenEventTypes(t *testing.T) {
	tokenEvents := []string{"token.created", "token.updated", "token.deleted", "token.expired"}
	for _, evt := range tokenEvents {
		if !isTokenEvent(evt) {
			t.Errorf("expected %q to be a token event", evt)
		}
	}

	nonTokenEvents := []string{"request.completed", "rule.violation", "budget.exceeded", "server.start"}
	for _, evt := range nonTokenEvents {
		if isTokenEvent(evt) {
			t.Errorf("expected %q to not be a token event", evt)
		}
	}
}

func TestVerifySignature(t *testing.T) {
	key := "test-key"
	body := []byte("test payload")

	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{"valid", validSig, true},
		{"wrong prefix", "md5=" + hex.EncodeToString(mac.Sum(nil)), false},
		{"empty", "", false},
		{"invalid hex", "sha256=not-hex", false},
		{"wrong key", "sha256=0000000000000000000000000000000000000000000000000000000000000000", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := verifySignature(body, tt.header, key)
			if got != tt.want {
				t.Errorf("verifySignature() = %v, want %v", got, tt.want)
			}
		})
	}
}
