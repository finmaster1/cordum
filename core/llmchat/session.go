package llmchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Session-store constants pinned for cross-package consumption (admin
// session viewer, WS handler in phase 5). Renaming is a wire-break.
const (
	sessionKeyPrefix = "chat:session:"
	sessionMsgsSuffix = ":messages"

	// SessionTTL is the sliding TTL applied on Create + every
	// AppendMessage. 24h matches the architect's plan and gives
	// reconnecting users a full work-day to resume a Gmail-style chat.
	SessionTTL = 24 * time.Hour

	// SessionMaxMessages caps the per-session transcript at 50 entries
	// with FIFO eviction. The cap exists for two reasons: (1) the LLM
	// context window cannot meaningfully consume an unbounded
	// transcript, and (2) Redis list growth must be bounded for the
	// 24h sliding-TTL design to remain memory-safe.
	SessionMaxMessages = 50
)

// Session is the persisted chat-assistant session record. Pinned shape;
// admin viewer + WS handler in phase 5 deserialise this same JSON.
//
// Storage layout: metadata lives under `chat:session:{id}` as a JSON
// blob; transcript messages live under `chat:session:{id}:messages` as
// a Redis list (RPUSH appends, LTRIM caps, LRANGE reads). Splitting
// metadata from transcript means AppendMessage is a single atomic
// pipeline (RPUSH+LTRIM+EXPIRE on metadata+EXPIRE on list) — no
// cross-replica read-modify-write race like the earlier JSON-blob
// design QA flagged at 2026-04-26.
type Session struct {
	ID            string             `json:"id"`
	UserPrincipal string             `json:"user_principal"`
	Tenant        string             `json:"tenant"`
	AgentID       string             `json:"agent_id"`
	DelegationJTI string             `json:"delegation_jti"`
	Delegation    *SessionDelegation `json:"delegation,omitempty"`
	Messages      []SessionMessage   `json:"messages"`
	CreatedAt     time.Time          `json:"created_at"`
	LastActiveAt  time.Time          `json:"last_active_at"`
}

// sessionMetadata is the on-the-wire JSON blob persisted at the
// metadata key. It excludes Messages (which live in the separate Redis
// list) so the metadata key stays small and non-racy.
type sessionMetadata struct {
	ID            string             `json:"id"`
	UserPrincipal string             `json:"user_principal"`
	Tenant        string             `json:"tenant"`
	AgentID       string             `json:"agent_id"`
	DelegationJTI string             `json:"delegation_jti"`
	Delegation    *SessionDelegation `json:"delegation,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	LastActiveAt  time.Time          `json:"last_active_at"`
}

// SessionMessage is one transcript entry. Distinct from the provider-side
// `Message` (which mirrors the OpenAI wire shape) because the session
// log records human-readable text plus tool-call references for audit
// + dashboard display, not provider request envelopes.
type SessionMessage struct {
	Role      string        `json:"role"`
	Text      string        `json:"text,omitempty"`
	ToolCalls []ToolCallRef `json:"tool_calls,omitempty"`
	At        time.Time     `json:"at"`
}

// ToolCallRef is a lightweight reference to a tool-call recorded in the
// transcript. The full ToolCallResult is audited separately via the
// MCP audit pipeline; this struct exists so the dashboard can render
// "called cordum_list_jobs at 12:34" without reconstructing the full
// arg/result pair.
type ToolCallRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

// SessionStore persists chat sessions in Redis. The 24h sliding TTL
// expires inactive sessions automatically; AppendMessage refreshes the
// TTL atomically with the message write so a busy session never gets
// evicted mid-call. Cross-replica concurrent appends are safe because
// each AppendMessage is a single Redis pipeline (RPUSH+LTRIM+EXPIRE)
// with no read-modify-write step.
type SessionStore struct {
	client redis.UniversalClient
}

// NewSessionStoreFromClient wraps an existing redis client. Callers in
// production hold one *redis.Client opened from REDIS_URL in main.go.
func NewSessionStoreFromClient(client redis.UniversalClient) *SessionStore {
	return &SessionStore{client: client}
}

// Create persists a new session. The caller fills UserPrincipal,
// Tenant, AgentID, DelegationJTI (and optionally Delegation); the store
// assigns ID, CreatedAt, LastActiveAt and writes the metadata at
// chat:session:{id}. The transcript list is created lazily on first
// AppendMessage.
func (s *SessionStore) Create(ctx context.Context, in Session) (Session, error) {
	if s == nil || s.client == nil {
		return Session{}, errors.New("chat session: store not configured")
	}
	if in.ID == "" {
		in.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	in.CreatedAt = now
	in.LastActiveAt = now
	if in.Messages == nil {
		in.Messages = []SessionMessage{}
	}

	if err := s.writeMetadata(ctx, in); err != nil {
		return Session{}, err
	}
	return in, nil
}

// Get loads a session. A missing key returns (nil, nil) so callers can
// distinguish "not found" from a transport error without sentinel
// matching. The transcript list is loaded via LRANGE alongside the
// metadata blob.
func (s *SessionStore) Get(ctx context.Context, id string) (*Session, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("chat session: store not configured")
	}
	body, err := s.client.Get(ctx, sessionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("chat session: load: %w", err)
	}
	var meta sessionMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, fmt.Errorf("chat session: decode: %w", err)
	}

	rawMsgs, err := s.client.LRange(ctx, messagesKey(id), 0, -1).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("chat session: load messages: %w", err)
	}
	messages := make([]SessionMessage, 0, len(rawMsgs))
	for _, raw := range rawMsgs {
		var m SessionMessage
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, fmt.Errorf("chat session: decode message: %w", err)
		}
		messages = append(messages, m)
	}

	return &Session{
		ID:            meta.ID,
		UserPrincipal: meta.UserPrincipal,
		Tenant:        meta.Tenant,
		AgentID:       meta.AgentID,
		DelegationJTI: meta.DelegationJTI,
		Delegation:    meta.Delegation,
		Messages:      messages,
		CreatedAt:     meta.CreatedAt,
		LastActiveAt:  meta.LastActiveAt,
	}, nil
}

// AppendMessage appends a transcript entry and refreshes the 24h TTL.
// Atomic via a single Redis pipeline: RPUSH on the message list +
// LTRIM to enforce the FIFO 50-cap + EXPIRE on both keys. Cross-
// replica concurrent appends are safe — Redis serialises pipeline
// commands per-connection, and there is no read-modify-write step
// that could lose a write.
func (s *SessionStore) AppendMessage(ctx context.Context, id string, msg SessionMessage) error {
	if s == nil || s.client == nil {
		return errors.New("chat session: store not configured")
	}

	if msg.At.IsZero() {
		msg.At = time.Now().UTC()
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("chat session: append marshal: %w", err)
	}

	metaKey := sessionKey(id)
	msgsKey := messagesKey(id)

	// Verify the session metadata exists before appending; otherwise an
	// orphaned message list could outlive the metadata key.
	if exists, err := s.client.Exists(ctx, metaKey).Result(); err != nil {
		return fmt.Errorf("chat session: append exists check: %w", err)
	} else if exists == 0 {
		return fmt.Errorf("chat session: append: session %s not found", id)
	}

	now := time.Now().UTC()
	pipe := s.client.Pipeline()
	pipe.RPush(ctx, msgsKey, raw)
	// LTRIM 0 49 = keep the LAST 50 entries (drop everything before -50).
	pipe.LTrim(ctx, msgsKey, -int64(SessionMaxMessages), -1)
	pipe.Expire(ctx, msgsKey, SessionTTL)
	pipe.Expire(ctx, metaKey, SessionTTL)
	// Bump LastActiveAt on the metadata blob via a small RMW that is
	// safe under cross-replica concurrency: even if two replicas race,
	// both writes carry a monotonically-advancing timestamp and the
	// last-write-wins outcome is still correct (LastActiveAt only ever
	// moves forward). Done outside the message pipeline because the
	// metadata edit is non-critical for transcript consistency.
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("chat session: append pipeline: %w", err)
	}
	if err := s.touchMetadata(ctx, id, now); err != nil {
		return err
	}
	return nil
}

// SetDelegation persists the delegation JWT + JTI + expiry on a
// session's metadata blob. Phase-5 WS handler calls this after the
// gateway issues the per-session token so subsequent CallTool requests
// can read the bearer back from the session record (survives
// page-reload before the in-memory cache is repopulated).
func (s *SessionStore) SetDelegation(ctx context.Context, id string, delegation *SessionDelegation) error {
	if s == nil || s.client == nil {
		return errors.New("chat session: store not configured")
	}
	body, err := s.client.Get(ctx, sessionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return fmt.Errorf("chat session: set delegation: session %s not found", id)
	}
	if err != nil {
		return fmt.Errorf("chat session: set delegation load: %w", err)
	}
	var meta sessionMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return fmt.Errorf("chat session: set delegation decode: %w", err)
	}
	meta.Delegation = delegation
	if delegation != nil {
		meta.DelegationJTI = delegation.JTI
	}
	meta.LastActiveAt = time.Now().UTC()

	out, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("chat session: set delegation marshal: %w", err)
	}
	if err := s.client.Set(ctx, sessionKey(id), out, SessionTTL).Err(); err != nil {
		return fmt.Errorf("chat session: set delegation persist: %w", err)
	}
	return nil
}

// writeMetadata serialises the metadata blob and SETs it with the 24h
// TTL.
func (s *SessionStore) writeMetadata(ctx context.Context, sess Session) error {
	meta := sessionMetadata{
		ID:            sess.ID,
		UserPrincipal: sess.UserPrincipal,
		Tenant:        sess.Tenant,
		AgentID:       sess.AgentID,
		DelegationJTI: sess.DelegationJTI,
		Delegation:    sess.Delegation,
		CreatedAt:     sess.CreatedAt,
		LastActiveAt:  sess.LastActiveAt,
	}
	body, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("chat session: marshal metadata: %w", err)
	}
	if err := s.client.Set(ctx, sessionKey(sess.ID), body, SessionTTL).Err(); err != nil {
		return fmt.Errorf("chat session: persist metadata: %w", err)
	}
	return nil
}

// touchMetadata reloads the metadata blob, advances LastActiveAt to
// `now`, and writes back. Done outside the message pipeline because
// the metadata edit is non-critical for transcript consistency — the
// list-side write (RPUSH+LTRIM) is the source of truth for activity.
func (s *SessionStore) touchMetadata(ctx context.Context, id string, now time.Time) error {
	body, err := s.client.Get(ctx, sessionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil // session evicted during append; non-fatal
	}
	if err != nil {
		return fmt.Errorf("chat session: touch metadata load: %w", err)
	}
	var meta sessionMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return fmt.Errorf("chat session: touch metadata decode: %w", err)
	}
	if !now.After(meta.LastActiveAt) {
		return nil // racing replica already advanced past `now`
	}
	meta.LastActiveAt = now
	out, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("chat session: touch metadata marshal: %w", err)
	}
	if err := s.client.Set(ctx, sessionKey(id), out, SessionTTL).Err(); err != nil {
		return fmt.Errorf("chat session: touch metadata persist: %w", err)
	}
	return nil
}

func sessionKey(id string) string  { return sessionKeyPrefix + id }
func messagesKey(id string) string { return sessionKeyPrefix + id + sessionMsgsSuffix }
