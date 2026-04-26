// Package eval declares the YAML golden-case schema + JSON-schema-Lite
// arg matcher used by the cordum-llm-chat tool-call eval harness
// (phase 11). The Case struct is intentionally exported so both the
// build-tagged harness (llmchat_tool_eval.go) and the default-tag
// regression tests (yaml_test.go, case_test.go) share one definition.
//
// See tests/eval/cases/SCHEMA.md for the human-facing contract.
package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Case is one golden eval row. The harness loads every *.yaml file
// under tests/eval/cases/ into this shape.
type Case struct {
	Name                     string             `yaml:"name"`
	Category                 string             `yaml:"category"`
	UserMessage              string             `yaml:"user_message,omitempty"`
	Turns                    []Turn             `yaml:"turns,omitempty"`
	ExpectedToolCalls        []ExpectedToolCall `yaml:"expected_tool_calls"`
	ExpectedSummaryContains  []string           `yaml:"expected_summary_contains,omitempty"`
	ForbiddenToolCalls       []string           `yaml:"forbidden_tool_calls,omitempty"`
	MaxToolCalls             int                `yaml:"max_tool_calls"`
	ExpectedToolCallsOrdered bool               `yaml:"expected_tool_calls_ordered,omitempty"`
}

// Turn is one entry in a multi-message conversation. Cases use Turns
// when context carry-over matters (multi_turn category).
type Turn struct {
	Role    string `yaml:"role"`
	Content string `yaml:"content"`
}

// ExpectedToolCall is the harness's pin for a single tool invocation.
// Args is intentionally a free-form map so the YAML author can pin a
// type ("int") or a literal value ("demo.mock-bank.transfer"); see
// MatchArgs.
type ExpectedToolCall struct {
	ToolName string         `yaml:"tool_name"`
	Args     map[string]any `yaml:"args,omitempty"`
}

// Validate checks the case is well-formed. Returns the first issue
// found so authors can fix bugs without playing whack-a-mole.
func (c *Case) Validate() error {
	if c.Name == "" {
		return errors.New("case.name is required")
	}
	if c.Category == "" {
		return errors.New("case.category is required")
	}
	if c.UserMessage == "" && len(c.Turns) == 0 {
		return errors.New("case must set either user_message or turns")
	}
	if c.UserMessage != "" && len(c.Turns) > 0 {
		return errors.New("case must set EITHER user_message OR turns, not both")
	}
	for i, t := range c.Turns {
		if t.Role != "user" && t.Role != "assistant" {
			return fmt.Errorf("turns[%d].role must be 'user' or 'assistant', got %q", i, t.Role)
		}
		if t.Content == "" {
			return fmt.Errorf("turns[%d].content is required", i)
		}
	}
	if c.MaxToolCalls < 0 {
		return errors.New("max_tool_calls must be >= 0")
	}
	for i, exp := range c.ExpectedToolCalls {
		if exp.ToolName == "" {
			return fmt.Errorf("expected_tool_calls[%d].tool_name is required", i)
		}
	}
	return nil
}

// ActualToolCall is the shape the harness extracts from the chat
// response; mirrors core/llmchat.FrameToolDetail{Name, Arguments}.
type ActualToolCall struct {
	Name string
	Args json.RawMessage
}

// MatchArgs returns nil when the actual JSON args satisfy the schema
// declared in the case. Required-key semantics: every key declared in
// `expected` must be present in `actual` with a compatible value.
// Extra keys in `actual` are allowed (the schema pins the minimum
// shape, not the exact shape).
//
// Type-only checks: when `expected[key]` is one of the literals
// "int", "float", "str", "bool", "array", "object", only the JSON
// type of `actual[key]` is verified. Otherwise an exact-string match
// is performed via fmt.Sprint on both sides — useful for pinning
// literal values like workflow_id="demo.mock-bank.transfer".
func MatchArgs(expected map[string]any, actual json.RawMessage) error {
	if expected == nil {
		return nil
	}
	var got map[string]any
	if len(actual) == 0 {
		return errors.New("actual args are empty but expected schema is non-empty")
	}
	if err := json.Unmarshal(actual, &got); err != nil {
		return fmt.Errorf("decode actual args: %w", err)
	}
	return matchObject(expected, got, "")
}

func matchObject(expected, actual map[string]any, path string) error {
	for key, want := range expected {
		fullKey := key
		if path != "" {
			fullKey = path + "." + key
		}
		have, ok := actual[key]
		if !ok {
			return fmt.Errorf("missing required key %q", fullKey)
		}
		if err := matchValue(want, have, fullKey); err != nil {
			return err
		}
	}
	return nil
}

