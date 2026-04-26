package llmchat

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAPISubstituter_ParsesFixture(t *testing.T) {
	t.Parallel()
	sub := NewOpenAPISubstituter(filepath.Join("testdata", "openapi-mini.yaml"))
	got, err := sub(context.Background())
	if err != nil {
		t.Fatalf("substituter: %v", err)
	}
	if !strings.Contains(got, "GET /api/v1/jobs") {
		t.Errorf("expected GET /api/v1/jobs in output; got %q", got)
	}
	if !strings.Contains(got, "POST /api/v1/jobs") {
		t.Errorf("expected POST /api/v1/jobs in output; got %q", got)
	}
	if !strings.Contains(got, "GET /api/v1/audit/verify") {
		t.Errorf("expected GET /api/v1/audit/verify in output; got %q", got)
	}
}

func TestOpenAPISubstituter_CompactSummaryFormat(t *testing.T) {
	t.Parallel()
	sub := NewOpenAPISubstituter(filepath.Join("testdata", "openapi-mini.yaml"))
	got, err := sub(context.Background())
	if err != nil {
		t.Fatalf("substituter: %v", err)
	}
	// Each summary line carries the canonical components.
	for _, want := range []string{
		"GET /api/v1/jobs — List jobs",
		"auth: apiKey",
		"required: tenant_id",
		"responses:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q; got %q", want, got)
		}
	}
	// 200 response should include the schema $ref short-name.
	if !strings.Contains(got, "200(JobList)") {
		t.Errorf("expected 200(JobList) ref short-name; got %q", got)
	}
}

func TestOpenAPISubstituter_MissingFile(t *testing.T) {
	t.Parallel()
	sub := NewOpenAPISubstituter(filepath.Join("testdata", "no-such-file.yaml"))
	got, err := sub(context.Background())
	if err != nil {
		t.Fatalf("missing file should be soft-fail; got err=%v", err)
	}
	if got != "" {
		t.Errorf("missing file should return empty string; got %q", got)
	}
}

func TestOpenAPISubstituter_MalformedYAML(t *testing.T) {
	t.Parallel()
	sub := NewOpenAPISubstituter(filepath.Join("testdata", "openapi-malformed.yaml"))
	got, err := sub(context.Background())
	if err != nil {
		t.Fatalf("malformed YAML should be soft-fail; got err=%v", err)
	}
	if got != "" {
		t.Errorf("malformed YAML should return empty string; got %q", got)
	}
}

func TestOpenAPISubstituter_RespectsContextCancel(t *testing.T) {
	t.Parallel()
	sub := NewOpenAPISubstituter(filepath.Join("testdata", "openapi-mini.yaml"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := sub(ctx)
	if err == nil {
		t.Fatal("expected ctx error on cancelled context")
	}
}
