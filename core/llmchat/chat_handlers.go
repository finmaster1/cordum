package llmchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/cordum/cordum/core/audit"
	gatewayauth "github.com/cordum/cordum/core/controlplane/gateway/auth"
	"github.com/cordum/cordum/core/licensing"
)

const (
	// HeaderChatSessionID is echoed on WS upgrades and accepted by all chat
	// transports for reconnect/resume.
	HeaderChatSessionID = "X-Chat-Session-Id"

	llmChatFeatureName = "llm_chat_assistant"
	maxWSMessageBytes  = 64 * 1024
	wsWriteQueueSize   = 64
)

var (
	errChatAuthRequired     = errors.New("chat authentication required")
	errChatSessionNotFound  = errors.New("chat session not found")
	errChatSessionForbidden = errors.New("chat session not found")
)

// chatRunner runs a user turn against the phase-4 agent loop.
type chatRunner interface {
	Turn(ctx context.Context, in TurnInput) <-chan Frame
}

// chatSessionStore is the slice of SessionStore needed by HTTP transports and
// the admin viewer.
type chatSessionStore interface {
	Get(ctx context.Context, id string) (*Session, error)
	Create(ctx context.Context, in Session) (Session, error)
	AppendMessage(ctx context.Context, id string, msg SessionMessage) error
	ListSessions(ctx context.Context, filter SessionListFilter) (SessionListPage, error)
}

type chatEntitlementResolver interface {
	Entitlements() licensing.Entitlements
}

type chatPermissionEnforcer interface {
	RequirePermission(r *http.Request, permission string) error
}

type chatAuditSender interface {
	Send(event audit.SIEMEvent)
}

// chatPostRequest is shared by POST and WS inbound user-message frames.
type chatPostRequest struct {
	SessionID string `json:"session_id,omitempty"`
	Message   string `json:"message"`
	Text      string `json:"text,omitempty"`
}

func (r chatPostRequest) userMessage() string {
	if msg := strings.TrimSpace(r.Message); msg != "" {
		return msg
	}
	return strings.TrimSpace(r.Text)
}

// chatPostResponse is returned by POST /api/v1/chat.
type chatPostResponse struct {
	SessionID string  `json:"session_id"`
	Assistant string  `json:"assistant"`
	Frames    []Frame `json:"frames"`
}

// ChatHandlers carries dependencies shared by the phase-5 chat endpoints.
type ChatHandlers struct {
	agent        chatRunner
	sessions     chatSessionStore
	entitlements chatEntitlementResolver
	permissions  chatPermissionEnforcer
	audit        chatAuditSender
	metrics      *Metrics

	upgrader websocket.Upgrader
	agentID  string

	userPrincipalFn func(r *http.Request) string
	tenantFn        func(r *http.Request) string

	activeMu       sync.Mutex
	activeSessions map[string]struct{}
}

// ChatHandlersConfig wires ChatHandlers.
type ChatHandlersConfig struct {
	Agent           chatRunner
	Sessions        chatSessionStore
	Entitlements    chatEntitlementResolver
	Permissions     chatPermissionEnforcer
	Audit           chatAuditSender
	Metrics         *Metrics
	AgentID         string
	UserPrincipalFn func(r *http.Request) string
	TenantFn        func(r *http.Request) string
}

// NewChatHandlers wires the chat HTTP/WS handlers from collaborators.
func NewChatHandlers(cfg ChatHandlersConfig) *ChatHandlers {
	if cfg.UserPrincipalFn == nil {
		cfg.UserPrincipalFn = defaultUserPrincipal
	}
	if cfg.TenantFn == nil {
		cfg.TenantFn = defaultTenant
	}
	if cfg.Metrics == nil {
		cfg.Metrics = NewNopMetrics()
	}
	return &ChatHandlers{
		agent:           cfg.Agent,
		sessions:        cfg.Sessions,
		entitlements:    cfg.Entitlements,
		permissions:     cfg.Permissions,
		audit:           cfg.Audit,
		metrics:         cfg.Metrics,
		agentID:         cfg.AgentID,
		userPrincipalFn: cfg.UserPrincipalFn,
		tenantFn:        cfg.TenantFn,
		activeSessions:  map[string]struct{}{},
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			Subprotocols:    []string{gatewayauth.WSAuthSubprotocol},
			CheckOrigin: func(_ *http.Request) bool {
				return true
			},
		},
	}
}

