package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cordum/cordum/core/mcp"
	"gopkg.in/yaml.v3"
)

const (
	// APISummaryPlaceholder is the system-prompt token filled by
	// APISubstituter.
	APISummaryPlaceholder = "{{api_summary}}"

	// EnvAPISpecPath is the production env var for the local OpenAPI 3
	// YAML file mounted into cordum-llm-chat.
	EnvAPISpecPath = "LLMCHAT_KNOWLEDGE_API_SPEC_PATH"

	// DefaultAPISpecPath is the in-container read-only mount path used by
	// Compose and Helm.
	DefaultAPISpecPath = "/etc/cordum/openapi.yaml"

	defaultAPITargetTokens = 8000
	defaultAPIMaxTokens    = 12000

	// approximateBytesPerToken is the shared deterministic token estimator.
	// Qwen/Ollama tokenization is model-specific; four bytes per token is a
	// conservative rule of thumb for the ASCII-heavy API/docs corpus.
	approximateBytesPerToken = 4
)

// APISubstituter reads a local OpenAPI 3 YAML file and renders a compact,
// deterministic Cordum API summary for insertion into the system prompt.
//
// It performs local file IO only; no network retrieval is permitted here.
type APISubstituter struct {
	path         string
	targetTokens int
	maxTokens    int
	redactor     mcp.ArgumentRedactor
}

// APISubstituterOption customizes APISubstituter for tests or future config
// wiring.
type APISubstituterOption func(*APISubstituter)

// WithAPITokenLimits overrides target and hard-max token ceilings. Non-positive
// values keep their defaults.
func WithAPITokenLimits(target, max int) APISubstituterOption {
	return func(s *APISubstituter) {
		if target > 0 {
			s.targetTokens = target
		}
		if max > 0 {
			s.maxTokens = max
		}
	}
}

// WithAPIRedactor overrides the redactor. Passing nil disables redaction and
// should only be used in tests.
func WithAPIRedactor(redactor mcp.ArgumentRedactor) APISubstituterOption {
	return func(s *APISubstituter) {
		s.redactor = redactor
	}
}

