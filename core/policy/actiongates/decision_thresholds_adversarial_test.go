package actiongates

import (
	"math"
	"sync"
	"testing"

	pb "github.com/cordum/cordum/core/protocol/pb/v1"
)

// Phase 7 — adversarial self-review test suite. Documents specific
// attack surfaces and fail-closed defenses against unusual inputs.
// Every test here exists because a manual review surfaced a concrete
// "what if an attacker..." question; the test pins the answer.

// TestClassifyByThresholds_NaNConfidenceFailsClosed — what if a
// producer passes a NaN confidence (e.g. a divide-by-zero in a
// statistical scorer)? Per IEEE 754, any NaN comparison returns
// false, so `confidence >= 0.8` is false. Verifies the helper falls
// through to REQUIRE_HUMAN (fail-closed) rather than DENY (over-block)
// or ALLOW (under-block).
func TestClassifyByThresholds_NaNConfidenceFailsClosed(t *testing.T) {
	t.Parallel()
	nan := float32(math.NaN())

	in := DecisionThresholdInput{
		Severity:       SeverityCritical,
		Confidence:     nan,
		ActionBinding:  ActionBindingActionBound,
		ProducerRuleID: "actiongate.url.metadata_aws",
		ProducerReason: "outbound to metadata service",
	}
	got := ClassifyByThresholds(in)
	if got.Decision != pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN {
		t.Fatalf("NaN confidence: decision = %v, want REQUIRE_HUMAN (fail-closed); result: %+v",
			got.Decision, got)
	}
	if got.SubReason != "action_bound:ambiguous" {
		t.Fatalf("NaN confidence: sub_reason = %q, want %q", got.SubReason, "action_bound:ambiguous")
	}
}

// TestClassifyByThresholds_NegativeAndOversizedConfidence — defensive
// coverage for off-range confidence values. Negative confidence fails
// the >=0.8 check (REQUIRE_HUMAN). Confidence > 1.0 passes the floor
// and DENYs (same as 0.95) — that's fine because high producer
// confidence stays high regardless of overflow normalization.
func TestClassifyByThresholds_NegativeAndOversizedConfidence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		conf     float32
		wantDec  pb.DecisionType
		wantSub  string
	}{
		{"negative", -0.5, pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN, "action_bound:ambiguous"},
		{"zero", 0.0, pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN, "action_bound:ambiguous"},
		{"just_below_floor", 0.79, pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN, "action_bound:ambiguous"},
		{"exactly_floor", 0.8, pb.DecisionType_DECISION_TYPE_DENY, "action_bound:high_severity:high_confidence"},
		{"just_above_floor", 0.81, pb.DecisionType_DECISION_TYPE_DENY, "action_bound:high_severity:high_confidence"},
		{"oversized", 1.5, pb.DecisionType_DECISION_TYPE_DENY, "action_bound:high_severity:high_confidence"},
		{"positive_infinity", float32(math.Inf(1)), pb.DecisionType_DECISION_TYPE_DENY, "action_bound:high_severity:high_confidence"},
		{"negative_infinity", float32(math.Inf(-1)), pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN, "action_bound:ambiguous"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := DecisionThresholdInput{
				Severity:       SeverityHigh,
				Confidence:     tc.conf,
				ActionBinding:  ActionBindingActionBound,
				ProducerRuleID: "actiongate.test.boundary",
				ProducerReason: "boundary case",
			}
			got := ClassifyByThresholds(in)
			if got.Decision != tc.wantDec {
				t.Fatalf("conf=%v: decision = %v, want %v", tc.conf, got.Decision, tc.wantDec)
			}
			if got.SubReason != tc.wantSub {
				t.Fatalf("conf=%v: sub_reason = %q, want %q", tc.conf, got.SubReason, tc.wantSub)
			}
		})
	}
}

// TestClassifyByThresholds_UninitializedSeverityFailsClosed — what if
// a caller forgets to set severity (zero value)? Should fail-closed to
// at least REQUIRE_HUMAN (treated as Medium internally), never widen
// to ALLOW based on the missing field.
func TestClassifyByThresholds_UninitializedSeverityFailsClosed(t *testing.T) {
	t.Parallel()

	in := DecisionThresholdInput{
		// Severity intentionally omitted (= SeverityUnspecified zero).
		Confidence:     0.99,
		ActionBinding:  ActionBindingActionBound,
		ProducerRuleID: "test.missing_severity",
		ProducerReason: "caller forgot to set severity",
	}
	got := ClassifyByThresholds(in)
	if got.Decision == pb.DecisionType_DECISION_TYPE_ALLOW || got.Decision == pb.DecisionType_DECISION_TYPE_ALLOW_WITH_CONSTRAINTS {
		t.Fatalf("uninitialised severity must not produce ALLOW family; got %v", got.Decision)
	}
	// SeverityUnspecified treated as Medium → action-bound + Medium →
	// REQUIRE_HUMAN (NOT DENY, because Medium doesn't clear the high-floor).
	if got.Decision != pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN {
		t.Fatalf("uninitialised severity: decision = %v, want REQUIRE_HUMAN", got.Decision)
	}
}

