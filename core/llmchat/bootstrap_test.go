package llmchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	capsdk "github.com/cordum-io/cap/v2/sdk/go"
	"github.com/cordum/cordum/core/audit"
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

// fakeAgentRegistry implements AgentRegistry for tests. It scripts
// per-method responses (Lookup / Register / SetScope) and records the
// inputs every call received so test assertions can verify both the
// shape (e.g. Register payload omits PreapprovedMutatingTools per
// rail #2) and the count (idempotency: Register called once across
// two Boot calls).
type fakeAgentRegistry struct {
	mu sync.Mutex

	lookupCalls   []lookupCall
	registerCalls []capsdk.AgentSpec
	setScopeCalls []capsdk.AgentScopeUpdate

	lookupFn   func(name, tenant string) (*capsdk.AgentIdentity, error)
	registerFn func(spec capsdk.AgentSpec) (string, error)
	setScopeFn func(update capsdk.AgentScopeUpdate) error
}

type lookupCall struct {
	Name   string
	Tenant string
}

func newFakeAgentRegistry() *fakeAgentRegistry {
	return &fakeAgentRegistry{}
}

func (f *fakeAgentRegistry) Lookup(_ context.Context, name, tenant string) (*capsdk.AgentIdentity, error) {
	f.mu.Lock()
	f.lookupCalls = append(f.lookupCalls, lookupCall{Name: name, Tenant: tenant})
	fn := f.lookupFn
	f.mu.Unlock()
	if fn == nil {
		return nil, capsdk.ErrAgentNotFound
	}
	return fn(name, tenant)
}

func (f *fakeAgentRegistry) Register(_ context.Context, spec capsdk.AgentSpec) (string, error) {
	f.mu.Lock()
	f.registerCalls = append(f.registerCalls, spec)
	fn := f.registerFn
	f.mu.Unlock()
	if fn == nil {
		return "", errors.New("fake registry: register not scripted")
	}
	return fn(spec)
}

func (f *fakeAgentRegistry) SetScope(_ context.Context, update capsdk.AgentScopeUpdate) error {
	f.mu.Lock()
	f.setScopeCalls = append(f.setScopeCalls, update)
	fn := f.setScopeFn
	f.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(update)
}

func (f *fakeAgentRegistry) LookupCalls() []lookupCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]lookupCall, len(f.lookupCalls))
	copy(out, f.lookupCalls)
	return out
}

func (f *fakeAgentRegistry) RegisterCalls() []capsdk.AgentSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]capsdk.AgentSpec, len(f.registerCalls))
	copy(out, f.registerCalls)
	return out
}

func (f *fakeAgentRegistry) SetScopeCalls() []capsdk.AgentScopeUpdate {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]capsdk.AgentScopeUpdate, len(f.setScopeCalls))
	copy(out, f.setScopeCalls)
	return out
}

// existingChatAssistant returns a populated AgentIdentity matching the
// canonical scope expected by verifyScope.
func existingChatAssistant(id, tenant string) *capsdk.AgentIdentity {
	return &capsdk.AgentIdentity{
		ID:                       id,
		Name:                     "chat-assistant",
		Owner:                    "system",
		Team:                     "system",
		RiskTier:                 "low",
		AllowedTools:             expectedAllowedTools(),
		PreapprovedMutatingTools: expectedPreapprovedMutatingTools(),
		DataClassifications:      expectedDataClassifications(),
		Status:                   "active",
	}
}

func TestBootstrap_LookupHit_NoRegister(t *testing.T) {
	t.Parallel()
	f := newFakeAgentRegistry()
	f.lookupFn = func(name, tenant string) (*capsdk.AgentIdentity, error) {
		if name != "chat-assistant" {
			t.Errorf("Lookup name = %q, want chat-assistant", name)
		}
		if tenant != "tenant-a" {
			t.Errorf("Lookup tenant = %q, want tenant-a", tenant)
		}
		return existingChatAssistant("chat-assistant-existing", tenant), nil
	}
	b := NewBootstrapper(f, "tenant-a", nil)
	id, err := b.Boot(context.Background())
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if id != "chat-assistant-existing" {
		t.Errorf("agent id = %q, want chat-assistant-existing", id)
	}
	if got := len(f.RegisterCalls()); got != 0 {
		t.Errorf("Register called %d times on lookup-hit, want 0", got)
	}
	if got := len(f.SetScopeCalls()); got != 0 {
		t.Errorf("SetScope called %d times on lookup-hit, want 0", got)
	}
}

