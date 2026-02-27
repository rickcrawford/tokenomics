package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/gzip"
)

// compressionWriter wraps an http.ResponseWriter to support response compression.
// It implements http.ResponseWriter, http.Flusher, and http.Hijacker.
type compressionWriter struct {
	http.ResponseWriter
	compressor io.WriteCloser
	encoding   string
	flushed    bool
}

// Write writes data to the response, compressing if enabled.
func (cw *compressionWriter) Write(b []byte) (int, error) {
	if cw.compressor != nil {
		return cw.compressor.Write(b)
	}
	return cw.ResponseWriter.Write(b)
}

// Close closes the compressor if present.
func (cw *compressionWriter) Close() error {
	if cw.compressor != nil {
		return cw.compressor.Close()
	}
	return nil
}

// Flush flushes the underlying writer.
func (cw *compressionWriter) Flush() error {
	if cw.compressor != nil {
		if f, ok := cw.compressor.(http.Flusher); ok {
			f.Flush()
		}
	}
	if f, ok := cw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
	cw.flushed = true
	return nil
}

// Hijack implements http.Hijacker.
func (cw *compressionWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	// Close compressor before hijacking
	cw.Close()
	if hijacker, ok := cw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}

// NewCompressionWriter creates a new compression wrapper based on Accept-Encoding header.
// It supports brotli (preferred) and gzip encoding.
func NewCompressionWriter(w http.ResponseWriter, r *http.Request) *compressionWriter {
	acceptEncoding := r.Header.Get("Accept-Encoding")
	cw := &compressionWriter{
		ResponseWriter: w,
		encoding:       "identity",
	}

	// Skip compression for small responses and certain content types
	if shouldCompress(r, acceptEncoding) {
		// Check for brotli support (preferred)
		if strings.Contains(acceptEncoding, "br") {
			cw.encoding = "br"
			cw.compressor = brotli.NewWriterLevel(w, brotli.DefaultCompression)
			w.Header().Set("Content-Encoding", "br")
			w.Header().Add("Vary", "Accept-Encoding")
			return cw
		}

		// Fall back to gzip
		if strings.Contains(acceptEncoding, "gzip") {
			cw.encoding = "gzip"
			gz, err := gzip.NewWriterLevel(w, gzip.DefaultCompression)
			if err == nil {
				cw.compressor = gz
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Add("Vary", "Accept-Encoding")
				return cw
			}
		}
	}

	// No compression
	w.Header().Add("Vary", "Accept-Encoding")
	return cw
}

// shouldCompress determines if response should be compressed.
func shouldCompress(r *http.Request, acceptEncoding string) bool {
	// Don't compress if Accept-Encoding doesn't include br or gzip
	if !strings.Contains(acceptEncoding, "br") && !strings.Contains(acceptEncoding, "gzip") {
		return false
	}

	// Don't compress WebSocket upgrades
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		return false
	}

	return true
}

// wrapWithCompression wraps an http.Handler to add compression support.
// It automatically handles brotli and gzip encoding based on Accept-Encoding header.
func wrapWithCompression(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cw := NewCompressionWriter(w, r)
		defer cw.Close()
		handler.ServeHTTP(cw, r)
	})
}