// HandleChatPost implements POST /api/v1/chat.
func (h *ChatHandlers) HandleChatPost(w http.ResponseWriter, r *http.Request) {
	if !h.requireChatEntitlement(w) {
		return
	}
	if r.Method != http.MethodPost {
		h.metrics.IncError(ErrorKindOther)
		writeChatError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	var req chatPostRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxWSMessageBytes)).Decode(&req); err != nil {
		h.metrics.IncError(ErrorKindOther)
		writeChatError(w, http.StatusBadRequest, "invalid_json", "request body must be JSON {session_id?, message}")
		return
	}
	msg := req.userMessage()
	if msg == "" {
		h.metrics.IncError(ErrorKindOther)
		writeChatError(w, http.StatusBadRequest, "empty_message", "message is required")
		return
	}
	if req.SessionID == "" {
		req.SessionID = r.Header.Get(HeaderChatSessionID)
	}
	session, err := h.resolveOrCreateSession(r.Context(), r, req.SessionID)
	if err != nil {
		h.metrics.IncError(errorKindForChatError(err))
		writeChatError(w, httpStatusForChatError(err), chatErrorCode(err), err.Error())
		return
	}
	h.metrics.IncSessions()
	defer h.metrics.DecSessions()
	frames := h.collectTurnFrames(r.Context(), session, msg)
	resp := chatPostResponse{SessionID: session.ID, Frames: frames}
	for _, frame := range frames {
		switch frame.Type {
		case FrameFinal:
			resp.Assistant = frame.Text
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleChatStream implements GET /api/v1/chat/stream (SSE fallback).
func (h *ChatHandlers) HandleChatStream(w http.ResponseWriter, r *http.Request) {
	if !h.requireChatEntitlement(w) {
		return
	}
	if r.Method != http.MethodGet {
		h.metrics.IncError(ErrorKindOther)
		writeChatError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
		return
	}
	msg := strings.TrimSpace(r.URL.Query().Get("message"))
	if msg == "" {
		h.metrics.IncError(ErrorKindOther)
		writeChatError(w, http.StatusBadRequest, "empty_message", "message query parameter is required")
		return
	}
	sessionID := firstNonEmpty(r.URL.Query().Get("session_id"), r.Header.Get(HeaderChatSessionID))
	session, err := h.resolveOrCreateSession(r.Context(), r, sessionID)
	if err != nil {
		h.metrics.IncError(errorKindForChatError(err))
		writeChatError(w, httpStatusForChatError(err), chatErrorCode(err), err.Error())
		return
	}
	h.metrics.IncSessions()
	defer h.metrics.DecSessions()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	for _, frame := range h.collectTurnFrames(r.Context(), session, msg) {
		raw, err := json.Marshal(frame)
		if err != nil {
			slog.Warn("llmchat: marshal SSE frame failed", "error", err, "session_id", session.ID)
			continue
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// HandleChatWS implements GET /api/v1/chat/ws.
func (h *ChatHandlers) HandleChatWS(w http.ResponseWriter, r *http.Request) {
	if !h.requireChatEntitlement(w) {
		return
	}
	if r.Method != http.MethodGet {
		h.metrics.IncError(ErrorKindOther)
		writeChatError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required for WS upgrade")
		return
	}
	sessionID := firstNonEmpty(r.URL.Query().Get("session_id"), r.Header.Get(HeaderChatSessionID))
	session, err := h.resolveOrCreateSession(r.Context(), r, sessionID)
	if err != nil {
		h.metrics.IncError(errorKindForChatError(err))
		writeChatError(w, httpStatusForChatError(err), chatErrorCode(err), err.Error())
		return
	}
	if !h.markSessionActive(session.ID) {
		h.metrics.IncError(ErrorKindOther)
		writeChatError(w, http.StatusConflict, "session_already_active", "session already has an active websocket")
		return
	}
	defer h.unmarkSessionActive(session.ID)

	conn, err := h.upgrader.Upgrade(w, r, http.Header{HeaderChatSessionID: {session.ID}})
	if err != nil {
		slog.Warn("llmchat: ws upgrade failed", "error", err, "session_id", session.ID)
		return
	}
	defer func() { _ = conn.Close() }()

	startedAt := time.Now()
	turnCount := 0
	h.emitSessionStarted(session)
	defer func() { h.emitSessionClosed(session, turnCount, time.Since(startedAt)) }()

	out := make(chan Frame, wsWriteQueueSize)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for frame := range out {
			if err := conn.WriteJSON(frame); err != nil {
				slog.Warn("llmchat: ws write failed", "error", err, "session_id", session.ID)
				return
			}
		}
	}()

	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if len(payload) > maxWSMessageBytes {
			h.metrics.IncError(ErrorKindOther)
			h.emitToWS(out, Frame{Type: FrameError, ErrorCode: "message_too_large", ErrorMsg: "message exceeds 64KiB", SessionID: session.ID})
			break
		}
		var msg chatPostRequest
		if err := json.Unmarshal(payload, &msg); err != nil {
			h.metrics.IncError(ErrorKindOther)
			h.emitToWS(out, Frame{Type: FrameError, ErrorCode: "invalid_json", ErrorMsg: "message must be JSON", SessionID: session.ID})
			continue
		}
		userMsg := msg.userMessage()
		if userMsg == "" {
			h.metrics.IncError(ErrorKindOther)
			h.emitToWS(out, Frame{Type: FrameError, ErrorCode: "empty_message", ErrorMsg: "message is required", SessionID: session.ID})
			continue
		}
		turnCount++
		frames := h.collectTurnFrames(r.Context(), session, userMsg)
		for _, frame := range frames {
			if !h.emitToWS(out, frame) {
				break
			}
		}
	}
	close(out)
	<-done
}

func (h *ChatHandlers) collectTurnFrames(ctx context.Context, session *Session, message string) []Frame {
	frames := make([]Frame, 0, 8)
	if h.agent == nil {
		return []Frame{{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "agent not configured", SessionID: session.ID}}
	}
	for frame := range h.agent.Turn(ctx, TurnInput{Session: session, UserMessage: message}) {
		frames = append(frames, h.decorateFrame(session, frame))
	}
	return frames
}

func (h *ChatHandlers) decorateFrame(session *Session, frame Frame) Frame {
	if frame.SessionID == "" && session != nil {
		frame.SessionID = session.ID
	}
	h.recordFrameMetrics(frame)
	return frame
}

func (h *ChatHandlers) recordFrameMetrics(frame Frame) {
	if h == nil || h.metrics == nil {
		return
	}
	if frame.Type == FrameError {
		h.metrics.IncError(errorKindForFrameError(frame.ErrorCode))
	}
}

func (h *ChatHandlers) emitToWS(out chan<- Frame, frame Frame) bool {
	select {
	case out <- frame:
		return true
	default:
		select {
		case out <- Frame{Type: FrameError, ErrorCode: "backpressure", ErrorMsg: "client too slow", SessionID: frame.SessionID}:
		default:
		}
		return false
	}
}

func (h *ChatHandlers) resolveOrCreateSession(ctx context.Context, r *http.Request, sessionID string) (*Session, error) {
	if h.sessions == nil {
		return nil, errSessionMissing
	}
	user := strings.TrimSpace(h.userPrincipalFn(r))
	tenant := strings.TrimSpace(h.tenantFn(r))
	sessionID = strings.TrimSpace(sessionID)
	if user == "" || tenant == "" {
		if sessionID != "" {
			return nil, errChatSessionNotFound
		}
		return nil, errChatAuthRequired
	}
	if sessionID != "" {
		existing, err := h.sessions.Get(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("load session: %w", err)
		}
		if existing != nil && sessionVisibleToUser(existing, user, tenant) {
			return existing, nil
		}
		if existing != nil {
			slog.Warn("llmchat: forged or cross-tenant chat session id rejected", "session_id", sessionID, "tenant", tenant)
			return nil, errChatSessionForbidden
		}
		created, err := h.sessions.Create(ctx, Session{ID: sessionID, UserPrincipal: user, Tenant: tenant, AgentID: h.agentID})
		if err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
		return &created, nil
	}
	created, err := h.sessions.Create(ctx, Session{ID: uuid.NewString(), UserPrincipal: user, Tenant: tenant, AgentID: h.agentID})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &created, nil
}

func sessionVisibleToUser(sess *Session, user, tenant string) bool {
	if sess == nil {
		return false
	}
	user = strings.TrimSpace(user)
	tenant = strings.TrimSpace(tenant)
	if user == "" || tenant == "" || strings.TrimSpace(sess.UserPrincipal) == "" || strings.TrimSpace(sess.Tenant) == "" {
		return false
	}
	return sess.UserPrincipal == user && sess.Tenant == tenant
}

func (h *ChatHandlers) requireChatEntitlement(w http.ResponseWriter) bool {
	if h != nil && h.entitlements != nil {
		entitlements := h.entitlements.Entitlements()
		if (&entitlements).FeatureEnabled(llmChatFeatureName) {
			return true
		}
	}
	if h != nil && h.metrics != nil {
		h.metrics.IncError(ErrorKindAuthRejected)
	}
	writeChatError(w, http.StatusPaymentRequired, "feature_unavailable", "chat requires Enterprise")
	return false
}

func (h *ChatHandlers) emitSessionStarted(s *Session) {
	if h == nil || h.audit == nil || s == nil {
		return
	}
	h.audit.Send(audit.SIEMEvent{
		Timestamp: time.Now().UTC(), EventType: audit.EventSystemAuth, Severity: "info",
		TenantID: s.Tenant, AgentID: s.AgentID, Identity: s.UserPrincipal, Action: audit.SIEMActionChatSessionStarted,
		Extra: map[string]string{"session_id": s.ID, "user_principal": s.UserPrincipal, "tenant": s.Tenant, "agent_id": s.AgentID},
	})
}

func (h *ChatHandlers) emitSessionClosed(s *Session, turnCount int, dur time.Duration) {
	if h == nil || h.audit == nil || s == nil {
		return
	}
	h.audit.Send(audit.SIEMEvent{
		Timestamp: time.Now().UTC(), EventType: audit.EventSystemAuth, Severity: "info",
		TenantID: s.Tenant, AgentID: s.AgentID, Identity: s.UserPrincipal, Action: audit.SIEMActionChatSessionClosed,
		Extra: map[string]string{
			"session_id": s.ID, "turn_count": fmt.Sprintf("%d", turnCount),
			"duration_ms": fmt.Sprintf("%d", dur.Milliseconds()),
		},
	})
}

func (h *ChatHandlers) markSessionActive(id string) bool {
	h.activeMu.Lock()
	defer h.activeMu.Unlock()
	if _, exists := h.activeSessions[id]; exists {
		return false
	}
	h.activeSessions[id] = struct{}{}
	h.metrics.IncSessions()
	return true
}

func (h *ChatHandlers) unmarkSessionActive(id string) {
	h.activeMu.Lock()
	if _, exists := h.activeSessions[id]; exists {
		delete(h.activeSessions, id)
		h.metrics.DecSessions()
	}
	h.activeMu.Unlock()
}

func defaultUserPrincipal(r *http.Request) string {
	if auth := gatewayauth.FromRequest(r); auth != nil && auth.PrincipalID != "" {
		return auth.PrincipalID
	}
	return ""
}

func defaultTenant(r *http.Request) string {
	if auth := gatewayauth.FromRequest(r); auth != nil && auth.Tenant != "" {
		return auth.Tenant
	}
	return ""
}

func writeChatError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": "request_failed", "code": code, "message": msg, "status": status})
}

func httpStatusForChatError(err error) int {
	if errors.Is(err, errSessionMissing) {
		return http.StatusServiceUnavailable
	}
	if errors.Is(err, errChatAuthRequired) {
		return http.StatusUnauthorized
	}
	if errors.Is(err, errChatSessionNotFound) || errors.Is(err, errChatSessionForbidden) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

func chatErrorCode(err error) string {
	if errors.Is(err, errSessionMissing) {
		return "session_store_unavailable"
	}
	if errors.Is(err, errChatAuthRequired) {
		return "authentication_required"
	}
	if errors.Is(err, errChatSessionNotFound) || errors.Is(err, errChatSessionForbidden) {
		return "not_found"
	}
	return "chat_failed"
}

func errorKindForChatError(err error) string {
	if err == nil {
		return ErrorKindOther
	}
	if errors.Is(err, errChatAuthRequired) || errors.Is(err, errChatSessionNotFound) || errors.Is(err, errChatSessionForbidden) {
		return ErrorKindAuthRejected
	}
	if errors.Is(err, errSessionMissing) {
		return ErrorKindRedisFailed
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "session") || strings.Contains(msg, "redis") || strings.Contains(msg, "persist"):
		return ErrorKindRedisFailed
	default:
		return ErrorKindOther
	}
}

func errorKindForFrameError(code string) string {
	switch code {
	case ErrorCodeProviderFailed:
		return ErrorKindVLLMCallFailed
	default:
		return ErrorKindOther
	}
}

func firstNonEmpty(vals ...string) string {
	for _, val := range vals {
		if strings.TrimSpace(val) != "" {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

var errSessionMissing = errors.New("session store not configured")
