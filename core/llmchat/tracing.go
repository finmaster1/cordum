package llmchat

import (
	"context"
	"net/http"
	"strings"

	"github.com/cordum/cordum/core/audit"
	gatewayauth "github.com/cordum/cordum/core/controlplane/gateway/auth"
	cordumotel "github.com/cordum/cordum/core/infra/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	tracerName = "cordum-llm-chat"
)

type tracedTurnRunner interface {
	Turn(ctx context.Context, in TurnInput) <-chan Frame
}

// TracingRunner wraps the informational chat turn loop with an OTEL span while
// keeping the Agent API unchanged. The wrapper never adds prompt or message
// text to span attributes.
type TracingRunner struct {
	inner   tracedTurnRunner
	backend string
}

type TracingRunnerConfig struct {
	Backend string
}

func NewTracingRunner(inner tracedTurnRunner, cfg TracingRunnerConfig) *TracingRunner {
	return &TracingRunner{inner: inner, backend: strings.TrimSpace(cfg.Backend)}
}

func (r *TracingRunner) Turn(ctx context.Context, in TurnInput) <-chan Frame {
	out := make(chan Frame, 8)
	if r == nil || r.inner == nil {
		close(out)
		return out
	}
	ctx, span := cordumotel.Tracer(tracerName).Start(ctx, "chat.turn",
		oteltrace.WithSpanKind(oteltrace.SpanKindInternal),
		oteltrace.WithAttributes(sessionTraceAttributes(in.Session, attribute.String("llm.backend", boundedTraceValue(r.backend)))...),
	)
	go func() {
		defer close(out)
		defer span.End()
		for frame := range r.inner.Turn(ctx, in) {
			if frame.Type == FrameError {
				span.SetStatus(codes.Error, boundedTraceValue(firstNonEmpty(frame.ErrorCode, frame.ErrorMsg)))
				if frame.ErrorCode != "" {
					span.SetAttributes(attribute.String("chat.error_code", boundedTraceValue(frame.ErrorCode)))
				}
			}
			select {
			case <-ctx.Done():
				span.SetStatus(codes.Error, boundedTraceValue(ctx.Err().Error()))
				return
			case out <- frame:
			}
		}
	}()
	return out
}

type TracingProviderConfig struct {
	Backend string
	Model   string
}

// TracingProvider instruments the OpenAI-compatible backend call. It only
// records bounded metadata and approximate token counts, never prompts.
type TracingProvider struct {
	inner   Provider
	backend string
	model   string
}

func NewTracingProvider(inner Provider, cfg TracingProviderConfig) Provider {
	if inner == nil {
		return nil
	}
	return &TracingProvider{
		inner:   inner,
		backend: strings.TrimSpace(cfg.Backend),
		model:   strings.TrimSpace(cfg.Model),
	}
}

func (p *TracingProvider) Complete(ctx context.Context, req CompleteRequest, mode SamplingMode) (<-chan Chunk, error) {
	ctx, span := cordumotel.Tracer(tracerName).Start(ctx, "llm.inference",
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
		oteltrace.WithAttributes(
			attribute.String("llm.backend", boundedTraceValue(p.backend)),
			attribute.String("llm.model", boundedTraceValue(p.model)),
			attribute.Int("llm.tokens.prompt", approximatePromptTokens(req)),
		),
	)
	stream, err := p.inner.Complete(ctx, req, mode)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, boundedTraceValue(err.Error()))
		span.End()
		return nil, err
	}
	out := make(chan Chunk, 8)
	go func() {
		defer close(out)
		defer span.End()
		completionTokens := 0
		for chunk := range stream {
			if chunk.Delta != "" {
				completionTokens += approximateTokens(chunk.Delta)
			}
			if chunk.Err != nil {
				span.RecordError(chunk.Err)
				span.SetStatus(codes.Error, boundedTraceValue(chunk.Err.Error()))
			}
			select {
			case <-ctx.Done():
				span.RecordError(ctx.Err())
				span.SetStatus(codes.Error, boundedTraceValue(ctx.Err().Error()))
				span.SetAttributes(attribute.Int("llm.tokens.completion", completionTokens))
				return
			case out <- chunk:
			}
		}
		span.SetAttributes(attribute.Int("llm.tokens.completion", completionTokens))
	}()
	return out, nil
}

