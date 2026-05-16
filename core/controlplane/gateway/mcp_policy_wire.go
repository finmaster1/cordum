package gateway

import (
	"context"
	"fmt"

	"github.com/cordum/cordum/core/infra/config"
	"github.com/cordum/cordum/core/mcp"
	"github.com/cordum/cordum/core/policy/actiongates"
)

// policyDispatcherAdapter wraps the production action-gate pipeline so
// the core/mcp policy layer can dispatch against it through the narrow
// mcp.PolicyDispatcher interface. The adapter exists to break the
// import cycle that core/policy/actiongates would otherwise close if
// core/mcp imported it (actiongates already imports core/mcp for
// AgentIdentity).
type policyDispatcherAdapter struct {
	pipeline *actiongates.Pipeline
}

// Dispatch maps the mcp call to actiongates.Pipeline.Run and adapts
// the returned ActionGateDecision into the mcp-local PolicyDecision
// shape. A nil pipeline returns the zero decision (treated as allow)
// so a gateway boot without the action gate wired falls through to
// the legacy approval flow without crashing.
//
// Constraints propagate when the gate fires ALLOW_WITH_CONSTRAINTS so
// the mcp audit event records the same `_constraints` payload that
// agentd consumers see — keeps the gate's verdict bound to the same
// constraint metadata across the hook + MCP surfaces (no parallel
// subsystem). No-op for non-AWC decisions: the source map is nil and
// the copy preserves nil rather than substituting an empty map.
func (a policyDispatcherAdapter) Dispatch(ctx context.Context, in *config.PolicyInput) (mcp.PolicyDecision, bool) {
	if a.pipeline == nil {
		return mcp.PolicyDecision{}, false
	}
	dec, fired := a.pipeline.Run(ctx, in)
	return mcp.PolicyDecision{
		Decision:    dec.Decision,
		GateID:      dec.GateID,
		Code:        dec.Code,
		Reason:      dec.Reason,
		SubReason:   dec.SubReason,
		Extra:       dec.Extra,
		Constraints: dec.Constraints,
	}, fired
}

// ConsumeActionGateDecision routes a REQUIRE_HUMAN gate decision into
// the existing MCPApprovalStore lifecycle. The gate has already
// computed the canonical action hash; we reuse it (instead of
// recomputing) so the gate's decision and the approval record stay
// bound to the same key. Returns the approval reference the client
// surfaces as an approval-pending marker.
//
// Precedence (matches gatewayApprovalGate.Check exactly):
//  1. MCPInvariantLookup DENY — SECURITY FLOOR, always wins.
//  2. PreapprovalLookup HIT — skip approval store entirely.
//  3. Fall through to MCPApprovalStore claim/enqueue.
//
// On invariant DENY this returns an error so the caller can map it
// to a -32097 / -32099 JSON-RPC code. On preapproval HIT, returns
// ("", nil) so the caller treats the call as immediately allowed.
func (g *gatewayApprovalGate) ConsumeActionGateDecision(ctx context.Context, _ mcp.PolicyDecision, ctxData mcp.ToolCallApprovalContext) (string, error) {
	if g == nil || g.store == nil {
		return "", fmt.Errorf("mcp gate: approval store unavailable")
	}
	// Invariant DENY first — fail closed regardless of the actiongate
	// decision so a pack-contributed actiongate ALLOW cannot override
	// a SecOps invariant block.
	if g.invariants != nil {
		rules := g.invariants.InvariantsForMCPTool(ctx)
		// We synthesize a minimal mcp.Tool/CallMetadata view from the
		// context payload so matchMCPInvariantDeny can run.
		tool := mcp.Tool{Name: ctxData.Tool}
		meta := MCPCallMetadata{Tenant: ctxData.Tenant, AgentID: ctxData.AgentID}
		if rule, denied := matchMCPInvariantDeny(rules, tool, meta); denied {
			return "", fmt.Errorf("%w: tool %q denied by invariant %q",
				ErrMCPInvariantDeny, ctxData.Tool, rule.ID)
		}
	}
	// Preapproval HIT short-circuits the approval store. The bridge's
	// pre event has already been emitted with Decision=require_approval;
	// returning ("", nil) lets the caller fall through to upstream as
	// if the call had been allowed. Production wiring should also fire
	// an audit-side preapproval emission via the invocation handle.
	if g.preapproval != nil && g.preapproval.IsPreapproved(ctx, ctxData.Tenant, ctxData.AgentID, ctxData.Tool) {
		return "", nil
	}
	// Fall through: claim-or-enqueue against the canonical action hash.
	if rec, err := g.store.ClaimPreApproved(ctx, ctxData.Tenant, ctxData.AgentID, ctxData.Tool, ctxData.ActionHash); err != nil {
		return "", fmt.Errorf("mcp gate: pre-approval claim: %w", err)
	} else if rec != nil {
		return rec.ID, nil
	}
	// No prior approval — enqueue pending and return the new ref. The
	// EnqueueMCPApproval store API requires an MCPApprovalRequest; we
	// build it from the context payload + action hash.
	req := &MCPApprovalRequest{
		Tenant:   ctxData.Tenant,
		AgentID:  ctxData.AgentID,
		ToolName: ctxData.Tool,
		ArgsHash: ctxData.ActionHash,
	}
	rec, err := g.store.EnqueueMCPApproval(ctx, req)
	if err != nil {
		return "", fmt.Errorf("mcp gate: enqueue approval: %w", err)
	}
	return rec.ID, nil
}

// BuildMCPPolicyDeps assembles the production ToolCallDeps the MCP
// server consumes via MCPServer.WithPolicyGate. Fail-closed contract:
// any nil required dep (pipeline, emitter, store) returns the zero
// ToolCallDeps so the MCPServer.WithPolicyGate partial-wiring guard
// resets the gate to off and HasPolicyGate() reports false. Without
// this guard, noop-fallback adapters would satisfy the interface
// check inside server.go while silently dropping every event, leaving
// HasPolicyGate() falsely true on inert wiring.
//
// gate (ApprovalHandoff) may legitimately be nil — handlers_mcp.go
// disables the MCP approval store when Redis is unavailable and the
// EvaluateToolCall path skips the REQUIRE_HUMAN handoff branch in
// that case. Pipeline/emitter/store are mandatory because their nil
// branches would produce a silently-degraded gate.
func BuildMCPPolicyDeps(pipeline *actiongates.Pipeline, gate *gatewayApprovalGate, emitter mcp.EventEmitter, store mcp.ArtifactStore) mcp.ToolCallDeps {
	if pipeline == nil || emitter == nil || store == nil {
		return mcp.ToolCallDeps{}
	}
	return mcp.ToolCallDeps{
		Pipeline:        policyDispatcherAdapter{pipeline: pipeline},
		EventEmitter:    emitter,
		ArtifactStore:   store,
		ApprovalHandoff: gate,
		Redactor:        mcp.DefaultRedactor(),
	}
}
