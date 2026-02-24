package store

import "github.com/rickcrawford/tokenomics/internal/policy"

// TokenRecord represents a stored token with its policy.
type TokenRecord struct {
	ID        int64
	TokenHash string
	Policy    *policy.Policy
	PolicyRaw string
	CreatedAt string
}

// TokenStore is the interface for token persistence and lookup.
type TokenStore interface {
	// Init initializes the store (create tables, load cache, etc.)
	Init() error

	// Create stores a new token hash → policy mapping.
	Create(tokenHash string, policyJSON string) error

	// Delete removes a token by its hash.
	Delete(tokenHash string) error

	// Lookup retrieves a policy by token hash. Returns nil if not found.
	Lookup(tokenHash string) (*policy.Policy, error)

	// List returns all token records.
	List() ([]TokenRecord, error)

	// Reload refreshes the in-memory cache from the database.
	Reload() error

	// Close cleans up resources.
	Close() error
}