func (p *TracingProvider) HealthCheck(ctx context.Context) error {
	ctx, span := cordumotel.Tracer(tracerName).Start(ctx, "llm.inference.health",
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
		oteltrace.WithAttributes(
			attribute.String("llm.backend", boundedTraceValue(p.backend)),
			attribute.String("llm.model", boundedTraceValue(p.model)),
		),
	)
	defer span.End()
	err := p.inner.HealthCheck(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, boundedTraceValue(err.Error()))
	}
	return err
}

type tracingSessionStoreInner interface {
	Get(ctx context.Context, id string) (*Session, error)
	Create(ctx context.Context, in Session) (Session, error)
	AppendMessage(ctx context.Context, id string, msg SessionMessage) error
	ListSessions(ctx context.Context, filter SessionListFilter) (SessionListPage, error)
}

// TracingSessionStore wraps Redis-backed session operations with bounded OTEL
// spans. It records IDs and roles but never transcript text.
type TracingSessionStore struct {
	inner tracingSessionStoreInner
}

func NewTracingSessionStore(inner tracingSessionStoreInner) *TracingSessionStore {
	return &TracingSessionStore{inner: inner}
}

func (s *TracingSessionStore) Get(ctx context.Context, id string) (*Session, error) {
	ctx, span := cordumotel.Tracer(tracerName).Start(ctx, "chat.session.read",
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
		oteltrace.WithAttributes(redisSessionAttrs(id)...),
	)
	defer span.End()
	out, err := s.inner.Get(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, boundedTraceValue(err.Error()))
	}
	if out != nil {
		span.SetAttributes(sessionTraceAttributes(out)...)
	}
	return out, err
}

func (s *TracingSessionStore) Create(ctx context.Context, in Session) (Session, error) {
	ctx, span := cordumotel.Tracer(tracerName).Start(ctx, "chat.session.write",
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
		oteltrace.WithAttributes(sessionTraceAttributes(&in, attribute.String("db.system", "redis"), attribute.String("db.operation", "create"))...),
	)
	defer span.End()
	out, err := s.inner.Create(ctx, in)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, boundedTraceValue(err.Error()))
		return out, err
	}
	span.SetAttributes(sessionTraceAttributes(&out)...)
	return out, nil
}

func (s *TracingSessionStore) AppendMessage(ctx context.Context, id string, msg SessionMessage) error {
	ctx, span := cordumotel.Tracer(tracerName).Start(ctx, "chat.session.write",
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
		oteltrace.WithAttributes(append(redisSessionAttrs(id), attribute.String("chat.message_role", boundedTraceValue(msg.Role)), attribute.String("db.operation", "append_message"))...),
	)
	defer span.End()
	err := s.inner.AppendMessage(ctx, id, msg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, boundedTraceValue(err.Error()))
	}
	return err
}

func (s *TracingSessionStore) ListSessions(ctx context.Context, filter SessionListFilter) (SessionListPage, error) {
	ctx, span := cordumotel.Tracer(tracerName).Start(ctx, "chat.session.read",
		oteltrace.WithSpanKind(oteltrace.SpanKindClient),
		oteltrace.WithAttributes(
			attribute.String("db.system", "redis"),
			attribute.String("db.operation", "scan_sessions"),
			attribute.String("chat.tenant", boundedTraceValue(filter.Tenant)),
			attribute.Bool("chat.all_tenants", filter.AllTenants),
			attribute.Int("chat.limit", filter.Limit),
		),
	)
	defer span.End()
	page, err := s.inner.ListSessions(ctx, filter)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, boundedTraceValue(err.Error()))
		return page, err
	}
	span.SetAttributes(attribute.Int("chat.sessions_returned", len(page.Items)))
	return page, nil
}

// TraceWSHandler creates explicit connect/disconnect spans around the
// websocket handler without changing the handler's auth/session logic.
func TraceWSHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attrs := requestTraceAttributes(r)
		ctx, span := cordumotel.Tracer(tracerName).Start(r.Context(), "chat.ws.connect",
			oteltrace.WithSpanKind(oteltrace.SpanKindServer),
			oteltrace.WithAttributes(attrs...),
		)
		next.ServeHTTP(w, r.WithContext(ctx))
		span.End()
		_, disconnect := cordumotel.Tracer(tracerName).Start(ctx, "chat.ws.disconnect",
			oteltrace.WithSpanKind(oteltrace.SpanKindInternal),
			oteltrace.WithAttributes(attrs...),
		)
		disconnect.End()
	})
}

