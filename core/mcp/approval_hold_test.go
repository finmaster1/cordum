package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cordum/cordum/core/edge"
)

// fakeApprovalClaimStore satisfies ApprovalClaimStore for tests. Tests
// inject a canned response (approval/consumed/err) and inspect the
// captured request to assert ProcessApprovalClaim built the claim from
// the right fields (tenant from context, args stripped of _approval_ref,
// input hash bound to the stripped form).
type fakeApprovalClaimStore struct {
	calls    int
	lastReq  edge.ApprovalClaimRequest
	approval *edge.EdgeApproval
	consumed bool
	err      error
}

func (f *fakeApprovalClaimStore) ClaimApproval(_ context.Context, req edge.ApprovalClaimRequest) (*edge.EdgeApproval, bool, error) {
	f.calls++
	f.lastReq = req
	return f.approval, f.consumed, f.err
}

func newApprovalHoldCtx() context.Context {
	return WithCallMetadata(context.Background(), CallMetadata{
		Tenant:      "tnt_a",
		Principal:   "principal-a",
		AgentID:     "evt_42",
		SessionID:   "sess_99",
		ExecutionID: "exec_88",
	})
}

// TestProcessApprovalClaim_NoApprovalRefArg_ShortCircuit asserts that
// when the caller did NOT supply an `_approval_ref` field, the helper
// returns the zero outcome (Consumed=false, ConflictErr=nil) and never
// touches the store. This is the hot path for first-time tool calls
// that have not yet been gated.
func TestProcessApprovalClaim_NoApprovalRefArg_ShortCircuit(t *testing.T) {
	t.Parallel()
	store := &fakeApprovalClaimStore{}
	deps := ApprovalHoldDeps{Store: store}
	outcome, err := ProcessApprovalClaim(newApprovalHoldCtx(), deps, ToolCallParams{
		Name:      "fs.read_file",
		Arguments: json.RawMessage(`{"path":"/etc/hostname"}`),
	})
	if err != nil {
		t.Fatalf("ProcessApprovalClaim returned err: %v", err)
	}
	if outcome.Consumed {
		t.Fatal("expected Consumed=false without _approval_ref")
	}
	if outcome.ConflictErr != nil {
		t.Fatalf("expected ConflictErr=nil without _approval_ref, got %v", outcome.ConflictErr)
	}
	if store.calls != 0 {
		t.Fatalf("store.calls = %d; want 0 (no claim should be presented)", store.calls)
	}
}

// TestProcessApprovalClaim_HappyPath asserts the consume succeeds when
// the store accepts the claim: outcome.Consumed=true, approval is
// surfaced, and StrippedArgs has the `_approval_ref` key removed so the
// upstream tool handler never sees the server-reserved field.
func TestProcessApprovalClaim_HappyPath(t *testing.T) {
	t.Parallel()
	expires := time.Date(2026, 5, 15, 20, 0, 0, 0, time.UTC)
	approval := &edge.EdgeApproval{
		ApprovalRef: "edge_appr_xyz",
		TenantID:    "tnt_a",
		Status:      edge.ApprovalStatusApproved,
		Decision:    edge.ApprovalDecisionApprove,
		ExpiresAt:   &expires,
	}
	store := &fakeApprovalClaimStore{approval: approval, consumed: true}
	deps := ApprovalHoldDeps{
		Store: store,
		PolicySnapshot: func(_ context.Context) string {
			return "policy-v7"
		},
	}
	outcome, err := ProcessApprovalClaim(newApprovalHoldCtx(), deps, ToolCallParams{
		Name:      "fs.write",
		Arguments: json.RawMessage(`{"path":"/var/data/x.db","_approval_ref":"edge_appr_xyz"}`),
	})
	if err != nil {
		t.Fatalf("ProcessApprovalClaim returned err: %v", err)
	}
	if !outcome.Consumed {
		t.Fatal("expected Consumed=true on store success")
	}
	if outcome.ClaimRef != "edge_appr_xyz" {
		t.Fatalf("ClaimRef = %q; want edge_appr_xyz", outcome.ClaimRef)
	}
	if strings.Contains(string(outcome.StrippedArgs), "_approval_ref") {
		t.Fatalf("StrippedArgs still contains _approval_ref: %s", outcome.StrippedArgs)
	}
	if !strings.Contains(string(outcome.StrippedArgs), `"path":"/var/data/x.db"`) {
		t.Fatalf("StrippedArgs lost the path field: %s", outcome.StrippedArgs)
	}
	if store.lastReq.PolicySnapshot != "policy-v7" {
		t.Fatalf("claim.PolicySnapshot = %q; want policy-v7 (from deps.PolicySnapshot)", store.lastReq.PolicySnapshot)
	}
	if store.lastReq.CallerAgentID != "principal-a" {
		t.Fatalf("claim.CallerAgentID = %q; want principal-a (from CallMetadata)", store.lastReq.CallerAgentID)
	}
}