// NewAPISubstituter constructs an APISubstituter. Path resolution is:
// explicit argument -> LLMCHAT_KNOWLEDGE_API_SPEC_PATH -> /etc/cordum/openapi.yaml.
func NewAPISubstituter(path string, opts ...APISubstituterOption) *APISubstituter {
	if strings.TrimSpace(path) == "" {
		path = strings.TrimSpace(os.Getenv(EnvAPISpecPath))
	}
	if strings.TrimSpace(path) == "" {
		path = DefaultAPISpecPath
	}
	s := &APISubstituter{
		path:         path,
		targetTokens: defaultAPITargetTokens,
		maxTokens:    defaultAPIMaxTokens,
		redactor:     mcp.DefaultRedactor(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Path returns the resolved local OpenAPI file path.
func (s *APISubstituter) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Load implements the PromptLoader-shaped contract: it returns the
// placeholder replacement blob.
func (s *APISubstituter) Load(ctx context.Context) (string, error) {
	if s == nil {
		return "", errors.New("llmchat knowledge api substituter is nil")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	spec, err := loadOpenAPISpec(ctx, s.path)
	if err != nil {
		return "", err
	}

	full := s.redact(renderOpenAPISummary(spec, false))
	if estimateTokens(full) <= s.targetTokens {
		return full, nil
	}

	compact := s.redact(renderOpenAPISummary(spec, true))
	if tokens := estimateTokens(compact); tokens > s.maxTokens {
		return "", fmt.Errorf("llmchat knowledge api summary exceeds token budget: tokens=%d max=%d path=%s", tokens, s.maxTokens, s.path)
	}
	return compact, nil
}

// Substitute replaces {{api_summary}} in template with the loaded API summary.
func (s *APISubstituter) Substitute(ctx context.Context, template string) (string, error) {
	blob, err := s.Load(ctx)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(template, APISummaryPlaceholder, blob), nil
}

func (s *APISubstituter) redact(text string) string {
	return redactKnowledgeText(s.redactor, text)
}

type openAPISpec struct {
	Security   []map[string][]any              `yaml:"security"`
	Paths      map[string]map[string]yaml.Node `yaml:"paths"`
	Components openAPIComponents               `yaml:"components"`
}

type openAPIComponents struct {
	Parameters map[string]openAPIParameter `yaml:"parameters"`
	Responses  map[string]openAPIResponse  `yaml:"responses"`
	Schemas    map[string]openAPISchema    `yaml:"schemas"`
}

type openAPIOperation struct {
	Summary     string                     `yaml:"summary"`
	Description string                     `yaml:"description"`
	OperationID string                     `yaml:"operationId"`
	Security    []map[string][]any         `yaml:"security"`
	Parameters  []openAPIParameter         `yaml:"parameters"`
	RequestBody *openAPIRequestBody        `yaml:"requestBody"`
	Responses   map[string]openAPIResponse `yaml:"responses"`
}

type openAPIParameter struct {
	Ref         string           `yaml:"$ref"`
	Name        string           `yaml:"name"`
	In          string           `yaml:"in"`
	Required    bool             `yaml:"required"`
	Description string           `yaml:"description"`
	Schema      openAPISchemaRef `yaml:"schema"`
}

type openAPIRequestBody struct {
	Ref         string                         `yaml:"$ref"`
	Required    bool                           `yaml:"required"`
	Description string                         `yaml:"description"`
	Content     map[string]openAPIContentEntry `yaml:"content"`
}

type openAPIResponse struct {
	Ref         string                         `yaml:"$ref"`
	Description string                         `yaml:"description"`
	Headers     map[string]openAPIHeader       `yaml:"headers"`
	Content     map[string]openAPIContentEntry `yaml:"content"`
}

type openAPIHeader struct {
	Description string           `yaml:"description"`
	Schema      openAPISchemaRef `yaml:"schema"`
}

type openAPIContentEntry struct {
	Schema openAPISchemaRef `yaml:"schema"`
}

type openAPISchema struct {
	Ref         string                      `yaml:"$ref"`
	Type        string                      `yaml:"type"`
	Title       string                      `yaml:"title"`
	Description string                      `yaml:"description"`
	Items       *openAPISchemaRef           `yaml:"items"`
	Properties  map[string]openAPISchemaRef `yaml:"properties"`
	AllOf       []openAPISchemaRef          `yaml:"allOf"`
	AnyOf       []openAPISchemaRef          `yaml:"anyOf"`
	OneOf       []openAPISchemaRef          `yaml:"oneOf"`
}

type openAPISchemaRef struct {
	Ref         string                      `yaml:"$ref"`
	Type        string                      `yaml:"type"`
	Title       string                      `yaml:"title"`
	Description string                      `yaml:"description"`
	Items       *openAPISchemaRef           `yaml:"items"`
	Properties  map[string]openAPISchemaRef `yaml:"properties"`
	AllOf       []openAPISchemaRef          `yaml:"allOf"`
	AnyOf       []openAPISchemaRef          `yaml:"anyOf"`
	OneOf       []openAPISchemaRef          `yaml:"oneOf"`
}

func loadOpenAPISpec(ctx context.Context, path string) (openAPISpec, error) {
	var spec openAPISpec
	if err := ctx.Err(); err != nil {
		return spec, err
	}
	cleaned := filepath.Clean(path)
	body, err := os.ReadFile(cleaned)
	if err != nil {
		return spec, fmt.Errorf("read OpenAPI spec %s: %w", cleaned, err)
	}
	if err := yaml.Unmarshal(body, &spec); err != nil {
		return spec, fmt.Errorf("parse OpenAPI spec %s: %w", cleaned, err)
	}
	if len(spec.Paths) == 0 {
		return spec, fmt.Errorf("parse OpenAPI spec %s: no paths found", cleaned)
	}
	return spec, nil
}

func renderOpenAPISummary(spec openAPISpec, compact bool) string {
	var b strings.Builder
	b.WriteString("# Cordum API summary\n")
	b.WriteString("Source: local OpenAPI 3 spec. Each entry lists method/path, purpose, auth, key schemas, and rate-limit metadata when present.\n\n")

	paths := make([]string, 0, len(spec.Paths))
	for path := range spec.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		methods := make([]string, 0, len(spec.Paths[path]))
		for method := range spec.Paths[path] {
			if isOpenAPIMethod(method) {
				methods = append(methods, method)
			}
		}
		sort.Strings(methods)
		for _, method := range methods {
			var op openAPIOperation
			node := spec.Paths[path][method]
			if err := node.Decode(&op); err != nil {
				continue
			}
			b.WriteString(formatOpenAPIOperation(spec, method, path, op, compact))
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func isOpenAPIMethod(method string) bool {
	switch strings.ToLower(method) {
	case "get", "put", "post", "delete", "patch", "head", "options", "trace":
		return true
	default:
		return false
	}
}

func formatOpenAPIOperation(spec openAPISpec, method, path string, op openAPIOperation, compact bool) string {
	var parts []string
	head := strings.ToUpper(method) + " " + path
	if summary := firstNonEmpty(op.Summary, op.OperationID); summary != "" {
		head += " — " + oneLine(summary)
	}
	parts = append(parts, head)

	security := op.Security
	if security == nil {
		security = spec.Security
	}
	if auth := formatOpenAPISecurity(security); auth != "" {
		parts = append(parts, "auth: "+auth)
	}
	if req := formatRequestSchemas(spec, op); req != "" {
		parts = append(parts, "request: "+req)
	}
	if params := formatRequiredParams(spec, op.Parameters, compact); params != "" {
		parts = append(parts, "required: "+params)
	}
	if resp := formatResponseSchemas(spec, op.Responses, compact); resp != "" {
		parts = append(parts, "responses: "+resp)
	}
	if limit := formatRateLimitMetadata(spec, op.Responses); limit != "" {
		parts = append(parts, "rate-limit: "+limit)
	}
	return "- " + strings.Join(parts, "; ")
}

func formatOpenAPISecurity(security []map[string][]any) string {
	if len(security) == 0 {
		return "public"
	}
	seen := make(map[string]struct{})
	for _, req := range security {
		if len(req) == 0 {
			seen["public"] = struct{}{}
			continue
		}
		for name := range req {
			seen[name] = struct{}{}
		}
	}
	return joinSortedKeys(seen, ",")
}

func formatRequestSchemas(spec openAPISpec, op openAPIOperation) string {
	if op.RequestBody == nil {
		return ""
	}
	names := schemaNamesFromContent(op.RequestBody.Content)
	return formatSchemaNames(spec, names, false)
}

func formatRequiredParams(spec openAPISpec, params []openAPIParameter, compact bool) string {
	var out []string
	for _, p := range params {
		p = resolveParameter(spec, p)
		if !p.Required {
			continue
		}
		name := p.Name
		if p.In != "" {
			name += " in " + p.In
		}
		if !compact && p.Description != "" {
			name += " (" + oneLine(p.Description) + ")"
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func formatResponseSchemas(spec openAPISpec, responses map[string]openAPIResponse, compact bool) string {
	if len(responses) == 0 {
		return ""
	}
	codes := make([]string, 0, len(responses))
	for code := range responses {
		if compact && !isSuccessStatusCode(code) {
			continue
		}
		codes = append(codes, code)
	}
	sort.Strings(codes)
	if compact && len(codes) == 0 {
		fallback := make([]string, 0, len(responses))
		for code := range responses {
			fallback = append(fallback, code)
		}
		sort.Strings(fallback)
		if len(fallback) > 0 {
			codes = append(codes, fallback[0])
		}
	}

	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		resp := resolveResponse(spec, responses[code])
		item := code
		if names := schemaNamesFromContent(resp.Content); len(names) > 0 {
			item += " " + formatSchemaNames(spec, names, compact)
		} else if compact || resp.Description == "" {
			// Keep status code only.
		} else {
			item += " " + oneLine(resp.Description)
		}
		parts = append(parts, item)
	}
	return strings.Join(parts, ", ")
}

func isSuccessStatusCode(code string) bool {
	return strings.HasPrefix(code, "2")
}

func formatRateLimitMetadata(spec openAPISpec, responses map[string]openAPIResponse) string {
	seen := make(map[string]struct{})
	for code, resp := range responses {
		resp = resolveResponse(spec, resp)
		if code == "429" {
			seen["HTTP 429"] = struct{}{}
		}
		for header := range resp.Headers {
			if isRateLimitHeader(header) {
				seen[header] = struct{}{}
			}
		}
	}
	return joinSortedKeys(seen, ",")
}

func isRateLimitHeader(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "ratelimit") ||
		strings.Contains(lower, "rate-limit") ||
		lower == "retry-after"
}

func schemaNamesFromContent(content map[string]openAPIContentEntry) []string {
	seen := make(map[string]struct{})
	for _, media := range content {
		for _, name := range schemaNames(media.Schema) {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func schemaNames(ref openAPISchemaRef) []string {
	var names []string
	if ref.Ref != "" {
		names = append(names, shortRefName(ref.Ref))
	}
	if ref.Title != "" {
		names = append(names, ref.Title)
	}
	if ref.Type != "" && len(names) == 0 {
		names = append(names, ref.Type)
	}
	if ref.Items != nil {
		names = append(names, schemaNames(*ref.Items)...)
	}
	for _, child := range ref.AllOf {
		names = append(names, schemaNames(child)...)
	}
	for _, child := range ref.AnyOf {
		names = append(names, schemaNames(child)...)
	}
	for _, child := range ref.OneOf {
		names = append(names, schemaNames(child)...)
	}
	for _, child := range ref.Properties {
		names = append(names, schemaNames(child)...)
	}
	return uniqueSorted(names)
}

func formatSchemaNames(spec openAPISpec, names []string, compact bool) string {
	var out []string
	for _, name := range uniqueSorted(names) {
		if !compact {
			if desc := schemaDescription(spec, name); desc != "" {
				out = append(out, name+" ("+oneLine(desc)+")")
				continue
			}
		}
		out = append(out, name)
	}
	return strings.Join(out, ", ")
}

func schemaDescription(spec openAPISpec, name string) string {
	if spec.Components.Schemas == nil {
		return ""
	}
	schema, ok := spec.Components.Schemas[name]
	if !ok {
		return ""
	}
	return firstNonEmpty(schema.Description, schema.Title)
}

func resolveParameter(spec openAPISpec, p openAPIParameter) openAPIParameter {
	if p.Ref == "" || spec.Components.Parameters == nil {
		return p
	}
	name := shortRefName(p.Ref)
	if resolved, ok := spec.Components.Parameters[name]; ok {
		return resolved
	}
	return p
}

func resolveResponse(spec openAPISpec, r openAPIResponse) openAPIResponse {
	if r.Ref == "" || spec.Components.Responses == nil {
		return r
	}
	name := shortRefName(r.Ref)
	if resolved, ok := spec.Components.Responses[name]; ok {
		return resolved
	}
	return r
}

func shortRefName(ref string) string {
	idx := strings.LastIndex(ref, "/")
	if idx < 0 {
		return ref
	}
	return ref[idx+1:]
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func joinSortedKeys(seen map[string]struct{}, sep string) string {
	if len(seen) == 0 {
		return ""
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, sep)
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{})
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func redactKnowledgeText(redactor mcp.ArgumentRedactor, text string) string {
	if redactor == nil || text == "" {
		return text
	}
	payload, err := json.Marshal(map[string]string{"content": text})
	if err != nil {
		return text
	}
	redacted := redactor.Redact(payload)
	var out map[string]string
	if err := json.Unmarshal(redacted, &out); err != nil {
		return text
	}
	if content, ok := out["content"]; ok {
		return content
	}
	return text
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + approximateBytesPerToken - 1) / approximateBytesPerToken
}
