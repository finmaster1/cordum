// Package-level chat-assistant agent bootstrap.
//
// On first boot the cordum-llm-chat process registers a "chat-assistant"
// agent identity with Cordum so that every subsequent CallTool carries
// the same CAP-tagged AgentIdentity any other Cordum agent does — this
// is the dogfooding integration point per task rail #1.
//
// CAP SDK gap: cap/sdk/go has no agent.go yet. The bootstrap therefore
// uses the MCP cordum_register_agent + cordum_set_agent_scope pair as
// the registration path. A followup task in the cap repo (filed in
// step 13) tracks adding native CAP wrappers; once they ship the
// bootstrap can switch over without changing its public surface.
package llmchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cordum/cordum/core/audit"
	"github.com/cordum/cordum/core/mcp"
)

// SIEMActionChatBootstrapRegistered is the canonical audit-event action
// emitted when the chat-assistant agent identity is created on first
// boot. Phase-5 WS handler will use parallel `chat.session_started`
// and `chat.session_closed` constants from the same family (filed as
// task-88a3bc57 followup); pre-defined here so this task's audit-event
// test asserts the right wire-format string and downstream tasks pin
// to the same family.
const SIEMActionChatBootstrapRegistered = "cap.agent_registered"

// AuditEmitter is the slice of audit.Chainer the bootstrap needs.
// Defined as an interface so tests can inject a recorder without
// pulling in a real Redis.
type AuditEmitter interface {
	Append(ctx context.Context, event *audit.SIEMEvent) error
}

// MCPCallToolClient is the slim contract bootstrap needs from the MCP
// client. The phase-2 *MCPClient satisfies it via a thin adapter; tests
// use a fake.
type MCPCallToolClient interface {
	CallTool(ctx context.Context, name string, args map[string]any) (*mcp.ToolCallResult, error)
}

// Bootstrapper handles idempotent chat-assistant registration on
// service startup. Boot is safe to call from a single goroutine on
// startup; the resulting agent identity is consumed by the rest of
// the service via the returned id.
type Bootstrapper struct {
	mcp     MCPCallToolClient
	tenant  string
	emitter AuditEmitter
}

