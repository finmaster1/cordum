package shadow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis keyspace for shadow findings. Pinned per PRD:
//
//	edge:shadow:finding:<finding_id>            — JSON record (string)
//	edge:shadow:index:<tenant_id>                — ZSET, score=created_at unix-ms
//	edge:shadow:index:<tenant_id>:status:<v>     — ZSET, secondary filter
//	edge:shadow:index:<tenant_id>:risk:<v>       — ZSET, secondary filter
//	edge:shadow:index:<tenant_id>:agent:<v>      — ZSET, secondary filter
//	edge:shadow:index:<tenant_id>:owner:<v>      — ZSET, secondary filter
//
// All secondary indexes share the same per-tenant scope to prevent
// cross-tenant index contamination. The tenant index is the broadest
// fallback; secondaries are used when the query's narrowest filter
// matches one of (status, risk, agent_product, owner).
const (
	redisKeyFinding     = "edge:shadow:finding:"
	redisKeyIndexTenant = "edge:shadow:index:"
	redisIndexSegStatus = ":status:"
	redisIndexSegRisk   = ":risk:"
	redisIndexSegAgent  = ":agent:"
	redisIndexSegOwner  = ":owner:"
	// overScanFactor controls how many index entries we pull per page when
	// secondary filters require post-fetch JSON validation. 3x balances
	// read amplification vs. round-trips on dense filter combinations.
	overScanFactor = 3
)

// RedisStore is the production Store backed by the shared gateway Redis
// client. It does NOT own the client (NewRedisStore stores the
// caller-owned client by reference) so callers control lifecycle.
type RedisStore struct {
	client            redis.UniversalClient
	now               func() time.Time
	idGen             func() string
	terminalRetention time.Duration
}

// StoreOption customizes RedisStore behavior. Primarily for tests that
// need to pin time, ids, or shorten retention windows.
type StoreOption func(*RedisStore)

// WithClock pins the store clock. Tests use this to make timestamps
// deterministic; production calls NewRedisStore without options.
func WithClock(now func() time.Time) StoreOption {
	return func(s *RedisStore) { s.now = now }
}

// WithIDGen pins the synthetic finding-id generator. Tests use this to
// produce stable ids without dragging crypto/rand into assertions.
func WithIDGen(gen func() string) StoreOption {
	return func(s *RedisStore) { s.idGen = gen }
}

// WithTerminalRetention overrides the TTL applied to resolved/suppressed
// records. Zero disables retention (records persist until manually
// pruned); useful for compliance-tenant configurations where shadow
// dispositions must be retained indefinitely.
func WithTerminalRetention(d time.Duration) StoreOption {
	return func(s *RedisStore) { s.terminalRetention = d }
}

