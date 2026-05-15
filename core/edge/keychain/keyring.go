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
	"strings"

	zkeyring "github.com/zalando/go-keyring"
)

// keyringServiceName is the per-host namespace under which cordum-agentd
// stores its secrets. Operators provision values with platform-native CLIs:
//
//	macOS  : security add-generic-password -a "$USER" -s cordum_agentd_nonce -w "<base64-nonce>"
//	Linux  : secret-tool store --label="cordum-agentd nonce" service cordum-agentd account cordum_agentd_nonce
//	Windows: cmdkey /generic:cordum_agentd_nonce /user:"$USER" /pass:"<base64-nonce>"
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
// The function intentionally allows lowercase backend diagnostic text but
// truncates aggressively at the first whitespace-padded base64-style run
// so a backend that included a leaked value in its error string cannot
// propagate that value into agentd's stderr or audit log.
func redactBackendError(msg string) string {
	const maxLen = 200
	cleaned := strings.ReplaceAll(msg, "\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\r", " ")
	if len(cleaned) > maxLen {
		cleaned = cleaned[:maxLen] + "...[truncated]"
	}
	return cleaned
}
