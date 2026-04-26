package llmchat

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestResolveProvider_OpenAI(t *testing.T) {
	t.Parallel()

	p, err := ResolveProvider(ProviderConfig{
		Kind:    "openai",
		BaseURL: "http://example.invalid/v1",
		Model:   "qwen3-coder",
	})
	if err != nil {
		t.Fatalf("ResolveProvider returned error: %v", err)
	}
	if p == nil {
		t.Fatal("expected provider, got nil")
	}
	if _, ok := p.(*OpenAIProvider); !ok {
		t.Fatalf("expected *OpenAIProvider, got %T", p)
	}
}

func TestResolveProvider_EmptyKind(t *testing.T) {
	t.Parallel()

	_, err := ResolveProvider(ProviderConfig{Kind: ""})
	if err == nil {
		t.Fatal("expected error for empty kind, got nil")
	}
	if !strings.Contains(err.Error(), "kind is required") {
		t.Fatalf("error %v missing 'kind is required'", err)
	}
}

func TestResolveProvider_UnknownKind(t *testing.T) {
	t.Parallel()

	_, err := ResolveProvider(ProviderConfig{Kind: "claude"})
	if err == nil {
		t.Fatal("expected error for unknown kind, got nil")
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Fatalf("error %v should mention the unknown kind", err)
	}
}

func TestSamplingMode_String(t *testing.T) {
	t.Parallel()

	cases := map[SamplingMode]string{
		SamplingModeToolCalls: "tool_calls",
		SamplingModeSummary:   "summary",
	}
	for mode, want := range cases {
		if got := mode.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", mode, got, want)
		}
	}
	// Out-of-range modes get a debuggable string rather than panic.
	if got := SamplingMode(99).String(); !strings.HasPrefix(got, "unknown(") {
		t.Errorf("SamplingMode(99).String() = %q, want unknown(...)", got)
	}
}

// Compile-time interface satisfaction guards. A signature drift on
// Provider would fail to compile here, surfacing immediately rather
// than at first use. No runtime assertion needed (the linter rejected
// the `if p == nil` form as impossible-condition; the var-decl form
// is the canonical Go idiom).
var (
	_ Provider = (*MockProvider)(nil)
	_ Provider = (*OpenAIProvider)(nil)
)

// errSentinel is an in-package error value used by mock-based tests
// to assert specific error propagation paths.
var errSentinel = errors.New("sentinel")

// TestMockHealthCheckPropagatesError keeps the wiring from drifting:
// HealthCheck is what /readyz uses, so the contract is load-bearing.
func TestMockHealthCheckPropagatesError(t *testing.T) {
	t.Parallel()

	m := NewMockProvider()
	m.SetHealthErr(errSentinel)
	if err := m.HealthCheck(context.Background()); !errors.Is(err, errSentinel) {
		t.Fatalf("HealthCheck = %v, want %v", err, errSentinel)
	}
	if got := m.HealthCalls(); got != 1 {
		t.Fatalf("HealthCalls = %d, want 1", got)
	}
}
