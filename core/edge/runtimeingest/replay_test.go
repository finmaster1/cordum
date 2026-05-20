package runtimeingest

import (
	"context"
	"errors"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newReplayWindowTestClient(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), PoolSize: 4})
	t.Cleanup(func() {
		_ = client.Close()
		mr.Close()
	})
	return client, mr
}

func TestReplayWindow_FirstNonceAccepted(t *testing.T) {
	ctx := context.Background()
	client, mr := newReplayWindowTestClient(t)
	window := NewReplayWindow(client, ReplayWindowTTL, MaxReplayWindowCardinality)

	accepted, err := window.Reserve(ctx, "tenant-a", "collector-x", "nonce-000000000001")
	if err != nil {
		t.Fatalf("Reserve first nonce: %v", err)
	}
	if !accepted {
		t.Fatal("Reserve first nonce accepted=false; want true")
	}

	key := ReplayWindowKeyPrefix + "tenant-a:collector-x"
	if !mr.Exists(key) {
		t.Fatalf("expected replay key %q to exist", key)
	}
	ttl := mr.TTL(key)
	if ttl <= 0 || ttl > ReplayWindowTTL {
		t.Fatalf("TTL(%s) = %v; want >0 and <= %v", key, ttl, ReplayWindowTTL)
	}
}

func TestReplayWindow_DuplicateNonceRejected(t *testing.T) {
	ctx := context.Background()
	client, _ := newReplayWindowTestClient(t)
	window := NewReplayWindow(client, ReplayWindowTTL, MaxReplayWindowCardinality)

	first, err := window.Reserve(ctx, "tenant-a", "collector-x", "nonce-000000000001")
	if err != nil || !first {
		t.Fatalf("first Reserve = (%v, %v); want (true, nil)", first, err)
	}
	second, err := window.Reserve(ctx, "tenant-a", "collector-x", "nonce-000000000001")
	if err != nil {
		t.Fatalf("duplicate Reserve: %v", err)
	}
	if second {
		t.Fatal("duplicate Reserve accepted=true; want false replay result")
	}
}

func TestReplayWindow_ReleaseAllowsRetry(t *testing.T) {
	ctx := context.Background()
	client, _ := newReplayWindowTestClient(t)
	window := NewReplayWindow(client, ReplayWindowTTL, MaxReplayWindowCardinality)
	nonce := "nonce-release-000001"

	first, err := window.Reserve(ctx, "tenant-a", "collector-x", nonce)
	if err != nil || !first {
		t.Fatalf("first Reserve = (%v, %v); want (true, nil)", first, err)
	}
	if err := window.Release(ctx, "tenant-a", "collector-x", nonce); err != nil {
		t.Fatalf("Release: %v", err)
	}
	retry, err := window.Reserve(ctx, "tenant-a", "collector-x", nonce)
	if err != nil || !retry {
		t.Fatalf("retry Reserve after Release = (%v, %v); want (true, nil)", retry, err)
	}
}

