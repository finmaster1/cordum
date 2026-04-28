package llmchat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cordum/cordum/core/audit"
	gatewayauth "github.com/cordum/cordum/core/controlplane/gateway/auth"
	cordumotel "github.com/cordum/cordum/core/infra/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type fakeTracingRunner struct{}

func (fakeTracingRunner) Turn(ctx context.Context, _ TurnInput) <-chan Frame {
	out := make(chan Frame, 1)
	go func() {
		defer close(out)
		select {
		case <-ctx.Done():
			out <- Frame{Type: FrameError, ErrorCode: ErrorCodeContextCancelled, ErrorMsg: ctx.Err().Error()}
		case out <- Frame{Type: FrameFinal, Text: "ok"}:
		}
	}()
	return out
}

type fakeTracingProvider struct {
	err error
}

func (p fakeTracingProvider) Complete(ctx context.Context, _ CompleteRequest, _ SamplingMode) (<-chan Chunk, error) {
	if p.err != nil {
		return nil, p.err
	}
	out := make(chan Chunk, 2)
	go func() {
		defer close(out)
		select {
		case <-ctx.Done():
			out <- Chunk{Err: ctx.Err(), Done: true}
		case out <- Chunk{Delta: "hello world"}:
			out <- Chunk{Done: true, FinishReason: "stop"}
		}
	}()
	return out, nil
}

func (p fakeTracingProvider) HealthCheck(context.Context) error { return p.err }

type fakeTracingSessionStore struct{}

func (fakeTracingSessionStore) Get(context.Context, string) (*Session, error) {
	return &Session{ID: "sess-1", Tenant: "tenant-a", UserPrincipal: "alice"}, nil
}

func (fakeTracingSessionStore) Create(_ context.Context, in Session) (Session, error) {
	if in.ID == "" {
		in.ID = "sess-created"
	}
	return in, nil
}

func (fakeTracingSessionStore) AppendMessage(context.Context, string, SessionMessage) error {
	return nil
}

func (fakeTracingSessionStore) ListSessions(context.Context, SessionListFilter) (SessionListPage, error) {
	return SessionListPage{Items: []SessionSummary{{ID: "sess-1"}}}, nil
}

type fakeTracingAuditSender struct {
	events []audit.SIEMEvent
}

func (s *fakeTracingAuditSender) Send(event audit.SIEMEvent) {
	s.events = append(s.events, event)
}

func (s *fakeTracingAuditSender) Close() error { return nil }

func installSpanRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	restore := cordumotel.SetTracerProviderForTest(tp)
	t.Cleanup(func() {
		restore()
		_ = tp.Shutdown(context.Background())
	})
	return recorder
}

func TestTracingRunnerEmitsChatTurnSpanWithSafeAttrs(t *testing.T) {
	recorder := installSpanRecorder(t)
	runner := NewTracingRunner(fakeTracingRunner{}, TracingRunnerConfig{Backend: "ollama-cpu"})

	session := &Session{ID: "sess-1", Tenant: "tenant-a", UserPrincipal: "alice@example.com"}
	var frames []Frame
	for frame := range runner.Turn(context.Background(), TurnInput{Session: session, UserMessage: "do not trace this prompt"}) {
		frames = append(frames, frame)
	}
	if len(frames) != 1 || frames[0].Type != FrameFinal {
		t.Fatalf("frames = %+v, want one final frame", frames)
	}

	span := findEndedSpan(t, recorder, "chat.turn")
	assertSpanAttr(t, span, "chat.session_id", "sess-1")
	assertSpanAttr(t, span, "chat.tenant", "tenant-a")
	assertSpanAttr(t, span, "chat.user_principal", "alice@example.com")
	assertSpanAttr(t, span, "llm.backend", "ollama-cpu")
	assertSpanDoesNotContain(t, span, "do not trace this prompt")
}

func TestTracingProviderEmitsInferenceSpanAndRecordsErrors(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		recorder := installSpanRecorder(t)
		provider := NewTracingProvider(fakeTracingProvider{}, TracingProviderConfig{Backend: "ollama-cpu", Model: "qwen2.5"})
		stream, err := provider.Complete(context.Background(), CompleteRequest{Messages: []Message{{Role: "user", Content: "hello"}}}, SamplingModeResponse)
		if err != nil {
			t.Fatalf("Complete: %v", err)
		}
		for range stream {
		}
		span := findEndedSpan(t, recorder, "llm.inference")
		assertSpanAttr(t, span, "llm.backend", "ollama-cpu")
		assertSpanAttr(t, span, "llm.model", "qwen2.5")
		assertSpanIntAttrAtLeast(t, span, "llm.tokens.prompt", 1)
		assertSpanIntAttrAtLeast(t, span, "llm.tokens.completion", 1)
	})

	t.Run("error", func(t *testing.T) {
		recorder := installSpanRecorder(t)
		provider := NewTracingProvider(fakeTracingProvider{err: errors.New("backend down")}, TracingProviderConfig{Backend: "ollama-cpu", Model: "qwen2.5"})
		if _, err := provider.Complete(context.Background(), CompleteRequest{}, SamplingModeResponse); err == nil {
			t.Fatal("expected provider error")
		}
		span := findEndedSpan(t, recorder, "llm.inference")
		if span.Status().Code != codes.Error {
			t.Fatalf("span status = %v, want error", span.Status())
		}
	})
}

