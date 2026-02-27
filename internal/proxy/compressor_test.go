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
		if _, err := w.Write([]byte("Hello, this is test content that should be compressed!")); err != nil {
			t.Fatalf("write response: %v", err)
		}
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
		if _, err := w.Write([]byte("Gzip test content")); err != nil {
			t.Fatalf("write response: %v", err)
		}
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
		if _, err := w.Write([]byte("Uncompressed content")); err != nil {
			t.Fatalf("write response: %v", err)
		}
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
		if _, err := w.Write([]byte("WebSocket response")); err != nil {
			t.Fatalf("write response: %v", err)
		}
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
		if _, err := w.Write([]byte("test")); err != nil {
			t.Fatalf("write response: %v", err)
		}
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

func TestCompressRequestBody_SmallBody(t *testing.T) {
	// Bodies smaller than 1 KB should not be compressed
	smallBody := []byte("small body")
	compressed, encoding, err := CompressRequestBody(smallBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if encoding != "" {
		t.Errorf("small body should not be compressed, got encoding %q", encoding)
	}
	if !bytes.Equal(compressed, smallBody) {
		t.Errorf("small body should be returned unchanged")
	}
}

func TestCompressRequestBody_BrotliCompression(t *testing.T) {
	// Create a body larger than 1 KB with repetitive content (compressible)
	body := make([]byte, 2000)
	for i := 0; i < len(body); i++ {
		body[i] = byte('a' + (i % 26))
	}

	compressed, encoding, err := CompressRequestBody(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if encoding != "br" && encoding != "gzip" {
		t.Fatalf("expected br or gzip encoding, got %q", encoding)
	}

	if len(compressed) >= len(body) {
		t.Errorf("compressed body should be smaller than original")
	}

	// Verify decompression works
	if encoding == "br" {
		br := brotli.NewReader(bytes.NewReader(compressed))
		decompressed, err := io.ReadAll(br)
		if err != nil {
			t.Fatalf("failed to decompress: %v", err)
		}
		if !bytes.Equal(decompressed, body) {
			t.Errorf("decompressed body does not match original")
		}
	}
}

func TestCompressRequestBody_GzipFallback(t *testing.T) {
	// Create a large body with less-repetitive content
	body := make([]byte, 3000)
	for i := 0; i < len(body); i++ {
		body[i] = byte((i % 256))
	}

	compressed, encoding, err := CompressRequestBody(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if encoding == "" {
		t.Errorf("expected compression encoding")
	}

	if len(compressed) >= len(body) {
		t.Logf("note: body not compressed (acceptable for less-compressible data)")
	}
}

func TestCompressRequestBody_IncompressibleData(t *testing.T) {
	// Create random data that doesn't compress well
	body := make([]byte, 2000)
	for i := 0; i < len(body); i++ {
		body[i] = byte(i % 256)
	}

	compressed, encoding, err := CompressRequestBody(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// For random data, compression might not be beneficial
	// The function should return original body with empty encoding
	if encoding == "" && !bytes.Equal(compressed, body) {
		t.Errorf("when compression not beneficial, should return original body")
	}
}

func TestCompressRequestBody_LargeJSON(t *testing.T) {
	// Simulate a large JSON request body
	jsonBody := []byte(`{
		"model": "gpt-4",
		"messages": [
			{"role": "user", "content": "` + strings.Repeat("Hello world, this is a test message. ", 50) + `"}
		]
	}`)

	compressed, encoding, err := CompressRequestBody(jsonBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(jsonBody) >= 1024 { // Only compress if >= 1KB
		if encoding == "" && len(compressed) >= len(jsonBody) {
			t.Logf("note: JSON body not compressed (acceptable)")
		} else if encoding != "" && len(compressed) >= len(jsonBody) {
			t.Errorf("compressed body should be smaller than original when compression used")
		}
	}
}
