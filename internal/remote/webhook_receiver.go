package remote

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rickcrawford/tokenomics/internal/config"
	"github.com/rickcrawford/tokenomics/internal/store"
)

// WebhookReceiver handles inbound webhook events from the central config
// server and triggers token sync on the local proxy. This provides push-based
// sync as an alternative (or complement) to polling.
type WebhookReceiver struct {
	cfg    config.WebhookReceiver
	store  store.TokenStore
	client *Client // optional, for full remote sync

	mu       sync.Mutex
	lastSync time.Time
}

// NewWebhookReceiver creates a handler that accepts inbound webhook events
// and triggers store reload or remote sync.
//
// If client is non-nil, receiving a token event triggers a full remote sync
// (fetching all tokens from the central server). If client is nil, the
// handler calls store.Reload() to refresh from the local database.
func NewWebhookReceiver(cfg config.WebhookReceiver, tokenStore store.TokenStore, client *Client) *WebhookReceiver {
	return &WebhookReceiver{
		cfg:    cfg,
		store:  tokenStore,
		client: client,
	}
}

// Path returns the URL path the receiver should be mounted on.
func (wr *WebhookReceiver) Path() string {
	if wr.cfg.Path != "" {
		return wr.cfg.Path
	}
	return "/v1/webhook"
}

// ServeHTTP handles inbound webhook POST requests.
func (wr *WebhookReceiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}

	// Authenticate: shared secret
	if wr.cfg.Secret != "" {
		if r.Header.Get("X-Webhook-Secret") != wr.cfg.Secret {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
	}

	// Authenticate: HMAC signature
	if wr.cfg.SigningKey != "" {
		sig := r.Header.Get("X-Webhook-Signature")
		if !verifySignature(body, sig, wr.cfg.SigningKey) {
			http.Error(w, `{"error":"invalid signature"}`, http.StatusForbidden)
			return
		}
	}

	// Parse the event to check if it is a token lifecycle event
	var event struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	// Only sync on token lifecycle events
	if !isTokenEvent(event.Type) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ignored","reason":"not a token event"}`))
		return
	}

	// Debounce: skip if we synced within the last second
	wr.mu.Lock()
	if time.Since(wr.lastSync) < time.Second {
		wr.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"debounced"}`))
		return
	}
	wr.lastSync = time.Now()
	wr.mu.Unlock()

	// Trigger sync asynchronously so we respond quickly
	go wr.sync(event.Type)

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"accepted"}`))
}

// sync triggers either a full remote sync or a local store reload.
func (wr *WebhookReceiver) sync(eventType string) {
	if wr.client != nil {
		n, err := wr.client.SyncTo(wr.store)
		if err != nil {
			log.Printf("[webhook-receiver] remote sync failed on %s: %v", eventType, err)
			return
		}
		if n > 0 {
			log.Printf("[webhook-receiver] synced %d token(s) on %s", n, eventType)
		}
	} else {
		if err := wr.store.Reload(); err != nil {
			log.Printf("[webhook-receiver] store reload failed on %s: %v", eventType, err)
		}
	}
}

// isTokenEvent returns true for token lifecycle event types.
func isTokenEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "token.")
}

// verifySignature checks the HMAC-SHA256 signature in "sha256=<hex>" format.
func verifySignature(body []byte, header, key string) bool {
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	received, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	expected := mac.Sum(nil)
	return hmac.Equal(received, expected)
}
