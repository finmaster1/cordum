// EDGE-141 — Shadow agent finding lifecycle handlers.
//
// HTTP surface mounted under /api/v1/edge/shadow-agents/:
//
//   - POST   /api/v1/edge/shadow-agents               — create (ingest one finding)
//   - GET    /api/v1/edge/shadow-agents               — list (cursor-paginated)
//   - GET    /api/v1/edge/shadow-agents/{finding_id}  — get
//   - POST   /api/v1/edge/shadow-agents/{finding_id}/resolve   — resolve
//   - POST   /api/v1/edge/shadow-agents/{finding_id}/suppress  — suppress
//   - POST   /api/v1/edge/shadow-agents/{finding_id}/ignore    — alias for suppress (compat)
//
// All routes share auth + tenant resolution via existing Edge helpers:
// requireEdgePermissionOrRole + edgeTenantFromRequest +
// resolveEdgeAuthPrincipal. Errors flow through writeEdgeError so the
// {code,message,request_id,details} envelope is consistent with the
// rest of /api/v1/edge.
//
// Observe/warn ONLY (EDGE-141 task rail): handlers do not enqueue
// remediation jobs, do not flip enforcement, and do not call into the
// Safety Kernel. They persist + audit the lifecycle disposition; the
// dashboard surfaces remain deferred.
package gateway

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cordum/cordum/core/audit"
	"github.com/cordum/cordum/core/controlplane/gateway/auth"
	"github.com/cordum/cordum/core/edge/shadow"
)

type shadowAgentCreateRequest struct {
	FindingID        string                  `json:"finding_id,omitempty"`
	TenantID         string                  `json:"tenant_id,omitempty"`
	OwnerPrincipalID string                  `json:"owner_principal_id"`
	PrincipalID      string                  `json:"principal_id,omitempty"`
	AgentProduct     string                  `json:"agent_product"`
	AgentID          string                  `json:"agent_id,omitempty"`
	Hostname         string                  `json:"hostname,omitempty"`
	Risk             shadow.FindingRisk      `json:"risk"`
	EvidenceType     string                  `json:"evidence_type"`
	EvidenceSummary  string                  `json:"evidence_summary,omitempty"`
	EvidenceArtifact *shadow.EvidencePointer `json:"evidence_artifact_ptr,omitempty"`
	RedactedPath     string                  `json:"redacted_path,omitempty"`
	DetectedAt       time.Time               `json:"detected_at"`
	Metadata         map[string]string       `json:"metadata,omitempty"`
}

type shadowAgentResolveRequest struct {
	Reason string `json:"reason,omitempty"`
}

type shadowAgentSuppressRequest struct {
	Reason          string     `json:"reason,omitempty"`
	SuppressedUntil *time.Time `json:"suppressed_until,omitempty"`
}

// handleCreateShadowAgentFinding ingests one shadow finding.
// Returns 201 + the persisted record on success.
func (s *server) handleCreateShadowAgentFinding(w http.ResponseWriter, r *http.Request) {
	if !s.requireEdgePermissionOrRole(w, r, auth.PermAuditExport, "admin") {
		return
	}
	store := s.shadowFindingStoreOrUnavailable(w, r)
	if store == nil {
		return
	}
	var body shadowAgentCreateRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		writeEdgeJSONDecodeError(w, r, err, "invalid shadow finding request")
		return
	}
	tenantID, ok := s.edgeTenantFromRequest(w, r, body.TenantID)
	if !ok {
		return
	}
	principalID, err := s.resolveEdgeAuthPrincipal(r)
	if err != nil {
		writeEdgeForbidden(w, r, err)
		return
	}
	// Caller-supplied principal_id may identify the detector (e.g. the
	// scanner service principal). If omitted, fall back to the auth
	// principal so the audit row always has identity.
	detector := strings.TrimSpace(body.PrincipalID)
	if detector == "" {
		detector = principalID
	}

	created, err := store.CreateFinding(r.Context(), shadow.CreateFindingRequest{
		FindingID:        body.FindingID,
		TenantID:         tenantID,
		OwnerPrincipalID: body.OwnerPrincipalID,
		PrincipalID:      detector,
		AgentProduct:     body.AgentProduct,
		AgentID:          body.AgentID,
		Hostname:         body.Hostname,
		Risk:             body.Risk,
		EvidenceType:     body.EvidenceType,
		EvidenceSummary:  body.EvidenceSummary,
		EvidenceArtifact: body.EvidenceArtifact,
		RedactedPath:     body.RedactedPath,
		DetectedAt:       body.DetectedAt,
		Metadata:         body.Metadata,
	})
	if err != nil {
		writeShadowFindingStoreError(w, r, err, "create shadow finding")
		return
	}
	s.emitShadowFindingAudit(r, audit.EventShadowAgentDetected, principalID, created, "")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, created)
}

