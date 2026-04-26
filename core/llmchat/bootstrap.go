// Package-level chat-assistant agent bootstrap.
//
// On first boot the cordum-llm-chat process registers a "chat-assistant"
// agent identity with Cordum so that every subsequent CallTool carries
// the same CAP-tagged AgentIdentity any other Cordum agent does — this
// is the dogfooding integration point per task rail #1.
//
// Registration goes through the CAP SDK's AgentClient
// (capsdk.AgentClient, shipped in cap PR #44 / commit aad9445). The
// SDK wraps the same control-plane endpoints (POST/GET/PUT
// /api/v1/agents) the gateway exposes — same audit chain, same
// approval-gate path. The earlier MCP-tool bootstrap fallback was
// removed per pre-GA / no-compat-shim policy
// (feedback_no_backwards_compat).
package llmchat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	capsdk "github.com/cordum-io/cap/v2/sdk/go"
	"github.com/cordum/cordum/core/audit"
	"github.com/cordum/cordum/core/mcp"
)

// AuditEmitter is the slice of audit.Chainer the bootstrap needs.
// Defined as an interface so tests can inject a recorder without
// pulling in a real Redis.
type AuditEmitter interface {
	Append(ctx context.Context, event *audit.SIEMEvent) error
}

// AgentRegistry is the slim contract bootstrap needs from the CAP SDK
// AgentClient. capsdk.AgentClient satisfies it directly; tests inject
// a fake.
//
// Behavior contract:
//   - Lookup: returns capsdk.ErrAgentNotFound when no match, or a
//     wrapped capsdk.ErrAgentDuplicate when more than one matches.
//   - Register: returns the server-assigned agent id; never grants
//     PreapprovedMutatingTools (rail #2).
//   - SetScope: applies the scope update; ALWAYS sends
//     preapproved_mutating_tools (deterministic revoke).
type AgentRegistry interface {
	Lookup(ctx context.Context, name, tenant string) (*capsdk.AgentIdentity, error)
	Register(ctx context.Context, spec capsdk.AgentSpec) (string, error)
	SetScope(ctx context.Context, update capsdk.AgentScopeUpdate) error
}

// Bootstrapper handles idempotent chat-assistant registration on
// service startup. Boot is safe to call from a single goroutine on
// startup; the resulting agent identity is consumed by the rest of
// the service via the returned id.
type Bootstrapper struct {
	registry AgentRegistry
	tenant   string
	emitter  AuditEmitter
}

// NewBootstrapper constructs a Bootstrapper bound to the supplied
// tenant. The tenant is forwarded to the lookup filter so the same
// chat-assistant identity is scoped per tenant in multi-tenant
// deployments. The emitter, when non-nil, receives a
// `chat.bootstrap_registered` SIEMEvent on first-boot register-success
// (NOT on lookup-hit reuse — the event represents agent creation, not
// service boot). The action string lives in core/audit so phase-5
// websocket/session handlers share the same chat.* action family.
func NewBootstrapper(registry AgentRegistry, tenant string, emitter AuditEmitter) *Bootstrapper {
	return &Bootstrapper{registry: registry, tenant: tenant, emitter: emitter}
}

// expectedAllowedTools is the canonical AllowedTools list for the
// chat-assistant agent identity. Includes ALL read-only MCP tools from
// core/mcp/tools.go (lines 25-38) plus the four mutating tools the
// chat-assistant is permitted to invoke. Any drift is caught by the
// scope-divergence check in Boot.
func expectedAllowedTools() []string {
	return []string{
		// All read-only discovery tools (core/mcp/tools.go:25-38).
		mcp.ToolListJobs,
		mcp.ToolGetJob,
		mcp.ToolListRuns,
		mcp.ToolGetRun,
		mcp.ToolRunTimeline,
		mcp.ToolListWorkflows,
		mcp.ToolListPacks,
		mcp.ToolListTopics,
		mcp.ToolListWorkers,
		mcp.ToolListAgents,
		mcp.ToolListPendingApprovals,
		mcp.ToolAuditQuery,
		mcp.ToolAuditVerify,
		mcp.ToolStatus,
		// Policy query (read-only inspection of policy bundles).
		mcp.ToolQueryPolicy,
		// Mutating tools: submit_job is the ONLY pre-approved one
		// (rail #2); the rest traverse the approval gate per call.
		mcp.ToolSubmitJob,
		mcp.ToolApproveJob,
		mcp.ToolRejectJob,
		mcp.ToolCancelJob,
		mcp.ToolTriggerWorkflow,
	}
}