// NewBootstrapper constructs a Bootstrapper bound to the supplied
// tenant. The tenant is forwarded to the lookup filter so the same
// chat-assistant identity is scoped per tenant in multi-tenant
// deployments. The emitter, when non-nil, receives a
// `cap.agent_registered` SIEMEvent on first-boot register-success
// (NOT on lookup-hit reuse — the event represents agent creation, not
// service boot).
func NewBootstrapper(client MCPCallToolClient, tenant string, emitter AuditEmitter) *Bootstrapper {
	return &Bootstrapper{mcp: client, tenant: tenant, emitter: emitter}
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

// Boot performs the idempotent registration flow: list → match → either
// reuse-existing or register+set-scope. Returns the chat-assistant
// agent id on success.
func (b *Bootstrapper) Boot(ctx context.Context) (string, error) {
	if b == nil || b.mcp == nil {
		return "", errors.New("llmchat/bootstrap: mcp client not configured")
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
		// Audit emission failure is logged but doesn't fail boot — the
		// agent identity is already created, and re-running boot
		// would hit lookup-hit and skip both the register and the
		// audit emission. The MCP register call itself produced an
		// `mcp.tool_invocation` audit entry via the existing
		// ToolInvocationAuditor pipeline; this event is the
		// chat-assistant-specific signal on top.
		slog.Warn("llmchat: bootstrap audit emit failed", "agent_id", id, "error", err)
	}
	slog.Info("llmchat: chat-assistant bootstrap registered", "agent_id", id, "tenant", b.tenant)
	return id, nil
}

// emitRegisteredAuditEvent writes a `cap.agent_registered` SIEMEvent
// into the audit chain on first-boot agent creation. No-op when no
// emitter was wired (tests that don't need audit-trail verification).
func (b *Bootstrapper) emitRegisteredAuditEvent(ctx context.Context, agentID string) error {
	if b.emitter == nil {
		return nil
	}
	return b.emitter.Append(ctx, &audit.SIEMEvent{
		Timestamp: time.Now().UTC(),
		EventType: "agent_lifecycle",
		Severity:  "info",
		TenantID:  b.tenant,
		AgentID:   agentID,
		AgentName: "chat-assistant",
		Action:    SIEMActionChatBootstrapRegistered,
		Decision:  "registered",
		Reason:    "chat-assistant first-boot bootstrap registration via MCP cordum_register_agent + cordum_set_agent_scope",
		Extra: map[string]string{
			"chat_assistant_agent_id":          agentID,
			"preapproved_mutating_tools_count": "1",
		},
	})
}

// agentRecord is the parsed representation of the cordum_list_agents
// page items relevant to bootstrap. Extra fields on the wire are
// ignored.
type agentRecord struct {
	ID                       string   `json:"id"`
	Name                     string   `json:"name"`
	TenantID                 string   `json:"tenant_id"`
	RiskTier                 string   `json:"risk_tier"`
	AllowedTools             []string `json:"allowed_tools"`
	PreapprovedMutatingTools []string `json:"preapproved_mutating_tools"`
	DataClassifications      []string `json:"data_classifications"`
}

func (b *Bootstrapper) lookupChatAssistant(ctx context.Context) (*agentRecord, error) {
	res, err := b.mcp.CallTool(ctx, mcp.ToolListAgents, map[string]any{
		"page_size": 50,
		"filter": map[string]any{
			"name":      "chat-assistant",
			"tenant_id": b.tenant,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("llmchat/bootstrap: list_agents: %w", err)
	}

	page, err := parseAgentPage(res)
	if err != nil {
		return nil, fmt.Errorf("llmchat/bootstrap: parse list_agents response: %w", err)
	}

	var matches []agentRecord
	for _, a := range page {
		if a.Name == "chat-assistant" && (b.tenant == "" || a.TenantID == "" || a.TenantID == b.tenant) {
			matches = append(matches, a)
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		out := matches[0]
		return &out, nil
	default:
		return nil, fmt.Errorf(
			"llmchat/bootstrap: multiple chat-assistant registrations queued (count=%d); "+
				"admin must clear duplicates before boot can proceed",
			len(matches))
	}
}

// parseAgentPage decodes the list_agents tool result. The MCP read-
// tool wraps payloads in a single text Content item carrying JSON;
// some bridges return either {items:[...]} or a bare array.
func parseAgentPage(res *mcp.ToolCallResult) ([]agentRecord, error) {
	if res == nil {
		return nil, errors.New("nil tool result")
	}
	if len(res.Content) == 0 {
		return nil, nil
	}
	body := strings.TrimSpace(res.Content[0].Text)
	if body == "" {
		return nil, nil
	}
	// Peek at the first non-whitespace byte to discriminate between
	// the {items:[...]} envelope (canonical ListPage) and a bare
	// array. Either form may carry zero elements; both decode to a
	// nil slice without error.
	switch body[0] {
	case '[':
		var arr []agentRecord
		if err := json.Unmarshal([]byte(body), &arr); err != nil {
			return nil, fmt.Errorf("unparseable list_agents bare array: %w", err)
		}
		return arr, nil
	case '{':
		var envelope struct {
			Items []agentRecord `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &envelope); err != nil {
			return nil, fmt.Errorf("unparseable list_agents envelope: %w", err)
		}
		return envelope.Items, nil
	default:
		return nil, fmt.Errorf("unparseable list_agents body: %s", body)
	}
}

// verifyScope rejects a divergent existing chat-assistant. The check
// is set-equality on AllowedTools (order-insensitive) + exact match on
// PreapprovedMutatingTools=[cordum_submit_job] (rail #2: widening
// requires policy-bundle update post-ship, not code).
func (b *Bootstrapper) verifyScope(existing *agentRecord) error {
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
	seen := make(map[string]struct{}, len(b))
	for _, s := range b {
		seen[s] = struct{}{}
	}
	for _, s := range a {
		if _, ok := seen[s]; !ok {
			return false
		}
	}
	return true
}

// register issues cordum_register_agent. Note: the MCP register tool's
// arg schema (registerAgentArgs) does NOT carry preapproved_mutating_tools
// — that's a follow-up set_agent_scope call (the architectural reason
// PreapprovedMutatingTools is treated as a separate post-registration
// scope adjustment).
func (b *Bootstrapper) register(ctx context.Context) (string, error) {
	args := map[string]any{
		"name":                 "chat-assistant",
		"description":          "Cordum self-hosted chat assistant (Qwen3-Coder via vLLM)",
		"owner":                "system",
		"team":                 "system",
		"risk_tier":            "medium",
		"allowed_tools":        expectedAllowedTools(),
		"data_classifications": []string{"public", "internal"},
	}
	res, err := b.mcp.CallTool(ctx, mcp.ToolRegisterAgent, args)
	if err != nil {
		return "", err
	}
	id, err := extractAgentID(res)
	if err != nil {
		return "", fmt.Errorf("parse register response: %w", err)
	}
	return id, nil
}

func (b *Bootstrapper) setScope(ctx context.Context, agentID string) error {
	args := map[string]any{
		"agent_id":                   agentID,
		"allowed_tools":              expectedAllowedTools(),
		"preapproved_mutating_tools": expectedPreapprovedMutatingTools(),
		"data_classifications":       []string{"public", "internal"},
	}
	if _, err := b.mcp.CallTool(ctx, mcp.ToolSetAgentScope, args); err != nil {
		return err
	}
	return nil
}

func extractAgentID(res *mcp.ToolCallResult) (string, error) {
	if res == nil || len(res.Content) == 0 {
		return "", errors.New("empty register response")
	}
	body := strings.TrimSpace(res.Content[0].Text)
	if body == "" {
		return "", errors.New("empty register body")
	}
	var parsed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return "", fmt.Errorf("decode register body: %w", err)
	}
	if parsed.ID == "" {
		return "", errors.New("register response missing id")
	}
	return parsed.ID, nil
}
