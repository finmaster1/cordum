package llmchat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cordum/cordum/core/audit"
	gatewayauth "github.com/cordum/cordum/core/controlplane/gateway/auth"
	"github.com/cordum/cordum/core/licensing"
)

type scriptedChatRunner struct {
	mu     sync.Mutex
	frames [][]Frame
	inputs []TurnInput
}

func (r *scriptedChatRunner) Turn(_ context.Context, in TurnInput) <-chan Frame {
	r.mu.Lock()
	r.inputs = append(r.inputs, in)
	idx := len(r.inputs) - 1
	frames := []Frame{{Type: FrameFinal, Text: "ok"}}
	if idx < len(r.frames) {
		frames = append([]Frame(nil), r.frames[idx]...)
	}
	r.mu.Unlock()
	return frameChan(frames)
}

func (r *scriptedChatRunner) snapshot() ([]TurnInput, []struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	turns := append([]TurnInput(nil), r.inputs...)
	return turns, nil
}

func frameChan(frames []Frame) <-chan Frame {
	out := make(chan Frame, len(frames))
	go func() {
		defer close(out)
		for _, f := range frames {
			out <- f
		}
	}()
	return out
}

type fakeChatSessionStore struct {
	mu       sync.Mutex
	byID     map[string]*Session
	createdN int
}

func newFakeChatSessionStore() *fakeChatSessionStore {
	return &fakeChatSessionStore{byID: map[string]*Session{}}
}

func (s *fakeChatSessionStore) Get(_ context.Context, id string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.byID[id]; ok {
		cp := *existing
		cp.Messages = append([]SessionMessage(nil), existing.Messages...)
		return &cp, nil
	}
	return nil, nil
}

func (s *fakeChatSessionStore) Create(_ context.Context, in Session) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdN++
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now().UTC()
	}
	if in.LastActiveAt.IsZero() {
		in.LastActiveAt = in.CreatedAt
	}
	if in.Messages == nil {
		in.Messages = []SessionMessage{}
	}
	stored := in
	s.byID[in.ID] = &stored
	return stored, nil
}

func (s *fakeChatSessionStore) AppendMessage(_ context.Context, id string, msg SessionMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.byID[id]
	if stored == nil {
		return errors.New("missing session")
	}
	stored.Messages = append(stored.Messages, msg)
	stored.LastActiveAt = time.Now().UTC()
	return nil
}

func (s *fakeChatSessionStore) ListSessions(_ context.Context, filter SessionListFilter) (SessionListPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	items := make([]SessionSummary, 0, len(s.byID))
	for _, sess := range s.byID {
		if !filter.AllTenants && filter.Tenant != "" && sess.Tenant != filter.Tenant {
			continue
		}
		if filter.Cursor != "" && sess.ID <= filter.Cursor {
			continue
		}
		items = append(items, SessionSummary{ID: sess.ID, Tenant: sess.Tenant, UserPrincipal: sess.UserPrincipal, AgentID: sess.AgentID, CreatedAt: sess.CreatedAt, LastActiveAt: sess.LastActiveAt})
	}
	if len(items) > limit {
		return SessionListPage{Items: items[:limit], NextCursor: items[limit-1].ID}, nil
	}
	return SessionListPage{Items: items}, nil
}

type fakeAuditSink struct {
	mu     sync.Mutex
	events []audit.SIEMEvent
}

func (s *fakeAuditSink) Send(e audit.SIEMEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *fakeAuditSink) snapshot() []audit.SIEMEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]audit.SIEMEvent, len(s.events))
	copy(out, s.events)
	return out
}

type fakeEntitlements struct{ enabled bool }

func (f fakeEntitlements) Entitlements() licensing.Entitlements {
	return licensing.Entitlements{LLMChatAssistant: f.enabled}
}

type fakePermissionEnforcer struct{ allow bool }

