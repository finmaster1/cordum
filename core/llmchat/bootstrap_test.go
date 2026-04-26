package llmchat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/cordum/cordum/core/audit"
	"github.com/cordum/cordum/core/mcp"
	"github.com/redis/go-redis/v9"
)

// recordingEmitter captures every SIEMEvent appended through the
// AuditEmitter interface. Test fixtures use it to assert the
// `chat.bootstrap_registered` event fires on first-boot register-success
// and NOT on lookup-hit reuse.
type recordingEmitter struct {
	mu        sync.Mutex
	events    []*audit.SIEMEvent
	failTimes int   // how many leading Append calls return failErr before succeeding
	failErr   error // the error to return; nil = use default
}

func (r *recordingEmitter) Append(_ context.Context, ev *audit.SIEMEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failTimes > 0 {
		r.failTimes--
		err := r.failErr
		if err == nil {
			err = errors.New("recordingEmitter scripted failure")
		}
		return err
	}
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
// `chat.bootstrap_registered` SIEMEvent is appended to the audit chain
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
	if ev.Action != audit.SIEMActionChatBootstrapRegistered {
		t.Errorf("Action = %q, want %q", ev.Action, audit.SIEMActionChatBootstrapRegistered)
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

// TestBootstrap_AuditEmitFailureFailsBoot verifies QA's fail-or-retry
// requirement: a persistent emitter failure surfaces as a Boot error
// (not silently swallowed). The retry path is also exercised — one
// transient failure must succeed on retry (CAS-contention case).
func TestBootstrap_AuditEmitFailureFailsBoot(t *testing.T) {
	t.Parallel()

	t.Run("permanent-failure-fails-boot", func(t *testing.T) {
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
		emitter := &recordingEmitter{failTimes: 99}
		b := NewBootstrapper(f, "tenant-a", emitter)
		_, err := b.Boot(context.Background())
		if err == nil {
			t.Fatal("Boot should fail when audit emitter cannot record chat.bootstrap_registered")
		}
		if !strings.Contains(err.Error(), "chat.bootstrap_registered audit emit failed") {
			t.Errorf("error %v should explain the failed audit emission", err)
		}
	})

	t.Run("one-transient-failure-then-success", func(t *testing.T) {
		t.Parallel()
		f := newFakeBootstrapClient()
		f.respond[mcp.ToolListAgents] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
			return listResult(nil), nil
		}
		f.respond[mcp.ToolRegisterAgent] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
			return registerResult("chat-assistant-resilient"), nil
		}
		f.respond[mcp.ToolSetAgentScope] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
			return okResult(), nil
		}
		emitter := &recordingEmitter{failTimes: 1}
		b := NewBootstrapper(f, "tenant-a", emitter)
		id, err := b.Boot(context.Background())
		if err != nil {
			t.Fatalf("Boot: %v (one retry should suffice)", err)
		}
		if id != "chat-assistant-resilient" {
			t.Errorf("id = %q, want chat-assistant-resilient", id)
		}
		if got := len(emitter.Events()); got != 1 {
			t.Errorf("Events = %d, want 1 (retry succeeded)", got)
		}
	})
}

// TestBootstrap_AuditEventVisibleViaConcreteChainer verifies the
// `chat.bootstrap_registered` event is appended to the canonical Redis-
// backed audit chain (not just an in-memory recorder). QA's DoD
// requires the event be assertable via the concrete audit pipeline
// — the equivalent of the `/api/v1/audit/events` query path. This
// test wires `audit.NewChainer` against a miniredis backend and
// reads the resulting Redis Stream entry to prove the event landed.
func TestBootstrap_AuditEventVisibleViaConcreteChainer(t *testing.T) {
	t.Parallel()
	srv, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(srv.Close)
	rdb := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	chainer := audit.NewChainer(rdb, "audit:chain:")

	f := newFakeBootstrapClient()
	f.respond[mcp.ToolListAgents] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return listResult(nil), nil
	}
	f.respond[mcp.ToolRegisterAgent] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return registerResult("chat-assistant-int"), nil
	}
	f.respond[mcp.ToolSetAgentScope] = func(_ map[string]any) (*mcp.ToolCallResult, error) {
		return okResult(), nil
	}

	b := NewBootstrapper(f, "tenant-int", chainer)
	id, err := b.Boot(context.Background())
	if err != nil {
		t.Fatalf("Boot via concrete chainer: %v", err)
	}

	// Read the tenant's audit stream directly — this is the same
	// substrate /api/v1/audit/events queries.
	streamKey := chainer.StreamKey("tenant-int")
	entries, err := rdb.XRange(context.Background(), streamKey, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRange: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit-chain entry, got %d", len(entries))
	}
	rawEvent, ok := entries[0].Values["event"].(string)
	if !ok {
		t.Fatalf("audit-chain entry missing 'event' field; values=%v", entries[0].Values)
	}
	var ev audit.SIEMEvent
	if err := json.Unmarshal([]byte(rawEvent), &ev); err != nil {
		t.Fatalf("decode chain event: %v", err)
	}
	if ev.Action != audit.SIEMActionChatBootstrapRegistered {
		t.Errorf("Action = %q, want %q", ev.Action, audit.SIEMActionChatBootstrapRegistered)
	}
	if ev.AgentID != id {
		t.Errorf("AgentID = %q, want %q", ev.AgentID, id)
	}
	if ev.TenantID != "tenant-int" {
		t.Errorf("TenantID = %q, want tenant-int", ev.TenantID)
	}
	if ev.EventHash == "" || ev.Seq == 0 {
		t.Errorf("chained event missing hash/seq: hash=%q seq=%d (the chainer is supposed to compute these)", ev.EventHash, ev.Seq)
	}
}

// TestBootstrap_NoAuditEventOnLookupHit verifies the inverse: a Boot
// that finds an existing chat-assistant must NOT emit a new
// `chat.bootstrap_registered` event. The event represents agent CREATION,
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
		t.Errorf("lookup-hit reuse must NOT emit chat.bootstrap_registered; got %d events", got)
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
