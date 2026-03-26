package pools

import (
	"strings"
	"testing"

	"github.com/cordum/cordum/core/infra/config"
)

func TestValidatePoolName(t *testing.T) {
	valid := []string{"abc", "my-pool", "pool-123", "a-b", "abc-def-ghi"}
	for _, name := range valid {
		if err := ValidatePoolName(name); err != nil {
			t.Errorf("ValidatePoolName(%q) unexpected error: %v", name, err)
		}
	}

	invalid := []struct {
		name string
		want string
	}{
		{"ab", "3-63 characters"},
		{strings.Repeat("a", 64), "3-63 characters"},
		{"-abc", "lowercase alphanumeric"},
		{"abc-", "lowercase alphanumeric"},
		{"a--b", "consecutive hyphens"},
		{"ABC", "lowercase alphanumeric"},
		{"my pool", "lowercase alphanumeric"},
		{"my_pool", "lowercase alphanumeric"},
	}
	for _, tt := range invalid {
		err := ValidatePoolName(tt.name)
		if err == nil {
			t.Errorf("ValidatePoolName(%q) expected error containing %q", tt.name, tt.want)
			continue
		}
		if !strings.Contains(err.Error(), tt.want) {
			t.Errorf("ValidatePoolName(%q) error %q should contain %q", tt.name, err, tt.want)
		}
	}
}

func TestValidateTopicName(t *testing.T) {
	valid := []string{"job.default", "job.b2b.orchestrate", "job.demo-bank.transfer", "job.test_topic.sub"}
	for _, topic := range valid {
		if err := ValidateTopicName(topic); err != nil {
			t.Errorf("ValidateTopicName(%q) unexpected error: %v", topic, err)
		}
	}

	invalid := []string{"", "not-a-job", "job", "sys.heartbeat", "JOB.test"}
	for _, topic := range invalid {
		if err := ValidateTopicName(topic); err == nil {
			t.Errorf("ValidateTopicName(%q) expected error", topic)
		}
	}
}

func TestValidatePoolConfig(t *testing.T) {
	// Valid configs
	for _, cfg := range []config.PoolConfig{
		{},
		{Status: "active"},
		{Status: "draining", Requires: []string{"docker"}},
		{Status: "inactive", Description: "old pool"},
	} {
		if err := ValidatePoolConfig(cfg); err != nil {
			t.Errorf("ValidatePoolConfig(%+v) unexpected error: %v", cfg, err)
		}
	}

	// Invalid status
	if err := ValidatePoolConfig(config.PoolConfig{Status: "bogus"}); err == nil {
		t.Error("expected error for invalid status")
	}

	// Blank requires entry
	if err := ValidatePoolConfig(config.PoolConfig{Requires: []string{"docker", ""}}); err == nil {
		t.Error("expected error for blank requires entry")
	}
}

func TestValidatePoolCreate(t *testing.T) {
	existing := map[string]config.PoolConfig{
		"pool-a": {Status: "active"},
	}

	// Create new pool — should succeed
	if err := ValidatePoolCreate("pool-b", config.PoolConfig{}, existing); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Duplicate — should fail
	err := ValidatePoolCreate("pool-a", config.PoolConfig{}, existing)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestValidatePoolUpdate(t *testing.T) {
	existing := map[string]config.PoolConfig{
		"pool-a": {Status: "active"},
	}

	// Update existing — should succeed
	if err := ValidatePoolUpdate("pool-a", config.PoolConfig{Status: "draining"}, existing); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Update non-existent — should fail
	err := ValidatePoolUpdate("pool-z", config.PoolConfig{}, existing)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestValidatePoolDelete(t *testing.T) {
	existing := map[string]config.PoolConfig{
		"pool-a": {},
		"pool-b": {},
	}
	topics := map[string][]string{
		"job.test": {"pool-a"},
	}

	// Delete pool with mapping — should fail
	err := ValidatePoolDelete("pool-a", existing, topics, false)
	if err == nil || !strings.Contains(err.Error(), "active topic mapping") {
		t.Errorf("expected mapping error, got: %v", err)
	}

	// Delete pool with mapping + force — should succeed
	if err := ValidatePoolDelete("pool-a", existing, topics, true); err != nil {
		t.Errorf("force delete unexpected error: %v", err)
	}

	// Delete pool without mapping — should succeed
	if err := ValidatePoolDelete("pool-b", existing, topics, false); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Delete non-existent — should fail
	err = ValidatePoolDelete("pool-z", existing, topics, false)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestValidateTopicPoolMapping(t *testing.T) {
	pools := map[string]config.PoolConfig{
		"pool-a": {},
		"pool-b": {},
	}

	// Valid mapping
	if err := ValidateTopicPoolMapping("job.test", []string{"pool-a", "pool-b"}, pools); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Empty pool list
	err := ValidateTopicPoolMapping("job.test", nil, pools)
	if err == nil || !strings.Contains(err.Error(), "at least one pool") {
		t.Errorf("expected 'at least one pool' error, got: %v", err)
	}

	// Non-existent pool
	err = ValidateTopicPoolMapping("job.test", []string{"pool-z"}, pools)
	if err == nil || !strings.Contains(err.Error(), "non-existent pool") {
		t.Errorf("expected 'non-existent pool' error, got: %v", err)
	}

	// Invalid topic
	err = ValidateTopicPoolMapping("bad-topic", []string{"pool-a"}, pools)
	if err == nil || !strings.Contains(err.Error(), "job.*") {
		t.Errorf("expected topic pattern error, got: %v", err)
	}
}
