// Webhook Collector Server
//
// A sample HTTP server that receives, verifies, and logs Tokenomics webhook events.
// Useful for development, debugging, and as a starting point for production integrations.
//
// Usage:
//
//	go run main.go                          # listen on :9090, no auth
//	go run main.go -addr :8888             # custom port
//	go run main.go -secret my-shared-key   # verify X-Webhook-Secret
//	go run main.go -signing-key hmac-key   # verify X-Webhook-Signature HMAC
//	go run main.go -secret s -signing-key k # both
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Event mirrors the Tokenomics event payload.
type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Timestamp string                 `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// Stats tracks event counts by type.
type Stats struct {
	mu     sync.Mutex
	counts map[string]int
	total  int
}

func (s *Stats) Record(eventType string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[eventType]++
	s.total++
}

func (s *Stats) JSON() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	data := map[string]interface{}{
		"total":  s.total,
		"counts": s.counts,
	}
	b, _ := json.MarshalIndent(data, "", "  ")
	return b
}

var (
	addr       = flag.String("addr", ":9090", "listen address")
	secret     = flag.String("secret", "", "expected X-Webhook-Secret value")
	signingKey = flag.String("signing-key", "", "HMAC-SHA256 signing key for X-Webhook-Signature verification")
	pretty     = flag.Bool("pretty", true, "pretty-print event JSON")
)

func main() {
	flag.Parse()

	stats := &Stats{counts: make(map[string]int)}

	log.SetFlags(0)
	log.SetOutput(os.Stdout)

	fmt.Println("╔══════════════════════════════════════════════════════╗")
	fmt.Println("║       Tokenomics Webhook Collector                  ║")
	fmt.Println("╚══════════════════════════════════════════════════════╝")
	fmt.Printf("  Listen:       %s\n", *addr)
	if *secret != "" {
		fmt.Printf("  Secret:       %s***\n", (*secret)[:min(3, len(*secret))])
	} else {
		fmt.Println("  Secret:       (none)")
	}
	if *signingKey != "" {
		fmt.Printf("  Signing Key:  %s***\n", (*signingKey)[:min(3, len(*signingKey))])
	} else {
		fmt.Println("  Signing Key:  (none)")
	}
	fmt.Printf("  Endpoints:    POST /webhook  GET /stats  GET /health\n")
	fmt.Println()

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", webhookHandler(stats))
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(stats.JSON())
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	log.Printf("Listening on %s ...\n\n", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func webhookHandler(stats *Stats) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Verify shared secret
		if *secret != "" {
			got := r.Header.Get("X-Webhook-Secret")
			if got != *secret {
				log.Printf("  [REJECTED] bad secret from %s\n", r.RemoteAddr)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		// Verify HMAC signature
		if *signingKey != "" {
			sig := r.Header.Get("X-Webhook-Signature")
			if !verifySignature(body, *signingKey, sig) {
				log.Printf("  [REJECTED] bad signature from %s\n", r.RemoteAddr)
				http.Error(w, "bad signature", http.StatusForbidden)
				return
			}
		}

		// Parse event
		var evt Event
		if err := json.Unmarshal(body, &evt); err != nil {
			log.Printf("  [ERROR] invalid JSON: %v\n", err)
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		stats.Record(evt.Type)

		// Log it
		ts := time.Now().Format("15:04:05.000")
		icon := eventIcon(evt.Type)

		fmt.Printf("─── %s %s %s ───────────────────────────\n", ts, icon, evt.Type)
		fmt.Printf("  ID:   %s\n", evt.ID)
		fmt.Printf("  Time: %s\n", evt.Timestamp)

		if len(evt.Data) > 0 {
			if *pretty {
				b, _ := json.MarshalIndent(evt.Data, "  ", "  ")
				fmt.Printf("  Data:\n  %s\n", string(b))
			} else {
				b, _ := json.Marshal(evt.Data)
				fmt.Printf("  Data: %s\n", string(b))
			}
		}
		fmt.Println()

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"received"}`))
	}
}

func verifySignature(body []byte, key, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sig := strings.TrimPrefix(signature, "sha256=")

	mac := hmac.New(sha256.New, []byte(key))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expected))
}

func eventIcon(eventType string) string {
	switch {
	case strings.HasPrefix(eventType, "token.created"):
		return "[+]"
	case strings.HasPrefix(eventType, "token.deleted"):
		return "[-]"
	case strings.HasPrefix(eventType, "token.updated"):
		return "[~]"
	case strings.HasPrefix(eventType, "token.expired"):
		return "[!]"
	case strings.HasPrefix(eventType, "rule.violation"):
		return "[X]"
	case strings.HasPrefix(eventType, "rule.warning"):
		return "[W]"
	case strings.HasPrefix(eventType, "rule.mask"):
		return "[M]"
	case strings.HasPrefix(eventType, "rule."):
		return "[R]"
	case strings.HasPrefix(eventType, "budget.exceeded"):
		return "[$]"
	case strings.HasPrefix(eventType, "budget."):
		return "[B]"
	case strings.HasPrefix(eventType, "rate."):
		return "[T]"
	case strings.HasPrefix(eventType, "request."):
		return "[>]"
	case strings.HasPrefix(eventType, "server."):
		return "[S]"
	default:
		return "[?]"
	}
}
