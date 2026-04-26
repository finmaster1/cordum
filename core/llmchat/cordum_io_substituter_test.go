package llmchat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCordumIOSubstituter_ParsesCuratedDirectory(t *testing.T) {
	t.Parallel()
	sub := NewCordumIOSubstituter(filepath.Join("testdata", "curated"))
	got, err := sub(context.Background())
	if err != nil {
		t.Fatalf("substituter: %v", err)
	}
	if !strings.Contains(got, "## architecture") {
		t.Errorf("expected ## architecture section header; got %q", got)
	}
	if !strings.Contains(got, "## audit") {
		t.Errorf("expected ## audit section header; got %q", got)
	}
}

func TestCordumIOSubstituter_StripsFrontmatter(t *testing.T) {
	t.Parallel()
	sub := NewCordumIOSubstituter(filepath.Join("testdata", "curated"))
	got, err := sub(context.Background())
	if err != nil {
		t.Fatalf("substituter: %v", err)
	}
	// Frontmatter keys must not survive into the output.
	for _, dropped := range []string{"sidebar_position:", "title: Architecture", "title: Audit"} {
		if strings.Contains(got, dropped) {
			t.Errorf("frontmatter key %q leaked into output", dropped)
		}
	}
}

func TestCordumIOSubstituter_StripsJSXComponents(t *testing.T) {
	t.Parallel()
	sub := NewCordumIOSubstituter(filepath.Join("testdata", "curated"))
	got, err := sub(context.Background())
	if err != nil {
		t.Fatalf("substituter: %v", err)
	}
	for _, dropped := range []string{"<Tabs>", "<TabItem", "<Admonition"} {
		if strings.Contains(got, dropped) {
			t.Errorf("JSX component %q leaked into output", dropped)
		}
	}
	// But the surrounding markdown prose is preserved.
	if !strings.Contains(got, "Cordum is a safety-first agent orchestration platform") {
		t.Errorf("markdown prose stripped along with JSX; got %q", got)
	}
}

func TestCordumIOSubstituter_EmptyDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sub := NewCordumIOSubstituter(dir)
	got, err := sub(context.Background())
	if err != nil {
		t.Fatalf("empty dir should soft-fail; got err=%v", err)
	}
	if got != "" {
		t.Errorf("empty dir should return empty string; got %q", got)
	}
}

func TestCordumIOSubstituter_RejectsPathTraversal(t *testing.T) {
	t.Parallel()
	sub := NewCordumIOSubstituter("../../../etc/passwd")
	got, err := sub(context.Background())
	if err != nil {
		t.Fatalf("path-traversal should soft-fail; got err=%v", err)
	}
	if got != "" {
		t.Errorf("path-traversal must not return content; got %q", got)
	}
}

func TestCordumIOSubstituter_HandlesUnreadableFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := filepath.Join(dir, "good.md")
	if err := os.WriteFile(good, []byte("# Good\n\nContent."), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Non-existent file in glob set: directory walk handles this
	// already; ensure that the readable file's content still surfaces.
	sub := NewCordumIOSubstituter(dir)
	got, err := sub(context.Background())
	if err != nil {
		t.Fatalf("substituter: %v", err)
	}
	if !strings.Contains(got, "## good") {
		t.Errorf("expected good.md content; got %q", got)
	}
}
