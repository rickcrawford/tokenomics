package events

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// WebhookConfig configures a single webhook endpoint.
type WebhookConfig struct {
	URL        string   `mapstructure:"url" json:"url"`
	Secret     string   `mapstructure:"secret" json:"secret,omitempty"`           // Shared secret sent as X-Webhook-Secret header
	SigningKey string   `mapstructure:"signing_key" json:"signing_key,omitempty"` // HMAC-SHA256 signing key; signature sent as X-Webhook-Signature
	Events     []string `mapstructure:"events" json:"events,omitempty"`           // Event type filter (supports trailing * wildcard); empty = all
	TimeoutSec int      `mapstructure:"timeout" json:"timeout,omitempty"`         // HTTP timeout in seconds (default 10)
}

// WebhookEmitter delivers events to an HTTP endpoint.
type WebhookEmitter struct {
	cfg    WebhookConfig
	client *http.Client
	queue  chan Event
	done   chan struct{}
	wg     sync.WaitGroup
}

// NewWebhookEmitter creates a webhook emitter that sends events asynchronously.
// Events are buffered in a channel and delivered in a background goroutine.
func NewWebhookEmitter(cfg WebhookConfig) *WebhookEmitter {
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	w := &WebhookEmitter{
		cfg: cfg,
		client: &http.Client{
			Timeout: timeout,
		},
		queue: make(chan Event, 256),
		done:  make(chan struct{}),
	}

	w.wg.Add(1)
	go w.worker()

	return w
}

// Emit queues an event for async delivery. Non-blocking; drops if the buffer is full.
func (w *WebhookEmitter) Emit(_ context.Context, event Event) error {
	if !w.matchesFilter(event.Type) {
		return nil
	}

	select {
	case w.queue <- event:
	default:
		log.Printf("[events] webhook queue full, dropping event %s (%s)", event.ID, event.Type)
	}
	return nil
}

// Close drains the queue and shuts down the worker.
func (w *WebhookEmitter) Close() error {
	close(w.done)
	w.wg.Wait()
	return nil
}

// matchesFilter checks if the event type passes the configured filter.
// Supports trailing wildcard: "token.*" matches "token.created", "token.deleted", etc.
// An empty filter matches everything.
func (w *WebhookEmitter) matchesFilter(eventType string) bool {
	if len(w.cfg.Events) == 0 {
		return true
	}
	for _, pattern := range w.cfg.Events {
		if pattern == eventType {
			return true
		}
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(eventType, prefix) {
				return true
			}
		}
	}
	return false
}

// worker processes queued events, delivering them with retries.
func (w *WebhookEmitter) worker() {
	defer w.wg.Done()

	for {
		select {
		case event := <-w.queue:
			w.deliver(event)
		case <-w.done:
			// Drain remaining events
			for {
				select {
				case event := <-w.queue:
					w.deliver(event)
				default:
					return
				}
			}
		}
	}
}

// deliver sends a single event with up to 3 retries and exponential backoff.
func (w *WebhookEmitter) deliver(event Event) {
	body := event.JSON()

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<uint(attempt)) * time.Second) // 2s, 4s
		}

		req, err := http.NewRequest(http.MethodPost, w.cfg.URL, bytes.NewReader(body))
		if err != nil {
			log.Printf("[events] webhook request error: %v", err)
			return // don't retry bad URLs
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Tokenomics-Webhook/1.0")
		req.Header.Set("X-Event-ID", event.ID)
		req.Header.Set("X-Event-Type", event.Type)

		// Shared secret authentication
		if w.cfg.Secret != "" {
			req.Header.Set("X-Webhook-Secret", w.cfg.Secret)
		}

		// HMAC-SHA256 signature
		if w.cfg.SigningKey != "" {
			sig := sign(body, w.cfg.SigningKey)
			req.Header.Set("X-Webhook-Signature", fmt.Sprintf("sha256=%s", sig))
		}

		resp, err := w.client.Do(req)
		if err != nil {
			log.Printf("[events] webhook delivery failed (attempt %d): %v", attempt+1, err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return // success
		}

		log.Printf("[events] webhook returned %d (attempt %d) for event %s", resp.StatusCode, attempt+1, event.ID)

		// Don't retry 4xx (client errors) except 429
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			return
		}
	}

	log.Printf("[events] webhook delivery exhausted retries for event %s (%s)", event.ID, event.Type)
}

// sign computes the HMAC-SHA256 signature of the payload.
func sign(payload []byte, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
