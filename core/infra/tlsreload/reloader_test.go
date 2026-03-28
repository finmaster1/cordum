package tlsreload

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeSelfSignedCert generates a self-signed cert and writes it to certPath/keyPath.
func writeSelfSignedCert(t *testing.T, certPath, keyPath, cn string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certFile, err := os.Create(certPath)
	if err != nil {
		t.Fatalf("create cert file: %v", err)
	}
	defer func() { _ = certFile.Close() }()
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("encode cert: %v", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyFile, err := os.Create(keyPath)
	if err != nil {
		t.Fatalf("create key file: %v", err)
	}
	defer func() { _ = keyFile.Close() }()
	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		t.Fatalf("encode key: %v", err)
	}
}

func TestNewCertReloader(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	writeSelfSignedCert(t, certPath, keyPath, "test-initial")

	r, err := NewCertReloader(certPath, keyPath, "test")
	if err != nil {
		t.Fatalf("NewCertReloader: %v", err)
	}
	if r.current.Load() == nil {
		t.Fatal("expected cert to be loaded")
	}
}

func TestNewCertReloader_InvalidPath(t *testing.T) {
	_, err := NewCertReloader("/nonexistent/tls.crt", "/nonexistent/tls.key", "test")
	if err == nil {
		t.Fatal("expected error for nonexistent cert files")
	}
}

func TestGetCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	writeSelfSignedCert(t, certPath, keyPath, "test-get")

	r, err := NewCertReloader(certPath, keyPath, "test")
	if err != nil {
		t.Fatalf("NewCertReloader: %v", err)
	}

	cert, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if cert == nil {
		t.Fatal("GetCertificate returned nil")
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	if parsed.Subject.CommonName != "test-get" {
		t.Errorf("expected CN=test-get, got %s", parsed.Subject.CommonName)
	}
}

func TestGetClientCertificate(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	writeSelfSignedCert(t, certPath, keyPath, "test-client")

	r, err := NewCertReloader(certPath, keyPath, "test")
	if err != nil {
		t.Fatalf("NewCertReloader: %v", err)
	}

	cert, err := r.GetClientCertificate(nil)
	if err != nil {
		t.Fatalf("GetClientCertificate: %v", err)
	}
	if cert == nil {
		t.Fatal("GetClientCertificate returned nil")
	}
}

func TestReloadOnFileChange(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	writeSelfSignedCert(t, certPath, keyPath, "cert-v1")

	r, err := NewCertReloader(certPath, keyPath, "test")
	if err != nil {
		t.Fatalf("NewCertReloader: %v", err)
	}

	// Verify initial cert
	cert, _ := r.GetCertificate(nil)
	parsed, _ := x509.ParseCertificate(cert.Certificate[0])
	if parsed.Subject.CommonName != "cert-v1" {
		t.Fatalf("expected CN=cert-v1, got %s", parsed.Subject.CommonName)
	}

	// Overwrite with new cert
	writeSelfSignedCert(t, certPath, keyPath, "cert-v2")

	// Manually reload
	if err := r.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	// Verify new cert
	cert, _ = r.GetCertificate(nil)
	parsed, _ = x509.ParseCertificate(cert.Certificate[0])
	if parsed.Subject.CommonName != "cert-v2" {
		t.Errorf("expected CN=cert-v2 after reload, got %s", parsed.Subject.CommonName)
	}
}

func TestWatchLoopDetectsChange(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	writeSelfSignedCert(t, certPath, keyPath, "watch-v1")

	r, err := NewCertReloader(certPath, keyPath, "test-watch")
	if err != nil {
		t.Fatalf("NewCertReloader: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.WatchLoop(ctx, 100*time.Millisecond)

	// Wait for initial poll cycle
	time.Sleep(200 * time.Millisecond)

	// Overwrite cert — ensure mod time actually changes
	time.Sleep(50 * time.Millisecond)
	writeSelfSignedCert(t, certPath, keyPath, "watch-v2")

	// Wait for WatchLoop to detect the change
	deadline := time.After(2 * time.Second)
	for {
		cert, _ := r.GetCertificate(nil)
		if cert != nil {
			parsed, _ := x509.ParseCertificate(cert.Certificate[0])
			if parsed.Subject.CommonName == "watch-v2" {
				return // success
			}
		}
		select {
		case <-deadline:
			t.Fatal("WatchLoop did not detect cert change within 2s")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func TestGetCertificate_NilAfterClear(t *testing.T) {
	r := &CertReloader{label: "test-nil"}
	// current is zero-value (nil pointer)
	_, err := r.GetCertificate(nil)
	if err == nil {
		t.Fatal("expected error when no cert loaded")
	}
}

func TestTLSConfigIntegration(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	writeSelfSignedCert(t, certPath, keyPath, "integration")

	r, err := NewCertReloader(certPath, keyPath, "integration")
	if err != nil {
		t.Fatalf("NewCertReloader: %v", err)
	}

	// Verify the GetCertificate callback works in a tls.Config
	cfg := &tls.Config{
		GetCertificate: r.GetCertificate,
		MinVersion:     tls.VersionTLS12,
	}

	cert, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatalf("tls.Config.GetCertificate: %v", err)
	}
	if cert == nil {
		t.Fatal("tls.Config.GetCertificate returned nil")
	}
}
