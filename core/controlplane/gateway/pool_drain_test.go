package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/cordum/cordum/core/configsvc"
	"github.com/cordum/cordum/core/infra/config"
	"github.com/cordum/cordum/core/infra/registry"
)

func seedDrainConfig(t *testing.T, s *server, pools map[string]config.PoolConfig) {
	t.Helper()
	err := s.configSvc.SetWithRetry(context.Background(), configsvc.ScopeSystem, "default", 1, func(doc *configsvc.Document) error {
		if doc.Data == nil {
			doc.Data = map[string]any{}
		}
		doc.Data["pools"] = map[string]any{
			"topics": map[string]any{"job.test": "test-pool"},
			"pools":  pools,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed drain config: %v", err)
	}
}

func getPoolStatus(t *testing.T, s *server, poolName string) string {
	t.Helper()
	doc, err := s.configSvc.Get(context.Background(), configsvc.ScopeSystem, "default")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	_, poolMap, err := extractPoolsFromConfig(doc)
	if err != nil {
		t.Fatalf("extract pools: %v", err)
	}
	p, ok := poolMap[poolName]
	if !ok {
		return "not_found"
	}
	return p.EffectiveStatus()
}

func TestDrainChecker_ZeroActiveJobs_TransitionsToInactive(t *testing.T) {
	s, _, _ := newTestGateway(t)
	seedDrainConfig(t, s, map[string]config.PoolConfig{
		"test-pool": {
			Status:              config.PoolStatusDraining,
			DrainStartedAt:      time.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339),
			DrainTimeoutSeconds: 300,
		},
	})
	// Snapshot with no workers in the pool
	seedSnapshot(t, s, registry.Snapshot{
		CapturedAt: time.Now().UTC().Format(time.RFC3339),
		Workers: []registry.WorkerSummary{
			{WorkerID: "w1", Pool: "other-pool", ActiveJobs: 5},
		},
	})

	checker := newPoolDrainChecker(s)
	checker.checkAll(context.Background())

	if status := getPoolStatus(t, s, "test-pool"); status != config.PoolStatusInactive {
		t.Errorf("expected inactive, got %q", status)
	}
}

func TestDrainChecker_ActiveJobs_RemainingDraining(t *testing.T) {
	s, _, _ := newTestGateway(t)
	seedDrainConfig(t, s, map[string]config.PoolConfig{
		"test-pool": {
			Status:              config.PoolStatusDraining,
			DrainStartedAt:      time.Now().UTC().Format(time.RFC3339),
			DrainTimeoutSeconds: 300,
		},
	})
	seedSnapshot(t, s, registry.Snapshot{
		CapturedAt: time.Now().UTC().Format(time.RFC3339),
		Workers: []registry.WorkerSummary{
			{WorkerID: "w1", Pool: "test-pool", ActiveJobs: 3},
		},
	})

	checker := newPoolDrainChecker(s)
	checker.checkAll(context.Background())

	if status := getPoolStatus(t, s, "test-pool"); status != config.PoolStatusDraining {
		t.Errorf("expected draining (jobs in flight), got %q", status)
	}
}

func TestDrainChecker_Timeout_ForcesInactive(t *testing.T) {
	s, _, _ := newTestGateway(t)
	seedDrainConfig(t, s, map[string]config.PoolConfig{
		"test-pool": {
			Status:              config.PoolStatusDraining,
			DrainStartedAt:      time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339),
			DrainTimeoutSeconds: 60, // 60s timeout, started 10min ago
		},
	})
	seedSnapshot(t, s, registry.Snapshot{
		CapturedAt: time.Now().UTC().Format(time.RFC3339),
		Workers: []registry.WorkerSummary{
			{WorkerID: "w1", Pool: "test-pool", ActiveJobs: 2},
		},
	})

	checker := newPoolDrainChecker(s)
	checker.checkAll(context.Background())

	if status := getPoolStatus(t, s, "test-pool"); status != config.PoolStatusInactive {
		t.Errorf("expected inactive (timeout), got %q", status)
	}
}

func TestDrainChecker_NoDrainingPools_Noop(t *testing.T) {
	s, _, _ := newTestGateway(t)
	seedDrainConfig(t, s, map[string]config.PoolConfig{
		"test-pool": {Status: config.PoolStatusActive},
	})

	checker := newPoolDrainChecker(s)
	checker.checkAll(context.Background())

	if status := getPoolStatus(t, s, "test-pool"); status != config.PoolStatusActive {
		t.Errorf("expected active (no-op), got %q", status)
	}
}
