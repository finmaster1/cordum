// EDGE-141 — Shadow agent finding model, store interface, validation.
//
// This file defines the persisted ShadowAgentFinding lifecycle record.
// The scanner-only Finding type in finding.go (status set
// {observed|unreadable|managed_skip|partial}) describes a single
// detection observation; ShadowAgentFinding is the long-lived
// lifecycle record that operators triage and dispose of via the
// /api/v1/edge/shadow-agents/* APIs.
//
// Lifecycle is observe/warn ONLY (epic rail "shadow detection defaults
// to opt-in observe mode only"): findings transition between
// {detected, resolved, suppressed}; no enforcement, no remediation
// execution, no Cordum Job creation. The store is responsible for
// validation, redaction, indexing, and retention TTLs only.
package shadow

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	edgecore "github.com/cordum/cordum/core/edge"
)

// FindingStatus is the lifecycle status of a persisted finding. Distinct
// from the scanner-only Status constants — this enum tracks operator
// disposition (detected → triaged into resolved/suppressed), not the
// detection observation.
type FindingStatus string

const (
	// FindingStatusDetected — finding is fresh, awaiting operator triage.
	FindingStatusDetected FindingStatus = "detected"
	// FindingStatusResolved — operator confirmed the shadow agent was
	// remediated (uninstalled, brought under management, false positive).
	// Terminal: writes never overwrite, retention TTL applies.
	FindingStatusResolved FindingStatus = "resolved"
	// FindingStatusSuppressed — operator deferred / accepted the finding
	// (e.g., approved exception, low-risk service account). Terminal:
	// writes never overwrite, retention TTL applies, may carry a
	// suppressed_until hint for time-bound exceptions.
	FindingStatusSuppressed FindingStatus = "suppressed"
)

// FindingRisk classifies the operator-visible severity of the finding.
// Mirrors the scanner Risk* constants (low/medium/high) but is its own
// type so a future divergence in lifecycle severity vs detection
// severity does not require a breaking rename.
type FindingRisk string

const (
	FindingRiskLow      FindingRisk = "low"
	FindingRiskMedium   FindingRisk = "medium"
	FindingRiskHigh     FindingRisk = "high"
	FindingRiskCritical FindingRisk = "critical"
)

// findingIDPrefix is the stable opaque prefix for synthetic finding ids
// minted when the caller omits one. Stable so downstream SIEM and audit
// consumers can recognise shadow records without parsing the full id.
const findingIDPrefix = "edge_shadow_"

// MaxEvidenceSummaryBytes bounds the JSON-persisted evidence summary
// after redaction. Larger summaries are rejected at validation time so a
// malicious uploader can't bloat Redis with multi-megabyte blobs.
const MaxEvidenceSummaryBytes = 2048

// MaxResolutionReasonBytes bounds the human-readable reason attached to
// resolve/suppress lifecycle changes. Tight cap — operators get a
// sentence, not a forensic narrative.
const MaxResolutionReasonBytes = 512

// MaxMetadataEntries / MaxMetadataValueBytes / MaxMetadataKeyBytes
// bound the small free-form metadata map. Mirrors edge.validateMetadata
// shape but enforced inline so the shadow store stays self-contained.
const (
	MaxMetadataEntries    = 16
	MaxMetadataKeyBytes   = 64
	MaxMetadataValueBytes = 256
)

// MaxListPageSize caps cursor pagination per request. Operators wanting
// more findings must paginate via cursor.
const (
	DefaultListPageSize = 50
	MaxListPageSize     = 200
)

// DefaultTerminalRetention is the TTL applied to resolved/suppressed
// records. Detected records carry no TTL — they remain until the
// operator triages them. Configurable via WithTerminalRetention so
// operator policy can extend or shorten.
const DefaultTerminalRetention = 90 * 24 * time.Hour

