package events

import (
	"crypto/rand"
	"fmt"
)

// generateID creates a unique event ID (evt_ prefix + 16 random hex bytes).
func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("evt_%x", b)
}
