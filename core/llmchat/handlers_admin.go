package llmchat

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	gatewayauth "github.com/cordum/cordum/core/controlplane/gateway/auth"
)

// SessionListFilter scopes/paginates admin chat-session listings.
type SessionListFilter struct {
	Tenant     string
	AllTenants bool
	Cursor     string
	Limit      int
}

// SessionSummary is one row in GET /api/v1/chat/sessions.
type SessionSummary struct {
	ID            string    `json:"id"`
	Tenant        string    `json:"tenant"`
	UserPrincipal string    `json:"user_principal"`
	AgentID       string    `json:"agent_id"`
	CreatedAt     time.Time `json:"created_at"`
	LastActiveAt  time.Time `json:"last_active_at"`
}

// SessionListPage is the admin list response wire shape.
type SessionListPage struct {
	Items      []SessionSummary `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// HandleListSessions implements GET /api/v1/chat/sessions.
func (h *ChatHandlers) HandleListSessions(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	requester := defaultUserPrincipal(r)
	logger := requestLogger(r.Context(), r, nil)
	defer func() {
		logger.Info("llmchat: admin_list_sessions", "requester", safeLogCorrelationValue(requester), "latency_ms", time.Since(startedAt).Milliseconds())
	}()
	if !h.requireChatEntitlement(w) {
		return
	}
	if r.Method != http.MethodGet {
		writeChatError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
		return
	}
	if !h.requireChatReadAll(w, r) {
		return
	}
	limit := parseSessionListLimit(r.URL.Query().Get("limit"))
	auth := gatewayauth.FromRequest(r)
	filter := SessionListFilter{Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")), Limit: limit}
	if auth != nil {
		filter.Tenant = auth.Tenant
		filter.AllTenants = auth.AllowCrossTenant
	}
	if h.sessions == nil {
		writeChatError(w, http.StatusServiceUnavailable, "session_store_unavailable", "session store not configured")
		return
	}
	page, err := h.sessions.ListSessions(r.Context(), filter)
	if err != nil {
		writeChatError(w, http.StatusInternalServerError, "session_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// HandleGetSession implements GET /api/v1/chat/sessions/{session_id}. The
// sessionID is passed explicitly so callers can wire it from any router.
func (h *ChatHandlers) HandleGetSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	startedAt := time.Now()
	requester := defaultUserPrincipal(r)
	logger := requestLogger(r.Context(), r, nil)
	defer func() {
		logger.Info("llmchat: admin_get_session",
			"session_id", safeLogCorrelationValue(sessionID),
			"requester", safeLogCorrelationValue(requester),
			"latency_ms", time.Since(startedAt).Milliseconds())
	}()
	if !h.requireChatEntitlement(w) {
		return
	}
	if r.Method != http.MethodGet {
		writeChatError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
		return
	}
	if !h.requireChatReadAll(w, r) {
		return
	}
	if h.sessions == nil {
		writeChatError(w, http.StatusServiceUnavailable, "session_store_unavailable", "session store not configured")
		return
	}
	sess, err := h.sessions.Get(r.Context(), strings.TrimSpace(sessionID))
	if err != nil {
		writeChatError(w, http.StatusInternalServerError, "session_get_failed", err.Error())
		return
	}
	if sess == nil || !adminCanSeeSession(r, sess) {
		writeChatError(w, http.StatusNotFound, "not_found", "chat session not found")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (h *ChatHandlers) requireChatReadAll(w http.ResponseWriter, r *http.Request) bool {
	if h.permissions != nil {
		if err := h.permissions.RequirePermission(r, gatewayauth.PermChatReadAll); err != nil {
			writeChatError(w, http.StatusForbidden, "forbidden", err.Error())
			return false
		}
		return true
	}
	writeChatError(w, http.StatusForbidden, "forbidden", gatewayauth.PermChatReadAll+" permission checker required")
	return false
}

func adminCanSeeSession(r *http.Request, sess *Session) bool {
	auth := gatewayauth.FromRequest(r)
	if auth == nil {
		return false
	}
	if auth.AllowCrossTenant {
		return true
	}
	return strings.TrimSpace(auth.Tenant) != "" && strings.TrimSpace(sess.Tenant) == strings.TrimSpace(auth.Tenant)
}

func parseSessionListLimit(raw string) int {
	limit := 50
	if strings.TrimSpace(raw) != "" {
		if parsed, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 200 {
		return 200
	}
	return limit
}

// ListSessions scans Redis session metadata. It intentionally returns summaries
// only; HandleGetSession loads the full transcript for one selected id.
func (s *SessionStore) ListSessions(ctx context.Context, filter SessionListFilter) (SessionListPage, error) {
	if s == nil || s.client == nil {
		return SessionListPage{}, errSessionMissing
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var cursor uint64
	items := make([]SessionSummary, 0, limit+1)
	for {
		keys, next, err := s.client.Scan(ctx, cursor, sessionKeyPrefix+"*", 100).Result()
		if err != nil {
			return SessionListPage{}, fmt.Errorf("scan chat sessions: %w", err)
		}
		cursor = next
		for _, key := range keys {
			if strings.HasSuffix(key, sessionMsgsSuffix) {
				continue
			}
			id := strings.TrimPrefix(key, sessionKeyPrefix)
			if id == "" || (filter.Cursor != "" && id <= filter.Cursor) {
				continue
			}
			sess, err := s.Get(ctx, id)
			if err != nil || sess == nil {
				continue
			}
			if !filter.AllTenants && filter.Tenant != "" && sess.Tenant != filter.Tenant {
				continue
			}
			items = append(items, SessionSummary{ID: sess.ID, Tenant: sess.Tenant, UserPrincipal: sess.UserPrincipal, AgentID: sess.AgentID, CreatedAt: sess.CreatedAt, LastActiveAt: sess.LastActiveAt})
		}
		if cursor == 0 || len(items) > limit {
			break
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	page := SessionListPage{Items: items}
	if len(items) > limit {
		page.NextCursor = items[limit-1].ID
		page.Items = items[:limit]
	}
	return page, nil
}
