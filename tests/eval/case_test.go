package eval

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMatchArgs_AllTypeSentinels(t *testing.T) {
	t.Parallel()
	expected := map[string]any{
		"a_int":    "int",
		"a_float":  "float",
		"a_str":    "str",
		"a_bool":   "bool",
		"a_array":  "array",
		"a_object": "object",
	}
	actual := json.RawMessage(`{
		"a_int": 5,
		"a_float": 3.14,
		"a_str": "hi",
		"a_bool": true,
		"a_array": [1, 2],
		"a_object": {"k": "v"}
	}`)
	if err := MatchArgs(expected, actual); err != nil {
		t.Errorf("MatchArgs: %v", err)
	}
}

func TestMatchArgs_TypeMismatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		expected map[string]any
		actual   string
		wantSub  string
	}{
		{
			"int_got_float",
			map[string]any{"x": "int"},
			`{"x": 3.5}`,
			"fractional",
		},
		{
			"int_got_string",
			map[string]any{"x": "int"},
			`{"x": "5"}`,
			"expected int",
		},
		{
			"str_got_int",
			map[string]any{"x": "str"},
			`{"x": 5}`,
			"expected str",
		},
		{
			"missing_required_key",
			map[string]any{"x": "int"},
			`{"y": 5}`,
			"missing required key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := MatchArgs(tt.expected, json.RawMessage(tt.actual))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantSub)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantSub)
			}
		})
	}
}

func TestMatchArgs_LiteralMatch(t *testing.T) {
	t.Parallel()
	expected := map[string]any{
		"workflow_id": "demo.mock-bank.transfer",
	}
	good := json.RawMessage(`{"workflow_id": "demo.mock-bank.transfer", "extra": "ok"}`)
	if err := MatchArgs(expected, good); err != nil {
		t.Errorf("matching literal failed: %v", err)
	}
	bad := json.RawMessage(`{"workflow_id": "demo.something-else"}`)
	if err := MatchArgs(expected, bad); err == nil {
		t.Error("mismatched literal should fail")
	}
}

func TestMatchArgs_NestedObject(t *testing.T) {
	t.Parallel()
	expected := map[string]any{
		"parameters": map[string]any{
			"amount": "int",
			"from":   "str",
			"to":     "str",
		},
	}
	good := json.RawMessage(`{"parameters": {"amount": 40, "from": "Alice", "to": "Bob", "note": "rent"}}`)
	if err := MatchArgs(expected, good); err != nil {
		t.Errorf("nested match: %v", err)
	}
	bad := json.RawMessage(`{"parameters": {"amount": "not-a-number", "from": "Alice", "to": "Bob"}}`)
	err := MatchArgs(expected, bad)
	if err == nil || !strings.Contains(err.Error(), "expected int") {
		t.Errorf("expected nested type error, got %v", err)
	}
}

func TestMatchArgs_ExtraKeysAllowed(t *testing.T) {
	t.Parallel()
	expected := map[string]any{"limit": "int"}
	got := json.RawMessage(`{"limit": 10, "since": "2025-01-01", "verbose": true}`)
	if err := MatchArgs(expected, got); err != nil {
		t.Errorf("extra keys must not fail: %v", err)
	}
}