// expectedPreapprovedMutatingTools pins the preapproved-mutating set
// to EXACTLY [cordum_submit_job]. Widening requires an admin policy-
// bundle update post-ship, not a code change (rail #2).
func expectedPreapprovedMutatingTools() []string {
	return []string{mcp.ToolSubmitJob}
}

// expectedDataClassifications is the canonical data-classification
// allowlist for the chat-assistant identity. Adjusting this list is
// an explicit policy-bundle change, not a code change.
func expectedDataClassifications() []string {
	return []string{"public", "internal"}
}

// Boot performs the idempotent registration flow: lookup → match → either
// reuse-existing or register+set-scope. Returns the chat-assistant
// agent id on success.
func (b *Bootstrapper) Boot(ctx context.Context) (string, error) {
	if b == nil || b.registry == nil {
		return "", errors.New("llmchat/bootstrap: agent registry not configured")
	}

	existing, err := b.lookupChatAssistant(ctx)
	if err != nil {
		return "", err
	}
	if existing != nil {
		if err := b.verifyScope(existing); err != nil {
			return "", err
		}
		slog.Info("llmchat: chat-assistant already registered, reusing",
			"agent_id", existing.ID, "tenant", b.tenant)
		return existing.ID, nil
	}

	id, err := b.register(ctx)
	if err != nil {
		return "", fmt.Errorf("llmchat/bootstrap: register: %w", err)
	}
	if err := b.setScope(ctx, id); err != nil {
		return "", fmt.Errorf(
			"llmchat/bootstrap: set_scope failed for partially-registered chat-assistant id=%s; "+
				"operator remediation: revoke the agent identity and re-run boot: %w",
			id, err)
	}
	if err := b.emitRegisteredAuditEvent(ctx, id); err != nil {
		// Audit emission failure is FATAL on first-boot register
		// because chat.bootstrap_registered is the canonical signal that
		// a new agent identity exists in the system; if we can't
		// record it, the audit trail is already split. Boot fails;
		// the operator's remediation is to repair the audit pipeline
		// (Redis + chainer) and re-run boot. The next Boot call
		// hits lookup-hit (the agent identity was created), so the
		// emit-on-create path won't fire again — this is by design.
		// QA reopen #2 at 2026-04-26 specifically required this be
		// fail-rather-than-swallow.
		return "", fmt.Errorf(
			"llmchat/bootstrap: chat.bootstrap_registered audit emit failed for chat-assistant id=%s; "+
				"boot aborted to keep audit trail consistent: %w",
			id, err)
	}
	slog.Info("llmchat: chat-assistant bootstrap registered", "agent_id", id, "tenant", b.tenant)
	return id, nil
}

// emitRegisteredAuditEvent writes a `chat.bootstrap_registered` SIEMEvent
// into the audit chain on first-boot agent creation. With one retry
// on transient failure (network blip, redis CAS contention) before
// surfacing the error to Boot, which then fails the entire bootstrap.
func (b *Bootstrapper) emitRegisteredAuditEvent(ctx context.Context, agentID string) error {
	if b.emitter == nil {
		return nil
	}
	event := func() *audit.SIEMEvent {
		return &audit.SIEMEvent{
			Timestamp: time.Now().UTC(),
			EventType: "agent_lifecycle",
			Severity:  "info",
			TenantID:  b.tenant,
			AgentID:   agentID,
			AgentName: "chat-assistant",
			Action:    audit.SIEMActionChatBootstrapRegistered,
			Decision:  "registered",
			Reason:    "chat-assistant first-boot bootstrap registration via CAP SDK control-plane wrappers",
			Extra: map[string]string{
				"chat_assistant_agent_id":          agentID,
				"preapproved_mutating_tools_count": "1",
			},
		}
	}

	if err := b.emitter.Append(ctx, event()); err != nil {
		slog.Warn("llmchat: bootstrap audit emit retrying", "agent_id", agentID, "error", err)
		// One retry: redis chain CAS can lose to a contending writer
		// once. Second loss is not transient — surface it.
		if retryErr := b.emitter.Append(ctx, event()); retryErr != nil {
			return retryErr
		}
	}
	return nil
}

