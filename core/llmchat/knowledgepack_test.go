package llmchat

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// scriptedPromptLoader is an in-memory PromptLoader returning a fixed
// string for every Load.
type scriptedPromptLoader struct{ text string }

func (s scriptedPromptLoader) Load(_ context.Context) (string, error) { return s.text, nil }

func TestKnowledgePack_PassThroughWhenNoSubs(t *testing.T) {
	t.Parallel()
	inner := scriptedPromptLoader{text: "Tools: {{api_summary}}\n\nDocs: {{cordum_io_summary}}"}
	loader := NewKnowledgePackLoader(inner)
	got, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(got, "{{api_summary}}") || !strings.Contains(got, "{{cordum_io_summary}}") {
		t.Errorf("unregistered placeholders should pass through; got %q", got)
	}
}

func TestKnowledgePack_SubstitutesBothPlaceholders(t *testing.T) {
	t.Parallel()
	inner := scriptedPromptLoader{text: "API:\n{{api_summary}}\n\nDOCS:\n{{cordum_io_summary}}"}
	loader := NewKnowledgePackLoader(inner)
	loader.Register("api_summary", func(_ context.Context) (string, error) {
		return "GET /api/v1/jobs — list jobs", nil
	})
	loader.Register("cordum_io_summary", func(_ context.Context) (string, error) {
		return "## Architecture\n\nCordum uses NATS + Redis.", nil
	})

	got, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(got, "GET /api/v1/jobs") {
		t.Errorf("api_summary missing; got %q", got)
	}
	if !strings.Contains(got, "Cordum uses NATS + Redis") {
		t.Errorf("cordum_io_summary missing; got %q", got)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unsubstituted token remains; got %q", got)
	}
}

func TestKnowledgePack_BudgetTruncates(t *testing.T) {
	inner := scriptedPromptLoader{text: "{{big}}"}
	loader := NewKnowledgePackLoader(inner, WithBudget(64)) // 64 tokens × 4 bytes/token = 256 bytes
	loader.Register("big", func(_ context.Context) (string, error) {
		return strings.Repeat("a", 1024), nil
	})

	got, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) > 256 {
		t.Errorf("output should be truncated to ≤256 bytes; got %d", len(got))
	}
}

func TestKnowledgePack_CacheHitWithinTTL(t *testing.T) {
	t.Parallel()
	var counter atomic.Int32
	inner := scriptedPromptLoader{text: "{{tok}}"}
	loader := NewKnowledgePackLoader(inner)
	loader.Register("tok", func(_ context.Context) (string, error) {
		counter.Add(1)
		return "value", nil
	})

	for i := range 3 {
		if _, err := loader.Load(context.Background()); err != nil {
			t.Fatalf("Load %d: %v", i, err)
		}
	}
	if got := counter.Load(); got != 1 {
		t.Errorf("substituter calls = %d, want 1 (cache hit)", got)
	}
}

func TestKnowledgePack_CacheExpiresAtTTL(t *testing.T) {
	t.Parallel()
	var counter atomic.Int32
	inner := scriptedPromptLoader{text: "{{tok}}"}
	now := time.Now()
	clock := now
	loader := NewKnowledgePackLoader(inner,
		WithTTL(5*time.Minute),
		WithNowFn(func() time.Time { return clock }),
	)
	loader.Register("tok", func(_ context.Context) (string, error) {
		counter.Add(1)
		return "value", nil
	})

	if _, err := loader.Load(context.Background()); err != nil {
		t.Fatalf("Load 1: %v", err)
	}
	clock = now.Add(6 * time.Minute) // jump past TTL
	if _, err := loader.Load(context.Background()); err != nil {
		t.Fatalf("Load 2: %v", err)
	}

	if got := counter.Load(); got != 2 {
		t.Errorf("substituter calls = %d, want 2 (cache expired at TTL)", got)
	}
}

func TestKnowledgePack_RefreshAllInvalidatesCache(t *testing.T) {
	t.Parallel()
	var counter atomic.Int32
	inner := scriptedPromptLoader{text: "{{tok}}"}
	loader := NewKnowledgePackLoader(inner)
	loader.Register("tok", func(_ context.Context) (string, error) {
		counter.Add(1)
		return "value", nil
	})

	if _, err := loader.Load(context.Background()); err != nil {
		t.Fatalf("Load 1: %v", err)
	}
	loader.RefreshAll()
	if _, err := loader.Load(context.Background()); err != nil {
		t.Fatalf("Load 2: %v", err)
	}

	if got := counter.Load(); got != 2 {
		t.Errorf("substituter calls = %d, want 2 (RefreshAll cleared cache)", got)
	}
}
