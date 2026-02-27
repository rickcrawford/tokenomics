package remote

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/rickcrawford/tokenomics/internal/events"

	bolt "go.etcd.io/bbolt"
)

var clientsBucket = []byte("webhook_clients")

// ClientRegistration represents a registered webhook client that receives
// push-based configuration updates from the central server.
type ClientRegistration struct {
	ID        string   `json:"id"`
	URL       string   `json:"url"`                    // Client's webhook endpoint URL
	Secret    string   `json:"secret,omitempty"`        // Shared secret for X-Webhook-Secret header
	SigningKey string  `json:"signing_key,omitempty"`   // HMAC-SHA256 key for X-Webhook-Signature header
	Events    []string `json:"events,omitempty"`        // Event type filter (supports trailing * wildcard); empty = all
	Insecure  bool     `json:"insecure,omitempty"`      // Skip TLS certificate verification (for self-signed certs)
	CreatedAt string   `json:"created_at"`
}

// ClientRegistry manages webhook client registrations in BoltDB and delivers
// events to all registered clients. It implements the events.Emitter interface.
type ClientRegistry struct {
	db *bolt.DB

	mu       sync.RWMutex
	emitters map[string]*events.WebhookEmitter // id -> active emitter
}

// NewClientRegistry opens a BoltDB file for client registrations and
// initializes emitters for all existing registrations.
func NewClientRegistry(dbPath string) (*ClientRegistry, error) {
	db, err := bolt.Open(dbPath, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open client registry db: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(clientsBucket)
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create clients bucket: %w", err)
	}

	cr := &ClientRegistry{
		db:       db,
		emitters: make(map[string]*events.WebhookEmitter),
	}

	// Load existing registrations and start emitters
	clients, err := cr.List()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("load existing clients: %w", err)
	}
	for _, c := range clients {
		cr.emitters[c.ID] = events.NewWebhookEmitter(registrationToConfig(c))
	}

	return cr, nil
}

// Register adds a new webhook client and starts its emitter.
func (cr *ClientRegistry) Register(reg ClientRegistration) (*ClientRegistration, error) {
	if reg.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if err := validateWebhookURL(reg.URL); err != nil {
		return nil, err
	}

	reg.ID = generateClientID()
	reg.CreatedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := json.Marshal(reg)
	if err != nil {
		return nil, fmt.Errorf("marshal registration: %w", err)
	}

	err = cr.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(clientsBucket)
		return b.Put([]byte(reg.ID), data)
	})
	if err != nil {
		return nil, fmt.Errorf("store registration: %w", err)
	}

	cr.mu.Lock()
	cr.emitters[reg.ID] = events.NewWebhookEmitter(registrationToConfig(reg))
	cr.mu.Unlock()

	log.Printf("[client-registry] registered client %s -> %s", reg.ID, reg.URL)
	return &reg, nil
}

// Unregister removes a webhook client and stops its emitter.
func (cr *ClientRegistry) Unregister(id string) error {
	err := cr.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(clientsBucket)
		if b.Get([]byte(id)) == nil {
			return fmt.Errorf("client not found")
		}
		return b.Delete([]byte(id))
	})
	if err != nil {
		return fmt.Errorf("delete registration: %w", err)
	}

	cr.mu.Lock()
	if em, ok := cr.emitters[id]; ok {
		em.Close()
		delete(cr.emitters, id)
	}
	cr.mu.Unlock()

	log.Printf("[client-registry] unregistered client %s", id)
	return nil
}

// Get retrieves a single client registration by ID.
func (cr *ClientRegistry) Get(id string) (*ClientRegistration, error) {
	var reg *ClientRegistration
	err := cr.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(clientsBucket)
		v := b.Get([]byte(id))
		if v == nil {
			return nil
		}
		reg = &ClientRegistration{}
		return json.Unmarshal(v, reg)
	})
	if err != nil {
		return nil, err
	}
	return reg, nil
}

// List returns all registered webhook clients.
func (cr *ClientRegistry) List() ([]ClientRegistration, error) {
	var clients []ClientRegistration
	err := cr.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(clientsBucket)
		return b.ForEach(func(k, v []byte) error {
			var reg ClientRegistration
			if err := json.Unmarshal(v, &reg); err != nil {
				log.Printf("[client-registry] warning: invalid record for key %s: %v", string(k), err)
				return nil
			}
			clients = append(clients, reg)
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list clients: %w", err)
	}
	return clients, nil
}

// Emit delivers an event to all registered webhook clients.
// Implements the events.Emitter interface.
func (cr *ClientRegistry) Emit(ctx context.Context, event events.Event) error {
	cr.mu.RLock()
	defer cr.mu.RUnlock()

	var firstErr error
	for _, em := range cr.emitters {
		if err := em.Emit(ctx, event); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Close stops all emitters and closes the BoltDB.
// Implements the events.Emitter interface.
func (cr *ClientRegistry) Close() error {
	cr.mu.Lock()
	for id, em := range cr.emitters {
		em.Close()
		delete(cr.emitters, id)
	}
	cr.mu.Unlock()

	if cr.db != nil {
		return cr.db.Close()
	}
	return nil
}

// registrationToConfig converts a ClientRegistration to a WebhookConfig
// for creating a WebhookEmitter.
func registrationToConfig(reg ClientRegistration) events.WebhookConfig {
	return events.WebhookConfig{
		URL:        reg.URL,
		Secret:     reg.Secret,
		SigningKey:  reg.SigningKey,
		Events:     reg.Events,
		Insecure:   reg.Insecure,
		TimeoutSec: 10,
	}
}

// generateClientID creates a random client identifier.
func generateClientID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fall back to timestamp-based entropy if random source fails.
		return fmt.Sprintf("cl_%d", time.Now().UnixNano())
	}
	return "cl_" + hex.EncodeToString(b)
}

func validateWebhookURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid url scheme: %s", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("invalid url: host is required")
	}
	return nil
}
