package llmchat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Session-store constants pinned for cross-package consumption (admin
// session viewer, WS handler in phase 5). Renaming is a wire-break.
const (
	sessionKeyPrefix  = "chat:session:"
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

// Hash field names for the metadata key. Pinned wire format; the admin
// session viewer (phase 5) reads these by name.
const (
	sessionFieldID                 = "id"
	sessionFieldUserPrincipal      = "user_principal"
	sessionFieldTenant             = "tenant"
	sessionFieldAgentID            = "agent_id"
	sessionFieldDelegationJTI      = "delegation_jti"
	sessionFieldDelegationJSON     = "delegation_json"
	sessionFieldCreatedAt          = "created_at_unix_nano"
	sessionFieldLastActiveAt       = "last_active_at_unix_nano"
	sessionFieldPendingToolCallSON = "pending_tool_call_json"
)

var appendMessageScript = redis.NewScript(`
local metaKey = KEYS[1]
local msgsKey = KEYS[2]
local msgJSON = ARGV[1]
local ttlSec = tonumber(ARGV[2])
local maxMsgs = tonumber(ARGV[3])
local lastActiveAt = ARGV[4]

if redis.call('EXISTS', metaKey) == 0 then
	return redis.error_reply('session not found')
end

redis.call('RPUSH', msgsKey, msgJSON)
redis.call('LTRIM', msgsKey, -maxMsgs, -1)
redis.call('EXPIRE', msgsKey, ttlSec)
redis.call('HSET', metaKey, '` + sessionFieldLastActiveAt + `', lastActiveAt)
redis.call('EXPIRE', metaKey, ttlSec)
return 'OK'
`)

// Session is the persisted chat-assistant session record. Pinned shape;
// admin viewer + WS handler in phase 5 deserialise this same JSON.
//
// Storage layout: metadata lives under `chat:session:{id}` as a Redis
// HASH (atomic field updates — no read-modify-write race); transcript
// messages live under `chat:session:{id}:messages` as a Redis list
// (RPUSH appends, LTRIM caps, LRANGE reads). Splitting metadata from
// transcript means AppendMessage is a single atomic Lua script (check
// metadata exists + RPUSH+LTRIM+EXPIRE on list + HSET last_active_at on
// metadata + EXPIRE on meta) and SetDelegation HSET-touches only the
// delegation fields, never clobbering activity timestamps or other
// concurrent writes.
type Session struct {
	ID              string             `json:"id"`
	UserPrincipal   string             `json:"user_principal"`
	Tenant          string             `json:"tenant"`
	AgentID         string             `json:"agent_id"`
	DelegationJTI   string             `json:"delegation_jti"`
	Delegation      *SessionDelegation `json:"delegation,omitempty"`
	Messages        []SessionMessage   `json:"messages"`
	CreatedAt       time.Time          `json:"created_at"`
	LastActiveAt    time.Time          `json:"last_active_at"`
	PendingToolCall *ToolCallRef       `json:"pending_tool_call,omitempty"`
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
// evicted mid-call. Cross-replica concurrent appends + delegation
// updates are safe because every mutation uses Redis atomic field
// updates (HSET, RPUSH, LTRIM) — no read-modify-write step that could
// lose a write.
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
// assigns ID, CreatedAt, LastActiveAt and writes the metadata HASH at
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

	fields := map[string]any{
		sessionFieldID:            in.ID,
		sessionFieldUserPrincipal: in.UserPrincipal,
		sessionFieldTenant:        in.Tenant,
		sessionFieldAgentID:       in.AgentID,
		sessionFieldDelegationJTI: in.DelegationJTI,
		sessionFieldCreatedAt:     strconv.FormatInt(now.UnixNano(), 10),
		sessionFieldLastActiveAt:  strconv.FormatInt(now.UnixNano(), 10),
	}
	if in.Delegation != nil {
		raw, err := json.Marshal(in.Delegation)
		if err != nil {
			return Session{}, fmt.Errorf("chat session: marshal delegation: %w", err)
		}
		fields[sessionFieldDelegationJSON] = string(raw)
		if in.DelegationJTI == "" {
			fields[sessionFieldDelegationJTI] = in.Delegation.JTI
		}
	}

	pipe := s.client.Pipeline()
	pipe.HSet(ctx, sessionKey(in.ID), fields)
	pipe.Expire(ctx, sessionKey(in.ID), SessionTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return Session{}, fmt.Errorf("chat session: persist: %w", err)
	}
	return in, nil
}

// Get loads a session. A missing key returns (nil, nil) so callers can
// distinguish "not found" from a transport error without sentinel
// matching. The transcript list is loaded via LRANGE alongside the
// metadata hash.
func (s *SessionStore) Get(ctx context.Context, id string) (*Session, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("chat session: store not configured")
	}
	fields, err := s.client.HGetAll(ctx, sessionKey(id)).Result()
	if err != nil {
		return nil, fmt.Errorf("chat session: load: %w", err)
	}
	if len(fields) == 0 {
		return nil, nil
	}
	sess, err := decodeSessionFields(fields)
	if err != nil {
		return nil, err
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
	sess.Messages = messages
	return sess, nil
}

// AppendMessage appends a transcript entry and refreshes the 24h TTL.
// Atomic via a single Redis Lua script: check metadata exists, RPUSH on
// the message list, LTRIM to enforce the FIFO 50-cap, HSET the
// LastActiveAt field, then EXPIRE both keys. Cross-replica concurrent
// appends + concurrent SetDelegation are safe — Redis serialises the
// script and HSET only touches the named field, leaving DelegationJSON /
// DelegationJTI / other metadata untouched.
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
	now := time.Now().UTC()
	if err := appendMessageScript.Run(
		ctx,
		s.client,
		[]string{metaKey, msgsKey},
		string(raw),
		int(SessionTTL.Seconds()),
		SessionMaxMessages,
		strconv.FormatInt(now.UnixNano(), 10),
	).Err(); err != nil {
		return fmt.Errorf("chat session: append script: %w", err)
	}
	return nil
}

// SetDelegation persists the delegation JWT + JTI + expiry on a
// session's metadata HASH. Atomic field-only update — does NOT
// touch CreatedAt, LastActiveAt, the messages list, or any other
// metadata. Concurrent AppendMessage on the same session keeps its
// LastActiveAt write isolated from this call's delegation write.
func (s *SessionStore) SetDelegation(ctx context.Context, id string, delegation *SessionDelegation) error {
	if s == nil || s.client == nil {
		return errors.New("chat session: store not configured")
	}
	metaKey := sessionKey(id)
	if exists, err := s.client.Exists(ctx, metaKey).Result(); err != nil {
		return fmt.Errorf("chat session: set delegation exists check: %w", err)
	} else if exists == 0 {
		return fmt.Errorf("chat session: set delegation: session %s not found", id)
	}

	fields := map[string]any{}
	if delegation == nil {
		// Clear: HDEL removes the fields. Redis treats missing keys
		// as no-op, so this is safe even if the fields were never set.
		pipe := s.client.Pipeline()
		pipe.HDel(ctx, metaKey, sessionFieldDelegationJSON, sessionFieldDelegationJTI)
		pipe.Expire(ctx, metaKey, SessionTTL)
		if _, err := pipe.Exec(ctx); err != nil {
			return fmt.Errorf("chat session: set delegation clear: %w", err)
		}
		return nil
	}
	raw, err := json.Marshal(delegation)
	if err != nil {
		return fmt.Errorf("chat session: set delegation marshal: %w", err)
	}
	fields[sessionFieldDelegationJSON] = string(raw)
	fields[sessionFieldDelegationJTI] = delegation.JTI

	pipe := s.client.Pipeline()
	pipe.HSet(ctx, metaKey, fields)
	pipe.Expire(ctx, metaKey, SessionTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("chat session: set delegation persist: %w", err)
	}
	return nil
}

// SetPendingToolCall persists (or clears) the in-flight tool call that
// is awaiting human approval on a session's metadata HASH. Atomic
// field-only update — does NOT touch CreatedAt, LastActiveAt, the
// messages list, or other metadata. Phase-5 WS handler resumes the
// agent loop after approval by reading this field via Get.
//
// Pass nil to clear the field (after the human approves and the loop
// resumes, or after a session-cancel).
func (s *SessionStore) SetPendingToolCall(ctx context.Context, id string, ref *ToolCallRef) error {
	if s == nil || s.client == nil {
		return errors.New("chat session: store not configured")
	}
	metaKey := sessionKey(id)
	if exists, err := s.client.Exists(ctx, metaKey).Result(); err != nil {
		return fmt.Errorf("chat session: set pending tool call exists check: %w", err)
	} else if exists == 0 {
		return fmt.Errorf("chat session: set pending tool call: session %s not found", id)
	}

	pipe := s.client.Pipeline()
	if ref == nil {
		pipe.HDel(ctx, metaKey, sessionFieldPendingToolCallSON)
	} else {
		raw, err := json.Marshal(ref)
		if err != nil {
			return fmt.Errorf("chat session: set pending tool call marshal: %w", err)
		}
		pipe.HSet(ctx, metaKey, sessionFieldPendingToolCallSON, string(raw))
	}
	pipe.Expire(ctx, metaKey, SessionTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("chat session: set pending tool call persist: %w", err)
	}
	return nil
}

// decodeSessionFields converts a HGETALL result into a Session. The
// hash schema is internal — wire shape is the JSON projection of
// Session, not the field-by-field hash.
func decodeSessionFields(fields map[string]string) (*Session, error) {
	sess := &Session{
		ID:            fields[sessionFieldID],
		UserPrincipal: fields[sessionFieldUserPrincipal],
		Tenant:        fields[sessionFieldTenant],
		AgentID:       fields[sessionFieldAgentID],
		DelegationJTI: fields[sessionFieldDelegationJTI],
	}
	if raw, ok := fields[sessionFieldCreatedAt]; ok && raw != "" {
		nano, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("chat session: parse created_at: %w", err)
		}
		sess.CreatedAt = time.Unix(0, nano).UTC()
	}
	if raw, ok := fields[sessionFieldLastActiveAt]; ok && raw != "" {
		nano, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("chat session: parse last_active_at: %w", err)
		}
		sess.LastActiveAt = time.Unix(0, nano).UTC()
	}
	if raw, ok := fields[sessionFieldDelegationJSON]; ok && raw != "" {
		var d SessionDelegation
		if err := json.Unmarshal([]byte(raw), &d); err != nil {
			return nil, fmt.Errorf("chat session: decode delegation: %w", err)
		}
		sess.Delegation = &d
	}
	if raw, ok := fields[sessionFieldPendingToolCallSON]; ok && raw != "" {
		var ref ToolCallRef
		if err := json.Unmarshal([]byte(raw), &ref); err != nil {
			return nil, fmt.Errorf("chat session: decode pending tool call: %w", err)
		}
		sess.PendingToolCall = &ref
	}
	return sess, nil
}

func sessionKey(id string) string  { return sessionKeyPrefix + id }
func messagesKey(id string) string { return sessionKeyPrefix + id + sessionMsgsSuffix }
