package gateway

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/cordum/cordum/core/configsvc"
	"github.com/cordum/cordum/core/infra/config"
)

const defaultDrainCheckInterval = 10 * time.Second

// poolDrainChecker monitors draining pools and auto-transitions them to
// inactive when all in-flight jobs complete or the drain timeout expires.
type poolDrainChecker struct {
	srv           *server
	checkInterval time.Duration
}

func newPoolDrainChecker(srv *server) *poolDrainChecker {
	return &poolDrainChecker{
		srv:           srv,
		checkInterval: defaultDrainCheckInterval,
	}
}

// Run starts the drain check loop. It blocks until ctx is cancelled.
func (d *poolDrainChecker) Run(ctx context.Context) {
	ticker := time.NewTicker(d.checkInterval)
	defer ticker.Stop()

	slog.Info("pool.drain.checker started", "interval", d.checkInterval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("pool.drain.checker stopped")
			return
		case <-ticker.C:
			d.checkAll(ctx)
		}
	}
}

func (d *poolDrainChecker) checkAll(ctx context.Context) {
	if d.srv.configSvc == nil {
		return
	}

	doc, err := d.srv.configSvc.Get(ctx, configsvc.ScopeSystem, "default")
	if err != nil {
		slog.Debug("pool.drain: config read failed", "error", err)
		return
	}

	_, poolMap, err := extractPoolsFromConfig(doc)
	if err != nil {
		slog.Debug("pool.drain: parse pools failed", "error", err)
		return
	}

	for name, pool := range poolMap {
		if pool.EffectiveStatus() != config.PoolStatusDraining {
			continue
		}
		d.checkDrain(ctx, name, pool)
	}
}

func (d *poolDrainChecker) checkDrain(ctx context.Context, poolName string, pool config.PoolConfig) {
	// Check drain timeout first
	if pool.DrainStartedAt != "" {
		startedAt, err := time.Parse(time.RFC3339, pool.DrainStartedAt)
		if err == nil {
			timeout := time.Duration(pool.DrainTimeoutSeconds) * time.Second
			if timeout <= 0 {
				timeout = 300 * time.Second
			}
			if time.Since(startedAt) > timeout {
				activeJobs := d.countActiveJobsForPool(poolName)
				slog.Warn("pool.drain.timeout",
					"pool", poolName,
					"elapsed", time.Since(startedAt).Round(time.Second),
					"timeout", timeout,
					"active_jobs", activeJobs)
				d.transitionToInactive(ctx, poolName, "drain timeout expired")
				return
			}
		}
	}

	// Count active jobs for this pool
	activeJobs := d.countActiveJobsForPool(poolName)
	if activeJobs == 0 {
		elapsed := "unknown"
		if pool.DrainStartedAt != "" {
			if startedAt, err := time.Parse(time.RFC3339, pool.DrainStartedAt); err == nil {
				elapsed = time.Since(startedAt).Round(time.Second).String()
			}
		}
		slog.Info("pool.drain.complete",
			"pool", poolName,
			"elapsed", elapsed,
			"reason", "no active jobs")
		d.transitionToInactive(ctx, poolName, "all jobs completed")
		return
	}

	slog.Debug("pool.drain.progress",
		"pool", poolName,
		"active_jobs", activeJobs)
}

func (d *poolDrainChecker) countActiveJobsForPool(poolName string) int32 {
	snap, err := d.srv.snapshotFromRedis()
	if err != nil || snap == nil {
		return 0
	}
	var total int32
	for _, w := range snap.Workers {
		if w.Pool == poolName {
			total += w.ActiveJobs
		}
	}
	return total
}

func (d *poolDrainChecker) transitionToInactive(ctx context.Context, poolName, reason string) {
	err := d.srv.configSvc.SetWithRetry(ctx, configsvc.ScopeSystem, "default", 3, func(doc *configsvc.Document) error {
		topics, poolMap, err := extractPoolsFromConfig(doc)
		if err != nil {
			return err
		}
		pool, ok := poolMap[poolName]
		if !ok {
			return nil // already removed
		}
		if pool.EffectiveStatus() != config.PoolStatusDraining {
			return nil // another replica already transitioned
		}
		pool.Status = config.PoolStatusInactive
		pool.DrainStartedAt = ""
		pool.DrainTimeoutSeconds = 0
		poolMap[poolName] = pool
		writePoolsToConfig(doc, topics, poolMap)
		return nil
	})
	if err != nil {
		if errors.Is(err, configsvc.ErrRevisionConflict) {
			slog.Debug("pool.drain.transition conflict — another replica handled it",
				"pool", poolName)
			return
		}
		slog.Error("pool.drain.transition failed",
			"pool", poolName,
			"reason", reason,
			"error", err)
		return
	}

	d.srv.publishConfigChanged("system", "default")
	slog.Info("pool.drain.transitioned",
		"pool", poolName,
		"status", config.PoolStatusInactive,
		"reason", reason)
}