func TestCase_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		c       Case
		wantErr string
	}{
		{
			"missing_name",
			Case{Category: "read_only", UserMessage: "x", MaxToolCalls: 1},
			"name is required",
		},
		{
			"missing_category",
			Case{Name: "x", UserMessage: "x", MaxToolCalls: 1},
			"category is required",
		},
		{
			"both_msg_and_turns",
			Case{
				Name: "x", Category: "read_only", UserMessage: "x",
				Turns:        []Turn{{Role: "user", Content: "x"}},
				MaxToolCalls: 1,
			},
			"EITHER user_message OR turns",
		},
		{
			"neither_msg_nor_turns",
			Case{Name: "x", Category: "read_only", MaxToolCalls: 1},
			"either user_message or turns",
		},
		{
			"negative_max_tool_calls",
			Case{Name: "x", Category: "read_only", UserMessage: "x", MaxToolCalls: -1},
			"max_tool_calls",
		},
		{
			"valid",
			Case{Name: "x", Category: "read_only", UserMessage: "x", MaxToolCalls: 1},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.c.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestCompareSummaries_NoDelta(t *testing.T) {
	t.Parallel()
	base := &EvalSummary{
		ModelName:  "qwen3_coder_30b_fp8",
		TotalCases: 2, PassedCases: 2,
		Cases: []CaseResult{
			{Name: "a", Pass: true, ToolCallAccuracy: 1, ArgValidity: 1, SummaryContainsHits: 1, SummaryContainsExpected: 1},
			{Name: "b", Pass: true, ToolCallAccuracy: 1, ArgValidity: 1, SummaryContainsHits: 1, SummaryContainsExpected: 1},
		},
	}
	cur := &EvalSummary{
		ModelName:  "qwen3_coder_30b_fp8",
		TotalCases: 2, PassedCases: 2,
		Cases: []CaseResult{
			{Name: "a", Pass: true, ToolCallAccuracy: 1, ArgValidity: 1, SummaryContainsHits: 1, SummaryContainsExpected: 1},
			{Name: "b", Pass: true, ToolCallAccuracy: 1, ArgValidity: 1, SummaryContainsHits: 1, SummaryContainsExpected: 1},
		},
	}
	report, severe := CompareSummaries(base, cur, 0.05)
	if severe != 0 {
		t.Errorf("expected severe=0, got %d", severe)
	}
	if !strings.Contains(report, "No score deltas") {
		t.Errorf("report should mention no deltas, got: %s", report)
	}
}

func TestCompareSummaries_RegressionAboveThreshold(t *testing.T) {
	t.Parallel()
	base := &EvalSummary{
		TotalCases: 1, PassedCases: 1,
		Cases: []CaseResult{
			{Name: "a", Pass: true, ToolCallAccuracy: 1, ArgValidity: 1, SummaryContainsHits: 1, SummaryContainsExpected: 1},
		},
	}
	cur := &EvalSummary{
		TotalCases: 1, PassedCases: 0,
		Cases: []CaseResult{
			{Name: "a", Pass: false, ToolCallAccuracy: 0.5, ArgValidity: 0.5, SummaryContainsHits: 0, SummaryContainsExpected: 1},
		},
	}
	report, severe := CompareSummaries(base, cur, 0.05)
	if severe != 1 {
		t.Errorf("expected severe=1, got %d", severe)
	}
	if !strings.Contains(report, "Regressions") {
		t.Errorf("report should flag regression: %s", report)
	}
}

func TestCompareSummaries_PlaceholderBaselineNeverSevere(t *testing.T) {
	t.Parallel()
	base := &EvalSummary{
		Provenance: ProvenancePlaceholder,
		Cases: []CaseResult{
			{Name: "a", ToolCallAccuracy: 1, ArgValidity: 1, SummaryContainsHits: 1, SummaryContainsExpected: 1},
		},
	}
	cur := &EvalSummary{
		Cases: []CaseResult{
			{Name: "a", ToolCallAccuracy: 0.1, ArgValidity: 0.1, SummaryContainsHits: 0, SummaryContainsExpected: 1},
		},
	}
	report, severe := CompareSummaries(base, cur, 0.05)
	if severe != 0 {
		t.Errorf("placeholder baseline must not produce severeFailures; got %d", severe)
	}
	if !strings.Contains(report, "Placeholder baseline") {
		t.Errorf("report should declare placeholder mode: %s", report)
	}
	if !strings.Contains(report, "Score deltas vs placeholder") {
		t.Errorf("report should re-label regression header in placeholder mode: %s", report)
	}
}

func TestCompareSummaries_CapturedBaselineStillSevere(t *testing.T) {
	t.Parallel()
	base := &EvalSummary{
		Provenance: ProvenanceCaptured,
		Cases: []CaseResult{
			{Name: "a", ToolCallAccuracy: 1, ArgValidity: 1, SummaryContainsHits: 1, SummaryContainsExpected: 1},
		},
	}
	cur := &EvalSummary{
		Cases: []CaseResult{
			{Name: "a", ToolCallAccuracy: 0.1, ArgValidity: 0.1, SummaryContainsHits: 0, SummaryContainsExpected: 1},
		},
	}
	_, severe := CompareSummaries(base, cur, 0.05)
	if severe != 1 {
		t.Errorf("captured baseline must still flag regressions; got severe=%d", severe)
	}
}

func TestCompareSummaries_ImprovementAboveThreshold(t *testing.T) {
	t.Parallel()
	base := &EvalSummary{
		Cases: []CaseResult{
			{Name: "a", ToolCallAccuracy: 0.4, ArgValidity: 0.4, SummaryContainsHits: 0, SummaryContainsExpected: 1},
		},
	}
	cur := &EvalSummary{
		Cases: []CaseResult{
			{Name: "a", ToolCallAccuracy: 1, ArgValidity: 1, SummaryContainsHits: 1, SummaryContainsExpected: 1},
		},
	}
	report, severe := CompareSummaries(base, cur, 0.05)
	if severe != 0 {
		t.Errorf("severe regressions = %d on improvement-only diff, want 0", severe)
	}
	if !strings.Contains(report, "Improvements") {
		t.Errorf("report should call out improvement: %s", report)
	}
}