type tracingAuditContextSender interface {
	SendWithContext(context.Context, audit.SIEMEvent)
}

type TracingAuditSender struct {
	inner audit.AuditSender
}

func NewTracingAuditSender(inner audit.AuditSender) audit.AuditSender {
	if inner == nil {
		return nil
	}
	return &TracingAuditSender{inner: inner}
}

func (s *TracingAuditSender) Send(event audit.SIEMEvent) {
	s.SendWithContext(context.Background(), event)
}

func (s *TracingAuditSender) SendWithContext(ctx context.Context, event audit.SIEMEvent) {
	if s == nil || s.inner == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	attrs := []attribute.KeyValue{
		attribute.String("audit.event_type", boundedTraceValue(event.EventType)),
		attribute.String("audit.action", boundedTraceValue(event.Action)),
		attribute.String("chat.tenant", boundedTraceValue(event.TenantID)),
	}
	if event.Extra != nil {
		if sessionID := event.Extra["session_id"]; sessionID != "" {
			attrs = append(attrs, attribute.String("chat.session_id", boundedTraceValue(sessionID)))
		}
	}
	ctx, span := cordumotel.Tracer(tracerName).Start(ctx, "chat.audit.emit",
		oteltrace.WithSpanKind(oteltrace.SpanKindInternal),
		oteltrace.WithAttributes(attrs...),
	)
	defer span.End()
	if traceID := span.SpanContext().TraceID(); traceID.IsValid() {
		event.Extra = cloneExtra(event.Extra)
		event.Extra["trace_id"] = traceID.String()
	}
	s.inner.Send(event)
}

func (s *TracingAuditSender) Close() error {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

func sessionTraceAttributes(session *Session, extra ...attribute.KeyValue) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 4+len(extra))
	if session != nil {
		attrs = append(attrs,
			attribute.String("chat.session_id", boundedTraceValue(session.ID)),
			attribute.String("chat.tenant", boundedTraceValue(session.Tenant)),
			attribute.String("chat.user_principal", boundedTraceValue(session.UserPrincipal)),
		)
	}
	return append(attrs, extra...)
}

func redisSessionAttrs(id string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("db.system", "redis"),
		attribute.String("chat.session_id", boundedTraceValue(id)),
	}
}

func requestTraceAttributes(r *http.Request) []attribute.KeyValue {
	attrs := []attribute.KeyValue{}
	if r == nil {
		return attrs
	}
	if sessionID := firstNonEmpty(r.URL.Query().Get("session_id"), r.Header.Get(HeaderChatSessionID)); sessionID != "" {
		attrs = append(attrs, attribute.String("chat.session_id", boundedTraceValue(sessionID)))
	}
	if authCtx := gatewayauth.FromRequest(r); authCtx != nil {
		attrs = append(attrs,
			attribute.String("chat.tenant", boundedTraceValue(authCtx.Tenant)),
			attribute.String("chat.user_principal", boundedTraceValue(authCtx.PrincipalID)),
		)
	}
	return attrs
}

func approximatePromptTokens(req CompleteRequest) int {
	total := 0
	for _, msg := range req.Messages {
		total += approximateTokens(msg.Content)
	}
	return total
}

func approximateTokens(s string) int {
	if s = strings.TrimSpace(s); s == "" {
		return 0
	}
	// Conservative English/code approximation used for bounded observability
	// only. It is not used for billing or policy enforcement.
	return (len(s) + 3) / 4
}

func boundedTraceValue(raw string) string {
	const maxTraceAttrLen = 128
	raw = strings.TrimSpace(raw)
	if len(raw) <= maxTraceAttrLen {
		return raw
	}
	runes := []rune(raw)
	if len(runes) <= maxTraceAttrLen {
		return raw
	}
	return string(runes[:maxTraceAttrLen])
}

func cloneExtra(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}
