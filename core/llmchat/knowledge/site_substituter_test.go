package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSiteSubstituterCuratesMDXDirectory(t *testing.T) {
	sub := NewSiteSubstituter(filepath.Join("testdata", "site"),
		WithSiteGlobs([]string{"concepts/*.mdx", "getting-started/*.md"}, []string{"**/draft.md"}),
	)
	got, err := sub.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assertContains(t, got, "## concepts/job.mdx")
	assertContains(t, got, "# Jobs and Tasks")
	assertContains(t, got, "An **epic** groups related Cordum work")
	assertContains(t, got, "A **task** is the executable unit")
	assertContains(t, got, "```yaml\nkind: Job\ntopic: job.demo.run\n```")
	assertContains(t, got, "## getting-started/quickstart.md")
	assertContains(t, got, "Enterprise tier")

	assertNotContains(t, got, "title: Jobs and tasks")
	assertNotContains(t, got, "import Tabs")
	assertNotContains(t, got, "<Tabs")
	assertNotContains(t, got, "<TabItem")
	assertNotContains(t, got, "<Callout")
	assertNotContains(t, got, "This file should be excluded")
	assertNotContains(t, got, "AKIA1234567890ABCDEF")
	assertContains(t, got, "[REDACTED:aws_access_key]")
}

func TestSiteSubstituterSubstituteTemplate(t *testing.T) {
	sub := NewSiteSubstituter(filepath.Join("testdata", "site"),
		WithSiteGlobs([]string{"getting-started/*.md"}, []string{"**/draft.md"}),
	)
	got, err := sub.Substitute(context.Background(), "before\n{{cordum_io_summary}}\nafter")
	if err != nil {
		t.Fatalf("Substitute() error = %v", err)
	}
	assertContains(t, got, "before")
	assertContains(t, got, "Quickstart")
	assertContains(t, got, "after")
	assertNotContains(t, got, "{{cordum_io_summary}}")
}

func TestSiteSubstituterPathAndGlobsFromEnv(t *testing.T) {
	t.Setenv(EnvSitePath, filepath.Join("testdata", "site"))
	t.Setenv(EnvIncludeGlobs, "concepts/*.mdx")
	t.Setenv(EnvExcludeGlobs, "")
	sub := NewSiteSubstituter("")
	if got, want := sub.Root(), filepath.Join("testdata", "site"); got != want {
		t.Fatalf("Root() = %q, want %q", got, want)
	}
	got, err := sub.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	assertContains(t, got, "concepts/job.mdx")
	assertNotContains(t, got, "getting-started/quickstart.md")
}

func TestSiteSubstituterMissingPathFailsClosed(t *testing.T) {
	sub := NewSiteSubstituter(filepath.Join("testdata", "missing-site"))
	_, err := sub.Load(context.Background())
	if err == nil {
		t.Fatal("Load() error = nil, want stat error")
	}
	if !strings.Contains(err.Error(), "stat site content") {
		t.Fatalf("Load() error = %q, want stat site content", err)
	}
}

func TestSiteSubstituterNoMarkdownFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := NewSiteSubstituter(dir)
	_, err := sub.Load(context.Background())
	if err == nil {
		t.Fatal("Load() error = nil, want no markdown files error")
	}
	if !strings.Contains(err.Error(), "no markdown files") {
		t.Fatalf("Load() error = %q, want no markdown files", err)
	}
}

func TestSiteSubstituterRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sub := NewSiteSubstituter(filepath.Join("testdata", "site"))
	_, err := sub.Load(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want context.Canceled", err)
	}
}

func TestSiteSubstituterBudgetRefusesOversizedBlob(t *testing.T) {
	sub := NewSiteSubstituter(filepath.Join("testdata", "site"), WithSiteTokenLimits(1, 2))
	_, err := sub.Load(context.Background())
	if err == nil {
		t.Fatal("Load() error = nil, want token-budget error")
	}
	if !strings.Contains(err.Error(), "exceeds token budget") {
		t.Fatalf("Load() error = %q, want exceeds token budget", err)
	}
}

func TestStripMDXMarkupPreservesCodeFences(t *testing.T) {
	got := stripMDXMarkup("import X from 'x'\n\n<Tabs>\n# Title\n```tsx\n<Tabs />\n```\n</Tabs>\n")
	assertContains(t, got, "# Title")
	assertContains(t, got, "```tsx\n<Tabs />\n```")
	assertNotContains(t, got, "import X")
}