// EvidencePointer references redacted evidence stored outside the
// finding record. Distinct from edgecore.ArtifactPointer because shadow
// findings are observed OUTSIDE of an EdgeSession/AgentExecution
// context — the existing ArtifactPointer requires session_id +
// execution_id + event_id, which a scanner finding does not have.
// Retention/redaction enums are reused from edgecore so SIEM/export
// pipelines can treat both pointer shapes uniformly.
type EvidencePointer struct {
	// TenantID MUST match the parent finding's TenantID; the store
	// rejects cross-tenant pointers at validation time so a misconfigured
	// uploader cannot stash evidence in another tenant's blob store.
	TenantID       string                  `json:"tenant_id"`
	URI            string                  `json:"uri"`
	SHA256         string                  `json:"sha256"`
	SizeBytes      int64                   `json:"size_bytes,omitempty"`
	RetentionClass edgecore.RetentionClass `json:"retention_class"`
	RedactionLevel edgecore.RedactionLevel `json:"redaction_level"`
	CreatedAt      time.Time               `json:"created_at"`
}

// ShadowAgentFinding is the lifecycle-tracked operator-visible record.
// Persisted at edge:shadow:finding:<finding_id>. Indexed by tenant +
// status + risk + agent_product + owner so list filters are O(narrowest-
// index) rather than O(tenant-fanout).
type ShadowAgentFinding struct {
	FindingID        string            `json:"finding_id"`
	TenantID         string            `json:"tenant_id"`
	OwnerPrincipalID string            `json:"owner_principal_id"`
	PrincipalID      string            `json:"principal_id"`
	AgentProduct     string            `json:"agent_product"`
	AgentID          string            `json:"agent_id,omitempty"`
	Hostname         string            `json:"hostname,omitempty"`
	Risk             FindingRisk       `json:"risk"`
	Status           FindingStatus     `json:"status"`
	EvidenceType     string            `json:"evidence_type"`
	EvidenceSummary  string            `json:"evidence_summary,omitempty"`
	EvidenceArtifact *EvidencePointer  `json:"evidence_artifact_ptr,omitempty"`
	RedactedPath     string            `json:"redacted_path,omitempty"`
	DetectedAt       time.Time         `json:"detected_at"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	ResolvedAt       *time.Time        `json:"resolved_at,omitempty"`
	ResolvedBy       string            `json:"resolved_by,omitempty"`
	ResolutionReason string            `json:"resolution_reason,omitempty"`
	SuppressedUntil  *time.Time        `json:"suppressed_until,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// CreateFindingRequest is the validated input used to mint a new
// finding. The store generates FindingID/CreatedAt/UpdatedAt/Status when
// the caller omits them; the caller MUST supply tenant, owner,
// principal, agent_product, risk, evidence_type, detected_at, and at
// least one of (evidence_summary, evidence_artifact_ptr).
type CreateFindingRequest struct {
	FindingID        string
	TenantID         string
	OwnerPrincipalID string
	PrincipalID      string
	AgentProduct     string
	AgentID          string
	Hostname         string
	Risk             FindingRisk
	EvidenceType     string
	EvidenceSummary  string
	EvidenceArtifact *EvidencePointer
	RedactedPath     string
	DetectedAt       time.Time
	Metadata         map[string]string
}

// ListFindingsQuery is the bounded filter set accepted by ListFindings.
// Empty fields mean "no filter on this dimension". TenantID is REQUIRED
// — the store refuses cross-tenant scans.
type ListFindingsQuery struct {
	TenantID         string
	Status           FindingStatus
	Risk             FindingRisk
	AgentProduct     string
	OwnerPrincipalID string
	Limit            int
	Cursor           string
}

// FindingPage is the cursor-paginated list response. NextCursor is
// empty when the caller has consumed the index.
type FindingPage struct {
	Findings   []ShadowAgentFinding `json:"findings"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

// ResolveRequest captures the lifecycle-change inputs the resolve API
// accepts. Only Status (implicit resolved), ResolvedBy, and
// ResolutionReason mutate; everything else is preserved verbatim.
type ResolveRequest struct {
	ResolvedBy string
	Reason     string
}

// SuppressRequest is the suppress-side analog of ResolveRequest. The
// optional SuppressedUntil hint records time-bound exceptions; the
// store does not auto-revert when the timestamp lapses (operators may
// re-trigger the scanner instead).
type SuppressRequest struct {
	SuppressedBy    string
	Reason          string
	SuppressedUntil *time.Time
}

// Store is the persistence contract for ShadowAgentFinding lifecycle
// records. All methods MUST tenant-scope reads/writes; cross-tenant
// access returns ErrNotFound rather than a typed cross-tenant error so
// callers cannot use the API surface to probe other tenants' findings.
type Store interface {
	// CreateFinding mints a new finding from a validated request. Idempotent
	// when the caller supplies an explicit FindingID and the existing
	// record is byte-equal; returns ErrAlreadyExists otherwise.
	CreateFinding(ctx context.Context, req CreateFindingRequest) (*ShadowAgentFinding, error)
	// GetFinding loads a finding by id within the given tenant; returns
	// ErrNotFound when the id is missing OR resolves to a different
	// tenant's record (cross-tenant probe defense).
	GetFinding(ctx context.Context, tenantID, findingID string) (*ShadowAgentFinding, error)
	// ListFindings returns a bounded page of findings matching the query.
	// Stale index entries (records expired by terminal-retention TTL) are
	// hidden from the response and opportunistically removed from the
	// secondary indexes.
	ListFindings(ctx context.Context, q ListFindingsQuery) (FindingPage, error)
	// ResolveFinding transitions a detected finding to resolved. Idempotent
	// when the finding is already in the resolved terminal state with the
	// same fields. Returns ErrTerminalConflict when transitioning from
	// suppressed → resolved (terminal states do not re-open in this
	// lifecycle).
	ResolveFinding(ctx context.Context, tenantID, findingID string, req ResolveRequest) (*ShadowAgentFinding, error)
	// SuppressFinding transitions a detected finding to suppressed. Same
	// idempotence/conflict rules as ResolveFinding apply.
	SuppressFinding(ctx context.Context, tenantID, findingID string, req SuppressRequest) (*ShadowAgentFinding, error)
}

// Sentinel errors. Handlers map these to specific HTTP status codes.
var (
	ErrNotFound         = errors.New("shadow finding: not found")
	ErrAlreadyExists    = errors.New("shadow finding: already exists")
	ErrTerminalConflict = errors.New("shadow finding: terminal status conflict")
	ErrInvalidCursor    = errors.New("shadow finding: invalid cursor")
	// ErrValidation is the umbrella for input-validation failures. Use
	// errors.Is to detect; the wrapped message carries the offending field.
	ErrValidation = errors.New("shadow finding: validation")
	// ErrStoreUnavailable is returned when the underlying Redis client
	// is nil or unreachable. Distinct from internal-error so handlers can
	// map to 503 rather than 500.
	ErrStoreUnavailable = errors.New("shadow finding: store unavailable")
)

// validFindingStatus / validFindingRisk gate enum inputs.
var (
	validFindingStatus = map[FindingStatus]struct{}{
		FindingStatusDetected:   {},
		FindingStatusResolved:   {},
		FindingStatusSuppressed: {},
	}
	validFindingRisk = map[FindingRisk]struct{}{
		FindingRiskLow:      {},
		FindingRiskMedium:   {},
		FindingRiskHigh:     {},
		FindingRiskCritical: {},
	}
	// validEvidenceType is intentionally loose — the scanner's
	// EvidenceConfigFile/EvidenceProcessName/EvidenceEnvironmentVar are
	// the well-known producers, but operators ingesting findings from
	// SIEM webhooks may supply their own short identifiers. Anything
	// non-empty and within byte cap passes; unknown values surface in
	// audit/dashboard as-is.
	validEvidenceType = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)
)

// normalizeAndValidateCreate normalizes a CreateFindingRequest into a
// persistable ShadowAgentFinding and rejects invalid inputs. now is
// injected so tests can pin time without monkey-patching.
func normalizeAndValidateCreate(req CreateFindingRequest, now time.Time, idGen func() string) (*ShadowAgentFinding, error) {
	tenantID := strings.TrimSpace(req.TenantID)
	if tenantID == "" {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrValidation)
	}
	owner := strings.TrimSpace(req.OwnerPrincipalID)
	if owner == "" {
		return nil, fmt.Errorf("%w: owner_principal_id is required", ErrValidation)
	}
	principal := strings.TrimSpace(req.PrincipalID)
	if principal == "" {
		return nil, fmt.Errorf("%w: principal_id is required", ErrValidation)
	}
	product := strings.ToLower(strings.TrimSpace(req.AgentProduct))
	if product == "" {
		return nil, fmt.Errorf("%w: agent_product is required", ErrValidation)
	}

	risk := FindingRisk(strings.ToLower(strings.TrimSpace(string(req.Risk))))
	if _, ok := validFindingRisk[risk]; !ok {
		return nil, fmt.Errorf("%w: risk must be one of low|medium|high|critical, got %q", ErrValidation, req.Risk)
	}

	evType := strings.ToLower(strings.TrimSpace(req.EvidenceType))
	if !validEvidenceType.MatchString(evType) {
		return nil, fmt.Errorf("%w: evidence_type must match [a-z0-9_]{1,32}, got %q", ErrValidation, req.EvidenceType)
	}

	summary := strings.TrimSpace(req.EvidenceSummary)
	if summary != "" {
		summary = stripSecretMarkers(summary)
		if len(summary) > MaxEvidenceSummaryBytes {
			return nil, fmt.Errorf("%w: evidence_summary exceeds %d bytes", ErrValidation, MaxEvidenceSummaryBytes)
		}
	}
	if summary == "" && req.EvidenceArtifact == nil {
		return nil, fmt.Errorf("%w: at least one of evidence_summary or evidence_artifact_ptr is required", ErrValidation)
	}

	var pointer *EvidencePointer
	if req.EvidenceArtifact != nil {
		ptr := *req.EvidenceArtifact
		if err := validateEvidencePointer(ptr, tenantID, now); err != nil {
			return nil, err
		}
		pointer = &ptr
	}

	if req.DetectedAt.IsZero() {
		return nil, fmt.Errorf("%w: detected_at is required", ErrValidation)
	}

	if err := validateFindingMetadata(req.Metadata); err != nil {
		return nil, err
	}

	id := strings.TrimSpace(req.FindingID)
	if id == "" {
		id = idGen()
	}
	if !strings.HasPrefix(id, findingIDPrefix) {
		id = findingIDPrefix + id
	}

	finding := &ShadowAgentFinding{
		FindingID:        id,
		TenantID:         tenantID,
		OwnerPrincipalID: owner,
		PrincipalID:      principal,
		AgentProduct:     product,
		AgentID:          strings.TrimSpace(req.AgentID),
		Hostname:         strings.TrimSpace(req.Hostname),
		Risk:             risk,
		Status:           FindingStatusDetected,
		EvidenceType:     evType,
		EvidenceSummary:  summary,
		EvidenceArtifact: pointer,
		RedactedPath:     RedactPath(req.RedactedPath),
		DetectedAt:       req.DetectedAt.UTC(),
		CreatedAt:        now,
		UpdatedAt:        now,
		Metadata:         copyMetadata(req.Metadata),
	}
	return finding, nil
}

func validateEvidencePointer(ptr EvidencePointer, parentTenant string, now time.Time) error {
	if strings.TrimSpace(ptr.TenantID) == "" {
		return fmt.Errorf("%w: evidence_artifact_ptr.tenant_id is required", ErrValidation)
	}
	if ptr.TenantID != parentTenant {
		return fmt.Errorf("%w: evidence_artifact_ptr.tenant_id must match finding tenant_id", ErrValidation)
	}
	if strings.TrimSpace(ptr.URI) == "" {
		return fmt.Errorf("%w: evidence_artifact_ptr.uri is required", ErrValidation)
	}
	if strings.TrimSpace(ptr.SHA256) == "" {
		return fmt.Errorf("%w: evidence_artifact_ptr.sha256 is required", ErrValidation)
	}
	switch ptr.RetentionClass {
	case edgecore.RetentionClassShort, edgecore.RetentionClassStandard, edgecore.RetentionClassAudit:
	default:
		return fmt.Errorf("%w: evidence_artifact_ptr.retention_class invalid: %q", ErrValidation, ptr.RetentionClass)
	}
	switch ptr.RedactionLevel {
	case edgecore.RedactionLevelStandard, edgecore.RedactionLevelStrict:
	default:
		return fmt.Errorf("%w: evidence_artifact_ptr.redaction_level invalid: %q", ErrValidation, ptr.RedactionLevel)
	}
	if ptr.CreatedAt.IsZero() {
		return fmt.Errorf("%w: evidence_artifact_ptr.created_at is required", ErrValidation)
	}
	if ptr.SizeBytes < 0 {
		return fmt.Errorf("%w: evidence_artifact_ptr.size_bytes must be >= 0", ErrValidation)
	}
	if !now.IsZero() && ptr.CreatedAt.After(now.Add(time.Hour)) {
		return fmt.Errorf("%w: evidence_artifact_ptr.created_at is in the future", ErrValidation)
	}
	return nil
}

func validateFindingMetadata(m map[string]string) error {
	if len(m) == 0 {
		return nil
	}
	if len(m) > MaxMetadataEntries {
		return fmt.Errorf("%w: metadata exceeds %d entries", ErrValidation, MaxMetadataEntries)
	}
	for k, v := range m {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("%w: metadata has empty key", ErrValidation)
		}
		if len(k) > MaxMetadataKeyBytes {
			return fmt.Errorf("%w: metadata key %q exceeds %d bytes", ErrValidation, k, MaxMetadataKeyBytes)
		}
		if len(v) > MaxMetadataValueBytes {
			return fmt.Errorf("%w: metadata value for %q exceeds %d bytes", ErrValidation, k, MaxMetadataValueBytes)
		}
	}
	return nil
}

func copyMetadata(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ResolveOrSuppress applies a lifecycle change to an existing finding
// in-place. Pure function — the store wraps it inside a Redis Tx to
// preserve atomicity. exposed for tests that exercise the transition
// logic without going through Redis.
func applyResolve(f *ShadowAgentFinding, req ResolveRequest, now time.Time) error {
	switch f.Status {
	case FindingStatusResolved:
		return nil // idempotent
	case FindingStatusSuppressed:
		return fmt.Errorf("%w: cannot resolve suppressed finding %s", ErrTerminalConflict, f.FindingID)
	case FindingStatusDetected:
		reason := strings.TrimSpace(req.Reason)
		if len(reason) > MaxResolutionReasonBytes {
			return fmt.Errorf("%w: reason exceeds %d bytes", ErrValidation, MaxResolutionReasonBytes)
		}
		f.Status = FindingStatusResolved
		f.ResolvedBy = strings.TrimSpace(req.ResolvedBy)
		f.ResolutionReason = stripSecretMarkers(reason)
		f.ResolvedAt = timePtr(now)
		f.UpdatedAt = now
		return nil
	default:
		return fmt.Errorf("%w: unknown status %q", ErrValidation, f.Status)
	}
}

func applySuppress(f *ShadowAgentFinding, req SuppressRequest, now time.Time) error {
	switch f.Status {
	case FindingStatusSuppressed:
		return nil // idempotent
	case FindingStatusResolved:
		return fmt.Errorf("%w: cannot suppress resolved finding %s", ErrTerminalConflict, f.FindingID)
	case FindingStatusDetected:
		reason := strings.TrimSpace(req.Reason)
		if len(reason) > MaxResolutionReasonBytes {
			return fmt.Errorf("%w: reason exceeds %d bytes", ErrValidation, MaxResolutionReasonBytes)
		}
		f.Status = FindingStatusSuppressed
		f.ResolvedBy = strings.TrimSpace(req.SuppressedBy)
		f.ResolutionReason = stripSecretMarkers(reason)
		f.ResolvedAt = timePtr(now)
		if req.SuppressedUntil != nil && !req.SuppressedUntil.IsZero() {
			until := req.SuppressedUntil.UTC()
			f.SuppressedUntil = &until
		}
		f.UpdatedAt = now
		return nil
	default:
		return fmt.Errorf("%w: unknown status %q", ErrValidation, f.Status)
	}
}

func timePtr(t time.Time) *time.Time {
	tt := t.UTC()
	return &tt
}
