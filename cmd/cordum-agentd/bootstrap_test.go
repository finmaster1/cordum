package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/cordum/cordum/core/edge/keychain"
)

func TestBootstrapReadsFromKeyringFirst(t *testing.T) {
	t.Parallel()

	kr := keychain.NewMockKeyring()
	if err := kr.Set(context.Background(), "cordum_agentd_nonce", syntheticNonceFromKeyring()); err != nil {
		t.Fatalf("Set nonce: %v", err)
	}
	if err := kr.Set(context.Background(), "cordum_api_key", "synthetic-keychain-api-key"); err != nil {
		t.Fatalf("Set api_key: %v", err)
	}

	env := map[string]string{
		"CORDUM_GATEWAY":   "http://127.0.0.1:8081",
		"CORDUM_TENANT_ID": "tenant-a",
		// CORDUM_AGENTD_NONCE + CORDUM_API_KEY intentionally absent — must come
		// from the keychain in this test, not from env.
	}
	resolved, err := loadBootstrapSecrets(context.Background(), kr, keychain.ModeStrict, env, bytes.NewBuffer(nil))
	if err != nil {
		t.Fatalf("loadBootstrapSecrets: %v", err)
	}
	if resolved["CORDUM_AGENTD_NONCE"] != syntheticNonceFromKeyring() {
		t.Fatalf("nonce not sourced from keychain: %q", resolved["CORDUM_AGENTD_NONCE"])
	}
	if resolved["CORDUM_API_KEY"] != "synthetic-keychain-api-key" {
		t.Fatalf("api_key not sourced from keychain: %q", resolved["CORDUM_API_KEY"])
	}
}

func TestBootstrapStrictModeRefusesEnvFallback(t *testing.T) {
	t.Parallel()

	kr := keychain.NewMockKeyring()
	kr.SetFailure(keychain.ErrKeyringUnavailable)

	env := map[string]string{
		"CORDUM_GATEWAY":      "http://127.0.0.1:8081",
		"CORDUM_TENANT_ID":    "tenant-a",
		"CORDUM_AGENTD_NONCE": "WOULD-LEAK-IF-USED",
		"CORDUM_API_KEY":      "WOULD-LEAK-IF-USED",
	}
	var stderr bytes.Buffer
	_, err := loadBootstrapSecrets(context.Background(), kr, keychain.ModeStrict, env, &stderr)
	if err == nil {
		t.Fatalf("strict mode accepted env fallback when keychain unavailable")
	}
	if !strings.Contains(err.Error(), "BOOTSTRAP-FAIL") {
		t.Fatalf("expected BOOTSTRAP-FAIL in error, got %q", err.Error())
	}
	if strings.Contains(stderr.String(), "WOULD-LEAK-IF-USED") {
		t.Fatalf("stderr echoed env secret bytes: %s", stderr.String())
	}
	if strings.Contains(err.Error(), "WOULD-LEAK-IF-USED") {
		t.Fatalf("error echoed env secret bytes: %v", err)
	}
}

func TestBootstrapDevModeFallsBackToEnvWhenKeychainEmpty(t *testing.T) {
	t.Parallel()

	kr := keychain.NewMockKeyring()
	env := map[string]string{
		"CORDUM_GATEWAY":      "http://127.0.0.1:8081",
		"CORDUM_TENANT_ID":    "tenant-a",
		"CORDUM_AGENTD_NONCE": "env-fallback-nonce",
		"CORDUM_API_KEY":      "env-fallback-api-key",
	}
	var stderr bytes.Buffer
	resolved, err := loadBootstrapSecrets(context.Background(), kr, keychain.ModeDev, env, &stderr)
	if err != nil {
		t.Fatalf("dev fallback: %v", err)
	}
	if resolved["CORDUM_AGENTD_NONCE"] != "env-fallback-nonce" {
		t.Fatalf("nonce not env-fallback in dev: %q", resolved["CORDUM_AGENTD_NONCE"])
	}
	if resolved["CORDUM_API_KEY"] != "env-fallback-api-key" {
		t.Fatalf("api_key not env-fallback in dev: %q", resolved["CORDUM_API_KEY"])
	}
}

// syntheticNonceFromKeyring returns a synthetic base64-encoded nonce that the
// agentd nonce validator (>=32 random bytes, base64) accepts. The exact value
// is irrelevant to bootstrap loading — it only matters that the test value is
// not derived from production secrets and is uniquely identifiable in test
// log assertions.
func syntheticNonceFromKeyring() string {
	return "c3ludGhldGljLW5vbmNlLWZyb20ta2V5Y2hhaW4tdGVzdC0xMjM0NTY3OA=="
}
