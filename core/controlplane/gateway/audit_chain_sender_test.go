package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/cordum/cordum/core/audit"
	"github.com/cordum/cordum/core/model"
	"github.com/redis/go-redis/v9"
)

func TestAuditChainSenderChainsAndForwards(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	chainer := audit.NewChainer(client, "")
	downstream := &testAuditSender{}
	sender := newAuditChainSender(chainer, downstream)

	sender.Send(audit.SIEMEvent{
		Timestamp: time.Now().UTC(),
		EventType: audit.EventSafetyApproval,
		Severity:  audit.SeverityInfo,
		TenantID:  "default",
		Action:    "approve",
		Reason:    "smoke approval",
	})

	if downstream.Len() != 1 {
		t.Fatalf("downstream event count = %d, want 1", downstream.Len())
	}
	forwarded := downstream.Get(0)
	if forwarded.Seq != 1 || forwarded.EventHash == "" {
		t.Fatalf("forwarded event missing chain fields: seq=%d hash=%q", forwarded.Seq, forwarded.EventHash)
	}

	result, err := audit.VerifyChain(context.Background(), client, chainer.StreamKey("default"), audit.VerifyOptions{})
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	if result.Status != audit.VerifyStatusOK || result.TotalEvents != 1 {
		t.Fatalf("verify result = status %q total %d, want ok/1", result.Status, result.TotalEvents)
	}
}

func TestAuditChainSenderAttributesTenantlessEventsToDefaultTenant(t *testing.T) {
	// Prior to 2026-05-16 the sink silently dropped tenantless events
	// (anonymous auth-middleware reads, system bootstrap events,
	// producer bugs that forgot to set TenantID). The Audit Log felt
	// incomplete as a result. The sink now attributes empty-tenant
	// events to model.DefaultTenant so they land on the default
	// tenant's chain instead of vanishing, while still forwarding
	// downstream and preserving per-tenant chain isolation
	// (the default tenant gets its own stream key).
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	chainer := audit.NewChainer(client, "")
	downstream := &testAuditSender{}
	sender := newAuditChainSender(chainer, downstream)

	sender.Send(audit.SIEMEvent{
		Timestamp: time.Now().UTC(),
		EventType: audit.EventSystemAuth,
		Severity:  audit.SeverityMedium,
		Action:    "auth.failure",
	})

	if downstream.Len() != 1 {
		t.Fatalf("downstream event count = %d, want 1", downstream.Len())
	}
	forwarded := downstream.Get(0)
	if forwarded.TenantID != model.DefaultTenant {
		t.Fatalf("forwarded TenantID = %q, want %q", forwarded.TenantID, model.DefaultTenant)
	}
	if forwarded.Seq != 1 || forwarded.EventHash == "" {
		t.Fatalf("forwarded event missing chain fields: seq=%d hash=%q", forwarded.Seq, forwarded.EventHash)
	}

	entries, err := client.XRange(context.Background(), chainer.StreamKey(model.DefaultTenant), "-", "+").Result()
	if err != nil {
		t.Fatalf("xrange default tenant chain: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("default-tenant chain entries = %d, want 1", len(entries))
	}

	result, err := audit.VerifyChain(context.Background(), client, chainer.StreamKey(model.DefaultTenant), audit.VerifyOptions{})
	if err != nil {
		t.Fatalf("verify default-tenant chain: %v", err)
	}
	if result.Status != audit.VerifyStatusOK || result.TotalEvents != 1 {
		t.Fatalf("verify result = status %q total %d, want ok/1", result.Status, result.TotalEvents)
	}
}
