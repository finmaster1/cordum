package knowledge

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cordum/cordum/core/mcp"
)

const (
	// CordumIOSummaryPlaceholder is the system-prompt token filled by
	// SiteSubstituter.
	CordumIOSummaryPlaceholder = "{{cordum_io_summary}}"

	// EnvSitePath is the production env var for the local checked-in or
	// mounted cordum.io/Docusaurus content directory.
	EnvSitePath = "LLMCHAT_KNOWLEDGE_SITE_PATH"

	// EnvIncludeGlobs optionally narrows the local site files to ingest.
	EnvIncludeGlobs = "LLMCHAT_KNOWLEDGE_INCLUDE_GLOBS"

	// EnvExcludeGlobs optionally removes local site files from ingestion.
	EnvExcludeGlobs = "LLMCHAT_KNOWLEDGE_EXCLUDE_GLOBS"

	// DefaultSitePath is the in-container read-only mount path used by Compose
	// and Helm.
	DefaultSitePath = "/etc/cordum/site-content/"

	defaultSiteTargetTokens = 6000
	defaultSiteMaxTokens    = 9000
)

var (
	jsxSelfClosingPattern = regexp.MustCompile(`<[A-Z][A-Za-z0-9]*(?:\s+[^>]*)?/>`)
	jsxOpenClosePattern   = regexp.MustCompile(`</?[A-Z][A-Za-z0-9]*(?:\s+[^>]*)?>`)
	mdxExpressionComment  = regexp.MustCompile(`\{/\*.*?\*/\}`)
	htmlCommentPattern    = regexp.MustCompile(`<!--.*?-->`)
)

// SiteSubstituter reads local cordum.io/Docusaurus markdown or MDX files and
// renders a compact, deterministic site summary for insertion into the system
// prompt.
type SiteSubstituter struct {
	root         string
	includeGlobs []string
	excludeGlobs []string
	targetTokens int
	maxTokens    int
	redactor     mcp.ArgumentRedactor
}

// SiteSubstituterOption customizes SiteSubstituter for tests or future config
// wiring.
type SiteSubstituterOption func(*SiteSubstituter)

// WithSiteGlobs overrides include/exclude glob lists. Globs match slash-form
// relative paths from the configured root.
func WithSiteGlobs(include, exclude []string) SiteSubstituterOption {
	return func(s *SiteSubstituter) {
		s.includeGlobs = normalizeGlobs(include)
		s.excludeGlobs = normalizeGlobs(exclude)
	}
}

// WithSiteTokenLimits overrides target and hard-max token ceilings. Non-positive
// values keep their defaults.
func WithSiteTokenLimits(target, max int) SiteSubstituterOption {
	return func(s *SiteSubstituter) {
		if target > 0 {
			s.targetTokens = target
		}
		if max > 0 {
			s.maxTokens = max
		}
	}
}

// WithSiteRedactor overrides the redactor. Passing nil disables redaction and
// should only be used in tests.
func WithSiteRedactor(redactor mcp.ArgumentRedactor) SiteSubstituterOption {
	return func(s *SiteSubstituter) {
		s.redactor = redactor
	}
}

// NewSiteSubstituter constructs a SiteSubstituter. Path resolution is:
// explicit argument -> LLMCHAT_KNOWLEDGE_SITE_PATH -> /etc/cordum/site-content/.
func NewSiteSubstituter(root string, opts ...SiteSubstituterOption) *SiteSubstituter {
	if strings.TrimSpace(root) == "" {
		root = strings.TrimSpace(os.Getenv(EnvSitePath))
	}
	if strings.TrimSpace(root) == "" {
		root = DefaultSitePath
	}
	s := &SiteSubstituter{
		root:         root,
		includeGlobs: normalizeGlobs(splitEnvList(os.Getenv(EnvIncludeGlobs))),
		excludeGlobs: normalizeGlobs(splitEnvList(os.Getenv(EnvExcludeGlobs))),
		targetTokens: defaultSiteTargetTokens,
		maxTokens:    defaultSiteMaxTokens,
		redactor:     mcp.DefaultRedactor(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Root returns the resolved local site content root.
func (s *SiteSubstituter) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

// Load implements the PromptLoader-shaped contract: it returns the
// placeholder replacement blob.
func (s *SiteSubstituter) Load(ctx context.Context) (string, error) {
	if s == nil {
		return "", errors.New("llmchat knowledge site substituter is nil")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	files, err := s.siteFiles()
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", fmt.Errorf("llmchat knowledge site content has no markdown files under %s", s.root)
	}

	var b strings.Builder
	b.WriteString("# cordum.io knowledge summary\n")
	b.WriteString("Source: local checked-in docs content. Headings, prose, and code fences are preserved; MDX layout tags are removed.\n")
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		body, err := os.ReadFile(file.abs)
		if err != nil {
			return "", fmt.Errorf("read site content %s: %w", file.abs, err)
		}
		cleaned := strings.TrimSpace(stripMDXMarkup(string(body)))
		if cleaned == "" {
			continue
		}
		b.WriteString("\n## ")
		b.WriteString(file.rel)
		b.WriteString("\n\n")
		b.WriteString(cleaned)
		b.WriteString("\n")
	}

	out := s.redact(b.String())
	if estimateTokens(out) > s.targetTokens {
		out = compressSiteSummary(out, s.targetTokens)
	}
	if tokens := estimateTokens(out); tokens > s.maxTokens {
		return "", fmt.Errorf("llmchat knowledge site summary exceeds token budget: tokens=%d max=%d path=%s", tokens, s.maxTokens, s.root)
	}
	return out, nil
}

// Substitute replaces {{cordum_io_summary}} in template with the loaded site
// summary.
func (s *SiteSubstituter) Substitute(ctx context.Context, template string) (string, error) {
	blob, err := s.Load(ctx)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(template, CordumIOSummaryPlaceholder, blob), nil
}

func (s *SiteSubstituter) redact(text string) string {
	return redactKnowledgeText(s.redactor, text)
}

type siteFile struct {
	abs string
	rel string
}

func (s *SiteSubstituter) siteFiles() ([]siteFile, error) {
	root := filepath.Clean(s.root)
	if hasDotDotPathElement(root) {
		return nil, fmt.Errorf("site content path %q contains traversal", s.root)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat site content %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("site content path %s is not a directory", root)
	}

	var files []siteFile
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !isMarkdownFile(path) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if hasDotDotPathElement(rel) {
			return fmt.Errorf("site content relative path %q contains traversal", rel)
		}
		if !matchesGlobs(rel, s.includeGlobs, true) {
			return nil
		}
		if matchesGlobs(rel, s.excludeGlobs, false) {
			return nil
		}
		files = append(files, siteFile{abs: path, rel: rel})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk site content %s: %w", root, err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	return files, nil
}

func isMarkdownFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".mdx":
		return true
	default:
		return false
	}
}