// TestProcessApprovalClaim_TypedConflictKind asserts that when the
// store returns *edge.ApprovalConflictError the helper surfaces the
// typed Kind on outcome.ConflictErr so the JSON-RPC layer can render
// error.data.kind without re-parsing the error message.
func TestProcessApprovalClaim_TypedConflictKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		kind edge.ApprovalConflictKind
	}{
		{"self_approval", edge.ApprovalConflictKindSelfApproval},
		{"args_mismatch", edge.ApprovalConflictKindArgsMismatch},
		{"policy_mismatch", edge.ApprovalConflictKindPolicyMismatch},
		{"expired", edge.ApprovalConflictKindExpired},
		// EDGE-103 DoD #4: duplicate consume + concurrent attempts MUST
		// fail closed. The store CAS surfaces "consumed" on the second
		// attempt; the wire adapter must pass it through verbatim so
		// the JSON-RPC -32096 error.data.kind matches the snake_case
		// enum the client branches on.
		{"consumed", edge.ApprovalConflictKindConsumed},
		{"tuple_mismatch", edge.ApprovalConflictKindTupleMismatch},
		{"cross_tenant", edge.ApprovalConflictKindCrossTenant},
		{"rejected", edge.ApprovalConflictKindRejected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeApprovalClaimStore{
				err: &edge.ApprovalConflictError{Kind: tc.kind, Reason: "test-fixture"},
			}
			outcome, err := ProcessApprovalClaim(newApprovalHoldCtx(), ApprovalHoldDeps{Store: store}, ToolCallParams{
				Name:      "fs.write",
				Arguments: json.RawMessage(`{"path":"/x","_approval_ref":"edge_appr_xyz"}`),
			})
			if err != nil {
				t.Fatalf("ProcessApprovalClaim should NOT surface ApprovalConflictError as a plain error; got %v", err)
			}
			if outcome.Consumed {
				t.Fatal("Consumed=true on conflict path")
			}
			if outcome.ConflictErr == nil {
				t.Fatal("ConflictErr is nil; expected typed kind")
			}
			if outcome.ConflictErr.Kind != tc.kind {
				t.Fatalf("ConflictErr.Kind = %q; want %q", outcome.ConflictErr.Kind, tc.kind)
			}
		})
	}
}

// TestProcessApprovalClaim_NotFoundMaps asserts that ErrNotFound from
// the store maps to ApprovalConflictKindNotFound on the outcome. The
// store returns this when the approval_ref doesn't resolve to any
// record (typo, cross-tenant probe, replay long after consume).
func TestProcessApprovalClaim_NotFoundMaps(t *testing.T) {
	t.Parallel()
	store := &fakeApprovalClaimStore{err: edge.ErrNotFound}
	outcome, err := ProcessApprovalClaim(newApprovalHoldCtx(), ApprovalHoldDeps{Store: store}, ToolCallParams{
		Name:      "fs.write",
		Arguments: json.RawMessage(`{"path":"/x","_approval_ref":"edge_appr_unknown"}`),
	})
	if err != nil {
		t.Fatalf("ProcessApprovalClaim returned err: %v", err)
	}
	if outcome.ConflictErr == nil || outcome.ConflictErr.Kind != edge.ApprovalConflictKindNotFound {
		t.Fatalf("expected ConflictErr.Kind=not_found, got %#v", outcome.ConflictErr)
	}
}

// TestProcessApprovalClaim_MissingMetadataFailsClosed asserts the
// helper refuses to dispatch a claim when CallMetadata is missing from
// context. This mirrors EDGE-102's EvaluateToolCall guard — without
// tenant attribution we cannot consume an approval safely.
func TestProcessApprovalClaim_MissingMetadataFailsClosed(t *testing.T) {
	t.Parallel()
	store := &fakeApprovalClaimStore{consumed: true}
	_, err := ProcessApprovalClaim(context.Background(), ApprovalHoldDeps{Store: store}, ToolCallParams{
		Name:      "fs.write",
		Arguments: json.RawMessage(`{"path":"/x","_approval_ref":"edge_appr_xyz"}`),
	})
	if err == nil {
		t.Fatal("expected missing_mcp_metadata error; got nil")
	}
	if !errors.Is(err, errMissingMCPMetadata) {
		t.Fatalf("expected errMissingMCPMetadata, got %v", err)
	}
	if store.calls != 0 {
		t.Fatalf("store.calls = %d; want 0 (no claim without metadata)", store.calls)
	}
}

