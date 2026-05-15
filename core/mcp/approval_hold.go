package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cordum/cordum/core/edge"
)

// approvalRefArgKey is the JSON property name on tool-call arguments
// that callers use to present an existing approval reference for a
// resume/retry of a previously-held tool call. The `_`-prefix marks
// the field as server-derived/server-reserved — it never reaches the
// upstream tool handler. Strip happens after the claim succeeds.
const approvalRefArgKey = "_approval_ref"

// ApprovalClaimStore is the narrow contract this layer needs from the
// Edge approval store. Production wires this to edge.RedisStore.ClaimApproval;
// tests inject an in-memory fake.
//
// The (consumed, approval, err) return follows the existing RedisStore
// shape: consumed=true with a non-nil approval on success; consumed=false
// + non-nil err typed as *edge.ApprovalConflictError on a fail-closed
// lifecycle conflict; consumed=false + nil err is the benign "approval
// not in claimable state" miss the caller maps to not_found.
type ApprovalClaimStore interface {
	ClaimApproval(ctx context.Context, req edge.ApprovalClaimRequest) (*edge.EdgeApproval, bool, error)
}

// ApprovalHoldDeps bundles the production wiring an approval-claim
// check consumes. Every field is optional so a server without the
// approval store wired short-circuits to "no claim presented"
// (the call proceeds to EDGE-102's policy evaluation as before).
type ApprovalHoldDeps struct {
	Store          ApprovalClaimStore
	PolicySnapshot func(ctx context.Context) string
}

// ApprovalClaimOutcome reports the result of inspecting tool-call
// arguments for an approval claim and consuming it when present.
//
// Consumed=false + nil ConflictErr + ClaimRef=="" means no `_approval_ref`
// was present; the caller proceeds with the normal policy path.
//
// Consumed=true + non-nil Approval means the store accepted the claim;
// the caller dispatches upstream with the approval-stripped arguments.
//
// Consumed=false + non-nil ConflictErr means the store refused the claim
// (fail-closed); the caller surfaces the conflict to the client as a
// JSON-RPC error keyed by ConflictErr.Kind.
type ApprovalClaimOutcome struct {
	Consumed     bool
	ClaimRef     string
	Approval     *edge.EdgeApproval
	ConflictErr  *edge.ApprovalConflictError
	StrippedArgs json.RawMessage
}

