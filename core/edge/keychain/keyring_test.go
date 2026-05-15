package keychain

import (
	"bytes"
	"context"
	"errors"
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
	if !strings.Contains(strings.ToLower(buf.String()), "keychain miss") &&
		!strings.Contains(strings.ToLower(buf.String()), "env fallback") {
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
