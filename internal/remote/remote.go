package remote

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rickcrawford/tokenomics/internal/store"
)

// Server is a lightweight HTTP server that serves tokens and config
// to remote proxy instances. It wraps a TokenStore and exposes
// read-only endpoints for syncing.
type Server struct {
	store  store.TokenStore
	apiKey string
	mux    *http.ServeMux
}

// NewServer creates a remote config server backed by the given token store.
// If apiKey is non-empty, requests must include it as a Bearer token.
func NewServer(tokenStore store.TokenStore, apiKey string) *Server {
	s := &Server{
		store:  tokenStore,
		apiKey: apiKey,
		mux:    http.NewServeMux(),
	}
	s.mux.HandleFunc("/api/v1/tokens", s.handleTokens)
	s.mux.HandleFunc("/api/v1/tokens/", s.handleTokenByHash)
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
	if auth == "" {
		http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
		return false
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	if token == auth || token != s.apiKey {
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
	json.NewEncoder(w).Encode(resp)
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
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// Client fetches tokens from a remote config server and syncs them
// into a local TokenStore.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client

	mu       sync.Mutex
	stopCh   chan struct{}
	stopped  bool
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

// HTTPError represents a non-200 response from the remote server.
type HTTPError struct {
	StatusCode int
}

func (e *HTTPError) Error() string {
	return "remote server returned " + http.StatusText(e.StatusCode)
}
