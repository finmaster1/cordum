package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPoolConfigSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pools.yaml")
	body := []byte("topics:\n  job.default: default\n  job.batch:\n    - batch\n    - batch-b\npools:\n  default:\n    requires: [\"docker\", \"git\"]\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := LoadPoolConfig(path)
	if err != nil {
		t.Fatalf("LoadPoolConfig returned error: %v", err)
	}
	if len(cfg.Topics["job.default"]) != 1 || cfg.Topics["job.default"][0] != "default" {
		t.Fatalf("unexpected topics: %#v", cfg.Topics)
	}
	if len(cfg.Topics["job.batch"]) != 2 || cfg.Topics["job.batch"][0] != "batch" {
		t.Fatalf("unexpected batch topics: %#v", cfg.Topics["job.batch"])
	}
	if cfg.Pools["default"].Requires[0] != "docker" {
		t.Fatalf("unexpected pool requires: %#v", cfg.Pools["default"].Requires)
	}
}

func TestParsePoolsConfigNewFormat(t *testing.T) {
	body := []byte(`topics:
  job.test: test-pool
pools:
  test-pool:
    requires: ["docker"]
    status: draining
    description: "A pool for tests"
    drain_started_at: "2026-03-26T10:00:00Z"
    drain_timeout_seconds: 600
`)
	cfg, err := ParsePoolsConfig(body)
	if err != nil {
		t.Fatalf("ParsePoolsConfig returned error: %v", err)
	}
	p := cfg.Pools["test-pool"]
	if p.Status != "draining" {
		t.Errorf("expected status draining, got %q", p.Status)
	}
	if p.Description != "A pool for tests" {
		t.Errorf("expected description, got %q", p.Description)
	}
	if p.DrainStartedAt != "2026-03-26T10:00:00Z" {
		t.Errorf("expected drain_started_at, got %q", p.DrainStartedAt)
	}
	if p.DrainTimeoutSeconds != 600 {
		t.Errorf("expected drain_timeout_seconds=600, got %d", p.DrainTimeoutSeconds)
	}
}

func TestParsePoolsConfigOldFormatBackwardCompat(t *testing.T) {
	body := []byte("topics:\n  job.legacy: legacy-pool\npools:\n  legacy-pool:\n    requires: []\n")
	cfg, err := ParsePoolsConfig(body)
	if err != nil {
		t.Fatalf("old format should still parse: %v", err)
	}
	p := cfg.Pools["legacy-pool"]
	if p.Status != "" {
		t.Errorf("expected empty status for old format, got %q", p.Status)
	}
	if p.EffectiveStatus() != PoolStatusActive {
		t.Errorf("expected EffectiveStatus()=active, got %q", p.EffectiveStatus())
	}
}

func TestEffectiveStatus(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"", PoolStatusActive},
		{"active", PoolStatusActive},
		{"draining", PoolStatusDraining},
		{"inactive", PoolStatusInactive},
	}
	for _, tt := range tests {
		p := PoolConfig{Status: tt.status}
		if got := p.EffectiveStatus(); got != tt.want {
			t.Errorf("EffectiveStatus(%q) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestLoadPoolConfigErrors(t *testing.T) {
	if _, err := LoadPoolConfig("nope.yaml"); err == nil {
		t.Fatalf("expected error for missing file")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(path, []byte("topics: {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadPoolConfig(path); err == nil {
		t.Fatalf("expected error for empty topics")
	}
}
