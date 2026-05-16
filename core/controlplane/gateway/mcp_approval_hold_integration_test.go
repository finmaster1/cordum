package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/cordum/cordum/core/edge"
	"github.com/cordum/cordum/core/mcp"
	"github.com/cordum/cordum/core/model"
	"github.com/redis/go-redis/v9"
)

// TestApprovalHoldMintConsumeIntegration_Miniredis is the EDGE-103
// umbrella reopen #2 regression. The previous false-green from
// `fakeApprovalClaimStore` (server_approval_hold_e2e_test.go:109) hid
// the InputHash divergence at mcp_policy_wire.go:157 because the fake
// returned canned `consumed=true` without validating the
// (tenant, session, execution, event, action_hash, input_hash, policy_snapshot)
// tuple. This integration test wires a real edge.RedisStore against a
// miniredis instance, runs the production gatewayApprovalGate.ConsumeActionGateDecision
// mint path, then runs the production ProcessApprovalClaim consume path
// against the SAME store with the SAME canonical args, and asserts the
// tuple-equality check (classifyApprovalClaimMismatch) does NOT surface
// kind=args_mismatch. Negative scenario: mutated args MUST surface kind=args_mismatch.
//
// Without this test the InputHash drift QA flagged (mint stores
// CanonicalActionHash; consume computes CanonicaliseArgs SHA-256) would
// re-regress silently.
func TestApprovalHoldMintConsumeIntegration_Miniredis(t *testing.T) {
	t.Parallel()
	mr := miniredis.RunT(t)
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), PoolSize: 1})
	t.Cleanup(func() { _ = client.Close() })

	// Use real `time.Now()` as the base. ProcessApprovalClaim sets
	// ConsumedAt = time.Now().UTC() internally (no Clock injection), so
	// pinning the store clock to a fixed past time would make every
	// claim look expired. Real-time base keeps the store's ExpiresAt
	// (base + defaultTTL) ahead of the claim's ConsumedAt.
	base := time.Now().UTC()
	edgeStore := edge.NewRedisStoreFromClient(client, edge.WithClock(func() time.Time { return base }))

	const (
		tenantID    = "tnt_a"
		sessionID   = "sess_hold_integration"
		executionID = "exec_hold_integration"
		eventID     = "agent_alpha" // mint side stores meta.AgentID as EventID; consume side echoes
		principalID = "alice"
		toolName    = "fs.write"
		serverName  = "cordum.builtin"
	)
	ctx := context.Background()

	// Seed the EdgeSession + AgentExecution + AgentActionEvent parents
	// that EnqueueApproval validates against. The parent event's
	// (InputHash, PolicySnapshot) MUST match what the mint produces or
	// validateApprovalEventBinding refuses with input_hash_mismatch.
	args := json.RawMessage(`{"path":"/etc/hostname","contents":"hi"}`)
	_, expectedInputHash := mcp.BuildMCPApprovalBinding(tenantID, serverName,
		mcp.ToolCallParams{Name: toolName, Arguments: args}, "policy-v7")
	seedApprovalParents(t, ctx, edgeStore, tenantID, sessionID, executionID, eventID, principalID, base, expectedInputHash)

	gate := &gatewayApprovalGate{
		store:          &MCPApprovalStore{},
		edgeStore:      edgeStore,
		policySnapshot: func(context.Context) string { return "policy-v7" },
		serverName:     serverName,
	}

	// Mint context carries the requester principal. The consume side
	// uses a DIFFERENT principal (`consumerPrincipal`) so the store's
	// "caller is requester" self-approval guard doesn't fire — that
	// check is separate from the InputHash binding this test pins.
	const consumerPrincipal = "agent-runner"
	ctxMeta := mcp.WithCallMetadata(ctx, mcp.CallMetadata{
		Tenant:      tenantID,
		Principal:   principalID,
		AgentID:     eventID,
		SessionID:   sessionID,
		ExecutionID: executionID,
	})
	consumeCtx := mcp.WithCallMetadata(ctx, mcp.CallMetadata{
		Tenant:      tenantID,
		Principal:   consumerPrincipal,
		AgentID:     eventID,
		SessionID:   sessionID,
		ExecutionID: executionID,
	})

	// Mint via ConsumeActionGateDecision (the QA-flagged path).
	ref, err := gate.ConsumeActionGateDecision(ctxMeta, mcp.PolicyDecision{}, mcp.ToolCallApprovalContext{
		Tenant:     tenantID,
		AgentID:    eventID,
		Server:     serverName,
		Tool:       toolName,
		ActionHash: mcp.CanonicalActionHash(tenantID, serverName, toolName, "/etc/hostname"),
		Args:       args,
	})
	if err != nil {
		t.Fatalf("ConsumeActionGateDecision mint: %v", err)
	}
	if ref == "" || ref[:10] != "edge_appr_" {
		t.Fatalf("ref = %q; want Edge-prefixed handle (mint must mint via edgeStore, not legacy)", ref)
	}

	// Out-of-band approve via the store API so the resume can claim it.
	approved, err := edgeStore.ApproveApproval(ctx, edge.ApprovalResolution{
		TenantID:    tenantID,
		ApprovalRef: ref,
		ResolverID:  "principal-reviewer",
		ResolvedBy:  "reviewer@example.invalid",
		Reason:      "integration test approve",
		ResolvedAt:  base, // ResolvedAt must be <= consumedAt (real time.Now() during ProcessApprovalClaim)
	})
	if err != nil {
		t.Fatalf("ApproveApproval: %v", err)
	}
	if approved.Status != edge.ApprovalStatusApproved {
		t.Fatalf("approval status = %q; want Approved", approved.Status)
	}

	// HAPPY PATH: resume with SAME args via ProcessApprovalClaim through
	// the real edge.RedisStore. Must NOT surface ArgsMismatch.
	resumeArgs := json.RawMessage(`{"path":"/etc/hostname","contents":"hi","_approval_ref":"` + ref + `"}`)
	outcome, err := mcp.ProcessApprovalClaim(consumeCtx, mcp.ApprovalHoldDeps{
		Store:          edgeStore,
		PolicySnapshot: func(context.Context) string { return "policy-v7" },
		ServerName:     serverName,
	}, mcp.ToolCallParams{Name: toolName, Arguments: resumeArgs})
	if err != nil {
		t.Fatalf("ProcessApprovalClaim happy path: %v", err)
	}
	if outcome.ConflictErr != nil {
		t.Fatalf("happy path returned ConflictErr.Kind=%q reason=%q — this is the EDGE-103 reopen #2 root cause (mint InputHash != consume InputHash); the fix at mcp_policy_wire.go:175 (mint via BuildMCPApprovalBinding) must hold",
			outcome.ConflictErr.Kind, outcome.ConflictErr.Reason)
	}
	if !outcome.Consumed {
		t.Fatal("outcome.Consumed = false on happy path; mint and consume tuples must match for dispatch to proceed")
	}
	if outcome.Approval == nil || outcome.Approval.ApprovalRef != ref {
		t.Fatalf("outcome.Approval mismatch: got %+v; want ref=%q", outcome.Approval, ref)
	}

	// NEGATIVE PATH: mutated args MUST surface kind=args_mismatch. Mint
	// a fresh approval (the first one is already consumed) and resume
	// with different args.
	seedApprovalParents(t, ctx, edgeStore, tenantID, sessionID+"_neg", executionID+"_neg", eventID, principalID, base, expectedInputHash)
	negCtx := mcp.WithCallMetadata(ctx, mcp.CallMetadata{
		Tenant:      tenantID,
		Principal:   principalID,
		AgentID:     eventID,
		SessionID:   sessionID + "_neg",
		ExecutionID: executionID + "_neg",
	})
	negConsumeCtx := mcp.WithCallMetadata(ctx, mcp.CallMetadata{
		Tenant:      tenantID,
		Principal:   consumerPrincipal,
		AgentID:     eventID,
		SessionID:   sessionID + "_neg",
		ExecutionID: executionID + "_neg",
	})
	negRef, err := gate.ConsumeActionGateDecision(negCtx, mcp.PolicyDecision{}, mcp.ToolCallApprovalContext{
		Tenant:     tenantID,
		AgentID:    eventID,
		Server:     serverName,
		Tool:       toolName,
		ActionHash: mcp.CanonicalActionHash(tenantID, serverName, toolName, "/etc/hostname"),
		Args:       args,
	})
	if err != nil {
		t.Fatalf("negative-path mint: %v", err)
	}
	if _, err := edgeStore.ApproveApproval(ctx, edge.ApprovalResolution{
		TenantID:    tenantID,
		ApprovalRef: negRef,
		ResolverID:  "principal-reviewer",
		ResolvedBy:  "reviewer@example.invalid",
		Reason:      "negative-path approve",
		ResolvedAt:  base,
	}); err != nil {
		t.Fatalf("ApproveApproval negative: %v", err)
	}

	mutatedArgs := json.RawMessage(`{"path":"/etc/passwd","contents":"hi","_approval_ref":"` + negRef + `"}`)
	negOutcome, err := mcp.ProcessApprovalClaim(negConsumeCtx, mcp.ApprovalHoldDeps{
		Store:          edgeStore,
		PolicySnapshot: func(context.Context) string { return "policy-v7" },
		ServerName:     serverName,
	}, mcp.ToolCallParams{Name: toolName, Arguments: mutatedArgs})
	if err != nil {
		t.Fatalf("ProcessApprovalClaim negative path returned hard error: %v", err)
	}
	if negOutcome.Consumed {
		t.Fatal("mutated-args resume succeeded — args binding regression; the gate MUST refuse when InputHash differs from minted")
	}
	if negOutcome.ConflictErr == nil {
		t.Fatal("mutated-args resume returned no conflict error; expected ArgsMismatch")
	}
	if negOutcome.ConflictErr.Kind != edge.ApprovalConflictKindArgsMismatch {
		t.Errorf("ConflictErr.Kind = %q; want %q (mutation-resistant assertion)",
			negOutcome.ConflictErr.Kind, edge.ApprovalConflictKindArgsMismatch)
	}

	// Sanity: a SECOND happy-path consume of the (already-consumed) ref
	// MUST fail with kind=not_found-or-similar. Tests the consume-once
	// contract under the real CAS path.
	dupOutcome, err := mcp.ProcessApprovalClaim(consumeCtx, mcp.ApprovalHoldDeps{
		Store:          edgeStore,
		PolicySnapshot: func(context.Context) string { return "policy-v7" },
		ServerName:     serverName,
	}, mcp.ToolCallParams{Name: toolName, Arguments: resumeArgs})
	if err != nil && !errors.Is(err, edge.ErrApprovalConflict) {
		t.Fatalf("duplicate consume hard error: %v", err)
	}
	if dupOutcome.Consumed {
		t.Fatal("duplicate consume succeeded — consume-once contract broken; CAS should have refused the second claim")
	}
}

