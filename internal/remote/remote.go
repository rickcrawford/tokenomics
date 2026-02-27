package remote

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rickcrawford/tokenomics/internal/store"
)

// Server is a lightweight HTTP server that serves tokens and config
// to remote proxy instances. It wraps a TokenStore and exposes
// read-only endpoints for syncing. Optionally manages webhook client
// registrations via a ClientRegistry.
type Server struct {
	store    store.TokenStore
	registry *ClientRegistry
	apiKey   string
	mux      *http.ServeMux
}

// NewServer creates a remote config server backed by the given token store.
// If apiKey is non-empty, requests must include it as a Bearer token.
// If registry is non-nil, client registration endpoints are enabled.
func NewServer(tokenStore store.TokenStore, apiKey string, registry ...*ClientRegistry) *Server {
	s := &Server{
		store:  tokenStore,
		apiKey: apiKey,
		mux:    http.NewServeMux(),
	}
	if len(registry) > 0 && registry[0] != nil {
		s.registry = registry[0]
	}
	s.mux.HandleFunc("/api/v1/tokens", s.handleTokens)
	s.mux.HandleFunc("/api/v1/tokens/", s.handleTokenByHash)
	s.mux.HandleFunc("/api/v1/clients", s.handleClients)
	s.mux.HandleFunc("/api/v1/clients/", s.handleClientByID)
	s.mux.HandleFunc("/health", s.handleHealth)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) bool {
	if s.apiKey == "" {
		return true
	}
	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
		return false
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.apiKey)) != 1 {
		http.Error(w, `{"error":"invalid api key"}`, http.StatusForbidden)
		return false
	}
	return true
}

// TokenResponse is the JSON shape for a single token in the API.
type TokenResponse struct {
	TokenHash string `json:"token_hash"`
	Policy    string `json:"policy"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if !s.authenticate(w, r) {
		return
	}

	records, err := s.store.List()
	if err != nil {
		http.Error(w, `{"error":"list tokens: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	resp := make([]TokenResponse, 0, len(records))
	for _, rec := range records {
		resp = append(resp, TokenResponse{
			TokenHash: rec.TokenHash,
			Policy:    rec.PolicyRaw,
			CreatedAt: rec.CreatedAt,
			ExpiresAt: rec.ExpiresAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("encode tokens response: %v", err)
	}
}

func (s *Server) handleTokenByHash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if !s.authenticate(w, r) {
		return
	}

	hash := strings.TrimPrefix(r.URL.Path, "/api/v1/tokens/")
	if hash == "" {
		http.Error(w, `{"error":"missing token hash"}`, http.StatusBadRequest)
		return
	}

	rec, err := s.store.Get(hash)
	if err != nil {
		http.Error(w, `{"error":"get token: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	if rec == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	resp := TokenResponse{
		TokenHash: rec.TokenHash,
		Policy:    rec.PolicyRaw,
		CreatedAt: rec.CreatedAt,
		ExpiresAt: rec.ExpiresAt,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("encode token response: %v", err)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
		log.Printf("health write error: %v", err)
	}
}

// handleClients handles GET (list) and POST (register) for webhook clients.
func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	if !s.authenticate(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.listClients(w, r)
	case http.MethodPost:
		s.registerClient(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleClientByID handles GET (get) and DELETE (unregister) for a specific client.
func (s *Server) handleClientByID(w http.ResponseWriter, r *http.Request) {
	if !s.authenticate(w, r) {
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/clients/")
	if id == "" {
		http.Error(w, `{"error":"missing client id"}`, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getClient(w, id)
	case http.MethodDelete:
		s.deleteClient(w, id)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) listClients(w http.ResponseWriter, _ *http.Request) {
	if s.registry == nil {
		http.Error(w, `{"error":"client registration not enabled"}`, http.StatusNotFound)
		return
	}

	clients, err := s.registry.List()
	if err != nil {
		http.Error(w, `{"error":"list clients: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	if clients == nil {
		clients = []ClientRegistration{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(clients); err != nil {
		log.Printf("encode clients response: %v", err)
	}
}

func (s *Server) registerClient(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		http.Error(w, `{"error":"client registration not enabled"}`, http.StatusNotFound)
		return
	}

	var req ClientRegistration
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	reg, err := s.registry.Register(req)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(reg); err != nil {
		log.Printf("encode register client response: %v", err)
	}
}

func (s *Server) getClient(w http.ResponseWriter, id string) {
	if s.registry == nil {
		http.Error(w, `{"error":"client registration not enabled"}`, http.StatusNotFound)
		return
	}

	client, err := s.registry.Get(id)
	if err != nil {
		http.Error(w, `{"error":"get client: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	if client == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(client); err != nil {
		log.Printf("encode client response: %v", err)
	}
}

func (s *Server) deleteClient(w http.ResponseWriter, id string) {
	if s.registry == nil {
		http.Error(w, `{"error":"client registration not enabled"}`, http.StatusNotFound)
		return
	}

	if err := s.registry.Unregister(id); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"status":"deleted"}`)); err != nil {
		log.Printf("delete client write error: %v", err)
	}
}

// Client fetches tokens from a remote config server and syncs them
// into a local TokenStore.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client

	mu      sync.Mutex
	stopCh  chan struct{}
	stopped bool
}

// NewClient creates a remote config client.
func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: httpClient,
		stopCh:     make(chan struct{}),
	}
}

// FetchTokens retrieves all tokens from the remote server.
func (c *Client) FetchTokens() ([]TokenResponse, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/v1/tokens", nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{StatusCode: resp.StatusCode}
	}

	var tokens []TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, err
	}
	return tokens, nil
}