// TestClassifyByThresholds_UninitializedActionBindingFailsClosed —
// what if a caller forgets to set action_binding? Helper must
// fail-closed to REQUIRE_HUMAN; never widen to ALLOW.
func TestClassifyByThresholds_UninitializedActionBindingFailsClosed(t *testing.T) {
	t.Parallel()

	in := DecisionThresholdInput{
		Severity:   SeverityLow,
		Confidence: 0.3,
		// ActionBinding intentionally omitted.
		EducationalContext: true, // even with edu=true, fail-closed
		ProducerRuleID:     "test.missing_binding",
		ProducerReason:     "caller forgot to set action binding",
	}
	got := ClassifyByThresholds(in)
	if got.Decision != pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN {
		t.Fatalf("uninitialised binding: decision = %v, want REQUIRE_HUMAN (fail-closed)", got.Decision)
	}
	if got.SubReason != "unspecified_binding" {
		t.Fatalf("uninitialised binding: sub_reason = %q, want %q", got.SubReason, "unspecified_binding")
	}
}

// TestClassifyByThresholds_ConcurrentCallsAreSafe — the helper is
// claimed pure and thread-safe in docs. Verify by hammering it from
// many goroutines and checking output consistency.
func TestClassifyByThresholds_ConcurrentCallsAreSafe(t *testing.T) {
	t.Parallel()

	in := DecisionThresholdInput{
		Severity:           SeverityHigh,
		Confidence:         0.92,
		ActionBinding:      ActionBindingPromptOnly,
		EducationalContext: true,
		ProducerRuleID:     "scanner.injection.rm_rf",
		ProducerReason:     "rm -rf defensive runbook",
	}

	const goroutines = 64
	const iters = 200
	var wg sync.WaitGroup
	mismatches := make(chan string, goroutines*iters)

	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iters {
				got := ClassifyByThresholds(in)
				if got.Decision != pb.DecisionType_DECISION_TYPE_ALLOW {
					mismatches <- "wrong decision"
					return
				}
				if got.RuleID != "scanner.injection.rm_rf" {
					mismatches <- "rule_id corrupted"
					return
				}
				if got.SubReason != "prompt_only:educational" {
					mismatches <- "sub_reason corrupted"
					return
				}
			}
		}()
	}
	wg.Wait()
	close(mismatches)
	if msg, ok := <-mismatches; ok {
		t.Fatalf("concurrent calls produced inconsistent results: %s", msg)
	}
}

// TestClassifyByThresholds_PreservesRuleIDAcrossAllPaths — the
// producer's rule_id MUST propagate through every routing path
// verbatim so audit consumers can attribute the decision to the
// originating producer regardless of how the helper routed it.
func TestClassifyByThresholds_PreservesRuleIDAcrossAllPaths(t *testing.T) {
	t.Parallel()

	const sentinelID = "test.sentinel.unique.rule.id.0xdeadbeef"

	cases := []DecisionThresholdInput{
		// Each row exercises a different routing path
		{Severity: SeverityCritical, Confidence: 0.99, ActionBinding: ActionBindingActionBound, ProducerRuleID: sentinelID},
		{Severity: SeverityMedium, Confidence: 0.7, ActionBinding: ActionBindingActionBound, ProducerRuleID: sentinelID},
		{Severity: SeverityLow, Confidence: 0.9, ActionBinding: ActionBindingActionBound, ProducerRuleID: sentinelID},
		{Severity: SeverityHigh, Confidence: 0.92, ActionBinding: ActionBindingPromptOnly, EducationalContext: true, ProducerRuleID: sentinelID},
		{Severity: SeverityLow, Confidence: 0.3, ActionBinding: ActionBindingPromptOnly, ProducerRuleID: sentinelID},
		{Severity: SeverityHigh, Confidence: 0.85, ActionBinding: ActionBindingPromptOnly, ProducerRuleID: sentinelID},
		{Severity: SeverityLow, Confidence: 0.5, ActionBinding: ActionBindingPromptOnly, ProducerRuleID: sentinelID, ProducerConstraints: map[string]any{"audit_tag": "x"}},
	}

	for _, in := range cases {
		got := ClassifyByThresholds(in)
		if got.RuleID != sentinelID {
			t.Fatalf("rule_id lost: input=%+v -> result.RuleID=%q, want %q", in, got.RuleID, sentinelID)
		}
	}
}