func matchValue(want, have any, path string) error {
	// Nested object: recurse with the inner schemas.
	if subSchema, ok := want.(map[string]any); ok {
		subActual, ok := have.(map[string]any)
		if !ok {
			return fmt.Errorf("key %q expected object, got %T", path, have)
		}
		return matchObject(subSchema, subActual, path)
	}

	// Type-tag literal (string sentinel).
	if tag, ok := want.(string); ok {
		switch tag {
		case "int":
			n, ok := have.(float64) // JSON numbers decode to float64 in Go
			if !ok {
				return fmt.Errorf("key %q expected int, got %T", path, have)
			}
			if n != float64(int64(n)) {
				return fmt.Errorf("key %q expected int, got fractional %v", path, n)
			}
			return nil
		case "float":
			if _, ok := have.(float64); !ok {
				return fmt.Errorf("key %q expected float, got %T", path, have)
			}
			return nil
		case "str":
			if _, ok := have.(string); !ok {
				return fmt.Errorf("key %q expected str, got %T", path, have)
			}
			return nil
		case "bool":
			if _, ok := have.(bool); !ok {
				return fmt.Errorf("key %q expected bool, got %T", path, have)
			}
			return nil
		case "array":
			if _, ok := have.([]any); !ok {
				return fmt.Errorf("key %q expected array, got %T", path, have)
			}
			return nil
		case "object":
			if _, ok := have.(map[string]any); !ok {
				return fmt.Errorf("key %q expected object, got %T", path, have)
			}
			return nil
		}
		// Not a sentinel — fall through to literal compare.
	}

	// Literal compare via fmt.Sprint (handles strings, numbers,
	// booleans without needing to enumerate every JSON type).
	if fmt.Sprint(want) != fmt.Sprint(have) {
		return fmt.Errorf("key %q expected literal %v, got %v", path, want, have)
	}
	return nil
}

// CaseResult is the per-case outcome the harness writes to JSON.
type CaseResult struct {
	Name                    string   `json:"name"`
	Category                string   `json:"category"`
	Pass                    bool     `json:"pass"`
	ToolCallAccuracy        float64  `json:"tool_call_accuracy"`
	ArgValidity             float64  `json:"arg_validity"`
	SummaryContainsHits     int      `json:"summary_contains_hits"`
	SummaryContainsExpected int      `json:"summary_contains_expected"`
	ForbiddenViolations     int      `json:"forbidden_violations"`
	ActualToolCalls         []string `json:"actual_tool_calls"`
	BudgetExceeded          bool     `json:"budget_exceeded"`
	OrderedMatchExpected    bool     `json:"ordered_match_expected,omitempty"`
	OrderedMatchPassed      bool     `json:"ordered_match_passed,omitempty"`
	Errors                  []string `json:"errors,omitempty"`
}

// EvalSummary aggregates per-case results into one report. Used by
// compare.go to diff two runs and by the GitHub workflow to post a PR
// comment.
//
// Provenance is "placeholder" for the structurally-valid v1 file
// committed before any GPU-backed capture, and "captured" once a real
// harness run has overwritten it. Comparator treats placeholders as
// informational (no severeFailures emitted) so the first real capture
// against a freshly pinned model is not mistaken for a regression.
type EvalSummary struct {
	Provenance     string             `json:"provenance,omitempty"`
	RunID          string             `json:"run_id"`
	ModelName      string             `json:"model_name"`
	VLLMURL        string             `json:"vllm_url"`
	StartedAt      string             `json:"started_at"`
	FinishedAt     string             `json:"finished_at"`
	TotalCases     int                `json:"total_cases"`
	PassedCases    int                `json:"passed_cases"`
	CategoryScores map[string]float64 `json:"category_scores"`
	Cases          []CaseResult       `json:"cases"`
}

const (
	// ProvenancePlaceholder marks a baseline file shipped without an
	// actual harness run (v1 ships with this; replaced at first real
	// capture per docs/llmchat/model-version-bump.md step 4).
	ProvenancePlaceholder = "placeholder"
	// ProvenanceCaptured marks a baseline produced by a real harness
	// run against a live vLLM.
	ProvenanceCaptured = "captured"
)

// LoadAllCases walks the cases directory recursively and parses every
// *.yaml file into a Case. Default-tag (no build constraint) so the
// case corpus is regression-tested in `go test ./...` without needing
// a vLLM stack. The eval-tag harness re-uses this function to load
// the same corpus.
func LoadAllCases(root string) ([]Case, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	out := make([]Case, 0, len(paths))
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		var c Case
		if err := yaml.Unmarshal(raw, &c); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		// Cross-check: file name must match case.name so authors
		// don't ship case_a.yaml with name: case_b.
		want := strings.TrimSuffix(filepath.Base(p), ".yaml")
		if c.Name != want {
			return nil, fmt.Errorf("%s: file name %q != case.name %q", p, want, c.Name)
		}
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, c)
	}
	return out, nil
}
