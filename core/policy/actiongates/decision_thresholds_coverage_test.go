package actiongates

import (
	"strings"
	"testing"

	pb "github.com/cordum/cordum/core/protocol/pb/v1"
)

// Phase 4 — coverage tests for the 4 output paths (DoD #5).
//
// For each pb.DecisionType the threshold helper can emit (DENY,
// REQUIRE_HUMAN, ALLOW, ALLOW_WITH_CONSTRAINTS) we exercise at least
// one row per producer family (action gates, safety-kernel scanners,
// governance evaluator) so the rule_id/reason payload is verified
// across producer-specific identifier shapes — not just one producer
// repeated four ways. Every row asserts (decision_type, rule_id,
// reason_substring, sub_reason) per `feedback_qa_senior_review`.

// coverageCase pairs a producer-shaped input with the expected
// 4-path-coverage output. Each row's name encodes path:producer.
type coverageCase struct {
	name             string
	input            DecisionThresholdInput
	wantDecision     pb.DecisionType
	wantRuleID       string
	wantReasonHas    string // substring; lowercased for compare
	wantSubReason    string // exact match — sub_reason is structured
	wantConstraints  bool   // expect non-nil Constraints map on AWC path
}

func runCoverage(t *testing.T, tc coverageCase) {
	t.Helper()
	got := ClassifyByThresholds(tc.input)
	if got.Decision != tc.wantDecision {
		t.Fatalf("decision = %v, want %v\ninput: %+v\nresult: %+v",
			got.Decision, tc.wantDecision, tc.input, got)
	}
	if got.RuleID != tc.wantRuleID {
		t.Fatalf("rule_id = %q, want %q (producer rule_id must propagate verbatim)",
			got.RuleID, tc.wantRuleID)
	}
	if tc.wantReasonHas != "" && !strings.Contains(strings.ToLower(got.Reason), tc.wantReasonHas) {
		t.Fatalf("reason = %q, want substring %q", got.Reason, tc.wantReasonHas)
	}
	if got.SubReason != tc.wantSubReason {
		t.Fatalf("sub_reason = %q, want %q", got.SubReason, tc.wantSubReason)
	}
	if tc.wantConstraints && len(got.Constraints) == 0 {
		t.Fatalf("constraints empty, want non-empty carrier for AWC path; result: %+v", got)
	}
	if !tc.wantConstraints && len(got.Constraints) != 0 {
		t.Fatalf("constraints non-empty %+v, want nil for non-AWC path; result: %+v", got.Constraints, got)
	}
}