// NewRedisStore wraps the shared redis client. Returns nil when client
// is nil so callers can pass through a missing-store sentinel without
// allocating a non-functional store.
func NewRedisStore(client redis.UniversalClient, opts ...StoreOption) *RedisStore {
	if client == nil {
		return nil
	}
	s := &RedisStore{
		client:            client,
		now:               func() time.Time { return time.Now().UTC() },
		idGen:             defaultIDGen,
		terminalRetention: DefaultTerminalRetention,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	if s.now == nil {
		s.now = func() time.Time { return time.Now().UTC() }
	}
	if s.idGen == nil {
		s.idGen = defaultIDGen
	}
	return s
}

func defaultIDGen() string {
	// 16 bytes of entropy → 32 hex chars; the findingIDPrefix is
	// applied by the normaliser. crypto/rand to avoid collisions across
	// concurrent emit paths.
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("ts%d", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

// findingKey returns the per-record JSON key.
func findingKey(id string) string {
	return redisKeyFinding + id
}

// tenantIndexKey returns the broad tenant-scoped index key.
func tenantIndexKey(tenant string) string {
	return redisKeyIndexTenant + tenant
}

// secondaryIndexKey returns a tenant-scoped index key for the given
// filter dimension (status/risk/agent/owner). seg is one of the
// redisIndexSeg* constants.
func secondaryIndexKey(tenant, seg, value string) string {
	return redisKeyIndexTenant + tenant + seg + value
}

// CreateFinding persists a new finding. Atomicity: the JSON write +
// every index member add happen inside a Redis pipeline so a single
// partial failure leaves no orphaned indexes.
func (s *RedisStore) CreateFinding(ctx context.Context, req CreateFindingRequest) (*ShadowAgentFinding, error) {
	if s == nil || s.client == nil {
		return nil, ErrStoreUnavailable
	}
	finding, err := normalizeAndValidateCreate(req, s.now(), s.idGen)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(finding)
	if err != nil {
		return nil, fmt.Errorf("shadow finding: marshal: %w", err)
	}

	key := findingKey(finding.FindingID)
	// SETNX-style idempotence: if the key already exists and is byte-equal,
	// treat as a successful re-create. Otherwise, ErrAlreadyExists.
	existing, getErr := s.client.Get(ctx, key).Bytes()
	if getErr == nil {
		var prev ShadowAgentFinding
		if err := json.Unmarshal(existing, &prev); err == nil && prev.TenantID == finding.TenantID {
			// Same id, same tenant — idempotent re-create. Return the existing
			// record so callers (and tests) observe the stable state.
			return &prev, nil
		}
		return nil, fmt.Errorf("%w: finding_id %s", ErrAlreadyExists, finding.FindingID)
	}
	if !errors.Is(getErr, redis.Nil) {
		return nil, fmt.Errorf("shadow finding: probe: %w", getErr)
	}

	score := float64(finding.CreatedAt.UnixMilli())
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, key, payload, 0)
	pipe.ZAdd(ctx, tenantIndexKey(finding.TenantID), redis.Z{Score: score, Member: finding.FindingID})
	pipe.ZAdd(ctx, secondaryIndexKey(finding.TenantID, redisIndexSegStatus, string(finding.Status)),
		redis.Z{Score: score, Member: finding.FindingID})
	pipe.ZAdd(ctx, secondaryIndexKey(finding.TenantID, redisIndexSegRisk, string(finding.Risk)),
		redis.Z{Score: score, Member: finding.FindingID})
	pipe.ZAdd(ctx, secondaryIndexKey(finding.TenantID, redisIndexSegAgent, finding.AgentProduct),
		redis.Z{Score: score, Member: finding.FindingID})
	pipe.ZAdd(ctx, secondaryIndexKey(finding.TenantID, redisIndexSegOwner, finding.OwnerPrincipalID),
		redis.Z{Score: score, Member: finding.FindingID})
	if _, err := pipe.Exec(ctx); err != nil {
		// Best-effort rollback on the JSON key. The index entries are
		// keyed by finding_id only, so future list calls will skip them
		// when GETing the missing record (stale index cleanup, below).
		_ = s.client.Del(ctx, key).Err()
		return nil, fmt.Errorf("shadow finding: pipeline: %w", err)
	}
	return finding, nil
}

// GetFinding loads a finding and enforces tenant ownership. Cross-tenant
// access returns ErrNotFound (not a tenant-mismatch error) so callers
// cannot use the get API to probe other tenants' records.
func (s *RedisStore) GetFinding(ctx context.Context, tenantID, findingID string) (*ShadowAgentFinding, error) {
	if s == nil || s.client == nil {
		return nil, ErrStoreUnavailable
	}
	tenantID = strings.TrimSpace(tenantID)
	findingID = strings.TrimSpace(findingID)
	if tenantID == "" || findingID == "" {
		return nil, ErrNotFound
	}
	raw, err := s.client.Get(ctx, findingKey(findingID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("shadow finding: get: %w", err)
	}
	var f ShadowAgentFinding
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("shadow finding: unmarshal: %w", err)
	}
	if f.TenantID != tenantID {
		return nil, ErrNotFound
	}
	if s.isExpiredTerminal(&f) {
		// Hide expired-terminal records; the cleanup pass below removes them.
		go s.purgeExpired(context.Background(), &f)
		return nil, ErrNotFound
	}
	return &f, nil
}

// ListFindings selects the narrowest applicable index, then post-filters
// records that don't match the remaining query dimensions. Pagination
// uses an opaque cursor (encoded "<score>:<finding_id>") so callers
// can resume without leaking Redis internals.
func (s *RedisStore) ListFindings(ctx context.Context, q ListFindingsQuery) (FindingPage, error) {
	if s == nil || s.client == nil {
		return FindingPage{}, ErrStoreUnavailable
	}
	tenant := strings.TrimSpace(q.TenantID)
	if tenant == "" {
		return FindingPage{}, fmt.Errorf("%w: tenant_id is required", ErrValidation)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultListPageSize
	}
	if limit > MaxListPageSize {
		limit = MaxListPageSize
	}

	indexKey, postFilters := chooseIndex(tenant, q)

	startScore, startID, err := decodeCursor(q.Cursor)
	if err != nil {
		return FindingPage{}, err
	}

	pulled, err := s.zScanDescending(ctx, indexKey, startScore, startID, limit*overScanFactor)
	if err != nil {
		return FindingPage{}, fmt.Errorf("shadow finding: list: %w", err)
	}

	findings := make([]ShadowAgentFinding, 0, limit)
	var staleIDs []string
	var lastMember zMember
	for _, m := range pulled {
		if int64(len(findings)) >= int64(limit) {
			break
		}
		raw, err := s.client.Get(ctx, findingKey(m.member)).Bytes()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				// Stale index entry; queue for opportunistic cleanup.
				staleIDs = append(staleIDs, m.member)
				continue
			}
			return FindingPage{}, fmt.Errorf("shadow finding: list-get: %w", err)
		}
		var f ShadowAgentFinding
		if err := json.Unmarshal(raw, &f); err != nil {
			staleIDs = append(staleIDs, m.member)
			continue
		}
		if f.TenantID != tenant {
			// Defence-in-depth: index entries should be tenant-scoped by
			// key construction, but a corrupted index member is treated
			// as stale rather than leaking cross-tenant data.
			staleIDs = append(staleIDs, m.member)
			continue
		}
		if s.isExpiredTerminal(&f) {
			staleIDs = append(staleIDs, m.member)
			continue
		}
		if !matchesPostFilters(&f, postFilters) {
			continue
		}
		findings = append(findings, f)
		lastMember = m
	}

	if len(staleIDs) > 0 {
		// Fire-and-forget cleanup. We use a fresh context so the caller's
		// canceled ctx does not abort cleanup work that survives the page.
		go s.opportunisticCleanup(context.Background(), tenant, staleIDs)
	}

	var nextCursor string
	if int64(len(findings)) >= int64(limit) && lastMember.member != "" {
		nextCursor = encodeCursor(lastMember.score, lastMember.member)
	}

	return FindingPage{Findings: findings, NextCursor: nextCursor}, nil
}

