package store

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/rickcrawford/tokenomics/internal/policy"

	bolt "go.etcd.io/bbolt"
)

var tokensBucket = []byte("tokens")

type BoltStore struct {
	dbPath string
	db     *bolt.DB

	mu    sync.RWMutex
	cache map[string]*policy.Policy

	stopWatch chan struct{}
}

func NewBoltStore(dbPath string) *BoltStore {
	return &BoltStore{
		dbPath:    dbPath,
		cache:     make(map[string]*policy.Policy),
		stopWatch: make(chan struct{}),
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
	PolicyJSON string `json:"policy"`
	CreatedAt  string `json:"created_at"`
}

func (s *BoltStore) Create(tokenHash string, policyJSON string) error {
	// Validate the policy JSON before storing
	if _, err := policy.Parse(policyJSON); err != nil {
		return err
	}

	rec := boltRecord{
		PolicyJSON: policyJSON,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
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
	return s.Reload()
}

func (s *BoltStore) Lookup(tokenHash string) (*policy.Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.cache[tokenHash]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (s *BoltStore) List() ([]TokenRecord, error) {
	var records []TokenRecord

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(tokensBucket)
		return b.ForEach(func(k, v []byte) error {
			var rec boltRecord
			if err := json.Unmarshal(v, &rec); err != nil {
				log.Printf("warning: invalid record for key %s: %v", string(k[:8]), err)
				return nil
			}

			p, err := policy.Parse(rec.PolicyJSON)
			if err != nil {
				log.Printf("warning: invalid policy for token %s: %v", string(k[:8]), err)
				return nil
			}

			records = append(records, TokenRecord{
				TokenHash: string(k),
				PolicyRaw: rec.PolicyJSON,
				Policy:    p,
				CreatedAt: rec.CreatedAt,
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
	newCache := make(map[string]*policy.Policy)

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(tokensBucket)
		return b.ForEach(func(k, v []byte) error {
			var rec boltRecord
			if err := json.Unmarshal(v, &rec); err != nil {
				log.Printf("warning: skipping key %s: %v", string(k[:8]), err)
				return nil
			}
			p, err := policy.Parse(rec.PolicyJSON)
			if err != nil {
				log.Printf("warning: skipping token %s: %v", string(k[:8]), err)
				return nil
			}
			newCache[string(k)] = p
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

// StartFileWatch starts a goroutine that polls the DB file for changes and reloads.
func (s *BoltStore) StartFileWatch(interval time.Duration) {
	go func() {
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
	close(s.stopWatch)
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
