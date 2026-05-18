package keychain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// sentinelLeakToken is a synthetic value embedded in test fixtures so we can
// assert it never appears verbatim in structured-logger output. The constant
// is intentionally unique and high-entropy so an accidental leak via fmt.%v,
// slog attribute, or wrapped error is detectable with a substring match.
const sentinelLeakToken = "SENTINEL_LEAK_3f9b2c11e4a87de9"

func TestKeyringSetGetRoundtrip(t *testing.T) {
	t.Parallel()
	kr := NewMockKeyring()
	ctx := context.Background()

	if err := kr.Set(ctx, "cordum_agentd_nonce", sentinelLeakToken); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := kr.Get(ctx, "cordum_agentd_nonce")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != sentinelLeakToken {
		t.Fatalf("Get roundtrip mismatch: got=%q want=%q", got, sentinelLeakToken)
	}
}

func TestKeyringGetMissing(t *testing.T) {
	t.Parallel()
	kr := NewMockKeyring()
	ctx := context.Background()

	_, err := kr.Get(ctx, "cordum_agentd_missing")
	if !errors.Is(err, ErrKeyringNotFound) {
		t.Fatalf("Get missing: err=%v, want ErrKeyringNotFound", err)
	}
}

func TestKeyringDelete(t *testing.T) {
	t.Parallel()
	kr := NewMockKeyring()
	ctx := context.Background()

	if err := kr.Set(ctx, "cordum_test_secret", "value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := kr.Delete(ctx, "cordum_test_secret"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := kr.Get(ctx, "cordum_test_secret"); !errors.Is(err, ErrKeyringNotFound) {
		t.Fatalf("post-delete Get: err=%v, want ErrKeyringNotFound", err)
	}
}

// TestMockKeyringDeleteEmptyName is the PR #276 Sub-H #33 regression.
// `MockKeyring.Delete("")` must mirror `Get`/`Set` and refuse the empty
// key with `ErrKeyringNotFound`, NOT silently no-op. Without this rail
// a future refactor could let a caller bug (an unset secret-name var)
// silently delete the entry the caller actually wanted to keep — or
// worse, prune *every* zero-key sentinel a future implementation may
// add. The test also pins the no-mutation invariant: a seeded key must
// still be reachable after a rejected empty-key Delete.
func TestMockKeyringDeleteEmptyName(t *testing.T) {
	t.Parallel()
	kr := NewMockKeyring()
	ctx := context.Background()

	const seededKey, seededVal = "cordum_seeded_secret", "stay_put"
	if err := kr.Set(ctx, seededKey, seededVal); err != nil {
		t.Fatalf("seed Set: %v", err)
	}

	if err := kr.Delete(ctx, ""); !errors.Is(err, ErrKeyringNotFound) {
		t.Fatalf("Delete empty: err=%v, want ErrKeyringNotFound", err)
	}

	// No-mutation invariant: the seeded key must survive an empty-key
	// Delete attempt. If Delete("") accidentally wildcards through the
	// store, this assertion catches it before the next CI run.
	got, err := kr.Get(ctx, seededKey)
	if err != nil {
		t.Fatalf("Get post-empty-delete: err=%v, want nil (seeded key must survive)", err)
	}
	if got != seededVal {
		t.Fatalf("Get post-empty-delete: value=%q, want %q (seeded value mutated)", got, seededVal)
	}
}

func TestKeyringStrictModeFailsClosedOnUnavailable(t *testing.T) {
	t.Parallel()
	kr := NewMockKeyring()
	kr.SetFailure(ErrKeyringUnavailable)
	ctx := context.Background()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_, err := LoadSecret(ctx, kr, ModeStrict, sentinelLeakToken, "cordum_agentd_nonce", logger)
	if !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("strict + unavailable: err=%v, want ErrKeyringUnavailable", err)
	}
	if strings.Contains(buf.String(), sentinelLeakToken) {
		t.Fatalf("logger leaked sentinel token: %s", buf.String())
	}
}

