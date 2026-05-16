package actiongates

import (
	"strings"
	"testing"

	pb "github.com/cordum/cordum/core/protocol/pb/v1"
)

// Phase 2 (TDD: red) tests for the 3-axis threshold model:
//
//   - severity:         low | medium | high | critical
//   - confidence:       0.0 .. 1.0 (gate's certainty the match is malicious)
//   - action_binding:   prompt-only (text mention) | action-bound (real
//                       ActionDescriptor.Kind with target_path/target_url/etc.)
//   - educational_ctx:  derived from session metadata only, NEVER from
//                       input_text/claim — see epic rail "Action-layer gates
//                       must use real backend/request metadata".
//
// Decision rule table:
//
//   HIGH-severity + HIGH-confidence + action-bound + !educational => DENY
//   HIGH-severity + HIGH-confidence + prompt-only  + educational  => ALLOW
//   HIGH-severity + HIGH-confidence + prompt-only  + !educational => REQUIRE_HUMAN
//   MEDIUM-severity OR LOW-confidence (prompt-only)              => REQUIRE_HUMAN
//   LOW-severity + prompt-only + educational                       => ALLOW
//
// These tests are RED until Phase 3 lands ClassifyByThresholds + its
// supporting types; compilation will fail with "undefined: ..." until
// then. That is the canonical TDD red state.

// TestClassifyByThresholds_FivePrimaryFalsePositives codifies DoD #4: the
// five over-refusal scenarios the threshold tuning must fix.
func TestClassifyByThresholds_FivePrimaryFalsePositives(t *testing.T) {
	t.Parallel()

	// Each row pairs a concrete benchmark FP scenario with the threshold
	// inputs producers will compute for it and the expected post-tuning
	// decision. rule_id is the existing producer rule the FP currently
	// hits — preserved across the tuning so audit trails stay stable.
	cases := []struct {
		name           string
		scenarioDoc    string
		input          DecisionThresholdInput
		wantDecision   pb.DecisionType
		wantRuleID     string // preserved verbatim from producer
		wantReasonHas  string
		wantSubReasonHas string
	}{
		{
			name:        "fp_a_defensive_etc_passwd_in_security_education",
			scenarioDoc: "Prompt asks how attackers exploit /etc/passwd in a security-training context; today the keyword scanner trips DENY. Threshold-tuned: prompt-only + educational => ALLOW.",
			input: DecisionThresholdInput{
				Severity:           SeverityHigh,
				Confidence:         0.92,
				ActionBinding:      ActionBindingPromptOnly,
				EducationalContext: true,
				ProducerRuleID:     "scanner.keyword.etc_passwd",
				ProducerReason:     "mention of /etc/passwd in input content",
			},
			wantDecision:     pb.DecisionType_DECISION_TYPE_ALLOW,
			wantRuleID:       "scanner.keyword.etc_passwd",
			wantReasonHas:    "educational",
			wantSubReasonHas: "prompt_only",
		},
		{
			name:        "fp_b_rm_rf_in_defensive_runbook",
			scenarioDoc: "Defensive incident-response runbook mentions `rm -rf /tmp/staging` cleanup; today the injection scanner trips DENY. Threshold-tuned: prompt-only + educational => ALLOW.",
			input: DecisionThresholdInput{
				Severity:           SeverityHigh,
				Confidence:         0.92,
				ActionBinding:      ActionBindingPromptOnly,
				EducationalContext: true,
				ProducerRuleID:     "scanner.injection.rm_rf",
				ProducerReason:     "shell injection fragment matched: rm -rf",
			},
			wantDecision:     pb.DecisionType_DECISION_TYPE_ALLOW,
			wantRuleID:       "scanner.injection.rm_rf",
			wantReasonHas:    "educational",
			wantSubReasonHas: "prompt_only",
		},
		{
			name:        "fp_c_api_key_rotation_procedure",
			scenarioDoc: "Compliance procedure document explains key rotation cadence; no key VALUE present, only `API key rotation` phrasing. Today secret_leak generic-credential regex over-refuses. Threshold-tuned: low-severity + prompt-only => ALLOW.",
			input: DecisionThresholdInput{
				Severity:           SeverityLow,
				Confidence:         0.55,
				ActionBinding:      ActionBindingPromptOnly,
				EducationalContext: true,
				ProducerRuleID:     "scanner.secret_leak.generic_credential",
				ProducerReason:     "generic credential assignment phrasing detected (no value)",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_ALLOW,
			wantRuleID:    "scanner.secret_leak.generic_credential",
			wantReasonHas: "educational",
		},
		{
			name:        "fp_d_approval_token_logging_in_compliance_docs",
			scenarioDoc: "Compliance doc describes logging approval-token IDs (not values) for audit. Today the secret_leak token regex over-refuses. Threshold-tuned: low-severity + prompt-only + educational => ALLOW.",
			input: DecisionThresholdInput{
				Severity:           SeverityLow,
				Confidence:         0.5,
				ActionBinding:      ActionBindingPromptOnly,
				EducationalContext: true,
				ProducerRuleID:     "scanner.secret_leak.token_logging",
				ProducerReason:     "token logging phrasing detected (no value)",
			},
			wantDecision:  pb.DecisionType_DECISION_TYPE_ALLOW,
			wantRuleID:    "scanner.secret_leak.token_logging",
			wantReasonHas: "educational",
		},
		{
			name:        "fp_e_metadata_service_education_169_254",
			scenarioDoc: "Security tutorial explains what 169.254.169.254 is and why outbound to it is dangerous; no actual outbound URL action present. Today URL gate over-refuses on the text mention. Threshold-tuned: prompt-only + educational => ALLOW.",
			input: DecisionThresholdInput{
				Severity:           SeverityHigh,
				Confidence:         0.95,
				ActionBinding:      ActionBindingPromptOnly,
				EducationalContext: true,
				ProducerRuleID:     "scanner.keyword.cloud_metadata_ip",
				ProducerReason:     "mention of cloud metadata service IP (169.254.169.254) in input",
			},
			wantDecision:     pb.DecisionType_DECISION_TYPE_ALLOW,
			wantRuleID:       "scanner.keyword.cloud_metadata_ip",
			wantReasonHas:    "educational",
			wantSubReasonHas: "prompt_only",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyByThresholds(tc.input)
			if got.Decision != tc.wantDecision {
				t.Fatalf("decision = %v, want %v\nscenario: %s\ninput: %+v\nreason: %q sub_reason: %q",
					got.Decision, tc.wantDecision, tc.scenarioDoc, tc.input, got.Reason, got.SubReason)
			}
			if tc.wantRuleID != "" && got.RuleID != tc.wantRuleID {
				t.Fatalf("rule_id = %q, want %q (producer rule_id must be preserved)", got.RuleID, tc.wantRuleID)
			}
			if tc.wantReasonHas != "" && !strings.Contains(strings.ToLower(got.Reason), tc.wantReasonHas) {
				t.Fatalf("reason = %q, want substring %q", got.Reason, tc.wantReasonHas)
			}
			if tc.wantSubReasonHas != "" && !strings.Contains(got.SubReason, tc.wantSubReasonHas) {
				t.Fatalf("sub_reason = %q, want substring %q", got.SubReason, tc.wantSubReasonHas)
			}
		})
	}
}

