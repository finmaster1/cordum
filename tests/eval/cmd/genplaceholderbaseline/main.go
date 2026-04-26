//go:build ignore

// genplaceholderbaseline emits the v1 placeholder baseline JSON used by
// the llm-chat tool-call eval harness. Run from the cordum repo root:
//
//	go run tests/eval/cmd/genplaceholderbaseline/main.go \
//	    > tests/eval/baseline/qwen3_coder_30b_fp8.json
//
// The output is a structurally-valid EvalSummary with provenance set to
// "placeholder" and every per-case CaseResult set to perfect-score
// shape. This is the v1 reference: the comparator detects the
// placeholder marker and downgrades severeFailures to zero, so the
// first real GPU-backed capture overwrites this file (per
// docs/llmchat/model-version-bump.md step 4) without being flagged as
// a regression.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	eval "github.com/cordum/cordum/tests/eval"
)

func main() {
	cases, err := eval.LoadAllCases("tests/eval/cases")
	if err != nil {
		fmt.Fprintf(os.Stderr, "load cases: %v\n", err)
		os.Exit(1)
	}
	if len(cases) == 0 {
		fmt.Fprintln(os.Stderr, "zero cases found under tests/eval/cases")
		os.Exit(1)
	}

	results := make([]eval.CaseResult, 0, len(cases))
	categoryCount := map[string]int{}
	for _, c := range cases {
		results = append(results, eval.CaseResult{
			Name:                    c.Name,
			Category:                c.Category,
			Pass:                    true,
			ToolCallAccuracy:        1,
			ArgValidity:             1,
			SummaryContainsHits:     len(c.ExpectedSummaryContains),
			SummaryContainsExpected: len(c.ExpectedSummaryContains),
			ForbiddenViolations:     0,
			ActualToolCalls:         nil,
			BudgetExceeded:          false,
			OrderedMatchExpected:    c.ExpectedToolCallsOrdered,
			OrderedMatchPassed:      c.ExpectedToolCallsOrdered,
		})
		categoryCount[c.Category]++
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })

	categoryScores := map[string]float64{}
	for cat := range categoryCount {
		categoryScores[cat] = 1.0
	}

	summary := eval.EvalSummary{
		Provenance:     eval.ProvenancePlaceholder,
		RunID:          "v1-placeholder-pending-real-capture",
		ModelName:      "qwen3_coder_30b_fp8",
		VLLMURL:        "<placeholder>",
		StartedAt:      "0001-01-01T00:00:00Z",
		FinishedAt:     "0001-01-01T00:00:00Z",
		TotalCases:     len(results),
		PassedCases:    len(results),
		CategoryScores: categoryScores,
		Cases:          results,
	}
	raw, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(raw); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()
}
