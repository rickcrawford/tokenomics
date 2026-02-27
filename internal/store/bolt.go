package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/rickcrawford/tokenomics/internal/events"
	"github.com/rickcrawford/tokenomics/internal/policy"

	bolt "go.etcd.io/bbolt"
)

var tokensBucket = []byte("tokens")

type BoltStore struct {
	dbPath        string
	db            *bolt.DB
	encryptionKey []byte // nil = no encryption
	emitter       events.Emitter

	mu    sync.RWMutex
	cache map[string]*cacheEntry

	stopWatch chan struct{}
	watchWG   sync.WaitGroup
	closeOnce sync.Once
}

// SetEmitter configures the event emitter for token lifecycle events.
func (s *BoltStore) SetEmitter(e events.Emitter) {
	s.emitter = e
}

func (s *BoltStore) emit(eventType string, data map[string]interface{}) {
	if s.emitter != nil {
		if err := s.emitter.Emit(context.Background(), events.New(eventType, data)); err != nil {
			log.Printf("store emit error (%s): %v", eventType, err)
		}
	}
}

// cacheEntry holds a parsed policy and its expiration for fast lookup.
type cacheEntry struct {
	policy    *policy.Policy
	expiresAt time.Time // zero value = no expiration
}

// NewBoltStore creates a new BoltStore. If encryptionSecret is non-empty,
// policies are encrypted at rest using AES-256-GCM.
func NewBoltStore(dbPath string, encryptionSecret string) *BoltStore {
	var key []byte
	if encryptionSecret != "" {
		derived := deriveKey(encryptionSecret)
		key = derived
	}
	return &BoltStore{
		dbPath:        dbPath,
		encryptionKey: key,
		cache:         make(map[string]*cacheEntry),
		stopWatch:     make(chan struct{}),
	}
}

func (s *BoltStore) Init() error {
	db, err := bolt.Open(s.dbPath, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return fmt.Errorf("open bolt db: %w", err)
	}
	s.db = db

	// Create bucket
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(tokensBucket)
		return err
	})
	if err != nil {
		return fmt.Errorf("create bucket: %w", err)
	}

	return s.Reload()
}

type boltRecord struct {
	PolicyJSON string `json:"policy"`              // Plaintext JSON (when no encryption)
	Encrypted  string `json:"encrypted,omitempty"` // Base64 AES-256-GCM ciphertext (when encrypted)
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at,omitempty"`
}

// encryptPolicy encrypts the policy JSON if an encryption key is set.
func (s *BoltStore) encryptPolicy(policyJSON string) (plain, encrypted string, err error) {
	if s.encryptionKey == nil {
		return policyJSON, "", nil
	}
	enc, err := encrypt([]byte(policyJSON), s.encryptionKey)
	if err != nil {
		return "", "", fmt.Errorf("encrypt policy: %w", err)
	}
	return "", enc, nil
}

// decryptPolicy returns the plaintext policy JSON from a record.
func (s *BoltStore) decryptPolicy(rec *boltRecord) (string, error) {
	if rec.Encrypted != "" {
		if s.encryptionKey == nil {
			return "", fmt.Errorf("encrypted record but no encryption key configured")
		}
		plain, err := decrypt(rec.Encrypted, s.encryptionKey)
		if err != nil {
			return "", fmt.Errorf("decrypt policy: %w", err)
		}
		return string(plain), nil
	}
	return rec.PolicyJSON, nil
}

func (s *BoltStore) Create(tokenHash string, policyJSON string, expiresAt string) error {
	// Validate the policy JSON before storing
	if _, err := policy.Parse(policyJSON); err != nil {
		return err
	}

	// Validate expiration if provided
	if expiresAt != "" {
		if _, err := time.Parse(time.RFC3339, expiresAt); err != nil {
			return fmt.Errorf("invalid expires_at: %w", err)
		}
	}

	plain, encrypted, err := s.encryptPolicy(policyJSON)
	if err != nil {
		return err
	}

	rec := boltRecord{
		PolicyJSON: plain,
		Encrypted:  encrypted,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
		ExpiresAt:  expiresAt,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}

	err = s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(tokensBucket)
		if b.Get([]byte(tokenHash)) != nil {
			return fmt.Errorf("token hash already exists")
		}
		return b.Put([]byte(tokenHash), data)
	})
	if err != nil {
		return fmt.Errorf("create token: %w", err)
	}

	s.emit(events.TokenCreated, map[string]interface{}{
		"token_hash": keyPrefix([]byte(tokenHash)), "expires_at": expiresAt,
	})

	return s.Reload()
}

