package llmchat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/cordum/cordum/core/audit"
	"github.com/cordum/cordum/core/mcp"
)

// recordingEmitter captures every SIEMEvent appended through the
// AuditEmitter interface. Test fixtures use it to assert the
// `cap.agent_registered` event fires on first-boot register-success
// and NOT on lookup-hit reuse.
type recordingEmitter struct {
	mu     sync.Mutex
	events []*audit.SIEMEvent
}

func (r *recordingEmitter) Append(_ context.Context, ev *audit.SIEMEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := *ev
	r.events = append(r.events, &clone)
	return nil
}

func (r *recordingEmitter) Events() []*audit.SIEMEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*audit.SIEMEvent, len(r.events))
	copy(out, r.events)
	return out
}

// fakeBootstrapClient scripts MCP CallTool responses for bootstrap.
type fakeBootstrapClient struct {
	mu    sync.Mutex
	calls []bootstrapCall
	// per-method scripted responses; indexed by tool name. The
	// response is returned as the parsed Result; nil err = success.
	respond map[string]func(args map[string]any) (*mcp.ToolCallResult, error)
}

type bootstrapCall struct {
	Name string
	Args map[string]any
}

func newFakeBootstrapClient() *fakeBootstrapClient {
	return &fakeBootstrapClient{
		respond: map[string]func(map[string]any) (*mcp.ToolCallResult, error){},
	}
}

func (f *fakeBootstrapClient) CallTool(_ context.Context, name string, args map[string]any) (*mcp.ToolCallResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, bootstrapCall{Name: name, Args: args})
	if h, ok := f.respond[name]; ok {
		return h(args)
	}
	return nil, errors.New("fake bootstrap: no handler for " + name)
}

func (f *fakeBootstrapClient) Calls() []bootstrapCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]bootstrapCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// listResult builds a fake cordum_list_agents response page.
func listResult(items []map[string]any) *mcp.ToolCallResult {
	page := map[string]any{"items": items}
	raw, _ := json.Marshal(page)
	return &mcp.ToolCallResult{
		Content: []mcp.ContentItem{{Type: "text", Text: string(raw)}},
	}
}

func registerResult(id string) *mcp.ToolCallResult {
	body := map[string]any{"id": id, "name": "chat-assistant"}
	raw, _ := json.Marshal(body)
	return &mcp.ToolCallResult{
		Content: []mcp.ContentItem{{Type: "text", Text: string(raw)}},
	}
}

func okResult() *mcp.ToolCallResult {
	return &mcp.ToolCallResult{
		Content: []mcp.ContentItem{{Type: "text", Text: `{"ok":true}`}},
	}
}

// stringSlice normalises an args[key] value into []string regardless of
// whether the caller produced []string, []any (as the JSON unmarshal
// path would produce), or nil.
func stringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, x := range s {
			if str, ok := x.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

func TestBootstrap_LookupHit_NoRegister(t *testing.T) {
	t.Parallel()
	f := newFakeBootstrapClient()
	f.respond[mcp.ToolListAgents] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return listResult([]map[string]any{
			{
				"id":                         "chat-assistant-existing",
				"name":                       "chat-assistant",
				"tenant_id":                  "tenant-a",
				"risk_tier":                  "medium",
				"allowed_tools":              expectedAllowedTools(),
				"preapproved_mutating_tools": []string{"cordum_submit_job"},
				"data_classifications":       []string{"public", "internal"},
			},
		}), nil
	}
	b := NewBootstrapper(f, "tenant-a", nil)
	id, err := b.Boot(context.Background())
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if id != "chat-assistant-existing" {
		t.Errorf("agent id = %q, want chat-assistant-existing", id)
	}
	for _, c := range f.Calls() {
		if c.Name == mcp.ToolRegisterAgent || c.Name == mcp.ToolSetAgentScope {
			t.Errorf("unexpected mutating call %s on lookup-hit", c.Name)
		}
	}
}

