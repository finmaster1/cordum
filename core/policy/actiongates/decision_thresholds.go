package actiongates

import (
	"strings"

	pb "github.com/cordum/cordum/core/protocol/pb/v1"
)

// 3-axis threshold model for routing scanner / gate findings to existing
// Cordum DecisionType values. Producers (action gates, safety-kernel
// scanners, governance evaluator) compute a {severity, confidence,
// action_binding, educational_context} tuple per finding and call
// ClassifyByThresholds to get a deterministic decision.
//
// Design constraints (task task-96f931fe rails):
//
//   - No new DecisionType values; this helper only emits existing pb values:
//     ALLOW / DENY / REQUIRE_HUMAN / ALLOW_WITH_CONSTRAINTS.
//   - No model-in-loop classifier — pure numeric + boolean inputs.
//   - EducationalContext is typed as bool so callers MUST derive it
//     from session metadata (auth context, pack manifest, tenant policy)
//     and never from input_text / claim_text. Widening this field to a
//     string would re-introduce an attacker-controlled spoof surface.
//
// Confidence floors are compiled-in. Phase 5 holdout regression may tune
// these via SafetyPolicy extension; the field hooks remain stable.

// SeverityLevel is the producer-reported severity of a finding. Ordered
// lowest-to-highest so callers can do `>= SeverityHigh` checks without
// string parsing.
type SeverityLevel int