// seedApprovalParents creates the EdgeSession + AgentExecution +
// AgentActionEvent records that edge.RedisStore.EnqueueApproval
// validates against. Without these the store rejects the enqueue
// before the tuple is even stored.
func seedApprovalParents(t *testing.T, ctx context.Context, store *edge.RedisStore, tenantID, sessionID, executionID, eventID, principalID string, base time.Time, inputHash string) {
	t.Helper()
	if err := store.CreateSession(ctx, edge.EdgeSession{
		SessionID:         sessionID,
		TenantID:          tenantID,
		PrincipalID:       principalID,
		PrincipalType:     edge.PrincipalTypeHuman,
		AgentProduct:      "Claude Code",
		AgentVersion:      "2.1.0",
		Mode:              edge.SessionModeLocalDev,
		PolicySnapshot:    "policy-v7",
		EnforcementLayers: edge.EnforcementLayers{"mcp": true},
		PolicyMode:        edge.PolicyModeEnforce,
		Status:            edge.SessionStatusRunning,
		RiskSummary:       edge.RiskSummary{MaxRisk: edge.RiskLevelLow},
		StartedAt:         base,
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := store.CreateExecution(ctx, edge.AgentExecution{
		ExecutionID:    executionID,
		SessionID:      sessionID,
		TenantID:       tenantID,
		Adapter:        edge.AdapterMCPGateway,
		Mode:           edge.ExecutionModeLocalDev,
		PolicySnapshot: "policy-v7",
		Status:         edge.ExecutionStatusRunning,
		StartedAt:      base.Add(time.Second),
	}); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	// EnqueueApproval requires a parent AgentActionEvent for binding.
	if _, err := store.AppendEvent(ctx, edge.AgentActionEvent{
		EventID:        eventID,
		SessionID:      sessionID,
		ExecutionID:    executionID,
		TenantID:       tenantID,
		PrincipalID:    principalID,
		Timestamp:      base.Add(2 * time.Second),
		Layer:          edge.LayerMCP,
		Kind:           edge.EventKindApprovalRequested,
		ActionName:     "fs.write",
		ToolName:       "fs.write",
		Decision:       edge.DecisionRequireApproval,
		Status:         edge.ActionStatusBlocked,
		InputHash:      inputHash,
		PolicySnapshot: "policy-v7",
	}); err != nil {
		t.Fatalf("AppendEvent parent: %v", err)
	}
}

// silence unused-import warnings when the file compiles without other
// types referenced.
var _ = model.DefaultTenant
