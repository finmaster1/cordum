package llmchat

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestSessionStore(t *testing.T) (*SessionStore, *miniredis.Miniredis) {
	t.Helper()
	srv, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(srv.Close)
	client := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewSessionStoreFromClient(client), srv
}

func TestSessionStore_CreatePersists(t *testing.T) {
	store, srv := newTestSessionStore(t)
	ctx := context.Background()

	in := Session{
		UserPrincipal: "alice@cordum.io",
		Tenant:        "tenant-a",
		AgentID:       "chat-assistant-1",
	}
	out, err := store.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.ID == "" {
		t.Fatal("session id should be assigned")
	}
	if out.UserPrincipal != "alice@cordum.io" {
		t.Errorf("UserPrincipal = %q, want alice@cordum.io", out.UserPrincipal)
	}
	if out.CreatedAt.IsZero() || out.LastActiveAt.IsZero() {
		t.Errorf("timestamps must be set: created=%v active=%v", out.CreatedAt, out.LastActiveAt)
	}
	if !srv.Exists("chat:session:" + out.ID) {
		t.Errorf("expected key chat:session:%s to exist in redis", out.ID)
	}
}

func TestSessionStore_GetExistingRoundTrip(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	in, err := store.Create(ctx, Session{UserPrincipal: "p", Tenant: "t", AgentID: "a"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("expected session, got nil")
	}
	if got.ID != in.ID || got.UserPrincipal != "p" || got.Tenant != "t" {
		t.Errorf("round-trip mismatch: %+v vs %+v", got, in)
	}
}

func TestSessionStore_GetMissingReturnsNilNil(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	got, err := store.Get(ctx, "no-such-session")
	if err != nil {
		t.Fatalf("Get on missing returned error: %v", err)
	}
	if got != nil {
		t.Errorf("Get on missing returned %+v, want nil", got)
	}
}

func TestSessionStore_AppendMessageBumpsActivityAndTTL(t *testing.T) {
	store, srv := newTestSessionStore(t)
	ctx := context.Background()

	in, err := store.Create(ctx, Session{UserPrincipal: "p", Tenant: "t", AgentID: "a"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	originalActive := in.LastActiveAt

	// Drift the miniredis clock so LastActiveAt visibly advances.
	srv.FastForward(2 * time.Hour)

	if err := store.AppendMessage(ctx, in.ID, SessionMessage{Role: "user", Text: "hi"}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	got, err := store.Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get post-append: %v", err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Text != "hi" {
		t.Errorf("Messages = %+v, want one entry with text 'hi'", got.Messages)
	}
	if !got.LastActiveAt.After(originalActive) {
		t.Errorf("LastActiveAt did not advance: original=%v got=%v", originalActive, got.LastActiveAt)
	}

	// Sliding TTL: another 22h forward + a fresh append must keep the
	// session alive past the original 24h boundary.
	srv.FastForward(22 * time.Hour)
	if err := store.AppendMessage(ctx, in.ID, SessionMessage{Role: "assistant", Text: "hello"}); err != nil {
		t.Fatalf("second AppendMessage: %v", err)
	}
	srv.FastForward(22 * time.Hour)
	got2, err := store.Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get after sliding: %v", err)
	}
	if got2 == nil {
		t.Fatal("session evicted despite sliding-TTL refresh")
	}
}

func TestSessionStore_AppendMessageCapsAt50(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	in, err := store.Create(ctx, Session{UserPrincipal: "p", Tenant: "t", AgentID: "a"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for i := range 60 {
		if err := store.AppendMessage(ctx, in.ID, SessionMessage{Role: "user", Text: "msg"}); err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}
	got, err := store.Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Messages) != 50 {
		t.Errorf("Messages length = %d, want 50 (FIFO cap)", len(got.Messages))
	}
}

func TestSessionStore_SlidingTTLExpiry(t *testing.T) {
	store, srv := newTestSessionStore(t)
	ctx := context.Background()

	in, err := store.Create(ctx, Session{UserPrincipal: "p", Tenant: "t", AgentID: "a"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	srv.FastForward(25 * time.Hour) // past 24h TTL with no activity

	got, err := store.Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get after expiry: %v", err)
	}
	if got != nil {
		t.Errorf("Get after expiry returned %+v, want nil", got)
	}
}

// TestSessionStore_TwoReplicasNoDataLoss verifies that two independent
// SessionStore instances backed by the same miniredis (modelling two
// cordum-llm-chat replicas) can append messages to the same session
// concurrently without losing writes. Regression guard for the
// pre-redesign JSON-blob race that QA flagged at 2026-04-26.
func TestSessionStore_TwoReplicasNoDataLoss(t *testing.T) {
	srv, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(srv.Close)

	clientA := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	t.Cleanup(func() { _ = clientA.Close() })
	t.Cleanup(func() { _ = clientB.Close() })

	storeA := NewSessionStoreFromClient(clientA)
	storeB := NewSessionStoreFromClient(clientB)
	ctx := context.Background()

	in, err := storeA.Create(ctx, Session{UserPrincipal: "p", Tenant: "t", AgentID: "a"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// 20 messages, half from replica A and half from replica B.
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := storeA.AppendMessage(ctx, in.ID, SessionMessage{Role: "user", Text: fmt.Sprintf("a-%d", i)}); err != nil {
				t.Errorf("storeA AppendMessage %d: %v", i, err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := storeB.AppendMessage(ctx, in.ID, SessionMessage{Role: "assistant", Text: fmt.Sprintf("b-%d", i)}); err != nil {
				t.Errorf("storeB AppendMessage %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	got, err := storeA.Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Messages) != 20 {
		t.Errorf("expected 20 messages after cross-replica concurrent appends, got %d (no data loss is the QA-mandated requirement)", len(got.Messages))
	}
}

func TestSessionStore_ConcurrentAppendMessage(t *testing.T) {
	store, _ := newTestSessionStore(t)
	ctx := context.Background()

	in, err := store.Create(ctx, Session{UserPrincipal: "p", Tenant: "t", AgentID: "a"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var wg sync.WaitGroup
	for _, text := range []string{"alpha", "beta"} {
		wg.Add(1)
		go func(t2 string) {
			defer wg.Done()
			if err := store.AppendMessage(ctx, in.ID, SessionMessage{Role: "user", Text: t2}); err != nil {
				t.Errorf("AppendMessage(%s): %v", t2, err)
			}
		}(text)
	}
	wg.Wait()

	got, err := store.Get(ctx, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Errorf("expected 2 messages after concurrent appends, got %d (%+v)", len(got.Messages), got.Messages)
	}
}
