package tls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

type CertPaths struct {
	CACert     string
	CAKey      string
	ServerCert string
	ServerKey  string
}

// EnsureCerts checks for existing certs or generates a CA + server cert pair.
func EnsureCerts(certDir string) (*CertPaths, error) {
	paths := &CertPaths{
		CACert:     filepath.Join(certDir, "ca.crt"),
		CAKey:      filepath.Join(certDir, "ca.key"),
		ServerCert: filepath.Join(certDir, "server.crt"),
		ServerKey:  filepath.Join(certDir, "server.key"),
	}

	// If server cert and key already exist, return them
	if fileExists(paths.ServerCert) && fileExists(paths.ServerKey) {
		return paths, nil
	}

	if err := os.MkdirAll(certDir, 0o700); err != nil {
		return nil, fmt.Errorf("create cert dir: %w", err)
	}

	// Generate CA
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate CA key: %w", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber: newSerial(),
		Subject: pkix.Name{
			Organization: []string{"Tokenomics CA"},
			CommonName:   "Tokenomics Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("create CA cert: %w", err)
	}

	if err := writePEM(paths.CACert, "CERTIFICATE", caCertDER); err != nil {
		return nil, err
	}
	caKeyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return nil, fmt.Errorf("marshal CA key: %w", err)
	}
	if err := writePEM(paths.CAKey, "EC PRIVATE KEY", caKeyDER); err != nil {
		return nil, err
	}

	// Parse back the CA cert for signing
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, fmt.Errorf("parse CA cert: %w", err)
	}

	// Generate server cert
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate server key: %w", err)
	}

	serverTemplate := &x509.Certificate{
		SerialNumber: newSerial(),
		Subject: pkix.Name{
			Organization: []string{"Tokenomics"},
			CommonName:   "localhost",
		},
		DNSNames:    []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	serverCertDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("create server cert: %w", err)
	}

	if err := writePEM(paths.ServerCert, "CERTIFICATE", serverCertDER); err != nil {
		return nil, err
	}
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return nil, fmt.Errorf("marshal server key: %w", err)
	}
	if err := writePEM(paths.ServerKey, "EC PRIVATE KEY", serverKeyDER); err != nil {
		return nil, err
	}

	return paths, nil
}

func newSerial() *big.Int {
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	return serial
}

func writePEM(path, blockType string, derBytes []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: derBytes})
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
