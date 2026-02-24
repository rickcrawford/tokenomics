package tls

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCerts_GeneratesNewCerts(t *testing.T) {
	certDir := t.TempDir()

	paths, err := EnsureCerts(certDir)
	if err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}

	// Verify all paths are set
	if paths.CACert == "" || paths.CAKey == "" || paths.ServerCert == "" || paths.ServerKey == "" {
		t.Fatalf("expected all paths to be set, got %+v", paths)
	}

	// Verify all files exist
	for _, p := range []string{paths.CACert, paths.CAKey, paths.ServerCert, paths.ServerKey} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file %s to exist: %v", p, err)
		}
	}

	// Verify paths are in the cert directory
	for _, p := range []string{paths.CACert, paths.CAKey, paths.ServerCert, paths.ServerKey} {
		if dir := filepath.Dir(p); dir != certDir {
			t.Errorf("path %s not in cert dir %s", p, certDir)
		}
	}
}

func TestEnsureCerts_CACertProperties(t *testing.T) {
	certDir := t.TempDir()

	paths, err := EnsureCerts(certDir)
	if err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}

	cert := loadCert(t, paths.CACert)

	if !cert.IsCA {
		t.Error("CA cert should have IsCA=true")
	}
	if cert.BasicConstraintsValid != true {
		t.Error("CA cert should have BasicConstraintsValid=true")
	}
	if cert.Subject.CommonName != "Tokenomics Root CA" {
		t.Errorf("CA CommonName = %q, want %q", cert.Subject.CommonName, "Tokenomics Root CA")
	}
	if len(cert.Subject.Organization) == 0 || cert.Subject.Organization[0] != "Tokenomics CA" {
		t.Errorf("CA Organization = %v, want [Tokenomics CA]", cert.Subject.Organization)
	}
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("CA cert should have KeyUsageCertSign")
	}
	if cert.KeyUsage&x509.KeyUsageCRLSign == 0 {
		t.Error("CA cert should have KeyUsageCRLSign")
	}
}

func TestEnsureCerts_ServerCertProperties(t *testing.T) {
	certDir := t.TempDir()

	paths, err := EnsureCerts(certDir)
	if err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}

	cert := loadCert(t, paths.ServerCert)

	if cert.IsCA {
		t.Error("server cert should not be a CA")
	}
	if cert.Subject.CommonName != "localhost" {
		t.Errorf("server CommonName = %q, want %q", cert.Subject.CommonName, "localhost")
	}

	// Check DNS names
	foundLocalhost := false
	for _, dns := range cert.DNSNames {
		if dns == "localhost" {
			foundLocalhost = true
		}
	}
	if !foundLocalhost {
		t.Errorf("server cert DNSNames = %v, expected to contain 'localhost'", cert.DNSNames)
	}

	// Check IP SANs
	foundIPv4 := false
	foundIPv6 := false
	for _, ip := range cert.IPAddresses {
		if ip.Equal(net.ParseIP("127.0.0.1")) {
			foundIPv4 = true
		}
		if ip.Equal(net.ParseIP("::1")) {
			foundIPv6 = true
		}
	}
	if !foundIPv4 {
		t.Error("server cert should include 127.0.0.1 in IP SANs")
	}
	if !foundIPv6 {
		t.Error("server cert should include ::1 in IP SANs")
	}

	// Check key usage
	if cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		t.Error("server cert should have KeyUsageDigitalSignature")
	}

	// Check extended key usage
	foundServerAuth := false
	for _, eku := range cert.ExtKeyUsage {
		if eku == x509.ExtKeyUsageServerAuth {
			foundServerAuth = true
		}
	}
	if !foundServerAuth {
		t.Error("server cert should have ExtKeyUsageServerAuth")
	}
}

func TestEnsureCerts_ServerCertSignedByCA(t *testing.T) {
	certDir := t.TempDir()

	paths, err := EnsureCerts(certDir)
	if err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}

	caCert := loadCert(t, paths.CACert)
	serverCert := loadCert(t, paths.ServerCert)

	// Build a CA cert pool and verify the server cert
	pool := x509.NewCertPool()
	pool.AddCert(caCert)

	opts := x509.VerifyOptions{
		Roots:     pool,
		DNSName:   "localhost",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	if _, err := serverCert.Verify(opts); err != nil {
		t.Fatalf("server cert not verified by CA: %v", err)
	}
}

func TestEnsureCerts_ReturnsExistingCerts(t *testing.T) {
	certDir := t.TempDir()

	// First call generates certs
	paths1, err := EnsureCerts(certDir)
	if err != nil {
		t.Fatalf("first EnsureCerts: %v", err)
	}

	// Read the original server cert content
	origServerCert, err := os.ReadFile(paths1.ServerCert)
	if err != nil {
		t.Fatalf("read server cert: %v", err)
	}

	// Second call should return existing certs without regenerating
	paths2, err := EnsureCerts(certDir)
	if err != nil {
		t.Fatalf("second EnsureCerts: %v", err)
	}

	// Paths should be the same
	if paths1.ServerCert != paths2.ServerCert {
		t.Errorf("ServerCert paths differ: %q vs %q", paths1.ServerCert, paths2.ServerCert)
	}
	if paths1.ServerKey != paths2.ServerKey {
		t.Errorf("ServerKey paths differ: %q vs %q", paths1.ServerKey, paths2.ServerKey)
	}

	// File content should be unchanged (not regenerated)
	newServerCert, err := os.ReadFile(paths2.ServerCert)
	if err != nil {
		t.Fatalf("read server cert after second call: %v", err)
	}
	if string(origServerCert) != string(newServerCert) {
		t.Error("server cert was regenerated on second call; expected reuse of existing cert")
	}
}

func TestEnsureCerts_FilePermissions(t *testing.T) {
	certDir := t.TempDir()

	paths, err := EnsureCerts(certDir)
	if err != nil {
		t.Fatalf("EnsureCerts: %v", err)
	}

	// Key files should have 0600 permissions
	for _, keyPath := range []string{paths.CAKey, paths.ServerKey} {
		info, err := os.Stat(keyPath)
		if err != nil {
			t.Fatalf("stat %s: %v", keyPath, err)
		}
		perm := info.Mode().Perm()
		if perm != 0o600 {
			t.Errorf("key file %s has permissions %o, want 0600", keyPath, perm)
		}
	}
}

func TestEnsureCerts_CreatesDirectory(t *testing.T) {
	base := t.TempDir()
	certDir := filepath.Join(base, "nested", "cert", "dir")

	paths, err := EnsureCerts(certDir)
	if err != nil {
		t.Fatalf("EnsureCerts with nested dir: %v", err)
	}

	if _, err := os.Stat(paths.ServerCert); err != nil {
		t.Fatalf("server cert should exist: %v", err)
	}
}

// loadCert is a test helper that reads and parses a PEM-encoded certificate file.
func loadCert(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cert %s: %v", path, err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatalf("no PEM block found in %s", path)
	}
	if block.Type != "CERTIFICATE" {
		t.Fatalf("PEM block type = %q, want CERTIFICATE", block.Type)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert %s: %v", path, err)
	}
	return cert
}