// ResolveFinding applies the resolve lifecycle transition atomically:
// the JSON write + status-index move happen inside a MULTI/EXEC block.
func (s *RedisStore) ResolveFinding(ctx context.Context, tenantID, findingID string, req ResolveRequest) (*ShadowAgentFinding, error) {
	return s.transitionFinding(ctx, tenantID, findingID, func(f *ShadowAgentFinding, now time.Time) error {
		return applyResolve(f, req, now)
	})
}

// SuppressFinding applies the suppress lifecycle transition atomically.
func (s *RedisStore) SuppressFinding(ctx context.Context, tenantID, findingID string, req SuppressRequest) (*ShadowAgentFinding, error) {
	return s.transitionFinding(ctx, tenantID, findingID, func(f *ShadowAgentFinding, now time.Time) error {
		return applySuppress(f, req, now)
	})
}

// transitionFinding centralises the optimistic-locking + index-move
// dance shared by resolve/suppress. The mutate fn is the only
// per-transition difference.
func (s *RedisStore) transitionFinding(
	ctx context.Context,
	tenantID, findingID string,
	mutate func(*ShadowAgentFinding, time.Time) error,
) (*ShadowAgentFinding, error) {
	if s == nil || s.client == nil {
		return nil, ErrStoreUnavailable
	}
	tenantID = strings.TrimSpace(tenantID)
	findingID = strings.TrimSpace(findingID)
	if tenantID == "" || findingID == "" {
		return nil, ErrNotFound
	}
	key := findingKey(findingID)
	raw, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("shadow finding: transition-get: %w", err)
	}
	var f ShadowAgentFinding
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("shadow finding: unmarshal: %w", err)
	}
	if f.TenantID != tenantID {
		return nil, ErrNotFound
	}
	prevStatus := f.Status
	if err := mutate(&f, s.now()); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(&f)
	if err != nil {
		return nil, fmt.Errorf("shadow finding: marshal: %w", err)
	}
	score := float64(f.CreatedAt.UnixMilli())

	pipe := s.client.TxPipeline()
	pipe.Set(ctx, key, payload, 0)
	if prevStatus != f.Status {
		pipe.ZRem(ctx, secondaryIndexKey(tenantID, redisIndexSegStatus, string(prevStatus)), findingID)
		pipe.ZAdd(ctx, secondaryIndexKey(tenantID, redisIndexSegStatus, string(f.Status)),
			redis.Z{Score: score, Member: findingID})
	}
	// Terminal retention: schedule TTL on the JSON key + every index entry.
	if isTerminal(f.Status) && s.terminalRetention > 0 {
		pipe.Expire(ctx, key, s.terminalRetention)
		// Index entries already exist; setting the TTL on the ZSET would
		// expire ALL members. Instead, the list path cleans expired
		// terminals opportunistically (see purgeExpired).
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("shadow finding: transition: %w", err)
	}
	return &f, nil
}