func TestKeyringDevModeFallsBackToEnv(t *testing.T) {
	t.Parallel()
	kr := NewMockKeyring()
	ctx := context.Background()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	got, err := LoadSecret(ctx, kr, ModeDev, sentinelLeakToken, "cordum_agentd_nonce", logger)
	if err != nil {
		t.Fatalf("dev fallback: %v", err)
	}
	if got != sentinelLeakToken {
		t.Fatalf("dev fallback: got=%q want=%q", got, sentinelLeakToken)
	}
	if strings.Contains(buf.String(), sentinelLeakToken) {
		t.Fatalf("logger leaked sentinel token during env fallback: %s", buf.String())
	}
	if !strings.Contains(strings.ToLower(buf.String()), "keychain.env_fallback") {
		t.Fatalf("dev fallback emitted no banner-warn: %s", buf.String())
	}
}

func TestKeyringDevModeFallsClosedOnEnvMissingToo(t *testing.T) {
	t.Parallel()
	kr := NewMockKeyring()
	ctx := context.Background()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	_, err := LoadSecret(ctx, kr, ModeDev, "", "cordum_agentd_nonce", logger)
	if !errors.Is(err, ErrKeyringNotFound) {
		t.Fatalf("dev + no env: err=%v, want ErrKeyringNotFound", err)
	}
}

func TestKeyringLogRedaction(t *testing.T) {
	t.Parallel()
	kr := NewMockKeyring()
	if err := kr.Set(context.Background(), "cordum_agentd_nonce", sentinelLeakToken); err != nil {
		t.Fatalf("Set: %v", err)
	}
	ctx := context.Background()
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	got, err := LoadSecret(ctx, kr, ModeStrict, "", "cordum_agentd_nonce", logger)
	if err != nil {
		t.Fatalf("LoadSecret: %v", err)
	}
	if got != sentinelLeakToken {
		t.Fatalf("loaded value mismatch: %q", got)
	}
	if strings.Contains(buf.String(), sentinelLeakToken) {
		t.Fatalf("logger leaked secret bytes on success path: %s", buf.String())
	}
}

func TestKeyringLoadSecretEmptyName(t *testing.T) {
	t.Parallel()
	kr := NewMockKeyring()
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	_, err := LoadSecret(ctx, kr, ModeStrict, "", "", logger)
	if !errors.Is(err, ErrKeyringNotFound) {
		t.Fatalf("empty secret name: err=%v, want ErrKeyringNotFound", err)
	}
}

// TestRedactBackendErrorSecretPatterns is the PR #276 Sub-G #22 regression.
// Third-party Keyring backends (godbus on Linux, security CLI on macOS,
// wincred on Windows) format their errors however they like; a value the
// caller passed in could be echoed back inside an error message. Every
// secret-shape we plausibly leak through `%w` / `Errorf("%s: %w", ...)` /
// slog %v MUST be redacted before the message reaches stderr or journald.
//
// Fixtures are deterministic synthetic strings (NEVER real secrets) chosen
// so that any verbatim substring appearance in the output is detectable
// with a single Contains() check — i.e. the test catches a "pattern
// missed entirely" failure mode, not just "regex wrote wrong replacement".
func TestRedactBackendErrorSecretPatterns(t *testing.T) {
	t.Parallel()

	const syntheticPEMBody = "MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDSENTINELPEMxyzABCDEFG12345"
	syntheticPEM := "-----BEGIN RSA PRIVATE KEY-----\n" + syntheticPEMBody + "\n-----END RSA PRIVATE KEY-----"
	const (
		syntheticBase64 = "U0VOVElORUxfQkFTRTY0X1RPS0VOX2FiY2RlZmdoaWprbG1ub3BxcnN0dXZ3eHl6"
		syntheticHex    = "abcdef0123456789abcdef0123456789sentinelhexfullsuffix01"
		syntheticJWT    = "eyJhbGciSENT.eyJzdWIiSENT.SIG_SENTINEL_3f9b2c11e4a87de9"
		syntheticSK     = "sk-SENTINELabcdefghijklmnopqrstuvwxyz0123456789ABCD"
		syntheticGHP    = "ghp_SENTINELabcdefghijklmnopqrstuvwxyz1234"
		syntheticGitPAT = "github_pat_SENTINELabcdefghijklmnopqrstuvwxyz1234"
		syntheticAKIA   = "AKIASENTINEL12345678"
		syntheticBearer = "Authorization: Bearer sentinel-bearer-token-3f9b2c11"
		syntheticUUID   = "11111111-2222-3333-4444-555566667777"
	)

	cases := []struct {
		name   string
		secret string
		input  string
	}{
		{name: "pem_private_key", secret: syntheticPEMBody, input: "keyring decode failed: " + syntheticPEM},
		{name: "long_base64_token", secret: syntheticBase64, input: "secret-service: invalid token " + syntheticBase64},
		{name: "long_hex_run", secret: syntheticHex, input: "wincred returned digest " + syntheticHex},
		{name: "jwt_three_segments", secret: syntheticJWT, input: "dbus returned token: " + syntheticJWT},
		{name: "openai_sk_token", secret: syntheticSK, input: "credential leak " + syntheticSK + " in backend"},
		{name: "github_classic_token", secret: syntheticGHP, input: "credential: " + syntheticGHP},
		{name: "github_fine_grained_pat", secret: syntheticGitPAT, input: "stored value " + syntheticGitPAT},
		{name: "aws_access_key", secret: syntheticAKIA, input: "wincred error: returned " + syntheticAKIA},
		{name: "authorization_bearer_header", secret: syntheticBearer, input: "keyring returned: " + syntheticBearer},
		{name: "uuid_secret_ref", secret: syntheticUUID, input: "keychain lookup failed for id " + syntheticUUID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := redactBackendError(tc.input)
			if !strings.Contains(out, "[REDACTED") {
				t.Fatalf("redactBackendError(%q): no [REDACTED marker present in %q", tc.name, out)
			}
			if strings.Contains(out, tc.secret) {
				t.Fatalf("redactBackendError(%q): output %q still contains secret substring %q", tc.name, out, tc.secret)
			}
		})
	}
}

