package llmchat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestKnowledgePack_IntegrationBothSubstituters wires the real
// OpenAPI + cordum.io substituters against the testdata fixtures and
// verifies the final composed prompt contains both knowledge blobs
// AND fits within the 256KB-per-blob budget.
func TestKnowledgePack_IntegrationBothSubstituters(t *testing.T) {
	t.Parallel()
	system := "You are the Cordum chat assistant.\n\n" +
		"## Tools\n{{api_summary}}\n\n## Knowledge\n{{cordum_io_summary}}\n"
	inner := scriptedPromptLoader{text: system}

	loader := NewKnowledgePackLoader(inner)
	loader.Register("api_summary", NewOpenAPISubstituter(filepath.Join("testdata", "openapi-mini.yaml")))
	loader.Register("cordum_io_summary", NewCordumIOSubstituter(filepath.Join("testdata", "curated")))

	got, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// (a) OpenAPI summary present.
	if !strings.Contains(got, "GET /api/v1/jobs") {
		t.Errorf("integration: OpenAPI summary missing; got %q", got)
	}
	// (b) cordum.io content present.
	if !strings.Contains(got, "## architecture") {
		t.Errorf("integration: cordum.io section missing; got %q", got)
	}
	// (c) No leftover placeholders.
	if strings.Contains(got, "{{api_summary}}") || strings.Contains(got, "{{cordum_io_summary}}") {
		t.Errorf("integration: placeholders not substituted; got %q", got)
	}
	// (d) Total size under the 256KB budget × 2 substituted blobs + system overhead.
	const maxBytes = 1 << 19 // 512KB upper bound across both blobs + system
	if len(got) > maxBytes {
		t.Errorf("integration: composed prompt exceeds 512KB cap; len=%d", len(got))
	}
}