func TestBootstrap_LookupMiss_RegistersAndSetsScope(t *testing.T) {
	t.Parallel()
	f := newFakeAgentRegistry()
	f.lookupFn = func(string, string) (*capsdk.AgentIdentity, error) {
		return nil, capsdk.ErrAgentNotFound
	}
	f.registerFn = func(spec capsdk.AgentSpec) (string, error) {
		if spec.Name != "chat-assistant" {
			t.Errorf("Register name = %q, want chat-assistant", spec.Name)
		}
		if spec.RiskTier != "low" {
			t.Errorf("Register risk_tier = %q, want low", spec.RiskTier)
		}
		// The Register payload MUST hit the AgentSpec shape, which
		// has no PreapprovedMutatingTools field at all (rail #2: only
		// SetScope grants the preapproved set). The struct-level
		// guarantee replaces the prior MCP-shape field-omission check.
		if !setsEqual(spec.AllowedTools, expectedAllowedTools()) {
			t.Errorf("Register AllowedTools mismatch: got %v want %v",
				spec.AllowedTools, expectedAllowedTools())
		}
		if !setsEqual(spec.DataClassifications, expectedDataClassifications()) {
			t.Errorf("Register DataClassifications mismatch: got %v want %v",
				spec.DataClassifications, expectedDataClassifications())
		}
		return "chat-assistant-new", nil
	}
	f.setScopeFn = func(update capsdk.AgentScopeUpdate) error {
		if update.AgentID != "chat-assistant-new" {
			t.Errorf("SetScope AgentID = %q, want chat-assistant-new", update.AgentID)
		}
		if !setsEqual(update.PreapprovedMutatingTools, expectedPreapprovedMutatingTools()) {
			t.Errorf("SetScope preapproved_mutating_tools = %v, want empty informational-only scope",
				update.PreapprovedMutatingTools)
		}
		if !setsEqual(update.AllowedTools, expectedAllowedTools()) {
			t.Errorf("SetScope AllowedTools mismatch: got %v want %v",
				update.AllowedTools, expectedAllowedTools())
		}
		return nil
	}

	b := NewBootstrapper(f, "tenant-a", nil)
	id, err := b.Boot(context.Background())
	if err != nil {
		t.Fatalf("Boot: %v", err)
	}
	if id != "chat-assistant-new" {
		t.Errorf("agent id = %q, want chat-assistant-new", id)
	}
	if got := len(f.RegisterCalls()); got != 1 {
		t.Errorf("Register called %d times, want 1", got)
	}
	if got := len(f.SetScopeCalls()); got != 1 {
		t.Errorf("SetScope called %d times, want 1", got)
	}
}

func TestBootstrap_RegisterFailed_NoSetScope(t *testing.T) {
	t.Parallel()
	f := newFakeAgentRegistry()
	f.lookupFn = func(string, string) (*capsdk.AgentIdentity, error) {
		return nil, capsdk.ErrAgentNotFound
	}
	f.registerFn = func(capsdk.AgentSpec) (string, error) {
		return "", errors.New("approval required")
	}
	b := NewBootstrapper(f, "tenant-a", nil)
	_, err := b.Boot(context.Background())
	if err == nil {
		t.Fatal("expected error when Register fails")
	}
	if got := len(f.SetScopeCalls()); got != 0 {
		t.Errorf("SetScope called %d times despite Register failure", got)
	}
}

func TestBootstrap_SetScopeFailed_PartialState(t *testing.T) {
	t.Parallel()
	f := newFakeAgentRegistry()
	f.lookupFn = func(string, string) (*capsdk.AgentIdentity, error) {
		return nil, capsdk.ErrAgentNotFound
	}
	f.registerFn = func(capsdk.AgentSpec) (string, error) {
		return "chat-assistant-partial", nil
	}
	f.setScopeFn = func(capsdk.AgentScopeUpdate) error {
		return errors.New("scope update failed")
	}

	b := NewBootstrapper(f, "tenant-a", nil)
	_, err := b.Boot(context.Background())
	if err == nil {
		t.Fatal("expected error on SetScope failure")
	}
	if !strings.Contains(err.Error(), "chat-assistant-partial") {
		t.Errorf("error %v should name the partially-registered agent for operator remediation", err)
	}
}

func TestBootstrap_Idempotent(t *testing.T) {
	t.Parallel()
	f := newFakeAgentRegistry()
	registered := false
	f.lookupFn = func(_, tenant string) (*capsdk.AgentIdentity, error) {
		if !registered {
			return nil, capsdk.ErrAgentNotFound
		}
		return existingChatAssistant("chat-assistant-1", tenant), nil
	}
	f.registerFn = func(capsdk.AgentSpec) (string, error) {
		registered = true
		return "chat-assistant-1", nil
	}

	b := NewBootstrapper(f, "tenant-a", nil)
	if _, err := b.Boot(context.Background()); err != nil {
		t.Fatalf("first Boot: %v", err)
	}
	if _, err := b.Boot(context.Background()); err != nil {
		t.Fatalf("second Boot: %v", err)
	}
	if got := len(f.RegisterCalls()); got != 1 {
		t.Errorf("Register called %d times across two Boot calls, want 1 (idempotent)", got)
	}
	if got := len(f.SetScopeCalls()); got != 1 {
		t.Errorf("SetScope called %d times across two Boot calls, want 1 (only on first-register)", got)
	}
}