// SyncTo fetches tokens from the remote server and writes them into the
// local store. Existing tokens with the same hash are updated. Tokens
// present locally but missing from the remote are left untouched (additive sync).
// Returns the number of tokens synced.
func (c *Client) SyncTo(localStore store.TokenStore) (int, error) {
	tokens, err := c.FetchTokens()
	if err != nil {
		return 0, err
	}

	synced := 0
	for _, t := range tokens {
		existing, err := localStore.Get(t.TokenHash)
		if err != nil {
			log.Printf("remote sync: error checking token %s: %v", t.TokenHash[:8], err)
			continue
		}

		if existing == nil {
			if err := localStore.Create(t.TokenHash, t.Policy, t.ExpiresAt); err != nil {
				log.Printf("remote sync: error creating token %s: %v", t.TokenHash[:8], err)
				continue
			}
			synced++
		} else if existing.PolicyRaw != t.Policy || existing.ExpiresAt != t.ExpiresAt {
			if err := localStore.Update(t.TokenHash, t.Policy, t.ExpiresAt); err != nil {
				log.Printf("remote sync: error updating token %s: %v", t.TokenHash[:8], err)
				continue
			}
			synced++
		}
	}

	return synced, nil
}

// StartPeriodicSync runs SyncTo on the given interval in a background goroutine.
// Call Stop() to cancel.
func (c *Client) StartPeriodicSync(localStore store.TokenStore, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-c.stopCh:
				return
			case <-ticker.C:
				n, err := c.SyncTo(localStore)
				if err != nil {
					log.Printf("remote sync error: %v", err)
					continue
				}
				if n > 0 {
					log.Printf("remote sync: %d token(s) synced", n)
				}
			}
		}
	}()
}

// Stop cancels periodic sync.
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.stopped {
		close(c.stopCh)
		c.stopped = true
	}
}

// RegisterWebhook registers this client's webhook endpoint with the central
// config server so the server will push token events to this proxy.
// Returns the registration ID on success.
func (c *Client) RegisterWebhook(reg ClientRegistration) (string, error) {
	body, err := json.Marshal(reg)
	if err != nil {
		return "", fmt.Errorf("marshal registration: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/v1/clients", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", &HTTPError{StatusCode: resp.StatusCode}
	}

	var result ClientRegistration
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return result.ID, nil
}

// UnregisterWebhook removes a previously registered webhook client from
// the central config server.
func (c *Client) UnregisterWebhook(clientID string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/api/v1/clients/"+clientID, nil)
	if err != nil {
		return err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &HTTPError{StatusCode: resp.StatusCode}
	}
	return nil
}

// HTTPError represents a non-200 response from the remote server.
type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return "remote server returned " + http.StatusText(e.StatusCode)
}