func (f fakePermissionEnforcer) RequirePermission(_ *http.Request, permission string) error {
	if permission != gatewayauth.PermChatReadAll {
		return errors.New("wrong permission")
	}
	if !f.allow {
		return errors.New("denied")
	}
	return nil
}

func newTestChatHandlers(runner *scriptedChatRunner, sessions *fakeChatSessionStore, enabled bool) *ChatHandlers {
	return NewChatHandlers(ChatHandlersConfig{
		Agent:           runner,
		Sessions:        sessions,
		Entitlements:    fakeEntitlements{enabled: enabled},
		AgentID:         "chat-assistant",
		UserPrincipalFn: func(_ *http.Request) string { return "alice" },
		TenantFn:        func(_ *http.Request) string { return "tenant-a" },
	})
}

func TestChatHandlers_LicenseGateAppliesToPostSSEAndWS(t *testing.T) {
	h := newTestChatHandlers(&scriptedChatRunner{}, newFakeChatSessionStore(), false)

	post := httptest.NewRecorder()
	h.HandleChatPost(post, httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(`{"message":"hi"}`)))
	if post.Code != http.StatusPaymentRequired || !strings.Contains(post.Body.String(), "feature_unavailable") {
		t.Fatalf("POST status/body = %d %s, want 402 feature_unavailable", post.Code, post.Body.String())
	}

	sse := httptest.NewRecorder()
	h.HandleChatStream(sse, httptest.NewRequest(http.MethodGet, "/api/v1/chat/stream?message=hi", nil))
	if sse.Code != http.StatusPaymentRequired || !strings.Contains(sse.Body.String(), "feature_unavailable") {
		t.Fatalf("SSE status/body = %d %s, want 402 feature_unavailable", sse.Code, sse.Body.String())
	}

	ws := httptest.NewRecorder()
	h.HandleChatWS(ws, httptest.NewRequest(http.MethodGet, "/api/v1/chat/ws", nil))
	if ws.Code != http.StatusPaymentRequired || !strings.Contains(ws.Body.String(), "feature_unavailable") {
		t.Fatalf("WS status/body = %d %s, want 402 feature_unavailable", ws.Code, ws.Body.String())
	}
}

func TestChatHandlers_PostSingleShotReturnsFinalAndFrames(t *testing.T) {
	runner := &scriptedChatRunner{frames: [][]Frame{{
		{Type: FrameUser, Text: "hi"},
		{Type: FrameAssistantDelta, Text: "No jobs"},
		{Type: FrameFinal, Text: "No jobs found."},
	}}}
	h := newTestChatHandlers(runner, newFakeChatSessionStore(), true)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(`{"message":"hi"}`))
	rr := httptest.NewRecorder()

	h.HandleChatPost(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp chatPostResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.SessionID == "" || resp.Assistant != "No jobs found." {
		t.Fatalf("resp=%+v want session id and final assistant", resp)
	}
	if got := frameTypes(resp.Frames); !sameFrameTypes(got, []FrameType{FrameUser, FrameAssistantDelta, FrameFinal}) {
		t.Fatalf("frame types=%v want informational-only frames", got)
	}
	turns, _ := runner.snapshot()
	if len(turns) != 1 || turns[0].UserMessage != "hi" {
		t.Fatalf("turns=%+v want one user turn", turns)
	}
}