// TestClassifyByThresholds_DenyWhenActionBoundAndHighConfidence proves the
// threshold helper does NOT silently widen the action-layer DENY surface
// (DoD #6: holdout rerun must show reduced over-refusal WITHOUT masking
// action-layer misses). A real action-bound malicious request with high
// confidence MUST still emit DENY even when educational_context is true.
func TestClassifyByThresholds_DenyWhenActionBoundAndHighConfidence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input DecisionThresholdInput
	}{
		{
			name: "action_bound_etc_shadow_write_high_conf",
			input: DecisionThresholdInput{
				Severity:           SeverityCritical,
				Confidence:         0.99,
				ActionBinding:      ActionBindingActionBound,
				EducationalContext: false,
				ProducerRuleID:     "actiongate.file.sensitive_path:etc_shadow",
				ProducerReason:     "filesystem access denied",
			},
		},
		{
			// Action-bound + high-confidence + educational_context MUST
			// still DENY: educational tag does NOT excuse a real action.
			// This is the canonical "confidence-as-weakening" defense
			// (Phase 7.b adversarial check).
			name: "action_bound_outbound_metadata_with_education_still_denies",
			input: DecisionThresholdInput{
				Severity:           SeverityCritical,
				Confidence:         0.99,
				ActionBinding:      ActionBindingActionBound,
				EducationalContext: true,
				ProducerRuleID:     "actiongate.url.metadata_aws",
				ProducerReason:     "metadata service access denied",
			},
		},
		{
			name: "action_bound_mcp_destructive_high_conf",
			input: DecisionThresholdInput{
				Severity:           SeverityHigh,
				Confidence:         0.95,
				ActionBinding:      ActionBindingActionBound,
				EducationalContext: false,
				ProducerRuleID:     "actiongate.mutation.destructive_no_approval",
				ProducerReason:     "destructive action requires approval",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyByThresholds(tc.input)
			if got.Decision != pb.DecisionType_DECISION_TYPE_DENY {
				t.Fatalf("decision = %v, want DENY (action-bound + high-conf must always deny regardless of educational flag)\ninput: %+v\nresult: %+v",
					got.Decision, tc.input, got)
			}
			if got.RuleID != tc.input.ProducerRuleID {
				t.Fatalf("rule_id = %q, want %q (producer rule_id must be preserved on DENY)",
					got.RuleID, tc.input.ProducerRuleID)
			}
		})
	}
}

