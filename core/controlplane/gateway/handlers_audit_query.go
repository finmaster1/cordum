package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cordum/cordum/core/audit"
	"github.com/cordum/cordum/core/controlplane/gateway/auth"
)

// auditQueryDefaultLimit / auditQueryMaxLimit cap how many events a
// single GET /api/v1/audit/query response carries. The MCP audit_query
// tool's default page_size is 50 (core/mcp/tools.go pagination), so
// match that here. The hard ceiling matches existing list endpoints.
const (
	auditQueryDefaultLimit = 50
	auditQueryMaxLimit     = 500
)

// auditQueryItem is the wire representation of one stream entry.
// We expose Stream ID alongside the SIEMEvent so the cursor passed back
// to the client is opaque-but-usable: a follow-up call with
// `?cursor=<last_stream_id>` continues exactly after the last entry
// without requiring the client to know about Redis Stream IDs.
type auditQueryItem struct {
	StreamID string          `json:"stream_id"`
	Event    audit.SIEMEvent `json:"event"`
}

type auditQueryResponse struct {
	Items      []auditQueryItem `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
	Total      int              `json:"total"`
}

// handleAuditQuery implements GET /api/v1/audit/query.
//
// Query parameters:
//
//	tenant   (optional) — tenant to query, must match caller's scope;
//	                      defaults to caller's tenant when omitted
//	type     (optional) — exact-match filter on SIEMEvent.EventType.
//	                      For compatibility with the original QA fix,
//	                      SIEMEvent.Action is also accepted as a fallback.
//	since    (optional) — RFC3339 timestamp or unix ms, inclusive lower bound
//	until    (optional) — RFC3339 timestamp or unix ms, inclusive upper bound
//	limit    (optional) — page size, default 50, capped at 500
//	cursor   (optional) — opaque continuation token from a prior
//	                      response's next_cursor field
//
// Response: { items: [{stream_id, event}], next_cursor?, total }.
//
// The MCP audit_query tool calls this endpoint via
// core/mcp/bridge_readonly.go:233. Without the gateway side wired up
// every audit_query invocation 404'd at runtime, leaving chat copilots
// unable to surface "which agents touched job X today" or "show me
// yesterday's policy denials" — the kind of question the entire
// auditability story rests on. See task-5b755f42 for the QA evidence
// that surfaced the gap.
func (s *server) handleAuditQuery(w http.ResponseWriter, r *http.Request) {
	client := s.redisClient()
	if !s.requireStoreAndPermissionOrRole(w, r, auth.PermAuditVerify, []string{"admin"}, client) {
		return
	}

	tenant, err := s.resolveTenant(r, strings.TrimSpace(r.URL.Query().Get("tenant")))
	if err != nil {
		writeErrorJSON(w, http.StatusForbidden, "tenant access denied")
		return
	}
	if err := s.requireTenantAccess(r, tenant); err != nil {
		writeErrorJSON(w, http.StatusForbidden, "tenant access denied")
		return
	}

	q := r.URL.Query()

	// Pagination + bounding. cursor is preferred when present; it
	// carries the last stream ID we returned, so the next page begins
	// strictly after it. since/until are independent absolute bounds.
	since := strings.TrimSpace(q.Get("since"))
	if since == "" {
		since = "-"
	} else {
		parsed, err := parseAuditQueryBound(since)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, fmt.Sprintf("since must be a non-negative unix millisecond or RFC3339 timestamp: %v", err))
			return
		}
		since = parsed
	}
	until := strings.TrimSpace(q.Get("until"))
	if until == "" {
		until = "+"
	} else {
		parsed, err := parseAuditQueryBound(until)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, fmt.Sprintf("until must be a non-negative unix millisecond or RFC3339 timestamp: %v", err))
			return
		}
		until = parsed
	}
	cursor := strings.TrimSpace(q.Get("cursor"))
	minID := since
	if cursor != "" {
		// Redis XRANGE supports the `(<id>` exclusive-start form.
		minID = "(" + cursor
	}

	limit := auditQueryDefaultLimit
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			writeErrorJSON(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		if v > auditQueryMaxLimit {
			v = auditQueryMaxLimit
		}
		limit = v
	}

	eventType := strings.TrimSpace(q.Get("type"))

	chainer := audit.NewChainer(client, "")
	streamKey := chainer.StreamKey(tenant)

	// Fetch one extra entry so we can tell whether more pages exist
	// without a separate count query. The extra is dropped before
	// encoding the response.
	entries, err := client.XRangeN(r.Context(), streamKey, minID, until, int64(limit+1)).Result()
	if err != nil {
		writeInternalError(w, r, "audit query: read chain", err)
		return
	}

	resp := auditQueryResponse{Items: []auditQueryItem{}}
	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}

	for _, entry := range entries {
		payload, ok := entry.Values["event"].(string)
		if !ok {
			// Skip malformed entries; integrity check belongs to the
			// /verify endpoint, not /query.
			continue
		}
		var ev audit.SIEMEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			continue
		}
		if eventType != "" && ev.EventType != eventType && ev.Action != eventType {
			continue
		}
		resp.Items = append(resp.Items, auditQueryItem{StreamID: entry.ID, Event: ev})
	}
	resp.Total = len(resp.Items)
	if hasMore && len(entries) > 0 {
		resp.NextCursor = entries[len(entries)-1].ID
	}

	writeJSON(w, resp)
}

func parseAuditQueryBound(raw string) (string, error) {
	if ms, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if ms < 0 {
			return "", fmt.Errorf("negative unix millisecond")
		}
		return strconv.FormatInt(ms, 10), nil
	}

	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return "", err
	}
	ms := ts.UTC().UnixMilli()
	if ms < 0 {
		return "", fmt.Errorf("timestamp is before unix epoch")
	}
	return strconv.FormatInt(ms, 10), nil
}
