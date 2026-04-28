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

// Session-store constants pinned for cross-package consumption (admin session
// viewer, WS handler). Renaming is a wire-break.
const (
	sessionKeyPrefix  = "chat:session:"
	sessionMsgsSuffix = ":messages"

	// SessionTTL is the sliding TTL applied on Create + every AppendMessage.
	SessionTTL = 24 * time.Hour

	// SessionMaxMessages caps the per-session transcript with FIFO eviction.
	SessionMaxMessages = 50
)

// Hash field names for the metadata key. Pinned wire format; the admin session
// viewer reads these by name.
const (
	sessionFieldID            = "id"
	sessionFieldUserPrincipal = "user_principal"
	sessionFieldTenant        = "tenant"
	sessionFieldAgentID       = "agent_id"
	sessionFieldCreatedAt     = "created_at_unix_nano"
	sessionFieldLastActiveAt  = "last_active_at_unix_nano"
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

// Session is the persisted chat-assistant session record. Informational-only
// sessions store identity, timestamps, and transcript only; per-session
// per-session action state was retired with informational-only chat.
type Session struct {
	ID            string           `json:"id"`
	UserPrincipal string           `json:"user_principal"`
	Tenant        string           `json:"tenant"`
	AgentID       string           `json:"agent_id"`
	Messages      []SessionMessage `json:"messages"`
	CreatedAt     time.Time        `json:"created_at"`
	LastActiveAt  time.Time        `json:"last_active_at"`
}

// SessionMessage is one transcript entry.
type SessionMessage struct {
	Role string    `json:"role"`
	Text string    `json:"text,omitempty"`
	At   time.Time `json:"at"`
}

// SessionStore persists chat sessions in Redis. The 24h sliding TTL expires
// inactive sessions automatically; AppendMessage refreshes the TTL atomically.
type SessionStore struct {
	client redis.UniversalClient
}

// NewSessionStoreFromClient wraps an existing redis client. Callers in
// production hold one *redis.Client opened from REDIS_URL in main.go.
func NewSessionStoreFromClient(client redis.UniversalClient) *SessionStore {
	return &SessionStore{client: client}
}

// Create persists a new session. The caller fills UserPrincipal, Tenant, and
// AgentID; the store assigns ID, CreatedAt, LastActiveAt and writes metadata.
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
		sessionFieldCreatedAt:     strconv.FormatInt(now.UnixNano(), 10),
		sessionFieldLastActiveAt:  strconv.FormatInt(now.UnixNano(), 10),
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
// distinguish "not found" from a transport error without sentinel matching.
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

// decodeSessionFields converts a HGETALL result into a Session.
func decodeSessionFields(fields map[string]string) (*Session, error) {
	sess := &Session{
		ID:            fields[sessionFieldID],
		UserPrincipal: fields[sessionFieldUserPrincipal],
		Tenant:        fields[sessionFieldTenant],
		AgentID:       fields[sessionFieldAgentID],
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
	return sess, nil
}

func sessionKey(id string) string  { return sessionKeyPrefix + id }
func messagesKey(id string) string { return sessionKeyPrefix + id + sessionMsgsSuffix }