func (s *BoltStore) Get(tokenHash string) (*TokenRecord, error) {
	var record *TokenRecord

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(tokensBucket)
		v := b.Get([]byte(tokenHash))
		if v == nil {
			return nil
		}

		var rec boltRecord
		if err := json.Unmarshal(v, &rec); err != nil {
			return fmt.Errorf("unmarshal record: %w", err)
		}

		policyJSON, err := s.decryptPolicy(&rec)
		if err != nil {
			return err
		}

		p, err := policy.Parse(policyJSON)
		if err != nil {
			return fmt.Errorf("parse policy: %w", err)
		}

		record = &TokenRecord{
			TokenHash: tokenHash,
			PolicyRaw: policyJSON,
			Policy:    p,
			CreatedAt: rec.CreatedAt,
			ExpiresAt: rec.ExpiresAt,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return record, nil
}

func (s *BoltStore) Update(tokenHash string, policyJSON string, expiresAt string) error {
	// Validate the policy JSON if provided
	if policyJSON != "" {
		if _, err := policy.Parse(policyJSON); err != nil {
			return err
		}
	}

	// Validate expiration if provided
	if expiresAt != "" && expiresAt != "clear" {
		if _, err := time.Parse(time.RFC3339, expiresAt); err != nil {
			return fmt.Errorf("invalid expires_at: %w", err)
		}
	}

	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(tokensBucket)
		existing := b.Get([]byte(tokenHash))
		if existing == nil {
			return fmt.Errorf("token not found")
		}

		var rec boltRecord
		if err := json.Unmarshal(existing, &rec); err != nil {
			return fmt.Errorf("unmarshal existing record: %w", err)
		}

		// Update policy if provided
		if policyJSON != "" {
			plain, encrypted, err := s.encryptPolicy(policyJSON)
			if err != nil {
				return err
			}
			rec.PolicyJSON = plain
			rec.Encrypted = encrypted
		}

		// Update expiration
		if expiresAt == "clear" {
			rec.ExpiresAt = ""
		} else if expiresAt != "" {
			rec.ExpiresAt = expiresAt
		}

		data, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("marshal updated record: %w", err)
		}

		return b.Put([]byte(tokenHash), data)
	})
	if err != nil {
		return fmt.Errorf("update token: %w", err)
	}

	s.emit(events.TokenUpdated, map[string]interface{}{
		"token_hash": keyPrefix([]byte(tokenHash)), "expires_at": expiresAt,
	})

	return s.Reload()
}

func (s *BoltStore) Delete(tokenHash string) error {
	var found bool
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(tokensBucket)
		if b.Get([]byte(tokenHash)) == nil {
			return nil
		}
		found = true
		return b.Delete([]byte(tokenHash))
	})
	if err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	if !found {
		return fmt.Errorf("token not found")
	}

	s.emit(events.TokenDeleted, map[string]interface{}{
		"token_hash": keyPrefix([]byte(tokenHash)),
	})

	return s.Reload()
}

func (s *BoltStore) Lookup(tokenHash string) (*policy.Policy, error) {
	s.mu.RLock()
	entry, ok := s.cache[tokenHash]
	if !ok {
		s.mu.RUnlock()
		return nil, nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		expiredAt := entry.expiresAt
		s.mu.RUnlock()

		emitExpired := false
		s.mu.Lock()
		if latest, exists := s.cache[tokenHash]; exists && !latest.expiresAt.IsZero() && time.Now().After(latest.expiresAt) {
			delete(s.cache, tokenHash)
			emitExpired = true
		}
		s.mu.Unlock()

		if emitExpired {
			s.emit(events.TokenExpired, map[string]interface{}{
				"token_hash": keyPrefix([]byte(tokenHash)),
				"expired_at": expiredAt.Format(time.RFC3339),
			})
		}
		return nil, nil
	}
	s.mu.RUnlock()
	return entry.policy, nil
}

func (s *BoltStore) List() ([]TokenRecord, error) {
	var records []TokenRecord

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(tokensBucket)
		return b.ForEach(func(k, v []byte) error {
			var rec boltRecord
			if err := json.Unmarshal(v, &rec); err != nil {
				log.Printf("warning: invalid record for key %s: %v", keyPrefix(k), err)
				return nil
			}

			policyJSON, err := s.decryptPolicy(&rec)
			if err != nil {
				log.Printf("warning: cannot decrypt policy for token %s: %v", keyPrefix(k), err)
				return nil
			}

			p, err := policy.Parse(policyJSON)
			if err != nil {
				log.Printf("warning: invalid policy for token %s: %v", keyPrefix(k), err)
				return nil
			}

			records = append(records, TokenRecord{
				TokenHash: string(k),
				PolicyRaw: policyJSON,
				Policy:    p,
				CreatedAt: rec.CreatedAt,
				ExpiresAt: rec.ExpiresAt,
			})
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}

	return records, nil
}

func (s *BoltStore) Reload() error {
	newCache := make(map[string]*cacheEntry)

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(tokensBucket)
		return b.ForEach(func(k, v []byte) error {
			var rec boltRecord
			if err := json.Unmarshal(v, &rec); err != nil {
				log.Printf("warning: skipping key %s: %v", keyPrefix(k), err)
				return nil
			}

			policyJSON, err := s.decryptPolicy(&rec)
			if err != nil {
				log.Printf("warning: skipping token %s: %v", keyPrefix(k), err)
				return nil
			}

			p, err := policy.Parse(policyJSON)
			if err != nil {
				log.Printf("warning: skipping token %s: %v", keyPrefix(k), err)
				return nil
			}

			entry := &cacheEntry{policy: p}
			if rec.ExpiresAt != "" {
				t, err := time.Parse(time.RFC3339, rec.ExpiresAt)
				if err == nil {
					entry.expiresAt = t
				}
			}

			newCache[string(k)] = entry
			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("reload: %w", err)
	}

	s.mu.Lock()
	s.cache = newCache
	s.mu.Unlock()

	return nil
}

// keyPrefix returns a safe prefix of the key for log messages.
func keyPrefix(k []byte) string {
	if len(k) > 8 {
		return string(k[:8])
	}
	return string(k)
}

// StartFileWatch starts a goroutine that polls the DB file for changes and reloads.
func (s *BoltStore) StartFileWatch(interval time.Duration) {
	s.watchWG.Add(1)
	go func() {
		defer s.watchWG.Done()
		var lastMod time.Time
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-s.stopWatch:
				return
			case <-ticker.C:
				info, err := os.Stat(s.dbPath)
				if err != nil {
					continue
				}
				if info.ModTime().After(lastMod) {
					lastMod = info.ModTime()
					if err := s.Reload(); err != nil {
						log.Printf("file watch reload error: %v", err)
					}
				}
			}
		}
	}()
}

func (s *BoltStore) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		close(s.stopWatch)
		s.watchWG.Wait()
		if s.db != nil {
			closeErr = s.db.Close()
		}
	})
	return closeErr
}