// TestLoadSecretLogsKeyringErrorClass is the PR #276 Sub-G #21 regression.
// When the backend fails, the structured log MUST surface a non-empty
// `keyring_error_class` attribute so operators can dispatch on category
// without parsing free-text. It MUST NOT echo the raw backend error text
// (custom Keyring impls may embed secrets in their error strings) and MUST
// NOT echo the env-fallback secret on the strict-mode failure path.
func TestLoadSecretLogsKeyringErrorClass(t *testing.T) {
	t.Parallel()

	const (
		syntheticBackend = "raw-backend-bytes-3f9b2c11-LEAK-IF-LOGGED"
		envFallback      = "WOULD-LEAK-IF-USED-IN-STRICT-MODE"
	)
	cases := []struct {
		name      string
		failWith  error
		mode      BootstrapMode
		wantClass string
	}{
		{
			name:      "unavailable_strict_logs_class",
			failWith:  fmt.Errorf("%w: %s", ErrKeyringUnavailable, syntheticBackend),
			mode:      ModeStrict,
			wantClass: "backend_unavailable",
		},
		{
			name:      "permission_strict_logs_class",
			failWith:  fmt.Errorf("%w: %s", ErrKeyringPermissionDenied, syntheticBackend),
			mode:      ModeStrict,
			wantClass: "permission_denied",
		},
		{
			name:      "not_found_strict_logs_class",
			failWith:  fmt.Errorf("%w: %s", ErrKeyringNotFound, syntheticBackend),
			mode:      ModeStrict,
			wantClass: "secret_not_found",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			kr := NewMockKeyring()
			kr.SetFailure(tc.failWith)
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
			_, err := LoadSecret(context.Background(), kr, tc.mode, envFallback, "cordum_agentd_nonce", logger)
			if err == nil {
				t.Fatalf("LoadSecret returned nil error for backend failure %v", tc.failWith)
			}
			out := buf.String()
			if !strings.Contains(out, "keyring_error_class="+tc.wantClass) {
				t.Fatalf("logs missing keyring_error_class=%q attribute; got %q", tc.wantClass, out)
			}
			if strings.Contains(out, syntheticBackend) {
				t.Fatalf("logs leaked raw backend bytes %q; got %q", syntheticBackend, out)
			}
			if strings.Contains(out, envFallback) {
				t.Fatalf("strict-mode logs leaked env-fallback secret %q; got %q", envFallback, out)
			}
		})
	}
}

func TestKeyringStrictModeIgnoresEnvFallback(t *testing.T) {
	t.Parallel()
	kr := NewMockKeyring()
	kr.SetFailure(ErrKeyringNotFound)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	_, err := LoadSecret(ctx, kr, ModeStrict, "would-leak-if-used", "cordum_agentd_nonce", logger)
	if !errors.Is(err, ErrKeyringNotFound) && !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("strict ignores env: err=%v, want ErrKeyringNotFound or ErrKeyringUnavailable", err)
	}
}