func TestBootstrap_LookupMiss_RegistersAndSetsScope(t *testing.T) {
	t.Parallel()
	f := newFakeBootstrapClient()
	f.respond[mcp.ToolListAgents] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return listResult(nil), nil
	}
	f.respond[mcp.ToolRegisterAgent] = func(args map[string]any) (*mcp.ToolCallResult, error) {
		// PreapprovedMutatingTools must NOT be in the register call.
		if _, ok := args["preapproved_mutating_tools"]; ok {
			t.Errorf("register call MUST NOT include preapproved_mutating_tools (use set_agent_scope)")
		}
		if got, _ := args["name"].(string); got != "chat-assistant" {
			t.Errorf("register name = %v, want chat-assistant", args["name"])
		}
		if got, _ := args["risk_tier"].(string); got != "medium" {
			t.Errorf("register risk_tier = %v, want medium", args["risk_tier"])
		}
		return registerResult("chat-assistant-new"), nil
	}
	f.respond[mcp.ToolSetAgentScope] = func(args map[string]any) (*mcp.ToolCallResult, error) {
		// PreapprovedMutatingTools is what set_scope is for. The
		// caller may pass []string or []any depending on construction;
		// normalise both to []string for assertion.
		got := stringSlice(args["preapproved_mutating_tools"])
		if len(got) != 1 || got[0] != "cordum_submit_job" {
			t.Errorf("set_scope preapproved_mutating_tools = %v, want [cordum_submit_job] only", got)
		}
		return okResult(), nil
	}

	b := NewBootstrapper(f, "tenant-a", nil)
	id, err := b.Boot(context.Background())
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if id != "chat-assistant-new" {
		t.Errorf("agent id = %q, want chat-assistant-new", id)
	}
}

func TestBootstrap_RegisterFailed_NoSetScope(t *testing.T) {
	t.Parallel()
	f := newFakeBootstrapClient()
	f.respond[mcp.ToolListAgents] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return listResult(nil), nil
	}
	f.respond[mcp.ToolRegisterAgent] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return nil, errors.New("approval required")
	}
	b := NewBootstrapper(f, "tenant-a", nil)
	_, err := b.Boot(context.Background())
	if err == nil {
		t.Fatal("expected error when register fails")
	}
	for _, c := range f.Calls() {
		if c.Name == mcp.ToolSetAgentScope {
			t.Error("set_agent_scope called despite register failure")
		}
	}
}

func TestBootstrap_SetScopeFailed_PartialState(t *testing.T) {
	t.Parallel()
	f := newFakeBootstrapClient()
	f.respond[mcp.ToolListAgents] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return listResult(nil), nil
	}
	f.respond[mcp.ToolRegisterAgent] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return registerResult("chat-assistant-partial"), nil
	}
	f.respond[mcp.ToolSetAgentScope] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return nil, errors.New("scope update failed")
	}

	b := NewBootstrapper(f, "tenant-a", nil)
	_, err := b.Boot(context.Background())
	if err == nil {
		t.Fatal("expected error on set_scope failure")
	}
	if !strings.Contains(err.Error(), "chat-assistant-partial") {
		t.Errorf("error %v should name the partially-registered agent for operator remediation", err)
	}
}

func TestBootstrap_Idempotent(t *testing.T) {
	t.Parallel()
	f := newFakeBootstrapClient()
	calls := 0
	f.respond[mcp.ToolListAgents] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		calls++
		if calls == 1 {
			return listResult(nil), nil
		}
		return listResult([]map[string]any{
			{
				"id":                         "chat-assistant-1",
				"name":                       "chat-assistant",
				"tenant_id":                  "tenant-a",
				"risk_tier":                  "medium",
				"allowed_tools":              expectedAllowedTools(),
				"preapproved_mutating_tools": []string{"cordum_submit_job"},
				"data_classifications":       []string{"public", "internal"},
			},
		}), nil
	}
	f.respond[mcp.ToolRegisterAgent] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return registerResult("chat-assistant-1"), nil
	}
	f.respond[mcp.ToolSetAgentScope] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return okResult(), nil
	}

	b := NewBootstrapper(f, "tenant-a", nil)
	if _, err := b.Boot(context.Background()); err != nil {
		t.Fatalf("first Boot: %v", err)
	}
	if _, err := b.Boot(context.Background()); err != nil {
		t.Fatalf("second Boot: %v", err)
	}
	registerCount := 0
	for _, c := range f.Calls() {
		if c.Name == mcp.ToolRegisterAgent {
			registerCount++
		}
	}
	if registerCount != 1 {
		t.Errorf("register called %d times, want 1 (idempotent)", registerCount)
	}
}