func TestTracingSessionStoreEmitsRedisSpans(t *testing.T) {
	recorder := installSpanRecorder(t)
	store := NewTracingSessionStore(fakeTracingSessionStore{})
	ctx := context.Background()
	if _, err := store.Create(ctx, Session{ID: "sess-1", Tenant: "tenant-a", UserPrincipal: "alice"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.Get(ctx, "sess-1"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := store.AppendMessage(ctx, "sess-1", SessionMessage{Role: "user", Text: "secret text must not be traced", At: time.Now()}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if _, err := store.ListSessions(ctx, SessionListFilter{Tenant: "tenant-a", Limit: 10}); err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	names := map[string]int{}
	for _, span := range recorder.Ended() {
		names[span.Name()]++
		assertSpanDoesNotContain(t, span, "secret text must not be traced")
	}
	if names["chat.session.write"] < 2 {
		t.Fatalf("chat.session.write spans = %d, want >=2", names["chat.session.write"])
	}
	if names["chat.session.read"] < 2 {
		t.Fatalf("chat.session.read spans = %d, want >=2", names["chat.session.read"])
	}
}

func TestTraceWSHandlerEmitsConnectAndDisconnectSpans(t *testing.T) {
	recorder := installSpanRecorder(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gatewayauth.FromRequest(r) == nil {
			t.Fatal("auth context missing")
		}
		w.WriteHeader(http.StatusSwitchingProtocols)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat/ws?session_id=sess-1", nil)
	req = req.WithContext(context.WithValue(req.Context(), gatewayauth.ContextKey{}, &gatewayauth.AuthContext{
		Tenant:      "tenant-a",
		PrincipalID: "alice",
	}))
	TraceWSHandler(next).ServeHTTP(httptest.NewRecorder(), req)

	connect := findEndedSpan(t, recorder, "chat.ws.connect")
	disconnect := findEndedSpan(t, recorder, "chat.ws.disconnect")
	assertSpanAttr(t, connect, "chat.session_id", "sess-1")
	assertSpanAttr(t, disconnect, "chat.session_id", "sess-1")
}

func TestTracingAuditSenderEmitsAuditSpanWithTraceID(t *testing.T) {
	recorder := installSpanRecorder(t)
	inner := &fakeTracingAuditSender{}
	sender := NewTracingAuditSender(inner)
	ctx, parent := cordumotel.Tracer("test").Start(context.Background(), "chat.ws.connect")
	sender.(interface {
		SendWithContext(context.Context, audit.SIEMEvent)
	}).SendWithContext(ctx, audit.SIEMEvent{
		EventType: audit.EventSystemAuth,
		Action:    audit.SIEMActionChatSessionStarted,
		TenantID:  "tenant-a",
		Extra: map[string]string{
			"session_id": "sess-1",
			"prompt":     "this user prompt must not be traced",
		},
	})
	parent.End()

	span := findEndedSpan(t, recorder, "chat.audit.emit")
	assertSpanAttr(t, span, "audit.event_type", audit.EventSystemAuth)
	assertSpanAttr(t, span, "audit.action", audit.SIEMActionChatSessionStarted)
	assertSpanAttr(t, span, "chat.session_id", "sess-1")
	assertSpanDoesNotContain(t, span, "this user prompt must not be traced")
	if len(inner.events) != 1 {
		t.Fatalf("events = %d, want 1", len(inner.events))
	}
	traceID := inner.events[0].Extra["trace_id"]
	if traceID == "" || traceID != span.SpanContext().TraceID().String() {
		t.Fatalf("event trace_id = %q, want span trace id %s", traceID, span.SpanContext().TraceID())
	}
	if parentSpanID := span.Parent().SpanID(); !parentSpanID.IsValid() {
		t.Fatalf("audit span parent = %s, want valid parent from request trace", parentSpanID)
	}
}

func findEndedSpan(t *testing.T, recorder *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range recorder.Ended() {
		if span.Name() == name {
			return span
		}
	}
	t.Fatalf("span %q not found; spans=%v", name, spanNames(recorder.Ended()))
	return nil
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name())
	}
	return names
}

func assertSpanAttr(t *testing.T, span sdktrace.ReadOnlySpan, key, want string) {
	t.Helper()
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			if got := attr.Value.AsString(); got != want {
				t.Fatalf("span %s attr %s = %q, want %q", span.Name(), key, got, want)
			}
			return
		}
	}
	t.Fatalf("span %s missing attr %s; attrs=%v", span.Name(), key, span.Attributes())
}

func assertSpanIntAttrAtLeast(t *testing.T, span sdktrace.ReadOnlySpan, key string, min int64) {
	t.Helper()
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			if got := attr.Value.AsInt64(); got < min {
				t.Fatalf("span %s attr %s = %d, want >=%d", span.Name(), key, got, min)
			}
			return
		}
	}
	t.Fatalf("span %s missing int attr %s; attrs=%v", span.Name(), key, span.Attributes())
}

func assertSpanDoesNotContain(t *testing.T, span sdktrace.ReadOnlySpan, forbidden string) {
	t.Helper()
	for _, attr := range span.Attributes() {
		if strings.Contains(attr.Value.AsString(), forbidden) {
			t.Fatalf("span %s leaked forbidden text in attr %s", span.Name(), attr.Key)
		}
	}
}