func TestChatHandlers_StreamSSEFrames(t *testing.T) {
	runner := &scriptedChatRunner{frames: [][]Frame{{
		{Type: FrameUser, Text: "hi"},
		{Type: FrameAssistantDelta, Text: "Hello"},
		{Type: FrameFinal, Text: "Hello"},
	}}}
	h := newTestChatHandlers(runner, newFakeChatSessionStore(), true)
	rr := httptest.NewRecorder()
	h.HandleChatStream(rr, httptest.NewRequest(http.MethodGet, "/api/v1/chat/stream?message=hi", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type=%q want text/event-stream", ct)
	}
	body := rr.Body.String()
	if strings.Count(body, "data: ") != 3 || !strings.Contains(body, `"type":"final"`) || !strings.HasSuffix(body, "\n\n") {
		t.Fatalf("bad SSE body: %q", body)
	}
}

func TestChatHandlers_ResumeRequiresTrustedNonEmptyPrincipalAndTenant(t *testing.T) {
	sessions := newFakeChatSessionStore()
	seedSession(t, sessions, "sess-victim", "tenant-a", "alice")
	runner := &scriptedChatRunner{}
	h := NewChatHandlers(ChatHandlersConfig{
		Agent:           runner,
		Sessions:        sessions,
		Entitlements:    fakeEntitlements{enabled: true},
		AgentID:         "chat-assistant",
		UserPrincipalFn: func(*http.Request) string { return "" },
		TenantFn:        func(*http.Request) string { return "" },
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(`{"session_id":"sess-victim","message":"hi"}`))
	rr := httptest.NewRecorder()
	h.HandleChatPost(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s want 404 for session resume without trusted identity", rr.Code, rr.Body.String())
	}
	turns, _ := runner.snapshot()
	if len(turns) != 0 {
		t.Fatalf("turns=%+v want no agent turn for unauthenticated resume", turns)
	}
	if sessions.createdN != 1 {
		t.Fatalf("createdN=%d want only seeded session, no replacement", sessions.createdN)
	}
}

func TestChatHandlers_DefaultIdentityIgnoresSpoofedHeaders(t *testing.T) {
	sessions := newFakeChatSessionStore()
	seedSession(t, sessions, "sess-victim", "tenant-a", "alice")
	runner := &scriptedChatRunner{}
	h := NewChatHandlers(ChatHandlersConfig{
		Agent:        runner,
		Sessions:     sessions,
		Entitlements: fakeEntitlements{enabled: true},
		AgentID:      "chat-assistant",
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(`{"session_id":"sess-victim","message":"hi"}`))
	req.Header.Set("X-Cordum-User-Principal", "alice")
	req.Header.Set("X-Principal-Id", "alice")
	req.Header.Set("X-Cordum-Tenant", "tenant-a")
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rr := httptest.NewRecorder()
	h.HandleChatPost(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s want 404 for spoofed header resume without trusted auth", rr.Code, rr.Body.String())
	}
	turns, _ := runner.snapshot()
	if len(turns) != 0 {
		t.Fatalf("turns=%+v want no agent turn for spoofed header resume", turns)
	}
	if sessions.createdN != 1 {
		t.Fatalf("createdN=%d want only seeded session, no replacement", sessions.createdN)
	}
}

func TestChatHandlers_ForgedSessionIDDoesNotCreateReplacement(t *testing.T) {
	sessions := newFakeChatSessionStore()
	seedSession(t, sessions, "sess-victim", "tenant-a", "alice")
	runner := &scriptedChatRunner{}
	h := NewChatHandlers(ChatHandlersConfig{
		Agent:           runner,
		Sessions:        sessions,
		Entitlements:    fakeEntitlements{enabled: true},
		AgentID:         "chat-assistant",
		UserPrincipalFn: func(*http.Request) string { return "bob" },
		TenantFn:        func(*http.Request) string { return "tenant-a" },
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewBufferString(`{"session_id":"sess-victim","message":"hi"}`))
	rr := httptest.NewRecorder()
	h.HandleChatPost(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s want 404 for forged session id", rr.Code, rr.Body.String())
	}
	turns, _ := runner.snapshot()
	if len(turns) != 0 {
		t.Fatalf("turns=%+v want no agent turn for forged session", turns)
	}
	if sessions.createdN != 1 {
		t.Fatalf("createdN=%d want only seeded session, no replacement", sessions.createdN)
	}
}

func TestChatFrameSchemaPinned(t *testing.T) {
	frames := []Frame{
		{Type: FrameUser, Text: "hello", SessionID: "sess-1"},
		{Type: FrameAssistantDelta, Text: "hel"},
		{Type: FrameFinal, Text: "done"},
		{Type: FrameError, IsError: true, ErrorCode: "boom", ErrorMsg: "nope"},
	}
	for _, frame := range frames {
		raw, err := json.Marshal(frame)
		if err != nil {
			t.Fatalf("marshal %s: %v", frame.Type, err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", frame.Type, err)
		}
		if got["type"] != string(frame.Type) {
			t.Fatalf("frame %s JSON=%s missing stable type", frame.Type, raw)
		}
		switch frame.Type {
		case FrameUser:
			assertJSONKeys(t, got, "type", "session_id", "text")
		case FrameAssistantDelta, FrameFinal:
			assertJSONKeys(t, got, "type", "text")
		case FrameError:
			assertJSONKeys(t, got, "type", "is_error", "error_code", "error_msg")
		}
	}
}

func assertJSONKeys(t *testing.T, got map[string]any, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("keys=%v want %v", keysOf(got), want)
	}
	for _, key := range want {
		if _, ok := got[key]; !ok {
			t.Fatalf("keys=%v missing %q", keysOf(got), key)
		}
	}
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestChatHandlers_AuditSessionStartedAndClosed(t *testing.T) {
	sink := &fakeAuditSink{}
	h := newTestChatHandlers(&scriptedChatRunner{}, newFakeChatSessionStore(), true)
	h.audit = sink
	sess := &Session{ID: "sess-test", UserPrincipal: "alice", Tenant: "tenant-a", AgentID: "chat-assistant"}

	h.emitSessionStarted(sess)
	h.emitSessionClosed(sess, 3, 250*time.Millisecond)

	events := sink.snapshot()
	if len(events) != 2 {
		t.Fatalf("events=%d want 2: %#v", len(events), events)
	}
	if events[0].EventType != audit.EventSystemAuth || events[0].Action != audit.SIEMActionChatSessionStarted {
		t.Fatalf("started event=%#v want EventSystemAuth action chat.session_started", events[0])
	}
	if events[0].Extra["session_id"] != "sess-test" || events[0].TenantID != "tenant-a" || events[0].Identity != "alice" {
		t.Fatalf("started event context wrong: %#v", events[0])
	}
	if events[1].Action != audit.SIEMActionChatSessionClosed || events[1].Extra["turn_count"] != "3" || events[1].Extra["duration_ms"] == "" {
		t.Fatalf("closed event wrong: %#v", events[1])
	}
	if _, ok := events[1].Extra["total_"+"tool_"+"calls"]; ok {
		t.Fatalf("closed event contains retired action-count metadata: %#v", events[1])
	}
}

func TestChainedAuditSender_AppendsChatLifecycleEventsToAuditChain(t *testing.T) {
	rdb, _ := newMiniredisClient(t)
	chainer := audit.NewChainer(rdb, audit.ChainKeyPrefix)
	sender := NewChainedAuditSender(chainer, nil)

	sender.Send(audit.SIEMEvent{
		Timestamp: time.Now().UTC(),
		EventType: audit.EventSystemAuth,
		Severity:  "info",
		TenantID:  "tenant-a",
		AgentID:   "chat-assistant",
		Identity:  "alice",
		Action:    audit.SIEMActionChatSessionStarted,
		Extra:     map[string]string{"session_id": "sess-chain"},
	})
	sender.Send(audit.SIEMEvent{
		Timestamp: time.Now().UTC(),
		EventType: audit.EventSystemAuth,
		Severity:  "info",
		TenantID:  "tenant-a",
		AgentID:   "chat-assistant",
		Identity:  "alice",
		Action:    audit.SIEMActionChatSessionClosed,
		Extra:     map[string]string{"session_id": "sess-chain"},
	})

	result, err := audit.VerifyChain(context.Background(), rdb, chainer.StreamKey("tenant-a"), audit.VerifyOptions{})
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if result.Status != audit.VerifyStatusOK || result.VerifiedEvents != 2 {
		t.Fatalf("verify result=%+v want status ok with 2 verified events", result)
	}
}