func TestBootstrap_DivergentScopeRejected(t *testing.T) {
	t.Parallel()
	f := newFakeBootstrapClient()
	f.respond[mcp.ToolListAgents] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return listResult([]map[string]any{
			{
				"id":                         "chat-assistant-bad",
				"name":                       "chat-assistant",
				"tenant_id":                  "tenant-a",
				"risk_tier":                  "medium",
				"allowed_tools":              []string{"cordum_list_jobs"}, // missing the rest
				"preapproved_mutating_tools": []string{"cordum_submit_job", "cordum_approve_job"},
				"data_classifications":       []string{"public"},
			},
		}), nil
	}
	b := NewBootstrapper(f, "tenant-a", nil)
	_, err := b.Boot(context.Background())
	if err == nil {
		t.Fatal("expected divergent-scope error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "divergent") {
		t.Errorf("error %v should mention divergent scope", err)
	}
}

// TestBootstrap_AuditEventOnFirstBootRegister verifies that
// `cap.agent_registered` SIEMEvent is appended to the audit chain
// when the chat-assistant identity is created on first boot. QA's
// DoD requires the event presence be asserted — this test is the
// canonical check.
func TestBootstrap_AuditEventOnFirstBootRegister(t *testing.T) {
	t.Parallel()
	f := newFakeBootstrapClient()
	f.respond[mcp.ToolListAgents] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return listResult(nil), nil
	}
	f.respond[mcp.ToolRegisterAgent] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return registerResult("chat-assistant-fresh"), nil
	}
	f.respond[mcp.ToolSetAgentScope] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return okResult(), nil
	}

	emitter := &recordingEmitter{}
	b := NewBootstrapper(f, "tenant-a", emitter)
	id, err := b.Boot(context.Background())
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}

	events := emitter.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	ev := events[0]
	if ev.Action != SIEMActionChatBootstrapRegistered {
		t.Errorf("Action = %q, want %q", ev.Action, SIEMActionChatBootstrapRegistered)
	}
	if ev.AgentID != id {
		t.Errorf("AgentID = %q, want %q", ev.AgentID, id)
	}
	if ev.AgentName != "chat-assistant" {
		t.Errorf("AgentName = %q, want chat-assistant", ev.AgentName)
	}
	if ev.TenantID != "tenant-a" {
		t.Errorf("TenantID = %q, want tenant-a", ev.TenantID)
	}
	if ev.Decision != "registered" {
		t.Errorf("Decision = %q, want registered", ev.Decision)
	}
}

// TestBootstrap_NoAuditEventOnLookupHit verifies the inverse: a Boot
// that finds an existing chat-assistant must NOT emit a new
// `cap.agent_registered` event. The event represents agent CREATION,
// not service boot.
func TestBootstrap_NoAuditEventOnLookupHit(t *testing.T) {
	t.Parallel()
	f := newFakeBootstrapClient()
	f.respond[mcp.ToolListAgents] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return listResult([]map[string]any{
			{
				"id":                         "chat-assistant-existing",
				"name":                       "chat-assistant",
				"tenant_id":                  "tenant-a",
				"risk_tier":                  "medium",
				"allowed_tools":              expectedAllowedTools(),
				"preapproved_mutating_tools": []string{"cordum_submit_job"},
				"data_classifications":       []string{"public", "internal"},
			},
		}), nil
	}

	emitter := &recordingEmitter{}
	b := NewBootstrapper(f, "tenant-a", emitter)
	if _, err := b.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}

	if got := len(emitter.Events()); got != 0 {
		t.Errorf("lookup-hit reuse must NOT emit cap.agent_registered; got %d events", got)
	}
}

func TestBootstrap_MultipleQueuedRegistrationsRejected(t *testing.T) {
	t.Parallel()
	f := newFakeBootstrapClient()
	f.respond[mcp.ToolListAgents] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return listResult([]map[string]any{
			{"id": "chat-assistant-1", "name": "chat-assistant", "tenant_id": "tenant-a", "risk_tier": "medium",
				"allowed_tools": expectedAllowedTools(), "preapproved_mutating_tools": []string{"cordum_submit_job"},
				"data_classifications": []string{"public", "internal"}},
			{"id": "chat-assistant-2", "name": "chat-assistant", "tenant_id": "tenant-a", "risk_tier": "medium",
				"allowed_tools": expectedAllowedTools(), "preapproved_mutating_tools": []string{"cordum_submit_job"},
				"data_classifications": []string{"public", "internal"}},
		}), nil
	}
	b := NewBootstrapper(f, "tenant-a", nil)
	_, err := b.Boot(context.Background())
	if err == nil {
		t.Fatal("expected error on multiple chat-assistant registrations")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "multiple") {
		t.Errorf("error %v should mention multiple registrations", err)
	}
}