// TestClassifyByThresholds_RequireHumanForAmbiguousPromptOnly proves the
// REQUIRE_HUMAN path is the default for ambiguous prompt-only content
// without educational context — the explicit goal of DoD #3.
func TestClassifyByThresholds_RequireHumanForAmbiguousPromptOnly(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input DecisionThresholdInput
	}{
		{
			name: "high_severity_prompt_only_no_education",
			input: DecisionThresholdInput{
				Severity:           SeverityHigh,
				Confidence:         0.85,
				ActionBinding:      ActionBindingPromptOnly,
				EducationalContext: false,
				ProducerRuleID:     "scanner.injection.curl_pipe_sh",
				ProducerReason:     "shell injection fragment matched",
			},
		},
		{
			name: "medium_severity_prompt_only",
			input: DecisionThresholdInput{
				Severity:           SeverityMedium,
				Confidence:         0.7,
				ActionBinding:      ActionBindingPromptOnly,
				EducationalContext: false,
				ProducerRuleID:     "scanner.injection.prompt_injection_phrase",
				ProducerReason:     "prompt injection phrasing",
			},
		},
		{
			name: "low_confidence_prompt_only",
			input: DecisionThresholdInput{
				Severity:           SeverityHigh,
				Confidence:         0.4,
				ActionBinding:      ActionBindingPromptOnly,
				EducationalContext: false,
				ProducerRuleID:     "scanner.secret_leak.generic_credential",
				ProducerReason:     "low-confidence credential pattern",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyByThresholds(tc.input)
			if got.Decision != pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN {
				t.Fatalf("decision = %v, want REQUIRE_HUMAN (ambiguous prompt-only without educational context)\ninput: %+v\nresult: %+v",
					got.Decision, tc.input, got)
			}
			if got.RuleID != tc.input.ProducerRuleID {
				t.Fatalf("rule_id = %q, want %q (preserved across REQUIRE_HUMAN routing)",
					got.RuleID, tc.input.ProducerRuleID)
			}
		})
	}
}

// TestClassifyByThresholds_AllowLowSeverityPromptOnly proves the LOW
// severity + prompt-only path emits ALLOW (no human round-trip for
// trivial mentions).
func TestClassifyByThresholds_AllowLowSeverityPromptOnly(t *testing.T) {
	t.Parallel()

	input := DecisionThresholdInput{
		Severity:           SeverityLow,
		Confidence:         0.3,
		ActionBinding:      ActionBindingPromptOnly,
		EducationalContext: false,
		ProducerRuleID:     "scanner.keyword.benign_mention",
		ProducerReason:     "benign keyword surface",
	}
	got := ClassifyByThresholds(input)
	if got.Decision != pb.DecisionType_DECISION_TYPE_ALLOW {
		t.Fatalf("decision = %v, want ALLOW (low-severity prompt-only)\nresult: %+v", got.Decision, got)
	}
}

// TestClassifyByThresholds_EducationalContextSourceIsTrusted documents the
// epic-rail invariant: ClassifyByThresholds consumes EducationalContext as
// a boolean it trusts. Callers MUST populate it from session metadata
// (auth context / pack manifest), NEVER from input_text or claim_text.
// This test exists to fail-loud if a future refactor adds a string-based
// `EducationalClaim` field that would re-introduce the input-text-spoof
// attack surface.
func TestClassifyByThresholds_EducationalContextIsBooleanNotString(t *testing.T) {
	t.Parallel()

	// Construct the input via struct literal. If a future change widens
	// EducationalContext to a string (input-text spoof surface), this
	// test fails because `true` is no longer assignable.
	in := DecisionThresholdInput{
		EducationalContext: true,
	}
	if !in.EducationalContext {
		t.Fatalf("educational_context must be a bool that is true when set; got %T value", in.EducationalContext)
	}
}
