package cmd

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/rickcrawford/tokenomics/internal/config"
)

func TestDetectRunningLocalProxy_HTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, port := hostPortFromURL(t, srv.URL)
	cfg := &config.Config{
		Server: config.ServerConfig{
			HTTPPort: port,
			TLS:      config.TLSConfig{Enabled: false},
		},
	}

	gotURL, ok := detectRunningLocalProxy(cfg)
	if !ok {
		t.Fatal("expected running local HTTP proxy to be detected")
	}
	wantURL := "http://localhost:" + strconv.Itoa(port)
	if gotURL != wantURL {
		t.Fatalf("detected URL = %q, want %q", gotURL, wantURL)
	}
}

func TestDetectRunningLocalProxy_HTTPS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, port := hostPortFromURL(t, srv.URL)
	cfg := &config.Config{
		Server: config.ServerConfig{
			HTTPSPort: port,
			TLS:       config.TLSConfig{Enabled: true},
		},
	}

	gotURL, ok := detectRunningLocalProxy(cfg)
	if !ok {
		t.Fatal("expected running local HTTPS proxy to be detected")
	}
	wantURL := "https://localhost:" + strconv.Itoa(port)
	if gotURL != wantURL {
		t.Fatalf("detected URL = %q, want %q", gotURL, wantURL)
	}
}

func TestDetectRunningLocalProxy_None(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			HTTPPort:  39999,
			HTTPSPort: 39998,
			TLS:       config.TLSConfig{Enabled: true},
		},
	}
	gotURL, ok := detectRunningLocalProxy(cfg)
	if ok || gotURL != "" {
		t.Fatalf("expected no running proxy, got (%q, %v)", gotURL, ok)
	}
}

func hostPortFromURL(t *testing.T, raw string) (string, int) {
	t.Helper()
	// raw is always of form scheme://host:port for httptest servers.
	hostPort := raw[stringsLastIndex(raw, "://")+3:]
	host, portRaw, err := net.SplitHostPort(hostPort)
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return host, port
}

func stringsLastIndex(s, sep string) int {
	last := -1
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			last = i
		}
	}
	return last
}
