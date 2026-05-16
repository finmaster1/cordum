// Package keychain provides a thin abstraction over the host operating
// system's native credential store. cordum-agentd uses it to source the
// boot-time secrets (CORDUM_AGENTD_NONCE, CORDUM_API_KEY) from the OS
// keychain instead of the developer-shell environment, closing the
// EDGE-031 tradeoff where same-user `ps -E` / `/proc/<pid>/environ` /
// shell history could expose runtime credentials.
//
// Backends:
//   - macOS: Security framework via `security` CLI (zalando/go-keyring darwin)
//   - Linux: Secret Service / libsecret via godbus
//   - Windows: Credential Manager via wincred (crypt32.dll)
//
// Tests use NewMockKeyring(); production code calls NewOSKeyring() at
// process startup and passes the resulting Keyring into LoadSecret.
package keychain

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	zkeyring "github.com/zalando/go-keyring"
)

// keyringServiceName is the per-host namespace under which cordum-agentd
// stores its secrets. Operators provision values with platform-native CLIs:
//
//	macOS  : security add-generic-password -a cordum_agentd_nonce -s cordum-agentd -w "<base64-nonce>"
//	Linux  : secret-tool store --label="cordum-agentd nonce" service cordum-agentd username cordum_agentd_nonce
//	Windows: cmdkey /generic:cordum-agentd:cordum_agentd_nonce /user:cordum_agentd_nonce /pass:"<base64-nonce>"
//
// The constant is intentionally not exported — callers receive a Keyring
// already bound to this namespace and have no reason to override it.
const keyringServiceName = "cordum-agentd"

// Keyring is the boundary between cordum-agentd's bootstrap flow and the
// underlying OS credential store. Implementations must be goroutine-safe:
// the agentd process may issue concurrent secret lookups during startup.
type Keyring interface {
	// Get returns the secret previously stored under key, or
	// ErrKeyringNotFound when no entry exists. Implementations MUST NOT
	// echo the returned value into any logger, error message, or
	// telemetry — the value flows through return-only.
	Get(ctx context.Context, key string) (string, error)

	// Set writes value under key. Existing values are overwritten. An
	// empty key returns an error without touching the backend.
	Set(ctx context.Context, key string, value string) error

	// Delete removes the entry for key. Deleting a missing key is not an
	// error; it allows idempotent test teardown and key rotation flows.
	Delete(ctx context.Context, key string) error
}

// NewOSKeyring returns a Keyring backed by the host's native credential
// store. Errors from the underlying backend are normalized into the
// package-level sentinels (ErrKeyringNotFound, ErrKeyringUnavailable,
// ErrKeyringPermissionDenied) so callers can dispatch on them without
// depending on platform-specific error strings.
func NewOSKeyring() Keyring {
	return &osKeyring{service: keyringServiceName}
}

type osKeyring struct {
	service string
}

func (o *osKeyring) Get(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if key == "" {
		return "", ErrKeyringNotFound
	}
	value, err := zkeyring.Get(o.service, key)
	if err != nil {
		return "", normalizeBackendError(err)
	}
	return value, nil
}

func (o *osKeyring) Set(ctx context.Context, key, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("keychain: empty key")
	}
	if err := zkeyring.Set(o.service, key, value); err != nil {
		return normalizeBackendError(err)
	}
	return nil
}

func (o *osKeyring) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if key == "" {
		return nil
	}
	err := zkeyring.Delete(o.service, key)
	if err == nil {
		return nil
	}
	if errors.Is(err, zkeyring.ErrNotFound) {
		return nil
	}
	return normalizeBackendError(err)
}

// normalizeBackendError maps zalando/go-keyring (and underlying OS) errors
// into the keychain package sentinels. The mapping is conservative: any
// error not explicitly recognized as "not found" or "permission denied" is
// classified as ErrKeyringUnavailable, so strict-mode callers fail closed
// rather than continuing with an ambiguous backend state.
func normalizeBackendError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, zkeyring.ErrNotFound) {
		return ErrKeyringNotFound
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "permission denied"),
		strings.Contains(msg, "access denied"),
		strings.Contains(msg, "user interaction not allowed"),
		strings.Contains(msg, "keychain item not granted"):
		return fmt.Errorf("%w: %s", ErrKeyringPermissionDenied, redactBackendError(msg))
	case strings.Contains(msg, "no such interface"),
		strings.Contains(msg, "service_unknown"),
		strings.Contains(msg, "could not get session"),
		strings.Contains(msg, "no dbus session"),
		strings.Contains(msg, "dbus-launch"),
		strings.Contains(msg, "secret service"),
		strings.Contains(msg, "no credential manager"),
		strings.Contains(msg, "element not found"),
		strings.Contains(msg, "the system cannot find"):
		return fmt.Errorf("%w: %s", ErrKeyringUnavailable, redactBackendError(msg))
	default:
		return fmt.Errorf("%w: %s", ErrKeyringUnavailable, redactBackendError(msg))
	}
}

// redactBackendError keeps the error category visible (D-Bus session, ACL,
// missing-entry) while stripping anything that might look like a secret.
// A custom Keyring implementation that included a raw value, JWT, base64
// or UUID-shaped token in its error string would otherwise propagate that
// material into agentd's stderr / audit log via Errorf("%s: %w", ...).
// The redact passes catch the common high-entropy substring shapes before
// the truncation pass so the surviving message is operator-readable but
// never carries a credential.
func redactBackendError(msg string) string {
	const maxLen = 200
	cleaned := strings.ReplaceAll(msg, "\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\r", " ")
	for _, p := range backendErrorRedactPatterns {
		cleaned = p.MustCompile.ReplaceAllString(cleaned, p.Replacement)
	}
	if len(cleaned) > maxLen {
		cleaned = cleaned[:maxLen] + "...[truncated]"
	}
	return cleaned
}

type backendRedactRule struct {
	MustCompile *regexp.Regexp
	Replacement string
}

// backendErrorRedactPatterns scrubs long high-entropy substrings before
// the truncation pass. Ordered most-specific → most-general so a JWT is
// caught before its base64 segments individually trip the generic rule.
var backendErrorRedactPatterns = []backendRedactRule{
	// JWT (three base64url segments separated by dots).
	{regexp.MustCompile(`\b[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\b`), "[REDACTED:jwt]"},
	// UUID v1..v5.
	{regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), "[REDACTED:uuid]"},
	// Long hex run (≥32 chars) — covers SHA256/512 digests + raw key material.
	{regexp.MustCompile(`(?i)\b[0-9a-f]{32,}\b`), "[REDACTED:hex]"},
	// Long base64/base64url run (≥24 chars) — covers raw tokens, nonces,
	// HMAC outputs. Constrained to alnum + url-safe + padding so we don't
	// chew through ordinary English.
	{regexp.MustCompile(`\b[A-Za-z0-9+/_\-]{24,}={0,2}\b`), "[REDACTED:base64]"},
}