func isTerminal(s FindingStatus) bool {
	return s == FindingStatusResolved || s == FindingStatusSuppressed
}

func (s *RedisStore) isExpiredTerminal(f *ShadowAgentFinding) bool {
	if !isTerminal(f.Status) || s.terminalRetention <= 0 || f.ResolvedAt == nil {
		return false
	}
	return s.now().Sub(*f.ResolvedAt) > s.terminalRetention
}

// purgeExpired removes a single expired terminal record + its index
// members. Best-effort; errors are swallowed because cleanup is
// secondary to user-facing list/get correctness.
func (s *RedisStore) purgeExpired(ctx context.Context, f *ShadowAgentFinding) {
	if s == nil || s.client == nil || f == nil {
		return
	}
	tenant := f.TenantID
	id := f.FindingID
	s.opportunisticCleanup(ctx, tenant, []string{id})
}

func (s *RedisStore) opportunisticCleanup(ctx context.Context, tenant string, ids []string) {
	if s == nil || s.client == nil || len(ids) == 0 {
		return
	}
	pipe := s.client.Pipeline()
	for _, id := range ids {
		pipe.Del(ctx, findingKey(id))
		pipe.ZRem(ctx, tenantIndexKey(tenant), id)
		for _, seg := range []string{redisIndexSegStatus, redisIndexSegRisk, redisIndexSegAgent, redisIndexSegOwner} {
			// We don't know which secondary-index buckets the id lives
			// in without re-deriving the original fields, which we no
			// longer have once the JSON is gone. ZREM is idempotent and
			// O(log N) so blasting every well-known status/risk value
			// per stale id is acceptable for the small set of values.
			for _, v := range knownEnumValuesForSeg(seg) {
				pipe.ZRem(ctx, secondaryIndexKey(tenant, seg, v), id)
			}
		}
	}
	_, _ = pipe.Exec(ctx)
}

// knownEnumValuesForSeg returns the closed-set values we ZREM during
// stale-index cleanup. For agent/owner segments the value-space is
// open — we accept that cleanup leaks a few stale member entries until
// the next ZADD overwrites them on a re-create.
func knownEnumValuesForSeg(seg string) []string {
	switch seg {
	case redisIndexSegStatus:
		return []string{string(FindingStatusDetected), string(FindingStatusResolved), string(FindingStatusSuppressed)}
	case redisIndexSegRisk:
		return []string{string(FindingRiskLow), string(FindingRiskMedium), string(FindingRiskHigh), string(FindingRiskCritical)}
	default:
		return nil
	}
}

