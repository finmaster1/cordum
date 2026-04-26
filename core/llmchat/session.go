package llmchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Session-store constants pinned for cross-package consumption (admin
// session viewer, WS handler in phase 5). Renaming is a wire-break.
const (
	sessionKeyPrefix = "chat:session:"

	// SessionTTL is the sliding TTL applied on Create + every
	// AppendMessage. 24h matches the architect's plan and gives
	// reconnecting users a full work-day to resume a Gmail-style chat.
	SessionTTL = 24 * time.Hour

	// SessionMaxMessages caps the per-session transcript at 50 entries
	// with FIFO eviction. The cap exists for two reasons: (1) the LLM
	// context window cannot meaningfully consume an unbounded
	// transcript, and (2) Redis hash growth must be bounded for the
	// 24h sliding-TTL design to remain memory-safe.
	SessionMaxMessages = 50
)

// Session is the persisted chat-assistant session record. Pinned shape;
// admin viewer + WS handler in phase 5 deserialise this same JSON.
type Session struct {
	ID            string           `json:"id"`
	UserPrincipal string           `json:"user_principal"`
	Tenant        string           `json:"tenant"`
	AgentID       string           `json:"agent_id"`
	DelegationJTI string           `json:"delegation_jti"`
	Messages      []SessionMessage `json:"messages"`
	CreatedAt     time.Time        `json:"created_at"`
	LastActiveAt  time.Time        `json:"last_active_at"`
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
// evicted mid-call.
type SessionStore struct {
	client redis.UniversalClient

	// appendMu serialises hash-read-modify-write inside AppendMessage
	// for the same process. Cross-process concurrency is rare (one
	// session is normally bound to one WS connection) but the mutex
	// removes the read-modify-write race in tests + multi-WS
	// scenarios. Cross-replica writes still risk last-write-wins;
	// that's acceptable for chat transcripts where the eventual
	// audit log is the source of truth.
	appendMu sync.Mutex
}

// NewSessionStoreFromClient wraps an existing redis client. Callers in
// production hold one *redis.Client opened from REDIS_URL in main.go.
func NewSessionStoreFromClient(client redis.UniversalClient) *SessionStore {
	return &SessionStore{client: client}
}

// Create persists a new session. The caller fills UserPrincipal,
// Tenant, AgentID, DelegationJTI; the store assigns ID, CreatedAt,
// LastActiveAt and writes the record at chat:session:{id}.
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

	body, err := json.Marshal(in)
	if err != nil {
		return Session{}, fmt.Errorf("chat session: marshal: %w", err)
	}
	key := sessionKey(in.ID)
	if err := s.client.Set(ctx, key, body, SessionTTL).Err(); err != nil {
		return Session{}, fmt.Errorf("chat session: persist: %w", err)
	}
	return in, nil
}

// Get loads a session. A missing key returns (nil, nil) so callers can
// distinguish "not found" from a transport error without sentinel
// matching.
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
	var sess Session
	if err := json.Unmarshal(body, &sess); err != nil {
		return nil, fmt.Errorf("chat session: decode: %w", err)
	}
	return &sess, nil
}

// AppendMessage appends a transcript entry, applies the FIFO 50-message
// cap, bumps LastActiveAt, and refreshes the 24h TTL — all in a single
// pipeline so a redis crash mid-call cannot leave the record on a
// stale TTL with new messages.
func (s *SessionStore) AppendMessage(ctx context.Context, id string, msg SessionMessage) error {
	if s == nil || s.client == nil {
		return errors.New("chat session: store not configured")
	}
	s.appendMu.Lock()
	defer s.appendMu.Unlock()

	key := sessionKey(id)
	body, err := s.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return fmt.Errorf("chat session: append: session %s not found", id)
	}
	if err != nil {
		return fmt.Errorf("chat session: append load: %w", err)
	}
	var sess Session
	if err := json.Unmarshal(body, &sess); err != nil {
		return fmt.Errorf("chat session: append decode: %w", err)
	}

	if msg.At.IsZero() {
		msg.At = time.Now().UTC()
	}
	sess.Messages = append(sess.Messages, msg)
	if len(sess.Messages) > SessionMaxMessages {
		sess.Messages = sess.Messages[len(sess.Messages)-SessionMaxMessages:]
	}
	sess.LastActiveAt = time.Now().UTC()

	out, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("chat session: append marshal: %w", err)
	}
	if err := s.client.Set(ctx, key, out, SessionTTL).Err(); err != nil {
		return fmt.Errorf("chat session: append persist: %w", err)
	}
	return nil
}

func sessionKey(id string) string { return sessionKeyPrefix + id }
