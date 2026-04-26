package llmchat

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// envOpenAPIPath overrides the default OpenAPI spec path. Empty falls
// back to defaultOpenAPIPath (set by Compose/Helm via a read-only
// bind mount of cordum's `make openapi` output).
const (
	envOpenAPIPath     = "LLMCHAT_OPENAPI_PATH"
	defaultOpenAPIPath = "/etc/cordum-llm-chat/openapi.yaml"
)

// openAPISpec is the permissive subset of the OpenAPI 3 schema this
// substituter consumes. We deliberately decode only the fields needed
// for the compact summary; everything else (request bodies, full
// schemas, examples) is dropped to keep the budget tight.
type openAPISpec struct {
	Paths map[string]map[string]openAPIOperation `yaml:"paths"`
}

type openAPIOperation struct {
	Summary     string                     `yaml:"summary"`
	OperationID string                     `yaml:"operationId"`
	Security    []map[string][]any         `yaml:"security"`
	Parameters  []openAPIParameter         `yaml:"parameters"`
	Responses   map[string]openAPIResponse `yaml:"responses"`
}

type openAPIParameter struct {
	Name     string `yaml:"name"`
	In       string `yaml:"in"`
	Required bool   `yaml:"required"`
}

type openAPIResponse struct {
	Description string                            `yaml:"description"`
	Content     map[string]openAPIResponseContent `yaml:"content"`
}

type openAPIResponseContent struct {
	Schema openAPISchemaRef `yaml:"schema"`
}

type openAPISchemaRef struct {
	Ref string `yaml:"$ref"`
}

// NewOpenAPISubstituter returns a Substituter closure that reads the
// OpenAPI spec on each (cached) invocation and emits a compact
// per-endpoint summary. path resolution: arg → env → default.
//
// All errors are graceful: missing/malformed file → ("", nil) +
// slog.Warn so the LLM still gets the rest of the prompt and the
// service stays operational. Rail #4 honoured — only LOCAL file IO,
// no HTTP fetches.
func NewOpenAPISubstituter(path string) Substituter {
	resolved := path
	if resolved == "" {
		resolved = os.Getenv(envOpenAPIPath)
	}
	if resolved == "" {
		resolved = defaultOpenAPIPath
	}

	return func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		body, err := os.ReadFile(filepath.Clean(resolved))
		if err != nil {
			if os.IsNotExist(err) {
				slog.Warn("llmchat/knowledgepack: openapi_file_missing", "path", resolved)
				return "", nil
			}
			slog.Error("llmchat/knowledgepack: openapi_read_failed", "path", resolved, "err", err)
			return "", nil
		}

		var spec openAPISpec
		if err := yaml.Unmarshal(body, &spec); err != nil {
			slog.Error("llmchat/knowledgepack: openapi_parse_failed", "path", resolved, "err", err)
			return "", nil
		}
		if len(spec.Paths) == 0 {
			slog.Warn("llmchat/knowledgepack: openapi_no_paths", "path", resolved)
			return "", nil
		}

		return renderOpenAPISummary(spec), nil
	}
}

// renderOpenAPISummary walks the parsed spec deterministically and
// emits one line per (path, method) operation with summary, auth
// scheme(s), required parameters, and response status codes (with
// $ref short-name for the success body).
func renderOpenAPISummary(spec openAPISpec) string {
	pathKeys := make([]string, 0, len(spec.Paths))
	for p := range spec.Paths {
		pathKeys = append(pathKeys, p)
	}
	sort.Strings(pathKeys)

	var out strings.Builder
	out.WriteString("# Cordum API summary\n\n")
	for _, path := range pathKeys {
		methods := spec.Paths[path]
		methodKeys := make([]string, 0, len(methods))
		for m := range methods {
			methodKeys = append(methodKeys, m)
		}
		sort.Strings(methodKeys)
		for _, method := range methodKeys {
			op := methods[method]
			out.WriteString(formatOpenAPIOperation(method, path, op))
			out.WriteString("\n")
		}
	}
	return out.String()
}

func formatOpenAPIOperation(method, path string, op openAPIOperation) string {
	var b strings.Builder
	b.WriteString(strings.ToUpper(method))
	b.WriteString(" ")
	b.WriteString(path)
	if op.Summary != "" {
		b.WriteString(" — ")
		b.WriteString(op.Summary)
	} else if op.OperationID != "" {
		b.WriteString(" — ")
		b.WriteString(op.OperationID)
	}

	if auth := formatOpenAPISecurity(op.Security); auth != "" {
		b.WriteString("; auth: ")
		b.WriteString(auth)
	}
	if req := formatOpenAPIRequiredParams(op.Parameters); req != "" {
		b.WriteString("; required: ")
		b.WriteString(req)
	}
	if resp := formatOpenAPIResponses(op.Responses); resp != "" {
		b.WriteString("; responses: ")
		b.WriteString(resp)
	}
	return b.String()
}

func formatOpenAPISecurity(security []map[string][]any) string {
	if len(security) == 0 {
		return ""
	}
	keys := map[string]struct{}{}
	for _, schema := range security {
		for k := range schema {
			keys[k] = struct{}{}
		}
	}
	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func formatOpenAPIRequiredParams(params []openAPIParameter) string {
	var required []string
	for _, p := range params {
		if p.Required {
			required = append(required, p.Name)
		}
	}
	sort.Strings(required)
	return strings.Join(required, ",")
}

func formatOpenAPIResponses(responses map[string]openAPIResponse) string {
	codes := make([]string, 0, len(responses))
	for code := range responses {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		resp := responses[code]
		// For success bodies, append the schema $ref short-name (last
		// path segment) so the LLM can ask follow-ups about the type
		// without needing the full schema body.
		ref := ""
		if code == "200" || code == "201" {
			for _, content := range resp.Content {
				if content.Schema.Ref != "" {
					ref = shortRefName(content.Schema.Ref)
					break
				}
			}
		}
		if ref != "" {
			parts = append(parts, fmt.Sprintf("%s(%s)", code, ref))
		} else {
			parts = append(parts, code)
		}
	}
	return strings.Join(parts, ",")
}

func shortRefName(ref string) string {
	idx := strings.LastIndex(ref, "/")
	if idx < 0 {
		return ref
	}
	return ref[idx+1:]
}
