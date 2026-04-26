package llmchat

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// envCordumIOPath overrides the default cordum.io content glob.
const (
	envCordumIOPath     = "LLMCHAT_CORDUM_IO_PATH"
	defaultCordumIOPath = "/etc/cordum-llm-chat/cordum-io"
)

// frontmatterPattern matches a leading YAML frontmatter block of the
// form `---\n<yaml>\n---\n`. Compiled once at package level.
var frontmatterPattern = regexp.MustCompile(`(?s)\A---\n.*?\n---\n`)

// jsxSelfClosingPattern matches self-closing JSX tags like `<Tabs/>`
// or `<MyComponent foo="bar" />`. Lowercase tags are markdown HTML
// passthrough (e.g. <br/>) and are kept; only PascalCase tags are
// stripped.
var jsxSelfClosingPattern = regexp.MustCompile(`(?s)<[A-Z][A-Za-z0-9]*(?:\s+[^>]*)?/>`)

// jsxPairedPattern matches paired JSX tags like
// `<Tabs>...</Tabs>`. Non-greedy; same-name nesting loses inner
// content (acceptable for the curated subset, rail #6).
var jsxPairedPattern = regexp.MustCompile(`(?s)<([A-Z][A-Za-z0-9]*)(?:\s+[^>]*)?>.*?</[A-Z][A-Za-z0-9]*>`)

// NewCordumIOSubstituter returns a Substituter closure that walks a
// directory of curated markdown files and emits a single text blob
// with section headers per file. Path resolution: arg → env →
// defaultCordumIOPath. The argument can be either a directory (walked
// recursively for *.md / *.mdx) or a glob pattern (filepath.Glob
// expanded directly).
//
// Frontmatter and PascalCase JSX components are stripped; markdown
// prose, headings, code fences, and lowercase HTML pass through.
//
// Errors are graceful per file: a single unreadable file is logged
// and skipped without aborting the entire walk (rail #6 — curated
// subset; one bad file MUST NOT abort the whole substituter).
func NewCordumIOSubstituter(path string) Substituter {
	resolved := path
	if resolved == "" {
		resolved = os.Getenv(envCordumIOPath)
	}
	if resolved == "" {
		resolved = defaultCordumIOPath
	}

	return func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		files, err := expandCordumIOPaths(resolved)
		if err != nil {
			slog.Warn("llmchat/knowledgepack: cordum_io_expand_failed", "path", resolved, "err", err)
			return "", nil
		}
		if len(files) == 0 {
			slog.Warn("llmchat/knowledgepack: cordum_io_no_files", "path", resolved)
			return "", nil
		}

		var out strings.Builder
		out.WriteString("# cordum.io knowledge\n\n")
		for _, f := range files {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			body, err := os.ReadFile(filepath.Clean(f))
			if err != nil {
				slog.Warn("llmchat/knowledgepack: cordum_io_file_skipped", "path", f, "err", err)
				continue
			}
			text := stripCordumIOMarkup(string(body))
			if strings.TrimSpace(text) == "" {
				continue
			}
			heading := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
			fmt.Fprintf(&out, "## %s\n\n%s\n\n---\n\n", heading, text)
		}
		return out.String(), nil
	}
}

// expandCordumIOPaths resolves the input path into a sorted list of
// markdown files. Two strategies, in order:
//  1. If the path is a directory, recursively walk it and collect
//     `.md` + `.mdx` files.
//  2. Otherwise treat the path as a glob and use filepath.Glob.
//
// The result is path-traversal-safe: any path containing `..` after
// filepath.Clean is rejected.
func expandCordumIOPaths(path string) ([]string, error) {
	cleaned := filepath.Clean(path)
	if strings.Contains(cleaned, "..") {
		return nil, fmt.Errorf("path %q contains traversal", path)
	}

	if info, err := os.Stat(cleaned); err == nil && info.IsDir() {
		return walkMarkdownDir(cleaned)
	}

	matches, err := filepath.Glob(cleaned)
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", cleaned, err)
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if isMarkdownExt(m) {
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out, nil
}

func walkMarkdownDir(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // tolerate per-entry walk errors
		}
		if d.IsDir() {
			return nil
		}
		if isMarkdownExt(p) {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %q: %w", root, err)
	}
	sort.Strings(out)
	return out, nil
}

func isMarkdownExt(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return ext == ".md" || ext == ".mdx"
}

// stripCordumIOMarkup strips Docusaurus-specific markup (frontmatter,
// PascalCase JSX components) while preserving markdown prose.
func stripCordumIOMarkup(text string) string {
	text = frontmatterPattern.ReplaceAllString(text, "")
	text = jsxPairedPattern.ReplaceAllString(text, "")
	text = jsxSelfClosingPattern.ReplaceAllString(text, "")
	return text
}
