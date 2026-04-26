package eval

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// DefaultDeltaThreshold is the regression budget. Any per-case score
// degrading by more than this fraction (5%) blocks a model bump
// without an explicit waiver in the PR description (rail #6).
const DefaultDeltaThreshold = 0.05

// CompareSummaries diffs `current` against `baseline` and returns a
// markdown-formatted report suitable for pasting into a PR comment.
// `severeFailures` counts cases that regressed beyond the threshold —
// callers can use it as the exit code for CI gating.
//
// When the baseline carries Provenance == ProvenancePlaceholder, the
// diff is reported as informational only (severeFailures is forced to
// 0). The placeholder represents the v1 schema-only baseline shipped
// before any GPU-backed capture; the first real run is expected to
// replace it, not match it.
func CompareSummaries(baseline, current *EvalSummary, threshold float64) (report string, severeFailures int) {
	if threshold <= 0 {
		threshold = DefaultDeltaThreshold
	}
	placeholderMode := baseline != nil && baseline.Provenance == ProvenancePlaceholder

	baseByName := indexCases(baseline)
	curByName := indexCases(current)

	var b strings.Builder
	fmt.Fprintf(&b, "# llm-chat eval — %s vs baseline %s\n\n",
		safeModel(current), safeModel(baseline))
	if placeholderMode {
		fmt.Fprintln(&b, "> **Placeholder baseline.** This file ships as the v1 schema reference; it has no real GPU-backed capture behind it. The diff below is informational. Per `docs/llmchat/model-version-bump.md` step 4, copy this run's `summary.json` over the baseline (with `provenance: \"captured\"`) to anchor the next gate.")
		fmt.Fprintln(&b)
	}
	fmt.Fprintf(&b, "- Threshold: %.0f%% per-case score delta\n", threshold*100)
	fmt.Fprintf(&b, "- Baseline: %d/%d cases passed (%.1f%%)\n",
		baseline.PassedCases, baseline.TotalCases, percent(baseline.PassedCases, baseline.TotalCases))
	fmt.Fprintf(&b, "- Current : %d/%d cases passed (%.1f%%)\n\n",
		current.PassedCases, current.TotalCases, percent(current.PassedCases, current.TotalCases))

	// Sort by name for stable diff output.
	allNames := mergeCaseNames(baseByName, curByName)
	sort.Strings(allNames)

	var (
		regressed []string
		improved  []string
		newFails  []string
		gone      []string
	)

	for _, name := range allNames {
		base, baseOK := baseByName[name]
		cur, curOK := curByName[name]
		switch {
		case baseOK && !curOK:
			gone = append(gone, name)
		case !baseOK && curOK:
			if !cur.Pass {
				newFails = append(newFails, name)
			}
		case baseOK && curOK:
			baseScore := caseScore(base)
			curScore := caseScore(cur)
			delta := curScore - baseScore
			switch {
			case delta < -threshold:
				regressed = append(regressed, fmt.Sprintf("%s (%.2f → %.2f, Δ%.0f%%)",
					name, baseScore, curScore, delta*100))
				if !placeholderMode {
					severeFailures++
				}
			case delta > threshold:
				improved = append(improved, fmt.Sprintf("%s (%.2f → %.2f, Δ+%.0f%%)",
					name, baseScore, curScore, delta*100))
			}
		}
	}

	if len(regressed) > 0 {
		header := fmt.Sprintf("## ❌ Regressions (>%.0f%% degradation)", threshold*100)
		if placeholderMode {
			header = fmt.Sprintf("## ℹ️ Score deltas vs placeholder (>%.0f%%)", threshold*100)
		}
		fmt.Fprintf(&b, "%s\n\n", header)
		for _, r := range regressed {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		fmt.Fprintln(&b)
	}
	if len(newFails) > 0 {
		fmt.Fprintln(&b, "## ⚠️ New failing cases")
		for _, n := range newFails {
			fmt.Fprintf(&b, "- %s\n", n)
		}
		fmt.Fprintln(&b)
	}
	if len(gone) > 0 {
		fmt.Fprintln(&b, "## 🗑️ Cases removed since baseline")
		for _, g := range gone {
			fmt.Fprintf(&b, "- %s\n", g)
		}
		fmt.Fprintln(&b)
	}
	if len(improved) > 0 {
		fmt.Fprintf(&b, "## ✅ Improvements (>%.0f%% gain)\n\n", threshold*100)
		for _, i := range improved {
			fmt.Fprintf(&b, "- %s\n", i)
		}
		fmt.Fprintln(&b)
	}
	if len(regressed) == 0 && len(newFails) == 0 && len(gone) == 0 && len(improved) == 0 {
		fmt.Fprintln(&b, "## 🟢 No score deltas exceed the threshold")
	}

	return b.String(), severeFailures
}

func indexCases(s *EvalSummary) map[string]CaseResult {
	if s == nil {
		return nil
	}
	out := make(map[string]CaseResult, len(s.Cases))
	for _, c := range s.Cases {
		out[c.Name] = c
	}
	return out
}

func mergeCaseNames(a, b map[string]CaseResult) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// caseScore composes the per-case axes into one comparable scalar in
// [0, 1]. Equal weight on tool-call accuracy + arg validity + summary
// hit rate. Forbidden violations + budget overruns hard-zero the
// score so a "regression" can be measured against zero.
func caseScore(c CaseResult) float64 {
	if c.ForbiddenViolations > 0 || c.BudgetExceeded {
		return 0
	}
	summaryScore := 1.0
	if c.SummaryContainsExpected > 0 {
		summaryScore = float64(c.SummaryContainsHits) / float64(c.SummaryContainsExpected)
	}
	score := (c.ToolCallAccuracy + c.ArgValidity + summaryScore) / 3.0
	if math.IsNaN(score) {
		return 0
	}
	return score
}

func percent(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d) * 100
}

func safeModel(s *EvalSummary) string {
	if s == nil || s.ModelName == "" {
		return "<unknown>"
	}
	return s.ModelName
}

// LoadSummary reads a JSON summary file from disk. Returns nil on
// missing-file (not an error — first run vs. no-baseline-yet).
func LoadSummary(path string) (*EvalSummary, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read summary %s: %w", path, err)
	}
	var s EvalSummary
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("decode summary %s: %w", path, err)
	}
	return &s, nil
}