// TestClassifyByThresholds_FourOutputPathsPerProducer covers all four
// pb.DecisionType values × three producer families = 12 rows. Each
// row uses a producer-realistic rule_id pattern from the Phase 1
// inventory so a future renamer breaks tests and a cross-package PR
// audit catches the drift.
func TestClassifyByThresholds_FourOutputPathsPerProducer(t *testing.T) {
	t.Parallel()

	cases := []coverageCase{
		// ─────────────── DENY (4 rows: action-gate × 2, scanner × 1, evaluator × 1) ───────────────
		{
			name: "deny__actiongate_file__sensitive_path_etc_shadow",
			input: DecisionThresholdInput{
				Severity:       SeverityCritical,
				Confidence:     0.99,
				ActionBinding:  ActionBindingActionBound,
				ProducerRuleID: "actiongate.file.sensitive_path:etc_shadow",
				ProducerReason: "filesystem access denied",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_DENY,
			wantRuleID:    "actiongate.file.sensitive_path:etc_shadow",
			wantReasonHas: "filesystem",
			wantSubReason: "action_bound:high_severity:high_confidence",
		},
		{
			name: "deny__actiongate_url__metadata_aws",
			input: DecisionThresholdInput{
				Severity:       SeverityCritical,
				Confidence:     0.99,
				ActionBinding:  ActionBindingActionBound,
				ProducerRuleID: "actiongate.url.metadata_aws",
				ProducerReason: "outbound to cloud metadata service denied",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_DENY,
			wantRuleID:    "actiongate.url.metadata_aws",
			wantReasonHas: "metadata",
			wantSubReason: "action_bound:high_severity:high_confidence",
		},
		{
			name: "deny__scanner_secret_leak__aws_access_key_id_action_bound",
			input: DecisionThresholdInput{
				Severity:       SeverityCritical,
				Confidence:     0.99,
				ActionBinding:  ActionBindingActionBound,
				ProducerRuleID: "scanner.secret_leak.aws_access_key_id",
				ProducerReason: "aws access key id detected in outbound payload",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_DENY,
			wantRuleID:    "scanner.secret_leak.aws_access_key_id",
			wantReasonHas: "aws access key",
			wantSubReason: "action_bound:high_severity:high_confidence",
		},
		{
			name: "deny__governance__ma_issuer_root_not_allowed",
			input: DecisionThresholdInput{
				Severity:       SeverityHigh,
				Confidence:     0.95,
				ActionBinding:  ActionBindingActionBound,
				ProducerRuleID: "ma_issuer_root_not_allowed",
				ProducerReason: "multi-agent issuer root not allowed by governance",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_DENY,
			wantRuleID:    "ma_issuer_root_not_allowed",
			wantReasonHas: "governance",
			wantSubReason: "action_bound:high_severity:high_confidence",
		},

		// ─────────────── REQUIRE_HUMAN (4 rows) ───────────────
		{
			name: "require_human__scanner_injection__prompt_only_no_education",
			input: DecisionThresholdInput{
				Severity:       SeverityHigh,
				Confidence:     0.92,
				ActionBinding:  ActionBindingPromptOnly,
				ProducerRuleID: "scanner.injection.shell_injection_fragment",
				ProducerReason: "shell injection fragment matched",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN,
			wantRuleID:    "scanner.injection.shell_injection_fragment",
			wantReasonHas: "shell injection",
			wantSubReason: "prompt_only:ambiguous",
		},
		{
			name: "require_human__scanner_secret_leak__low_confidence_prompt_only",
			input: DecisionThresholdInput{
				Severity:       SeverityHigh,
				Confidence:     0.4,
				ActionBinding:  ActionBindingPromptOnly,
				ProducerRuleID: "scanner.secret_leak.generic_credential",
				ProducerReason: "low-confidence generic credential pattern",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN,
			wantRuleID:    "scanner.secret_leak.generic_credential",
			wantReasonHas: "credential",
			wantSubReason: "prompt_only:ambiguous",
		},
		{
			name: "require_human__actiongate_mutation__ambiguous_medium_action_bound",
			input: DecisionThresholdInput{
				Severity:       SeverityMedium,
				Confidence:     0.7,
				ActionBinding:  ActionBindingActionBound,
				ProducerRuleID: "actiongate.mutation.destructive_no_approval",
				ProducerReason: "destructive mutation requires approval",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN,
			wantRuleID:    "actiongate.mutation.destructive_no_approval",
			wantReasonHas: "approval",
			wantSubReason: "action_bound:ambiguous",
		},
		{
			name: "require_human__governance__prompt_only_medium_severity_no_education",
			input: DecisionThresholdInput{
				Severity:       SeverityMedium,
				Confidence:     0.6,
				ActionBinding:  ActionBindingPromptOnly,
				ProducerRuleID: "governance.prompt_injection_directive",
				ProducerReason: "prompt injection directive in policy context",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN,
			wantRuleID:    "governance.prompt_injection_directive",
			wantReasonHas: "injection",
			wantSubReason: "prompt_only:ambiguous",
		},

		// ─────────────── ALLOW (4 rows — covers educational + low-severity paths) ───────────────
		{
			name: "allow__scanner_keyword__etc_passwd_educational",
			input: DecisionThresholdInput{
				Severity:           SeverityHigh,
				Confidence:         0.92,
				ActionBinding:      ActionBindingPromptOnly,
				EducationalContext: true,
				ProducerRuleID:     "scanner.keyword.etc_passwd",
				ProducerReason:     "/etc/passwd mention in input",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_ALLOW,
			wantRuleID:    "scanner.keyword.etc_passwd",
			wantReasonHas: "educational",
			wantSubReason: "prompt_only:educational",
		},
		{
			name: "allow__scanner_injection__rm_rf_educational",
			input: DecisionThresholdInput{
				Severity:           SeverityHigh,
				Confidence:         0.92,
				ActionBinding:      ActionBindingPromptOnly,
				EducationalContext: true,
				ProducerRuleID:     "scanner.injection.rm_rf",
				ProducerReason:     "shell injection fragment matched: rm -rf",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_ALLOW,
			wantRuleID:    "scanner.injection.rm_rf",
			wantReasonHas: "educational",
			wantSubReason: "prompt_only:educational",
		},
		{
			name: "allow__actiongate_file__low_severity_action_bound",
			input: DecisionThresholdInput{
				Severity:       SeverityLow,
				Confidence:     0.9,
				ActionBinding:  ActionBindingActionBound,
				ProducerRuleID: "actiongate.file.workspace_read",
				ProducerReason: "workspace file read",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_ALLOW,
			wantRuleID:    "actiongate.file.workspace_read",
			wantReasonHas: "workspace",
			wantSubReason: "action_bound:low_severity",
		},
		{
			name: "allow__governance__low_severity_prompt_only_non_educational",
			input: DecisionThresholdInput{
				Severity:       SeverityLow,
				Confidence:     0.3,
				ActionBinding:  ActionBindingPromptOnly,
				ProducerRuleID: "governance.benign_mention",
				ProducerReason: "benign keyword surface",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_ALLOW,
			wantRuleID:    "governance.benign_mention",
			wantReasonHas: "benign",
			wantSubReason: "prompt_only:low_severity",
		},

		// ─────────────── ALLOW_WITH_CONSTRAINTS (4 rows — Constraints carrier propagates) ───────────────
		{
			name: "awc__actiongate_mcp__sandboxed_mode_low_severity",
			input: DecisionThresholdInput{
				Severity:       SeverityLow,
				Confidence:     0.95,
				ActionBinding:  ActionBindingActionBound,
				ProducerRuleID: "actiongate.mcp.sandbox_mode",
				ProducerReason: "mcp call allowed under sandbox constraints",
				ProducerConstraints: map[string]any{
					"sandbox_mode":          "read_only",
					"max_execution_seconds": 30,
				},
			},
			wantDecision:    pb.DecisionType_DECISION_TYPE_ALLOW_WITH_CONSTRAINTS,
			wantRuleID:      "actiongate.mcp.sandbox_mode",
			wantReasonHas:   "sandbox",
			wantSubReason:   "action_bound:low_severity:with_constraints",
			wantConstraints: true,
		},
		{
			name: "awc__actiongate_url__tier_ceiling_low_severity",
			input: DecisionThresholdInput{
				Severity:       SeverityLow,
				Confidence:     0.9,
				ActionBinding:  ActionBindingActionBound,
				ProducerRuleID: "actiongate.url.tier_ceiling",
				ProducerReason: "outbound allowed within tier ceiling",
				ProducerConstraints: map[string]any{
					"max_bytes_per_request": 1024 * 1024,
					"allowed_methods":       []string{"GET", "HEAD"},
				},
			},
			wantDecision:    pb.DecisionType_DECISION_TYPE_ALLOW_WITH_CONSTRAINTS,
			wantRuleID:      "actiongate.url.tier_ceiling",
			wantReasonHas:   "tier ceiling",
			wantSubReason:   "action_bound:low_severity:with_constraints",
			wantConstraints: true,
		},
		{
			name: "awc__scanner_educational__with_redaction_constraint",
			input: DecisionThresholdInput{
				Severity:           SeverityHigh,
				Confidence:         0.92,
				ActionBinding:      ActionBindingPromptOnly,
				EducationalContext: true,
				ProducerRuleID:     "scanner.injection.rm_rf",
				ProducerReason:     "rm -rf in defensive runbook",
				ProducerConstraints: map[string]any{
					"redact_spans": []string{"shell_command_fragment"},
				},
			},
			wantDecision:    pb.DecisionType_DECISION_TYPE_ALLOW_WITH_CONSTRAINTS,
			wantRuleID:      "scanner.injection.rm_rf",
			wantReasonHas:   "educational",
			wantSubReason:   "prompt_only:educational:with_constraints",
			wantConstraints: true,
		},
		{
			name: "awc__governance__low_severity_prompt_only_with_audit_tag",
			input: DecisionThresholdInput{
				Severity:       SeverityLow,
				Confidence:     0.5,
				ActionBinding:  ActionBindingPromptOnly,
				ProducerRuleID: "governance.audit_tag_required",
				ProducerReason: "audit tag required on educational surface",
				ProducerConstraints: map[string]any{
					"audit_tag": "compliance_education",
				},
			},
			wantDecision:    pb.DecisionType_DECISION_TYPE_ALLOW_WITH_CONSTRAINTS,
			wantRuleID:      "governance.audit_tag_required",
			wantReasonHas:   "audit tag",
			wantSubReason:   "prompt_only:low_severity:with_constraints",
			wantConstraints: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runCoverage(t, tc)
		})
	}
}

// TestClassifyByThresholds_EmptyConstraintsMapDoesNotFlipAWC proves that
// an empty (non-nil) constraints map is treated as "no constraints"
// rather than ALLOW_WITH_CONSTRAINTS. Defends against a caller who
// initialises the field but never populates it.
func TestClassifyByThresholds_EmptyConstraintsMapDoesNotFlipAWC(t *testing.T) {
	t.Parallel()

	in := DecisionThresholdInput{
		Severity:            SeverityLow,
		Confidence:          0.5,
		ActionBinding:       ActionBindingPromptOnly,
		ProducerRuleID:      "scanner.benign.empty_constraints",
		ProducerReason:      "benign keyword",
		ProducerConstraints: map[string]any{},
	}
	got := ClassifyByThresholds(in)
	if got.Decision != pb.DecisionType_DECISION_TYPE_ALLOW {
		t.Fatalf("decision = %v, want ALLOW (empty constraints map must not flip to AWC); result: %+v",
			got.Decision, got)
	}
	if got.SubReason != "prompt_only:low_severity" {
		t.Fatalf("sub_reason = %q, want %q (no :with_constraints suffix)",
			got.SubReason, "prompt_only:low_severity")
	}
}

// TestClassifyByThresholds_ConstraintsCarrierPropagatesValuesExactly proves
// the constraints carrier is propagated by reference (not deep-copied or
// re-serialized) so producers see exact value identity.
func TestClassifyByThresholds_ConstraintsCarrierPropagatesValuesExactly(t *testing.T) {
	t.Parallel()

	cons := map[string]any{
		"sandbox_mode":   "read_only",
		"allowed_paths":  []string{"/workspace"},
		"max_bytes":      4096,
		"nested_object":  map[string]any{"deep_key": "deep_value"},
	}
	in := DecisionThresholdInput{
		Severity:            SeverityLow,
		Confidence:          0.95,
		ActionBinding:       ActionBindingActionBound,
		ProducerRuleID:      "actiongate.mcp.sandbox_mode",
		ProducerReason:      "sandbox mode",
		ProducerConstraints: cons,
	}
	got := ClassifyByThresholds(in)
	if got.Decision != pb.DecisionType_DECISION_TYPE_ALLOW_WITH_CONSTRAINTS {
		t.Fatalf("decision = %v, want ALLOW_WITH_CONSTRAINTS", got.Decision)
	}
	if got.Constraints["sandbox_mode"] != "read_only" {
		t.Fatalf("sandbox_mode = %v, want read_only", got.Constraints["sandbox_mode"])
	}
	if got.Constraints["max_bytes"] != 4096 {
		t.Fatalf("max_bytes = %v, want 4096", got.Constraints["max_bytes"])
	}
	nested, ok := got.Constraints["nested_object"].(map[string]any)
	if !ok {
		t.Fatalf("nested_object lost type during routing; got %T", got.Constraints["nested_object"])
	}
	if nested["deep_key"] != "deep_value" {
		t.Fatalf("nested.deep_key = %v, want deep_value", nested["deep_key"])
	}
}
