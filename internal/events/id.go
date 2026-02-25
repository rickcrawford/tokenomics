package events

import (
	"crypto/rand"
	"fmt"
	"time"
)

// generateID creates a unique event ID (evt_ prefix + 16 random hex bytes).
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("evt_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("evt_%x", b)
}