// TestBuildApprovalClaimRequest_MintAndConsumeProduceMatchingHashes is the
// EDGE-103 reopen #1 regression: the mint side (gateway handoff) and the
// consume side (ProcessApprovalClaim) MUST derive the SAME ActionHash and
// InputHash from the same tenant/server/tool/args/policy snapshot tuple,
// or the edge.RedisStore.ClaimApproval check returns args_mismatch even
// on legitimate retries. Centralising the binding in one helper is the
// safest defense against drift.
func TestBuildApprovalClaimRequest_MintAndConsumeProduceMatchingHashes(t *testing.T) {
	t.Parallel()
	meta := CallMetadata{
		Tenant:      "tnt_a",
		Principal:   "alice@corp",
		AgentID:     "agent_alpha",
		SessionID:   "sess_99",
		ExecutionID: "exec_88",
	}
	params := ToolCallParams{
		Name:      "fs.write",
		Arguments: json.RawMessage(`{"path":"/var/data/x.db","contents":"hi"}`),
	}
	const server = "cordum.builtin"
	const policySnapshot = "policy-v7"

	mintAction, mintInput := BuildMCPApprovalBinding(meta.Tenant, server, params, policySnapshot)
	consumeAction, consumeInput := BuildMCPApprovalBinding(meta.Tenant, server, params, policySnapshot)

	if mintAction == "" || mintInput == "" {
		t.Fatalf("mint binding produced empty hashes: action=%q input=%q", mintAction, mintInput)
	}
	if mintAction != consumeAction {
		t.Errorf("ActionHash drift: mint=%q consume=%q", mintAction, consumeAction)
	}
	if mintInput != consumeInput {
		t.Errorf("InputHash drift: mint=%q consume=%q", mintInput, consumeInput)
	}

	// Changing args MUST flip InputHash but keep ActionHash stable
	// (ActionHash binds tenant/server/tool/path; InputHash binds args).
	mutated := ToolCallParams{
		Name:      params.Name,
		Arguments: json.RawMessage(`{"path":"/var/data/x.db","contents":"different"}`),
	}
	mutatedAction, mutatedInput := BuildMCPApprovalBinding(meta.Tenant, server, mutated, policySnapshot)
	if mutatedAction != mintAction {
		t.Errorf("ActionHash flipped on args-only change; mint=%q mutated=%q", mintAction, mutatedAction)
	}
	if mutatedInput == mintInput {
		t.Errorf("InputHash did NOT change on args mutation; both = %q (args-mismatch defense broken)", mintInput)
	}
}

// TestBuildMCPApprovalBinding_StripsApprovalRef ensures the binding
// helper ignores the server-reserved `_approval_ref` field when hashing
// args. Without this, the resume retry's args (which still carry
// `_approval_ref`) would hash differently from the original gated args,
// and ClaimApproval would surface args_mismatch on every legitimate
// retry.
func TestBuildMCPApprovalBinding_StripsApprovalRef(t *testing.T) {
	t.Parallel()
	original := ToolCallParams{
		Name:      "fs.write",
		Arguments: json.RawMessage(`{"path":"/x","contents":"hi"}`),
	}
	withRef := ToolCallParams{
		Name:      "fs.write",
		Arguments: json.RawMessage(`{"path":"/x","contents":"hi","_approval_ref":"edge_appr_xyz"}`),
	}
	_, origIn := BuildMCPApprovalBinding("tnt_a", "cordum.builtin", original, "p")
	_, refIn := BuildMCPApprovalBinding("tnt_a", "cordum.builtin", withRef, "p")
	if origIn != refIn {
		t.Errorf("InputHash differs once `_approval_ref` is present; orig=%q with-ref=%q (would force args_mismatch on every legitimate retry)",
			origIn, refIn)
	}
}

// TestApprovalConflictKindFromError exercises the helper that the
// JSON-RPC layer uses to extract the typed kind from a generic error.
func TestApprovalConflictKindFromError(t *testing.T) {
	t.Parallel()
	if kind, ok := ApprovalConflictKindFromError(&edge.ApprovalConflictError{Kind: edge.ApprovalConflictKindConsumed}); !ok || kind != edge.ApprovalConflictKindConsumed {
		t.Fatalf("typed extraction failed: (%q,%v)", kind, ok)
	}
	if _, ok := ApprovalConflictKindFromError(errors.New("random error")); ok {
		t.Fatal("non-typed error should return ok=false")
	}
	if _, ok := ApprovalConflictKindFromError(nil); ok {
		t.Fatal("nil error should return ok=false")
	}
}