// lookupChatAssistant queries the agent registry for an existing
// chat-assistant identity in this tenant. ErrAgentNotFound is
// translated to (nil, nil) so Boot can take the register path; an
// ErrAgentDuplicate is wrapped with operator-actionable context.
func (b *Bootstrapper) lookupChatAssistant(ctx context.Context) (*capsdk.AgentIdentity, error) {
	got, err := b.registry.Lookup(ctx, "chat-assistant", b.tenant)
	if err == nil {
		return got, nil
	}
	if errors.Is(err, capsdk.ErrAgentNotFound) {
		return nil, nil
	}
	if errors.Is(err, capsdk.ErrAgentDuplicate) {
		return nil, fmt.Errorf(
			"llmchat/bootstrap: multiple chat-assistant registrations queued; "+
				"admin must clear duplicates before boot can proceed: %w",
			err)
	}
	return nil, fmt.Errorf("llmchat/bootstrap: lookup chat-assistant: %w", err)
}

// verifyScope rejects a divergent existing chat-assistant. The check
// is set-equality on AllowedTools (order-insensitive) + exact match on
// PreapprovedMutatingTools=[cordum_submit_job] (rail #2: widening
// requires policy-bundle update post-ship, not code).
func (b *Bootstrapper) verifyScope(existing *capsdk.AgentIdentity) error {
	if !setsEqual(existing.AllowedTools, expectedAllowedTools()) {
		return fmt.Errorf(
			"llmchat/bootstrap: divergent allowed_tools on existing chat-assistant id=%s; got=%v want=%v",
			existing.ID, existing.AllowedTools, expectedAllowedTools())
	}
	if !setsEqual(existing.PreapprovedMutatingTools, expectedPreapprovedMutatingTools()) {
		return fmt.Errorf(
			"llmchat/bootstrap: divergent preapproved_mutating_tools on chat-assistant id=%s; got=%v want=%v (rail #2)",
			existing.ID, existing.PreapprovedMutatingTools, expectedPreapprovedMutatingTools())
	}
	return nil
}

func setsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(b))
	for _, s := range b {
		counts[s]++
	}
	for _, s := range a {
		if counts[s] == 0 {
			return false
		}
		counts[s]--
	}
	return true
}

// register creates a new chat-assistant identity. The CAP SDK's
// Register method deliberately omits PreapprovedMutatingTools per
// rail #2 (post-registration SetScope privilege only) — the
// preapproved set is applied by the follow-up setScope call.
func (b *Bootstrapper) register(ctx context.Context) (string, error) {
	id, err := b.registry.Register(ctx, capsdk.AgentSpec{
		Name:                "chat-assistant",
		Description:         "Cordum self-hosted chat assistant (Qwen3-Coder via vLLM)",
		Owner:               "system",
		Team:                "system",
		RiskTier:            "medium",
		AllowedTools:        expectedAllowedTools(),
		DataClassifications: expectedDataClassifications(),
	})
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("llmchat/bootstrap: registry returned empty agent id")
	}
	return id, nil
}

// setScope applies the canonical allowed-tools + preapproved-mutating
// + data-classification scope to a freshly-registered chat-assistant.
// PreapprovedMutatingTools is sent explicitly (capsdk.SetScope always
// transmits it for deterministic revoke semantics).
func (b *Bootstrapper) setScope(ctx context.Context, agentID string) error {
	return b.registry.SetScope(ctx, capsdk.AgentScopeUpdate{
		AgentID:                  agentID,
		AllowedTools:             expectedAllowedTools(),
		PreapprovedMutatingTools: expectedPreapprovedMutatingTools(),
		DataClassifications:      expectedDataClassifications(),
	})
}