func stripMDXMarkup(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	lines := strings.Split(input, "\n")

	var out []string
	inFence := false
	inFrontmatter := false
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		inFrontmatter = true
		lines = lines[1:]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inFrontmatter {
			if trimmed == "---" {
				inFrontmatter = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			out = append(out, strings.TrimRight(line, " \t"))
			continue
		}
		if inFence {
			out = append(out, strings.TrimRight(line, " \t"))
			continue
		}
		if trimmed == "" {
			out = append(out, "")
			continue
		}
		if strings.HasPrefix(trimmed, "import ") || strings.HasPrefix(trimmed, "export ") {
			continue
		}
		line = mdxExpressionComment.ReplaceAllString(line, "")
		line = htmlCommentPattern.ReplaceAllString(line, "")
		line = jsxSelfClosingPattern.ReplaceAllString(line, "")
		line = jsxOpenClosePattern.ReplaceAllString(line, "")
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return collapseExcessBlankLines(strings.Join(out, "\n"))
}

func collapseExcessBlankLines(input string) string {
	var out []string
	blank := 0
	for _, line := range strings.Split(input, "\n") {
		if strings.TrimSpace(line) == "" {
			blank++
			if blank > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blank = 0
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func compressSiteSummary(input string, targetTokens int) string {
	maxBytes := targetTokens * approximateBytesPerToken
	if maxBytes <= 0 || len(input) <= maxBytes {
		return input
	}
	marker := "\n\n[knowledge-pack truncated deterministically to fit token budget]\n"
	limit := maxBytes - len(marker)
	if limit <= 0 {
		return marker
	}
	cut := strings.LastIndex(input[:limit], "\n")
	if cut <= 0 {
		cut = limit
	}
	return strings.TrimSpace(input[:cut]) + marker
}

func splitEnvList(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func normalizeGlobs(globs []string) []string {
	out := make([]string, 0, len(globs))
	seen := make(map[string]struct{}, len(globs))
	for _, glob := range globs {
		glob = filepath.ToSlash(strings.TrimSpace(glob))
		if glob == "" {
			continue
		}
		if _, ok := seen[glob]; ok {
			continue
		}
		seen[glob] = struct{}{}
		out = append(out, glob)
	}
	sort.Strings(out)
	return out
}

func matchesGlobs(rel string, globs []string, defaultWhenEmpty bool) bool {
	if len(globs) == 0 {
		return defaultWhenEmpty
	}
	rel = filepath.ToSlash(rel)
	for _, glob := range globs {
		if globMatch(glob, rel) {
			return true
		}
	}
	return false
}

func globMatch(pattern, rel string) bool {
	pattern = filepath.ToSlash(pattern)
	if ok, _ := filepath.Match(pattern, rel); ok {
		return true
	}
	if strings.HasPrefix(pattern, "**/") {
		tail := strings.TrimPrefix(pattern, "**/")
		if ok, _ := filepath.Match(tail, rel); ok {
			return true
		}
		if ok, _ := filepath.Match(tail, filepath.Base(rel)); ok {
			return true
		}
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return rel == prefix || strings.HasPrefix(rel, prefix+"/")
	}
	if strings.Contains(pattern, "**/") {
		parts := strings.Split(pattern, "**/")
		return strings.HasPrefix(rel, strings.TrimSuffix(parts[0], "/")+"/") &&
			globMatch(parts[len(parts)-1], filepath.Base(rel))
	}
	return false
}

func hasDotDotPathElement(path string) bool {
	for _, part := range strings.FieldsFunc(filepath.ToSlash(path), func(r rune) bool { return r == '/' }) {
		if part == ".." {
			return true
		}
	}
	return false
}
