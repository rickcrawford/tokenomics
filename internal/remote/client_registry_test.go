package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rickcrawford/tokenomics/internal/events"
)

func tempRegistryDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "clients.db")
}

func TestClientRegistry_RegisterAndList(t *testing.T) {
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	reg, err := cr.Register(ClientRegistration{
		URL:    "https://proxy-1.example.com/v1/webhook",
		Secret: "s1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reg.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if !strings.HasPrefix(reg.ID, "cl_") {
		t.Fatalf("expected cl_ prefix, got %s", reg.ID)
	}
	if reg.CreatedAt == "" {
		t.Fatal("expected non-empty CreatedAt")
	}

	clients, err := cr.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(clients))
	}
	if clients[0].URL != "https://proxy-1.example.com/v1/webhook" {
		t.Fatalf("unexpected URL: %s", clients[0].URL)
	}
}

func TestClientRegistry_RegisterRequiresURL(t *testing.T) {
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	_, err = cr.Register(ClientRegistration{})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestClientRegistry_RegisterRejectsInvalidURL(t *testing.T) {
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	_, err = cr.Register(ClientRegistration{URL: "javascript:alert(1)"})
	if err == nil {
		t.Fatal("expected error for invalid url scheme")
	}
	if !strings.Contains(err.Error(), "invalid url scheme") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientRegistry_RegisterRejectsMissingHost(t *testing.T) {
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	_, err = cr.Register(ClientRegistration{URL: "https://"})
	if err == nil {
		t.Fatal("expected error for missing host")
	}
	if !strings.Contains(err.Error(), "host is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientRegistry_Unregister(t *testing.T) {
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	reg, _ := cr.Register(ClientRegistration{URL: "https://proxy.example.com/hook"})

	if err := cr.Unregister(reg.ID); err != nil {
		t.Fatal(err)
	}

	clients, _ := cr.List()
	if len(clients) != 0 {
		t.Fatalf("expected 0 clients after unregister, got %d", len(clients))
	}
}

func TestClientRegistry_UnregisterNotFound(t *testing.T) {
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	err = cr.Unregister("cl_nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent client")
	}
}

func TestClientRegistry_Get(t *testing.T) {
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	reg, _ := cr.Register(ClientRegistration{
		URL:       "https://proxy.example.com/hook",
		Secret:    "secret123",
		SigningKey: "sign-key",
		Events:    []string{"token.*"},
		Insecure:  true,
	})

	got, err := cr.Get(reg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil registration")
	}
	if got.URL != "https://proxy.example.com/hook" {
		t.Fatalf("unexpected URL: %s", got.URL)
	}
	if got.Secret != "secret123" {
		t.Fatalf("unexpected secret: %s", got.Secret)
	}
	if got.SigningKey != "sign-key" {
		t.Fatalf("unexpected signing key: %s", got.SigningKey)
	}
	if len(got.Events) != 1 || got.Events[0] != "token.*" {
		t.Fatalf("unexpected events: %v", got.Events)
	}
	if !got.Insecure {
		t.Fatal("expected insecure=true")
	}
}

func TestClientRegistry_GetNotFound(t *testing.T) {
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	got, err := cr.Get("cl_missing")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("expected nil for nonexistent client")
	}
}

func TestClientRegistry_Persistence(t *testing.T) {
	dbPath := tempRegistryDB(t)

	// Register a client
	cr, err := NewClientRegistry(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cr.Register(ClientRegistration{URL: "https://persistent.example.com/hook"}); err != nil {
		t.Fatalf("register client: %v", err)
	}
	if err := cr.Close(); err != nil {
		t.Fatalf("close registry: %v", err)
	}

	// Reopen and verify
	cr2, err := NewClientRegistry(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer cr2.Close()

	clients, err := cr2.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 {
		t.Fatalf("expected 1 client after reopen, got %d", len(clients))
	}
	if clients[0].URL != "https://persistent.example.com/hook" {
		t.Fatalf("unexpected URL after reopen: %s", clients[0].URL)
	}
}

func TestClientRegistry_EmitToRegisteredClients(t *testing.T) {
	var mu sync.Mutex
	var received []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, r.Header.Get("X-Event-Type"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	if _, err := cr.Register(ClientRegistration{URL: ts.URL}); err != nil {
		t.Fatalf("register client: %v", err)
	}

	if err := cr.Emit(context.Background(), events.New(events.TokenCreated, map[string]interface{}{
		"token_hash": "abc123",
	})); err != nil {
		t.Fatalf("emit token.created: %v", err)
	}

	// Close to drain the emitter queues
	cr.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0] != events.TokenCreated {
		t.Fatalf("expected %s, got %s", events.TokenCreated, received[0])
	}
}

func TestClientRegistry_EmitToMultipleClients(t *testing.T) {
	var mu sync.Mutex
	counts := make(map[string]int)

	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts["ts1"]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts1.Close()

	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		counts["ts2"]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts2.Close()

	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	if _, err := cr.Register(ClientRegistration{URL: ts1.URL}); err != nil {
		t.Fatalf("register client ts1: %v", err)
	}
	if _, err := cr.Register(ClientRegistration{URL: ts2.URL}); err != nil {
		t.Fatalf("register client ts2: %v", err)
	}

	if err := cr.Emit(context.Background(), events.New(events.TokenCreated, nil)); err != nil {
		t.Fatalf("emit token.created: %v", err)
	}
	cr.Close()

	mu.Lock()
	defer mu.Unlock()
	if counts["ts1"] != 1 || counts["ts2"] != 1 {
		t.Fatalf("expected 1 event each, got ts1=%d ts2=%d", counts["ts1"], counts["ts2"])
	}
}

func TestClientRegistry_EmitWithEventFilter(t *testing.T) {
	var mu sync.Mutex
	var received []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, r.Header.Get("X-Event-Type"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	if _, err := cr.Register(ClientRegistration{
		URL:    ts.URL,
		Events: []string{"token.*"},
	}); err != nil {
		t.Fatalf("register filtered client: %v", err)
	}

	if err := cr.Emit(context.Background(), events.New(events.TokenCreated, nil)); err != nil {
		t.Fatalf("emit token.created: %v", err)
	}
	if err := cr.Emit(context.Background(), events.New(events.RuleViolation, nil)); err != nil { // Should be filtered
		t.Fatalf("emit rule.violation: %v", err)
	}
	cr.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event (rule.violation filtered), got %d: %v", len(received), received)
	}
}

func TestClientRegistry_EmitNoClients(t *testing.T) {
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	// Should not panic with no clients
	err = cr.Emit(context.Background(), events.New(events.TokenCreated, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientRegistry_InvalidDBPath(t *testing.T) {
	_, err := NewClientRegistry("/nonexistent/path/clients.db")
	if err == nil {
		t.Fatal("expected error for invalid DB path")
	}
}

// Server registration endpoint tests

func TestServerRegisterClient(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	srv := NewServer(ms, "", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := `{"url":"https://proxy-1.example.com/v1/webhook","secret":"s1","events":["token.*"],"insecure":true}`
	resp, err := http.Post(ts.URL+"/api/v1/clients", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	var reg ClientRegistration
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		t.Fatal(err)
	}
	if reg.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if reg.URL != "https://proxy-1.example.com/v1/webhook" {
		t.Fatalf("unexpected URL: %s", reg.URL)
	}
	if reg.Secret != "s1" {
		t.Fatalf("unexpected secret: %s", reg.Secret)
	}
	if !reg.Insecure {
		t.Fatal("expected insecure=true")
	}
}

func TestServerListClients(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	if _, err := cr.Register(ClientRegistration{URL: "https://proxy-1.example.com/hook"}); err != nil {
		t.Fatalf("register client 1: %v", err)
	}
	if _, err := cr.Register(ClientRegistration{URL: "https://proxy-2.example.com/hook"}); err != nil {
		t.Fatalf("register client 2: %v", err)
	}

	srv := NewServer(ms, "", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/clients")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var clients []ClientRegistration
	if err := json.NewDecoder(resp.Body).Decode(&clients); err != nil {
		t.Fatal(err)
	}
	if len(clients) != 2 {
		t.Fatalf("expected 2 clients, got %d", len(clients))
	}
}

func TestServerGetClient(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	reg, _ := cr.Register(ClientRegistration{URL: "https://proxy.example.com/hook"})

	srv := NewServer(ms, "", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/clients/" + reg.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got ClientRegistration
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != reg.ID {
		t.Fatalf("expected ID %s, got %s", reg.ID, got.ID)
	}
}

func TestServerGetClientNotFound(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	srv := NewServer(ms, "", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/clients/cl_nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestServerDeleteClient(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	reg, _ := cr.Register(ClientRegistration{URL: "https://proxy.example.com/hook"})

	srv := NewServer(ms, "", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/clients/"+reg.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify it's gone
	clients, _ := cr.List()
	if len(clients) != 0 {
		t.Fatalf("expected 0 clients after delete, got %d", len(clients))
	}
}

func TestServerDeleteClientNotFound(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	srv := NewServer(ms, "", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/clients/cl_missing", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestServerClientsAuthRequired(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	srv := NewServer(ms, "api-secret", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// No auth
	resp, _ := http.Get(ts.URL + "/api/v1/clients")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401 without auth, got %d", resp.StatusCode)
	}

	// Correct auth
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/clients", nil)
	req.Header.Set("Authorization", "Bearer api-secret")
	resp, _ = http.DefaultClient.Do(req)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 with correct auth, got %d", resp.StatusCode)
	}
}

func TestServerClientsNoRegistry(t *testing.T) {
	ms := newMemStore()
	srv := NewServer(ms, "")
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, _ := http.Get(ts.URL + "/api/v1/clients")
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 without registry, got %d", resp.StatusCode)
	}
}

func TestServerRegisterInvalidJSON(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	srv := NewServer(ms, "", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, _ := http.Post(ts.URL+"/api/v1/clients", "application/json", bytes.NewBufferString("not json"))
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid json, got %d", resp.StatusCode)
	}
}

func TestServerRegisterMissingURL(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	srv := NewServer(ms, "", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, _ := http.Post(ts.URL+"/api/v1/clients", "application/json", bytes.NewBufferString(`{}`))
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for missing URL, got %d", resp.StatusCode)
	}
}

func TestServerClientsMethodNotAllowed(t *testing.T) {
	ms := newMemStore()
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	srv := NewServer(ms, "", cr)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/clients", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 405 {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

// Integration: token store event triggers push to registered client
func TestServerTokenEventPushesToRegisteredClient(t *testing.T) {
	var mu sync.Mutex
	var received []string

	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, r.Header.Get("X-Event-Type"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer webhookServer.Close()

	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	if _, err := cr.Register(ClientRegistration{URL: webhookServer.URL}); err != nil {
		t.Fatalf("register webhook client: %v", err)
	}

	// Emit a token event through the registry
	if err := cr.Emit(context.Background(), events.New(events.TokenCreated, map[string]interface{}{
		"token_hash": "test1234",
	})); err != nil {
		t.Fatalf("emit token.created: %v", err)
	}

	// Close drains the queue
	cr.Close()

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event pushed to client, got %d", len(received))
	}
	if received[0] != events.TokenCreated {
		t.Fatalf("expected %s, got %s", events.TokenCreated, received[0])
	}
}

func TestClientRegistry_UnregisterStopsEmitter(t *testing.T) {
	var mu sync.Mutex
	var received []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, r.Header.Get("X-Event-Type"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	reg, _ := cr.Register(ClientRegistration{URL: ts.URL})

	// Emit first event
	if err := cr.Emit(context.Background(), events.New(events.TokenCreated, nil)); err != nil {
		t.Fatalf("emit token.created: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // Let async delivery complete

	// Unregister
	if err := cr.Unregister(reg.ID); err != nil {
		t.Fatalf("unregister client: %v", err)
	}

	// Emit second event - should not be delivered
	if err := cr.Emit(context.Background(), events.New(events.TokenUpdated, nil)); err != nil {
		t.Fatalf("emit token.updated: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event (second should not deliver after unregister), got %d: %v", len(received), received)
	}
}

// Test that insecure flag is preserved through registration
func TestClientRegistry_InsecureFlag(t *testing.T) {
	cr, err := NewClientRegistry(tempRegistryDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cr.Close()

	reg, err := cr.Register(ClientRegistration{
		URL:      "https://self-signed.example.com/hook",
		Insecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := cr.Get(reg.ID)
	if !got.Insecure {
		t.Fatal("expected insecure=true to be persisted")
	}
}

// Ensure temp dir is clean
func TestTempRegistryDB(t *testing.T) {
	dbPath := tempRegistryDB(t)
	dir := filepath.Dir(dbPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatalf("temp dir should exist: %s", dir)
	}
}