func TestBootstrap_DivergentScopeRejected(t *testing.T) {
	t.Parallel()
	f := newFakeAgentRegistry()
	f.lookupFn = func(_, tenant string) (*capsdk.AgentIdentity, error) {
		return &capsdk.AgentIdentity{
			ID:                       "chat-assistant-bad",
			Name:                     "chat-assistant",
			RiskTier:                 "low",
			AllowedTools:             []string{"cordum_list_jobs"}, // informational-only must be empty
			PreapprovedMutatingTools: []string{"cordum_submit_job"},
			DataClassifications:      []string{"public"},
		}, nil
	}
	b := NewBootstrapper(f, "tenant-a", nil)
	_, err := b.Boot(context.Background())
	if err == nil {
		t.Fatal("expected divergent-scope error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "divergent") {
		t.Errorf("error %v should mention divergent scope", err)
	}
	if got := len(f.RegisterCalls()); got != 0 {
		t.Errorf("Register called on divergent existing identity, want 0")
	}
}

// TestBootstrap_AuditEventOnFirstBootRegister verifies that
// `chat.bootstrap_registered` SIEMEvent is appended to the audit chain
// when the chat-assistant identity is created on first boot.
func TestBootstrap_AuditEventOnFirstBootRegister(t *testing.T) {
	t.Parallel()
	f := newFakeAgentRegistry()
	f.lookupFn = func(string, string) (*capsdk.AgentIdentity, error) {
		return nil, capsdk.ErrAgentNotFound
	}
	f.registerFn = func(capsdk.AgentSpec) (string, error) {
		return "chat-assistant-fresh", nil
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
	if !strings.Contains(ev.Reason, "CAP SDK") {
		t.Errorf("Reason = %q, should mention CAP SDK control-plane wrappers", ev.Reason)
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
		f := newFakeAgentRegistry()
		f.lookupFn = func(string, string) (*capsdk.AgentIdentity, error) {
			return nil, capsdk.ErrAgentNotFound
		}
		f.registerFn = func(capsdk.AgentSpec) (string, error) {
			return "chat-assistant-fresh", nil
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
		f := newFakeAgentRegistry()
		f.lookupFn = func(string, string) (*capsdk.AgentIdentity, error) {
			return nil, capsdk.ErrAgentNotFound
		}
		f.registerFn = func(capsdk.AgentSpec) (string, error) {
			return "chat-assistant-resilient", nil
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
// backed audit chain (not just an in-memory recorder). Wires
// `audit.NewChainer` against a miniredis backend and reads the
// resulting Redis Stream entry to prove the event landed.
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

	f := newFakeAgentRegistry()
	f.lookupFn = func(string, string) (*capsdk.AgentIdentity, error) {
		return nil, capsdk.ErrAgentNotFound
	}
	f.registerFn = func(capsdk.AgentSpec) (string, error) {
		return "chat-assistant-int", nil
	}

	b := NewBootstrapper(f, "tenant-int", chainer)
	id, err := b.Boot(context.Background())
	if err != nil {
		t.Fatalf("Boot via concrete chainer: %v", err)
	}

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
		t.Errorf("chained event missing hash/seq: hash=%q seq=%d", ev.EventHash, ev.Seq)
	}
}

// TestBootstrap_NoAuditEventOnLookupHit verifies the inverse: a Boot
// that finds an existing chat-assistant must NOT emit a new
// `chat.bootstrap_registered` event. The event represents agent CREATION,
// not service boot.
func TestBootstrap_NoAuditEventOnLookupHit(t *testing.T) {
	t.Parallel()
	f := newFakeAgentRegistry()
	f.lookupFn = func(_, tenant string) (*capsdk.AgentIdentity, error) {
		return existingChatAssistant("chat-assistant-existing", tenant), nil
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
	f := newFakeAgentRegistry()
	f.lookupFn = func(string, string) (*capsdk.AgentIdentity, error) {
		// Mirrors capsdk.AgentClient.Lookup, which surfaces a
		// duplicate-aware sentinel when the gateway returns more
		// than one match.
		return nil, fmt.Errorf("multiple matches: %w", capsdk.ErrAgentDuplicate)
	}
	b := NewBootstrapper(f, "tenant-a", nil)
	_, err := b.Boot(context.Background())
	if err == nil {
		t.Fatal("expected error on multiple chat-assistant registrations")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "multiple") {
		t.Errorf("error %v should mention multiple registrations", err)
	}
	if got := len(f.RegisterCalls()); got != 0 {
		t.Errorf("Register called %d times despite duplicate lookup, want 0", got)
	}
}

// TestBootstrap_NilRegistry_FailsBoot guards against accidental nil
// registry wiring at service boot — the failure mode must be a clear
// error, not a panic in lookupChatAssistant.
func TestBootstrap_NilRegistry_FailsBoot(t *testing.T) {
	t.Parallel()
	b := NewBootstrapper(nil, "tenant-a", nil)
	_, err := b.Boot(context.Background())
	if err == nil {
		t.Fatal("Boot must fail when registry is nil")
	}
	if !strings.Contains(err.Error(), "registry not configured") {
		t.Errorf("error %v should mention registry not configured", err)
	}
}