// ProcessApprovalClaim inspects the tool-call arguments for an
// `_approval_ref` field and, when present, calls the approval store's
// atomic Consume. On success the returned StrippedArgs has the
// `_approval_ref` field removed so the upstream tool handler never
// sees the server-reserved field; the policy gate also sees the
// stripped form (the request that was originally authorized).
//
// The Edge approval store enforces tenant isolation, expiry, args/policy
// matching, and the self-approval defense-in-depth check. ProcessApprovalClaim
// is the wire-format adapter — it does NOT duplicate any of those checks.
func ProcessApprovalClaim(ctx context.Context, deps ApprovalHoldDeps, params ToolCallParams) (ApprovalClaimOutcome, error) {
	if deps.Store == nil {
		return ApprovalClaimOutcome{}, nil
	}
	if len(params.Arguments) == 0 {
		return ApprovalClaimOutcome{}, nil
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(params.Arguments, &parsed); err != nil {
		// Malformed args is the caller's problem — the policy gate handles
		// it. ProcessApprovalClaim short-circuits.
		return ApprovalClaimOutcome{}, nil
	}
	rawRef, ok := parsed[approvalRefArgKey]
	if !ok {
		return ApprovalClaimOutcome{}, nil
	}
	var approvalRef string
	if err := json.Unmarshal(rawRef, &approvalRef); err != nil {
		return ApprovalClaimOutcome{}, fmt.Errorf("invalid %s: must be a string", approvalRefArgKey)
	}
	approvalRef = strings.TrimSpace(approvalRef)
	if approvalRef == "" {
		return ApprovalClaimOutcome{}, fmt.Errorf("invalid %s: must be non-empty", approvalRefArgKey)
	}

	meta, ok := CallMetadataFromContext(ctx)
	if !ok || strings.TrimSpace(meta.Tenant) == "" {
		return ApprovalClaimOutcome{}, errMissingMCPMetadata
	}

	// Strip the reserved key before computing the canonical hash + before
	// the upstream handler ever sees the payload.
	delete(parsed, approvalRefArgKey)
	strippedBytes, err := json.Marshal(parsed)
	if err != nil {
		return ApprovalClaimOutcome{}, fmt.Errorf("re-marshal stripped args: %w", err)
	}

	policySnapshot := ""
	if deps.PolicySnapshot != nil {
		policySnapshot = deps.PolicySnapshot(ctx)
	}

	claim := edge.ApprovalClaimRequest{
		TenantID:       meta.Tenant,
		ApprovalRef:    approvalRef,
		SessionID:      meta.SessionID,
		ExecutionID:    meta.ExecutionID,
		EventID:        meta.AgentID,
		ActionHash:     CanonicalActionHash(meta.Tenant, "", params.Name, ""),
		InputHash:      hashStrippedArgs(strippedBytes),
		PolicySnapshot: policySnapshot,
		ConsumedAt:     time.Now().UTC(),
		CallerAgentID:  meta.Principal,
	}

	approval, consumed, claimErr := deps.Store.ClaimApproval(ctx, claim)
	if claimErr != nil {
		var conflict *edge.ApprovalConflictError
		if errors.As(claimErr, &conflict) {
			return ApprovalClaimOutcome{
				ClaimRef:    approvalRef,
				ConflictErr: conflict,
			}, nil
		}
		if errors.Is(claimErr, edge.ErrApprovalConflict) {
			return ApprovalClaimOutcome{
				ClaimRef:    approvalRef,
				ConflictErr: &edge.ApprovalConflictError{Kind: edge.ApprovalConflictKindUnknown, Reason: claimErr.Error()},
			}, nil
		}
		if errors.Is(claimErr, edge.ErrNotFound) {
			return ApprovalClaimOutcome{
				ClaimRef:    approvalRef,
				ConflictErr: &edge.ApprovalConflictError{Kind: edge.ApprovalConflictKindNotFound, Reason: "approval not found"},
			}, nil
		}
		return ApprovalClaimOutcome{}, claimErr
	}
	if !consumed || approval == nil {
		return ApprovalClaimOutcome{
			ClaimRef:    approvalRef,
			ConflictErr: &edge.ApprovalConflictError{Kind: edge.ApprovalConflictKindNotFound, Reason: "approval not claimable"},
		}, nil
	}
	return ApprovalClaimOutcome{
		Consumed:     true,
		ClaimRef:     approvalRef,
		Approval:     approval,
		StrippedArgs: strippedBytes,
	}, nil
}

// hashStrippedArgs returns a stable hash for the args payload after the
// reserved `_approval_ref` key has been removed. The hash binds the
// approval lifecycle to the EXACT args shape — any caller-side mutation
// of the args between hold and resume produces a different hash and
// the store-level args_mismatch check fires.
func hashStrippedArgs(stripped []byte) string {
	if len(stripped) == 0 {
		return ""
	}
	return CanonicalActionHash("", "", "", string(stripped))
}

// ApprovalConflictKindFromError extracts the typed
// edge.ApprovalConflictKind from any error returned by the approval
// store. Unknown errors return (Kind="", false) so callers can decide
// whether to map to a generic JSON-RPC error or a structured one.
func ApprovalConflictKindFromError(err error) (edge.ApprovalConflictKind, bool) {
	var typed *edge.ApprovalConflictError
	if errors.As(err, &typed) {
		return typed.Kind, true
	}
	return edge.ApprovalConflictKindUnknown, false
}