// handleListShadowAgentFindings returns a bounded page of findings for
// the requested tenant. Query params: risk, status, agent (alias for
// agent_product), owner, limit, cursor.
func (s *server) handleListShadowAgentFindings(w http.ResponseWriter, r *http.Request) {
	if !s.requireEdgePermissionOrRole(w, r, auth.PermAuditRead, "admin") {
		return
	}
	store := s.shadowFindingStoreOrUnavailable(w, r)
	if store == nil {
		return
	}
	tenantID, ok := s.edgeTenantFromRequest(w, r, "")
	if !ok {
		return
	}
	q, ok := parseShadowFindingListQuery(w, r, tenantID)
	if !ok {
		return
	}
	page, err := store.ListFindings(r.Context(), q)
	if err != nil {
		writeShadowFindingStoreError(w, r, err, "list shadow findings")
		return
	}
	writeJSON(w, page)
}

// handleGetShadowAgentFinding returns a single finding scoped to the
// caller's tenant. Cross-tenant lookup returns 404 (never 403) to
// avoid leaking tuple existence.
func (s *server) handleGetShadowAgentFinding(w http.ResponseWriter, r *http.Request) {
	if !s.requireEdgePermissionOrRole(w, r, auth.PermAuditRead, "admin") {
		return
	}
	store := s.shadowFindingStoreOrUnavailable(w, r)
	if store == nil {
		return
	}
	tenantID, ok := s.edgeTenantFromRequest(w, r, "")
	if !ok {
		return
	}
	findingID, ok := requireEdgePathParam(w, r, "finding_id")
	if !ok {
		return
	}
	f, err := store.GetFinding(r.Context(), tenantID, findingID)
	if err != nil {
		writeShadowFindingStoreError(w, r, err, "get shadow finding")
		return
	}
	writeJSON(w, f)
}

// handleResolveShadowAgentFinding flips a detected finding to resolved.
func (s *server) handleResolveShadowAgentFinding(w http.ResponseWriter, r *http.Request) {
	if !s.requireEdgePermissionOrRole(w, r, auth.PermAuditExport, "admin") {
		return
	}
	store := s.shadowFindingStoreOrUnavailable(w, r)
	if store == nil {
		return
	}
	tenantID, ok := s.edgeTenantFromRequest(w, r, "")
	if !ok {
		return
	}
	findingID, ok := requireEdgePathParam(w, r, "finding_id")
	if !ok {
		return
	}
	principalID, err := s.resolveEdgeAuthPrincipal(r)
	if err != nil {
		writeEdgeForbidden(w, r, err)
		return
	}
	var body shadowAgentResolveRequest
	if r.ContentLength > 0 {
		if err := decodeJSONBody(w, r, &body); err != nil {
			writeEdgeJSONDecodeError(w, r, err, "invalid shadow finding resolve request")
			return
		}
	}
	updated, err := store.ResolveFinding(r.Context(), tenantID, findingID, shadow.ResolveRequest{
		ResolvedBy: principalID,
		Reason:     body.Reason,
	})
	if err != nil {
		writeShadowFindingStoreError(w, r, err, "resolve shadow finding")
		return
	}
	s.emitShadowFindingAudit(r, audit.EventShadowAgentResolved, principalID, updated, body.Reason)
	writeJSON(w, updated)
}

