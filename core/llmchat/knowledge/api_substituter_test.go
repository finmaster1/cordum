package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPISubstituterCuratesOpenAPI(t *testing.T) {
	sub := NewAPISubstituter(filepath.Join("testdata", "openapi_fixture.yaml"))
	got, err := sub.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assertContains(t, got, "GET /api/v1/jobs — List jobs")
	assertContains(t, got, "POST /api/v1/jobs — Submit a new job")
	assertContains(t, got, "auth: ApiKeyAuth")
	assertContains(t, got, "required: X-Cordum-Tenant in header")
	assertContains(t, got, "JobListResponse (Paginated list of Cordum jobs")
	assertContains(t, got, "SubmitJobRequest (Job submission payload)")
	assertContains(t, got, "SubmitJobResponse (Accepted job envelope)")
	assertContains(t, got, "rate-limit: HTTP 429,Retry-After")
	assertContains(t, got, "GET /healthz — Service health; auth: public")

	assertNotContains(t, got, "should_not_appear")
	assertNotContains(t, got, "examples:")
	assertNotContains(t, got, "AKIA1234567890ABCDEF")
	assertContains(t, got, "[REDACTED:aws_access_key]")
}

func TestAPISubstituterSubstituteTemplate(t *testing.T) {
	sub := NewAPISubstituter(filepath.Join("testdata", "openapi_fixture.yaml"))
	got, err := sub.Substitute(context.Background(), "before\n{{api_summary}}\nafter")
	if err != nil {
		t.Fatalf("Substitute() error = %v", err)
	}
	assertContains(t, got, "before")
	assertContains(t, got, "GET /api/v1/jobs")
	assertContains(t, got, "after")
	assertNotContains(t, got, "{{api_summary}}")
}

func TestAPISubstituterPathFromEnv(t *testing.T) {
	t.Setenv(EnvAPISpecPath, filepath.Join("testdata", "openapi_fixture.yaml"))
	sub := NewAPISubstituter("")
	if got, want := sub.Path(), filepath.Join("testdata", "openapi_fixture.yaml"); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestAPISubstituterMissingFileFailsClosed(t *testing.T) {
	sub := NewAPISubstituter(filepath.Join("testdata", "missing.yaml"))
	_, err := sub.Load(context.Background())
	if err == nil {
		t.Fatal("Load() error = nil, want missing-file error")
	}
	if !strings.Contains(err.Error(), "read OpenAPI spec") {
		t.Fatalf("Load() error = %q, want read OpenAPI spec", err)
	}
}

func TestAPISubstituterMalformedYAMLFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(path, []byte("paths:\n  /x: [not valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := NewAPISubstituter(path)
	_, err := sub.Load(context.Background())
	if err == nil {
		t.Fatal("Load() error = nil, want parse error")
	}
	if !strings.Contains(err.Error(), "parse OpenAPI spec") {
		t.Fatalf("Load() error = %q, want parse OpenAPI spec", err)
	}
}

func TestAPISubstituterRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sub := NewAPISubstituter(filepath.Join("testdata", "openapi_fixture.yaml"))
	_, err := sub.Load(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want context.Canceled", err)
	}
}

func TestAPISubstituterBudgetRefusesOversizedBlob(t *testing.T) {
	sub := NewAPISubstituter(
		filepath.Join("testdata", "openapi_fixture.yaml"),
		WithAPITokenLimits(1, 2),
	)
	_, err := sub.Load(context.Background())
	if err == nil {
		t.Fatal("Load() error = nil, want token-budget error")
	}
	if !strings.Contains(err.Error(), "exceeds token budget") {
		t.Fatalf("Load() error = %q, want exceeds token budget", err)
	}
}

func TestEstimateTokensUsesFourByteHeuristic(t *testing.T) {
	if got, want := estimateTokens("12345"), 2; got != want {
		t.Fatalf("estimateTokens() = %d, want %d", got, want)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("output missing %q:\n%s", needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("output unexpectedly contains %q:\n%s", needle, haystack)
	}
}
