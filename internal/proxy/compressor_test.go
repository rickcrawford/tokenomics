package proxy

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestCompressionWriter_BrotliEncoding(t *testing.T) {
	// Create a simple handler that writes text
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, this is test content that should be compressed!"))
	})

	// Create request with brotli support
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "br, gzip")

	rr := httptest.NewRecorder()

	// Wrap handler with compression
	wrappedHandler := wrapWithCompression(handler)
	wrappedHandler.ServeHTTP(rr, req)

	// Check response
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// Check encoding header
	encoding := rr.Header().Get("Content-Encoding")
	if encoding != "br" {
		t.Fatalf("expected br encoding, got %q", encoding)
	}

	// Verify body is actually brotli compressed
	body := rr.Body.Bytes()
	br := brotli.NewReader(bytes.NewReader(body))
	decompressed, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("failed to decompress brotli: %v", err)
	}

	expected := "Hello, this is test content that should be compressed!"
	if string(decompressed) != expected {
		t.Errorf("decompressed content mismatch: expected %q, got %q", expected, string(decompressed))
	}
}

func TestCompressionWriter_GzipFallback(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Gzip test content"))
	})

	// Request with only gzip support (no brotli)
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate")

	rr := httptest.NewRecorder()
	wrappedHandler := wrapWithCompression(handler)
	wrappedHandler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	encoding := rr.Header().Get("Content-Encoding")
	if encoding != "gzip" {
		t.Fatalf("expected gzip encoding, got %q", encoding)
	}

	// Verify body is gzip compressed
	body := rr.Body.Bytes()
	gr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create gzip reader: %v", err)
	}
	decompressed, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("failed to decompress gzip: %v", err)
	}

	expected := "Gzip test content"
	if string(decompressed) != expected {
		t.Errorf("decompressed content mismatch: expected %q, got %q", expected, string(decompressed))
	}
}

func TestCompressionWriter_NoCompressionWhenNotAccepted(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Uncompressed content"))
	})

	// Request with no compression support
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "identity")

	rr := httptest.NewRecorder()
	wrappedHandler := wrapWithCompression(handler)
	wrappedHandler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	encoding := rr.Header().Get("Content-Encoding")
	if encoding != "" {
		t.Fatalf("expected no compression, got %q", encoding)
	}

	// Body should be uncompressed
	expected := "Uncompressed content"
	if rr.Body.String() != expected {
		t.Errorf("content mismatch: expected %q, got %q", expected, rr.Body.String())
	}
}

func TestCompressionWriter_WebSocketBypass(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("WebSocket response"))
	})

	// WebSocket upgrade request
	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Accept-Encoding", "br, gzip")

	rr := httptest.NewRecorder()
	wrappedHandler := wrapWithCompression(handler)
	wrappedHandler.ServeHTTP(rr, req)

	// Should not compress WebSocket upgrade requests
	encoding := rr.Header().Get("Content-Encoding")
	if encoding != "" {
		t.Fatalf("WebSocket upgrade should not be compressed, got %q", encoding)
	}

	// Body should be uncompressed
	if rr.Body.String() != "WebSocket response" {
		t.Errorf("expected uncompressed body for WebSocket upgrade")
	}
}

func TestCompressionWriter_VaryHeader(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test"))
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Accept-Encoding", "br")

	rr := httptest.NewRecorder()
	wrappedHandler := wrapWithCompression(handler)
	wrappedHandler.ServeHTTP(rr, req)

	vary := rr.Header().Get("Vary")
	if !strings.Contains(vary, "Accept-Encoding") {
		t.Errorf("expected Vary header to contain Accept-Encoding, got %q", vary)
	}
}

func TestShouldCompress(t *testing.T) {
	tests := []struct {
		name            string
		acceptEncoding  string
		isWebSocket     bool
		shouldCompress  bool
	}{
		{"brotli support", "br, gzip", false, true},
		{"gzip only", "gzip, deflate", false, true},
		{"no compression", "identity", false, false},
		{"empty accept-encoding", "", false, false},
		{"websocket upgrade", "br, gzip", true, false},
		{"websocket with gzip", "gzip", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.acceptEncoding != "" {
				req.Header.Set("Accept-Encoding", tt.acceptEncoding)
			}
			if tt.isWebSocket {
				req.Header.Set("Upgrade", "websocket")
				req.Header.Set("Connection", "Upgrade")
			}

			result := shouldCompress(req, tt.acceptEncoding)
			if result != tt.shouldCompress {
				t.Errorf("expected %v, got %v", tt.shouldCompress, result)
			}
		})
	}
}
