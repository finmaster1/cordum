package eval

import (
	"testing"
)

// TestCasesParseable walks the entire cases/ corpus and asserts every
// YAML file (a) parses without error, (b) has all required fields,
// (c) has its file name aligned with case.name. Runs in default
// `go test ./...` so a contributor adding a malformed case can't ship
// it past CI without noticing.
func TestCasesParseable(t *testing.T) {
	t.Parallel()
	cases, err := LoadAllCases("cases")
	if err != nil {
		t.Fatalf("LoadAllCases: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("zero cases under tests/eval/cases/; corpus must not be empty")
	}

	categoryCounts := map[string]int{}
	for _, c := range cases {
		categoryCounts[c.Category]++
	}

	// Per task DoD #2: minimum 5 cases per category across 6 categories.
	requiredCategories := []string{
		"read_only",
		"filtered_reads",
		"preapproved_mutations",
		"approval_gated_mutations",
		"multi_turn",
		"guardrail_triggers",
	}
	for _, cat := range requiredCategories {
		if got := categoryCounts[cat]; got < 5 {
			t.Errorf("category %q has %d cases, want >= 5 (task DoD #2)", cat, got)
		}
	}
}

// TestPlaceholderBaselineMatchesCorpus loads the committed v1 baseline
// JSON and asserts (a) it parses, (b) provenance is "placeholder",
// (c) it covers exactly the same set of case names as the on-disk
// corpus. Regenerate via `go run
// tests/eval/cmd/genplaceholderbaseline/main.go > tests/eval/baseline/qwen3_coder_30b_fp8.json`
// when the corpus changes.
func TestPlaceholderBaselineMatchesCorpus(t *testing.T) {
	t.Parallel()
	baseline, err := LoadSummary("baseline/qwen3_coder_30b_fp8.json")
	if err != nil {
		t.Fatalf("LoadSummary: %v", err)
	}
	if baseline == nil {
		t.Fatal("baseline file missing — regenerate via cmd/genplaceholderbaseline")
	}
	if baseline.Provenance != ProvenancePlaceholder {
		t.Errorf("baseline.Provenance = %q, want %q", baseline.Provenance, ProvenancePlaceholder)
	}

	cases, err := LoadAllCases("cases")
	if err != nil {
		t.Fatalf("LoadAllCases: %v", err)
	}
	corpus := map[string]string{}
	for _, c := range cases {
		corpus[c.Name] = c.Category
	}
	if got, want := len(baseline.Cases), len(corpus); got != want {
		t.Errorf("baseline cases=%d, corpus cases=%d — regenerate baseline", got, want)
	}
	for _, br := range baseline.Cases {
		if cat, ok := corpus[br.Name]; !ok {
			t.Errorf("baseline references case %q that is not in corpus", br.Name)
		} else if br.Category != cat {
			t.Errorf("baseline case %q has category=%q, corpus has %q",
				br.Name, br.Category, cat)
		}
	}
	for name := range corpus {
		found := false
		for _, br := range baseline.Cases {
			if br.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("corpus case %q missing from baseline — regenerate baseline", name)
		}
	}
}

// TestCasesUniqueNames asserts no two cases share a name. The harness
// assumes Case.Name is the unique identifier when writing per-case
// JSON results.
func TestCasesUniqueNames(t *testing.T) {
	t.Parallel()
	cases, err := LoadAllCases("cases")
	if err != nil {
		t.Fatalf("LoadAllCases: %v", err)
	}
	seen := map[string]string{} // name → first category
	for _, c := range cases {
		if prev, ok := seen[c.Name]; ok {
			t.Errorf("duplicate case name %q (first under %q, then under %q)",
				c.Name, prev, c.Category)
		}
		seen[c.Name] = c.Category
	}
}
