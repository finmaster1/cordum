// Package tlsreload provides a TLS certificate reloader that watches cert
// files on disk and atomically swaps them when changes are detected.
// New connections use the latest cert; existing connections are not disrupted.
package tlsreload

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"
)

// CertReloader watches TLS certificate and key files on disk, reloading them
// when their modification times change. It is safe for concurrent use.
type CertReloader struct {
	certPath string
	keyPath  string
	current  atomic.Pointer[tls.Certificate]
	label    string // human-readable label for log messages
}

// NewCertReloader creates a new CertReloader that loads the initial cert from
// the given paths. The label is used in log messages (e.g., "gateway-http").
func NewCertReloader(certPath, keyPath, label string) (*CertReloader, error) {
	r := &CertReloader{
		certPath: certPath,
		keyPath:  keyPath,
		label:    label,
	}
	if err := r.reload(); err != nil {
		return nil, fmt.Errorf("initial cert load (%s): %w", label, err)
	}
	return r, nil
}

// GetCertificate returns the current certificate. It implements the
// tls.Config.GetCertificate callback signature.
func (r *CertReloader) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert := r.current.Load()
	if cert == nil {
		return nil, fmt.Errorf("tlsreload(%s): no certificate loaded", r.label)
	}
	return cert, nil
}

// GetClientCertificate returns the current certificate for client-side TLS.
// It implements the tls.Config.GetClientCertificate callback signature.
func (r *CertReloader) GetClientCertificate(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	cert := r.current.Load()
	if cert == nil {
		return nil, fmt.Errorf("tlsreload(%s): no certificate loaded", r.label)
	}
	return cert, nil
}

// WatchLoop polls the cert and key files at the given interval, reloading
// them when modification times change. It blocks until ctx is cancelled.
func (r *CertReloader) WatchLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	lastCertMod, lastKeyMod := r.modTimes()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			certMod, keyMod := r.modTimes()
			if certMod.Equal(lastCertMod) && keyMod.Equal(lastKeyMod) {
				continue
			}
			if err := r.reload(); err != nil {
				slog.Error("tls cert reload failed",
					"label", r.label,
					"cert", r.certPath,
					"error", err,
				)
				continue
			}
			lastCertMod = certMod
			lastKeyMod = keyMod
			slog.Info("tls cert reloaded",
				"label", r.label,
				"cert", r.certPath,
				"key", r.keyPath,
			)
		}
	}
}

func (r *CertReloader) reload() error {
	cert, err := tls.LoadX509KeyPair(r.certPath, r.keyPath)
	if err != nil {
		return err
	}
	r.current.Store(&cert)
	return nil
}

func (r *CertReloader) modTimes() (time.Time, time.Time) {
	var certMod, keyMod time.Time
	if info, err := os.Stat(r.certPath); err == nil {
		certMod = info.ModTime()
	}
	if info, err := os.Stat(r.keyPath); err == nil {
		keyMod = info.ModTime()
	}
	return certMod, keyMod
}