// handleSuppressShadowAgentFinding flips a detected finding to
// suppressed. Mounted at both /suppress (canonical) and /ignore
// (PRD-compat alias).
func (s *server) handleSuppressShadowAgentFinding(w http.ResponseWriter, r *http.Request) {
	if !s.requireEdgePermissionOrRole(w, r, auth.PermAuditExport, "admin") {
		return
	}
	store := s.shadowFindingStoreOrUnavailable(w, r)
	if store == nil {
		return
	}
	tenantID, ok := s.edgeTenantFromRequest(w, r, "")
	if !ok {
		return
	}
	findingID, ok := requireEdgePathParam(w, r, "finding_id")
	if !ok {
		return
	}
	principalID, err := s.resolveEdgeAuthPrincipal(r)
	if err != nil {
		writeEdgeForbidden(w, r, err)
		return
	}
	var body shadowAgentSuppressRequest
	if r.ContentLength > 0 {
		if err := decodeJSONBody(w, r, &body); err != nil {
			writeEdgeJSONDecodeError(w, r, err, "invalid shadow finding suppress request")
			return
		}
	}
	updated, err := store.SuppressFinding(r.Context(), tenantID, findingID, shadow.SuppressRequest{
		SuppressedBy:    principalID,
		Reason:          body.Reason,
		SuppressedUntil: body.SuppressedUntil,
	})
	if err != nil {
		writeShadowFindingStoreError(w, r, err, "suppress shadow finding")
		return
	}
	s.emitShadowFindingAudit(r, audit.EventShadowAgentSuppressed, principalID, updated, body.Reason)
	writeJSON(w, updated)
}

// shadowFindingStoreOrUnavailable returns the configured store or
// writes a 503 envelope and returns nil. Mirrors edgeStoreOrUnavailable.
func (s *server) shadowFindingStoreOrUnavailable(w http.ResponseWriter, r *http.Request) shadow.Store {
	if s == nil || s.shadowFindingStore == nil {
		writeEdgeError(w, r, http.StatusServiceUnavailable, edgeErrCodeStoreUnavailable, "shadow finding store unavailable", nil)
		return nil
	}
	return s.shadowFindingStore
}

// parseShadowFindingListQuery extracts validated list filters from the
// URL. Returns ok=false after writing the appropriate 400 envelope.
func parseShadowFindingListQuery(w http.ResponseWriter, r *http.Request, tenantID string) (shadow.ListFindingsQuery, bool) {
	q := r.URL.Query()
	out := shadow.ListFindingsQuery{
		TenantID:         tenantID,
		Status:           shadow.FindingStatus(strings.ToLower(strings.TrimSpace(q.Get("status")))),
		Risk:             shadow.FindingRisk(strings.ToLower(strings.TrimSpace(q.Get("risk")))),
		AgentProduct:     strings.ToLower(strings.TrimSpace(firstNonEmpty(q.Get("agent_product"), q.Get("agent")))),
		OwnerPrincipalID: strings.TrimSpace(q.Get("owner")),
		Cursor:           strings.TrimSpace(q.Get("cursor")),
	}
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		n := edgeQueryLimit(r)
		if n <= 0 {
			writeEdgeError(w, r, http.StatusBadRequest, edgeErrCodeInvalidRequest, "limit must be a positive integer", nil)
			return shadow.ListFindingsQuery{}, false
		}
		out.Limit = n
	}
	if out.Status != "" && out.Status != shadow.FindingStatusDetected && out.Status != shadow.FindingStatusResolved && out.Status != shadow.FindingStatusSuppressed {
		writeEdgeError(w, r, http.StatusBadRequest, edgeErrCodeInvalidRequest, "status must be one of detected|resolved|suppressed", nil)
		return shadow.ListFindingsQuery{}, false
	}
	if out.Risk != "" && out.Risk != shadow.FindingRiskLow && out.Risk != shadow.FindingRiskMedium && out.Risk != shadow.FindingRiskHigh && out.Risk != shadow.FindingRiskCritical {
		writeEdgeError(w, r, http.StatusBadRequest, edgeErrCodeInvalidRequest, "risk must be one of low|medium|high|critical", nil)
		return shadow.ListFindingsQuery{}, false
	}
	return out, true
}