func TestReplayWindow_DoesNotPersistRawNonce(t *testing.T) {
	ctx := context.Background()
	client, _ := newReplayWindowTestClient(t)
	window := NewReplayWindow(client, ReplayWindowTTL, MaxReplayWindowCardinality)
	nonce := "nonce-raw-canary-0001"

	accepted, err := window.Reserve(ctx, "tenant-a", "collector-x", nonce)
	if err != nil || !accepted {
		t.Fatalf("Reserve = (%v, %v); want (true, nil)", accepted, err)
	}
	members, err := client.SMembers(ctx, ReplayWindowKeyPrefix+"tenant-a:collector-x").Result()
	if err != nil {
		t.Fatalf("SMembers replay key: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("members len = %d; want 1", len(members))
	}
	if members[0] == nonce {
		t.Fatal("replay window persisted raw nonce; want hashed value only")
	}
	if want := replayNonceDigest(nonce); members[0] != want {
		t.Fatalf("stored member = %q; want nonce digest %q", members[0], want)
	}
}

func TestReplayWindow_CrossCollectorIsolated(t *testing.T) {
	ctx := context.Background()
	client, _ := newReplayWindowTestClient(t)
	window := NewReplayWindow(client, ReplayWindowTTL, MaxReplayWindowCardinality)

	if ok, err := window.Reserve(ctx, "tenant-a", "collector-x", "nonce-000000000001"); err != nil || !ok {
		t.Fatalf("collector-x Reserve = (%v, %v); want (true, nil)", ok, err)
	}
	if ok, err := window.Reserve(ctx, "tenant-a", "collector-y", "nonce-000000000001"); err != nil || !ok {
		t.Fatalf("collector-y Reserve = (%v, %v); want (true, nil)", ok, err)
	}
}

func TestReplayWindow_CrossTenantIsolated(t *testing.T) {
	ctx := context.Background()
	client, _ := newReplayWindowTestClient(t)
	window := NewReplayWindow(client, ReplayWindowTTL, MaxReplayWindowCardinality)

	if ok, err := window.Reserve(ctx, "tenant-a", "collector-x", "nonce-000000000001"); err != nil || !ok {
		t.Fatalf("tenant-a Reserve = (%v, %v); want (true, nil)", ok, err)
	}
	if ok, err := window.Reserve(ctx, "tenant-b", "collector-x", "nonce-000000000001"); err != nil || !ok {
		t.Fatalf("tenant-b Reserve = (%v, %v); want (true, nil)", ok, err)
	}
}

func TestReplayWindow_CapExhaustionRefuses(t *testing.T) {
	ctx := context.Background()
	client, _ := newReplayWindowTestClient(t)
	window := NewReplayWindow(client, time.Hour, 2)

	for _, nonce := range []string{"nonce-000000000001", "nonce-000000000002"} {
		if ok, err := window.Reserve(ctx, "tenant-a", "collector-x", nonce); err != nil || !ok {
			t.Fatalf("Reserve(%s) = (%v, %v); want (true, nil)", nonce, ok, err)
		}
	}
	ok, err := window.Reserve(ctx, "tenant-a", "collector-x", "nonce-000000000003")
	if !errors.Is(err, ErrReplayWindowFull) {
		t.Fatalf("Reserve over cap error = %v; want ErrReplayWindowFull", err)
	}
	if ok {
		t.Fatal("Reserve over cap accepted=true; want false")
	}
}

func TestReplayWindow_CrossInstanceShared(t *testing.T) {
	ctx := context.Background()
	client, _ := newReplayWindowTestClient(t)
	firstWindow := NewReplayWindow(client, ReplayWindowTTL, MaxReplayWindowCardinality)
	secondWindow := NewReplayWindow(client, ReplayWindowTTL, MaxReplayWindowCardinality)

	first, err := firstWindow.Reserve(ctx, "tenant-a", "collector-x", "nonce-000000000001")
	if err != nil || !first {
		t.Fatalf("first instance Reserve = (%v, %v); want (true, nil)", first, err)
	}
	second, err := secondWindow.Reserve(ctx, "tenant-a", "collector-x", "nonce-000000000001")
	if err != nil {
		t.Fatalf("second instance Reserve: %v", err)
	}
	if second {
		t.Fatal("second instance accepted duplicate nonce; want replay=false")
	}
}

func TestReplayWindow_TTLExpiryAcceptsAgain(t *testing.T) {
	ctx := context.Background()
	client, mr := newReplayWindowTestClient(t)
	window := NewReplayWindow(client, time.Hour, MaxReplayWindowCardinality)

	if ok, err := window.Reserve(ctx, "tenant-a", "collector-x", "nonce-000000000001"); err != nil || !ok {
		t.Fatalf("first Reserve = (%v, %v); want (true, nil)", ok, err)
	}
	mr.FastForward(2 * time.Hour)
	if ok, err := window.Reserve(ctx, "tenant-a", "collector-x", "nonce-000000000001"); err != nil || !ok {
		t.Fatalf("post-expiry Reserve = (%v, %v); want (true, nil)", ok, err)
	}
}
