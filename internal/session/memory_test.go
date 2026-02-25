package session

import (
	"sync"
	"testing"
)

func TestNewMemoryStore(t *testing.T) {
	m := NewMemoryStore()
	if m == nil {
		t.Fatal("expected non-nil MemoryStore")
	}
}

func TestGetUsage(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(m *MemoryStore)
		tokenHash string
		want      int64
	}{
		{
			name:      "non-existent token returns zero",
			setup:     func(m *MemoryStore) {},
			tokenHash: "unknown",
			want:      0,
		},
		{
			name: "existing token returns stored value",
			setup: func(m *MemoryStore) {
				m.AddUsage("token_a", 100)
			},
			tokenHash: "token_a",
			want:      100,
		},
		{
			name: "different tokens are independent",
			setup: func(m *MemoryStore) {
				m.AddUsage("token_x", 50)
				m.AddUsage("token_y", 200)
			},
			tokenHash: "token_x",
			want:      50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMemoryStore()
			tt.setup(m)

			got, err := m.GetUsage(tt.tokenHash)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("GetUsage(%q) = %d, want %d", tt.tokenHash, got, tt.want)
			}
		})
	}
}

func TestAddUsage(t *testing.T) {
	tests := []struct {
		name      string
		additions []int64
		wantFinal int64
	}{
		{
			name:      "single addition",
			additions: []int64{42},
			wantFinal: 42,
		},
		{
			name:      "multiple additions accumulate",
			additions: []int64{10, 20, 30},
			wantFinal: 60,
		},
		{
			name:      "zero addition",
			additions: []int64{0},
			wantFinal: 0,
		},
		{
			name:      "large values",
			additions: []int64{1000000, 2000000},
			wantFinal: 3000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMemoryStore()
			var total int64
			for _, count := range tt.additions {
				var err error
				total, err = m.AddUsage("test_token", count)
				if err != nil {
					t.Fatalf("AddUsage error: %v", err)
				}
			}
			if total != tt.wantFinal {
				t.Fatalf("AddUsage returned %d, want %d", total, tt.wantFinal)
			}

			got, err := m.GetUsage("test_token")
			if err != nil {
				t.Fatalf("GetUsage error: %v", err)
			}
			if got != tt.wantFinal {
				t.Fatalf("GetUsage = %d, want %d", got, tt.wantFinal)
			}
		})
	}
}

func TestAddUsage_ReturnsRunningTotal(t *testing.T) {
	m := NewMemoryStore()

	got1, _ := m.AddUsage("tok", 10)
	if got1 != 10 {
		t.Fatalf("first AddUsage returned %d, want 10", got1)
	}

	got2, _ := m.AddUsage("tok", 25)
	if got2 != 35 {
		t.Fatalf("second AddUsage returned %d, want 35", got2)
	}

	got3, _ := m.AddUsage("tok", 5)
	if got3 != 40 {
		t.Fatalf("third AddUsage returned %d, want 40", got3)
	}
}

func TestReset(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(m *MemoryStore)
		resetKey  string
		checkKey  string
		wantAfter int64
	}{
		{
			name: "reset existing token clears usage",
			setup: func(m *MemoryStore) {
				m.AddUsage("to_reset", 500)
			},
			resetKey:  "to_reset",
			checkKey:  "to_reset",
			wantAfter: 0,
		},
		{
			name:      "reset non-existent token is no-op",
			setup:     func(m *MemoryStore) {},
			resetKey:  "nonexistent",
			checkKey:  "nonexistent",
			wantAfter: 0,
		},
		{
			name: "reset one token does not affect others",
			setup: func(m *MemoryStore) {
				m.AddUsage("keep", 100)
				m.AddUsage("remove", 200)
			},
			resetKey:  "remove",
			checkKey:  "keep",
			wantAfter: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMemoryStore()
			tt.setup(m)

			err := m.Reset(tt.resetKey)
			if err != nil {
				t.Fatalf("Reset error: %v", err)
			}

			got, err := m.GetUsage(tt.checkKey)
			if err != nil {
				t.Fatalf("GetUsage error: %v", err)
			}
			if got != tt.wantAfter {
				t.Fatalf("GetUsage(%q) after Reset = %d, want %d", tt.checkKey, got, tt.wantAfter)
			}
		})
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := NewMemoryStore()
	const goroutines = 100
	const addPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < addPerGoroutine; j++ {
				if _, err := m.AddUsage("concurrent_token", 1); err != nil {
					t.Errorf("AddUsage error: %v", err)
				}
			}
		}()
	}

	wg.Wait()

	got, err := m.GetUsage("concurrent_token")
	if err != nil {
		t.Fatalf("GetUsage error: %v", err)
	}

	want := int64(goroutines * addPerGoroutine)
	if got != want {
		t.Fatalf("GetUsage after concurrent adds = %d, want %d", got, want)
	}
}

func TestConcurrentMixedOperations(t *testing.T) {
	m := NewMemoryStore()
	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 3) // add, get, reset goroutines

	// Concurrent adds
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			m.AddUsage("mixed_token", 1)
		}()
	}

	// Concurrent gets
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			m.GetUsage("mixed_token")
		}()
	}

	// Concurrent resets (on a different key to avoid total interference)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			m.Reset("other_token")
		}()
	}

	// Should complete without panics or race conditions
	wg.Wait()
}
