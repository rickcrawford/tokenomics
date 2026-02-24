package session

import "sync"

type MemoryStore struct {
	mu    sync.RWMutex
	usage map[string]int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		usage: make(map[string]int64),
	}
}

func (m *MemoryStore) GetUsage(tokenHash string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.usage[tokenHash], nil
}

func (m *MemoryStore) AddUsage(tokenHash string, count int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage[tokenHash] += count
	return m.usage[tokenHash], nil
}

func (m *MemoryStore) Reset(tokenHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.usage, tokenHash)
	return nil
}