// chooseIndex picks the narrowest tenant-scoped index for the query and
// returns the leftover filters that have to be applied post-fetch.
func chooseIndex(tenant string, q ListFindingsQuery) (string, postFilterSet) {
	post := postFilterSet{
		status:       q.Status,
		risk:         q.Risk,
		agentProduct: strings.ToLower(strings.TrimSpace(q.AgentProduct)),
		owner:        strings.TrimSpace(q.OwnerPrincipalID),
	}
	// Pick the narrowest available index in deterministic order so two
	// equally-narrow filter combinations choose the same path.
	switch {
	case post.status != "":
		idx := secondaryIndexKey(tenant, redisIndexSegStatus, string(post.status))
		post.status = ""
		return idx, post
	case post.risk != "":
		idx := secondaryIndexKey(tenant, redisIndexSegRisk, string(post.risk))
		post.risk = ""
		return idx, post
	case post.agentProduct != "":
		idx := secondaryIndexKey(tenant, redisIndexSegAgent, post.agentProduct)
		post.agentProduct = ""
		return idx, post
	case post.owner != "":
		idx := secondaryIndexKey(tenant, redisIndexSegOwner, post.owner)
		post.owner = ""
		return idx, post
	default:
		return tenantIndexKey(tenant), post
	}
}

type postFilterSet struct {
	status       FindingStatus
	risk         FindingRisk
	agentProduct string
	owner        string
}

func matchesPostFilters(f *ShadowAgentFinding, p postFilterSet) bool {
	if p.status != "" && f.Status != p.status {
		return false
	}
	if p.risk != "" && f.Risk != p.risk {
		return false
	}
	if p.agentProduct != "" && f.AgentProduct != p.agentProduct {
		return false
	}
	if p.owner != "" && f.OwnerPrincipalID != p.owner {
		return false
	}
	return true
}

type zMember struct {
	member string
	score  float64
}

// zScanDescending pulls index members in descending score order with
// cursor support. Wraps ZREVRANGEBYSCORE so consumers don't have to
// build the Redis args by hand.
func (s *RedisStore) zScanDescending(
	ctx context.Context,
	key string,
	startScore float64,
	startID string,
	limit int,
) ([]zMember, error) {
	max := "+inf"
	if startScore > 0 {
		// Use exclusive max so the cursor entry itself is skipped — the
		// caller has already seen it.
		max = "(" + formatFloat(startScore)
	}
	if startID != "" && startScore > 0 {
		// startID + startScore together form the cursor: same-score
		// entries are ordered lexicographically by member, so we may
		// need a follow-up scan at exactly startScore for entries
		// after startID. We accept a slight over-scan by pulling
		// (startScore .. -inf] inclusive and post-filtering the
		// startScore tie. Simpler than ZRANGEBYLEX gymnastics; the
		// over-scan is bounded by limit*overScanFactor.
		max = "(" + formatFloat(startScore+1)
	}
	cmd := s.client.ZRevRangeByScoreWithScores(ctx, key, &redis.ZRangeBy{
		Min:    "-inf",
		Max:    max,
		Offset: 0,
		Count:  int64(limit),
	})
	pairs, err := cmd.Result()
	if err != nil {
		return nil, err
	}
	out := make([]zMember, 0, len(pairs))
	skipping := startID != "" && startScore > 0
	for _, p := range pairs {
		member, _ := p.Member.(string)
		if skipping {
			if p.Score == startScore && member == startID {
				skipping = false
				continue
			}
			if p.Score < startScore {
				skipping = false
			}
		}
		out = append(out, zMember{member: member, score: p.Score})
	}
	return out, nil
}

func formatFloat(f float64) string {
	return fmt.Sprintf("%.6f", f)
}

func encodeCursor(score float64, id string) string {
	return fmt.Sprintf("%.6f:%s", score, id)
}

func decodeCursor(c string) (float64, string, error) {
	c = strings.TrimSpace(c)
	if c == "" {
		return 0, "", nil
	}
	idx := strings.IndexByte(c, ':')
	if idx <= 0 || idx == len(c)-1 {
		return 0, "", ErrInvalidCursor
	}
	var score float64
	if _, err := fmt.Sscanf(c[:idx], "%f", &score); err != nil {
		return 0, "", ErrInvalidCursor
	}
	return score, c[idx+1:], nil
}