// writeShadowFindingStoreError maps store-layer sentinel errors to HTTP
// envelopes. Anything unrecognised falls through to a sanitised 500 so
// raw Redis details never leak.
func writeShadowFindingStoreError(w http.ResponseWriter, r *http.Request, err error, operation string) {
	switch {
	case errors.Is(err, shadow.ErrNotFound):
		writeEdgeError(w, r, http.StatusNotFound, edgeErrCodeNotFound, "shadow finding not found", nil)
	case errors.Is(err, shadow.ErrAlreadyExists):
		writeEdgeError(w, r, http.StatusConflict, edgeErrCodeConflict, "shadow finding already exists", nil)
	case errors.Is(err, shadow.ErrTerminalConflict):
		writeEdgeError(w, r, http.StatusConflict, edgeErrCodeConflict, err.Error(), nil)
	case errors.Is(err, shadow.ErrValidation):
		writeEdgeError(w, r, http.StatusBadRequest, edgeErrCodeInvalidRequest, err.Error(), nil)
	case errors.Is(err, shadow.ErrInvalidCursor):
		writeEdgeError(w, r, http.StatusBadRequest, edgeErrCodeInvalidRequest, "invalid cursor", nil)
	case errors.Is(err, shadow.ErrStoreUnavailable):
		writeEdgeError(w, r, http.StatusServiceUnavailable, edgeErrCodeStoreUnavailable, "shadow finding store unavailable", nil)
	default:
		writeEdgeInternalError(w, r, operation, err)
	}
}

// emitShadowFindingAudit emits the lifecycle audit event through the
// existing gateway exporter. Payloads carry already-redacted summary +
// artifact pointer metadata; raw evidence summaries are NEVER included.
func (s *server) emitShadowFindingAudit(r *http.Request, eventType, actor string, f *shadow.ShadowAgentFinding, reason string) {
	if s == nil || s.auditExporter == nil || f == nil {
		return
	}
	severity := audit.SeverityInfo
	switch f.Risk {
	case shadow.FindingRiskHigh, shadow.FindingRiskCritical:
		severity = audit.SeverityHigh
	case shadow.FindingRiskMedium:
		severity = audit.SeverityMedium
	}
	extra := map[string]string{
		"finding_id":    f.FindingID,
		"agent_product": f.AgentProduct,
		"risk":          string(f.Risk),
		"status":        string(f.Status),
		"evidence_type": f.EvidenceType,
	}
	if f.AgentID != "" {
		extra["agent_id"] = f.AgentID
	}
	if f.Hostname != "" {
		extra["hostname"] = f.Hostname
	}
	if f.RedactedPath != "" {
		extra["redacted_path"] = f.RedactedPath
	}
	if f.OwnerPrincipalID != "" {
		extra["owner_principal_id"] = f.OwnerPrincipalID
	}
	if f.EvidenceArtifact != nil {
		extra["evidence_artifact_uri"] = f.EvidenceArtifact.URI
		extra["evidence_artifact_sha256"] = f.EvidenceArtifact.SHA256
	}
	if reason != "" {
		// Cap reason at audit-payload size to avoid bloating SIEM rows.
		const auditReasonMaxBytes = 256
		clipped := reason
		if len(clipped) > auditReasonMaxBytes {
			clipped = clipped[:auditReasonMaxBytes]
		}
		extra["reason"] = clipped
	}
	s.auditExporter.Send(audit.SIEMEvent{
		Timestamp: time.Now().UTC(),
		EventType: eventType,
		Severity:  severity,
		TenantID:  f.TenantID,
		Action:    eventType,
		Identity:  actor,
		Extra:     extra,
	})
}
