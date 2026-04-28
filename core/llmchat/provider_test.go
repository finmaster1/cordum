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

	if got := SamplingModeResponse.String(); got != "response" {
		t.Fatalf("SamplingModeResponse.String() = %q, want response", got)
	}
	if got := SamplingMode(99).String(); !strings.HasPrefix(got, "unknown(") {
		t.Errorf("SamplingMode(99).String() = %q, want unknown(...)", got)
	}
}

var (
	_ Provider = (*MockProvider)(nil)
	_ Provider = (*OpenAIProvider)(nil)
)

var errSentinel = errors.New("sentinel")

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