const (
	// SeverityUnspecified is the zero value; callers should not pass this.
	// ClassifyByThresholds treats it as SeverityMedium so a missing severity
	// can never accidentally widen DENY into ALLOW.
	SeverityUnspecified SeverityLevel = iota
	SeverityLow
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

// ActionBinding distinguishes a real backend action (an
// ActionDescriptor with Kind=file/url/mcp_call/mutation and a populated
// TargetPath/TargetURL/etc.) from a prompt-only text mention that
// merely matched a scanner pattern. Action-bound findings carry far
// more attack-surface weight than text-only mentions; the routing
// table reflects that.
type ActionBinding int

const (
	ActionBindingUnspecified ActionBinding = iota
	// ActionBindingPromptOnly means the finding fired on input/output
	// content scanning; no ActionDescriptor.Kind targeting a real backend
	// resource is present. Text mentions of /etc/passwd in a runbook
	// fall here.
	ActionBindingPromptOnly
	// ActionBindingActionBound means a structured ActionDescriptor is
	// present and the finding targets a real backend operation
	// (file write, outbound URL, MCP tool call, destructive mutation).
	ActionBindingActionBound
)

// DecisionThresholdInput is the structured input ClassifyByThresholds
// consumes. All fields are caller-populated; the function is pure and
// thread-safe.
type DecisionThresholdInput struct {
	// Severity is the finding severity. Required; SeverityUnspecified is
	// treated as SeverityMedium to fail-closed.
	Severity SeverityLevel
	// Confidence is the producer's certainty (0.0..1.0) that the match
	// is genuinely malicious (vs. educational / discussion / false
	// positive). Scanner-emitted regex confidences (see
	// core/controlplane/safetykernel/scanners.go) plug in directly.
	Confidence float32
	// ActionBinding indicates whether a real ActionDescriptor backs the
	// finding (see ActionBindingPromptOnly vs. ActionBindingActionBound).
	ActionBinding ActionBinding
	// EducationalContext is the trust signal that the calling context
	// is a security-training / compliance-docs / defensive runbook
	// surface. MUST come from session metadata; NEVER from input_text
	// (epic rail "Action-layer gates must use real backend/request
	// metadata, not user-claimed text").
	EducationalContext bool
	// ProducerRuleID is the existing rule id the producer would have
	// stamped on the unrouted decision (e.g.
	// `actiongate.file.sensitive_path:etc_shadow`,
	// `scanner.injection.rm_rf`). Preserved verbatim through routing so
	// audit trails stay stable across threshold tuning.
	ProducerRuleID string
	// ProducerReason is the human-readable reason the producer would
	// have emitted unrouted. The helper may prefix it (e.g.
	// "educational context: ...") so the final reason string stays
	// informative.
	ProducerReason string
	// ProducerConstraints, when non-nil, carries the structured
	// constraints map a producer wants to attach when an action is
	// allowed under restrictions (sandboxed verb, read-only mode,
	// tier ceiling). The shape mirrors ActionGateDecision.Constraints
	// and core/edge/agentd EvaluateResponse.Constraints — a single
	// canonical carrier across the hook + MCP surfaces. Presence of
	// any entries flips the ALLOW routing to ALLOW_WITH_CONSTRAINTS.
	ProducerConstraints map[string]any
}

// DecisionThresholdResult is what ClassifyByThresholds returns.
// Decision is one of the existing pb.DecisionType values; producers
// then build their wire-format response (ActionGateDecision /
// PolicyDecision / Finding) from it.
type DecisionThresholdResult struct {
	Decision    pb.DecisionType
	RuleID      string
	Reason      string
	SubReason   string
	Constraints map[string]any
}

// confidenceHighFloor is the threshold at or above which a finding's
// confidence is treated as high-confidence. Tuned to 0.8 to clear the
// existing scanner regex confidences (0.8..0.99) while leaving room
// for low-confidence (<0.8) patterns to be routed to REQUIRE_HUMAN.
const confidenceHighFloor float32 = 0.8

// ClassifyByThresholds maps a finding to an existing pb DecisionType
// using the 3-axis rule table:
//
//	action-bound + severity>=High + confidence>=0.8 => DENY
//	action-bound + severity==Low                    => ALLOW
//	action-bound + otherwise                        => REQUIRE_HUMAN
//	prompt-only  + severity==Low                    => ALLOW
//	prompt-only  + educational_context              => ALLOW (reason
//	                                                   prefixed
//	                                                   "educational
//	                                                   context: …")
//	prompt-only  + otherwise                        => REQUIRE_HUMAN
//
// The producer's rule_id is preserved verbatim across all routings.
// SubReason encodes the routing path for audit (e.g.
// "action_bound:high_severity:high_confidence",
// "prompt_only:educational").
func ClassifyByThresholds(in DecisionThresholdInput) DecisionThresholdResult {
	sev := in.Severity
	if sev == SeverityUnspecified {
		sev = SeverityMedium
	}

	out := DecisionThresholdResult{
		RuleID: in.ProducerRuleID,
		Reason: in.ProducerReason,
	}

	switch in.ActionBinding {
	case ActionBindingActionBound:
		if sev >= SeverityHigh && in.Confidence >= confidenceHighFloor {
			out.Decision = pb.DecisionType_DECISION_TYPE_DENY
			out.SubReason = "action_bound:high_severity:high_confidence"
			return out
		}
		if sev == SeverityLow {
			return applyConstraints(out, "action_bound:low_severity", in.ProducerConstraints)
		}
		out.Decision = pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN
		out.SubReason = "action_bound:ambiguous"
		return out

	case ActionBindingPromptOnly:
		if in.EducationalContext {
			out.Reason = prefixEducational(in.ProducerReason)
			return applyConstraints(out, "prompt_only:educational", in.ProducerConstraints)
		}
		if sev == SeverityLow {
			return applyConstraints(out, "prompt_only:low_severity", in.ProducerConstraints)
		}
		out.Decision = pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN
		out.SubReason = "prompt_only:ambiguous"
		return out

	default:
		// Unspecified binding — fail-closed to REQUIRE_HUMAN so an
		// uninitialized caller can never accidentally widen DENY into
		// ALLOW.
		out.Decision = pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN
		out.SubReason = "unspecified_binding"
		return out
	}
}

// applyConstraints finalises an ALLOW result. When producer-supplied
// constraints are present (non-nil, non-empty), the decision becomes
// ALLOW_WITH_CONSTRAINTS and the carrier is propagated through. An
// empty or nil map yields plain ALLOW. SubReason captures the
// pre-constraint routing path so audit consumers can attribute the
// route regardless of whether constraints attached.
func applyConstraints(out DecisionThresholdResult, subReason string, constraints map[string]any) DecisionThresholdResult {
	if len(constraints) > 0 {
		out.Decision = pb.DecisionType_DECISION_TYPE_ALLOW_WITH_CONSTRAINTS
		out.SubReason = subReason + ":with_constraints"
		out.Constraints = constraints
		return out
	}
	out.Decision = pb.DecisionType_DECISION_TYPE_ALLOW
	out.SubReason = subReason
	return out
}

// prefixEducational ensures the reason carries the literal word
// "educational" so audit consumers and tests can detect the routing
// path from the reason field alone. Idempotent if the producer reason
// already mentions it.
func prefixEducational(producerReason string) string {
	if producerReason == "" {
		return "educational context"
	}
	if strings.Contains(strings.ToLower(producerReason), "educational") {
		return producerReason
	}
	return "educational context: " + producerReason
}
