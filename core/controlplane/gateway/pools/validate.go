package pools

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cordum/cordum/core/infra/config"
)

var (
	poolNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)
	topicRe    = regexp.MustCompile(`^job\.[a-zA-Z0-9_-]+(\.[a-zA-Z0-9_*-]+)*$`)
)

var validStatuses = map[string]bool{
	"":                        true,
	config.PoolStatusActive:   true,
	config.PoolStatusDraining: true,
	config.PoolStatusInactive: true,
}

// ValidatePoolName checks that a pool name is lowercase alphanumeric with
// hyphens, 3-63 characters, no leading/trailing hyphens, no double hyphens.
func ValidatePoolName(name string) error {
	if len(name) < 3 || len(name) > 63 {
		return fmt.Errorf("pool name must be 3-63 characters, got %d", len(name))
	}
	if strings.Contains(name, "--") {
		return fmt.Errorf("pool name must not contain consecutive hyphens: %q", name)
	}
	if !poolNameRe.MatchString(name) {
		return fmt.Errorf("pool name must be lowercase alphanumeric with hyphens: %q", name)
	}
	return nil
}

// ValidateTopicName checks that a topic follows the job.{segment} pattern.
func ValidateTopicName(topic string) error {
	if topic == "" {
		return fmt.Errorf("topic name must not be empty")
	}
	if !topicRe.MatchString(topic) {
		return fmt.Errorf("topic must match job.* pattern: %q", topic)
	}
	return nil
}

// ValidatePoolConfig checks that a pool config has valid field values.
func ValidatePoolConfig(cfg config.PoolConfig) error {
	if !validStatuses[cfg.Status] {
		return fmt.Errorf("invalid pool status %q: must be active, draining, or inactive", cfg.Status)
	}
	for _, req := range cfg.Requires {
		if strings.TrimSpace(req) == "" {
			return fmt.Errorf("pool requires entry must not be blank")
		}
	}
	if cfg.DrainTimeoutSeconds < 0 {
		return fmt.Errorf("drain_timeout_seconds must be non-negative, got %d", cfg.DrainTimeoutSeconds)
	}
	return nil
}

// ValidatePoolCreate checks that a pool does not already exist.
func ValidatePoolCreate(name string, cfg config.PoolConfig, existing map[string]config.PoolConfig) error {
	if err := ValidatePoolName(name); err != nil {
		return err
	}
	if err := ValidatePoolConfig(cfg); err != nil {
		return err
	}
	if _, exists := existing[name]; exists {
		return fmt.Errorf("pool %q already exists", name)
	}
	return nil
}

// ValidatePoolUpdate checks that a pool exists before updating.
func ValidatePoolUpdate(name string, cfg config.PoolConfig, existing map[string]config.PoolConfig) error {
	if err := ValidatePoolName(name); err != nil {
		return err
	}
	if err := ValidatePoolConfig(cfg); err != nil {
		return err
	}
	if _, exists := existing[name]; !exists {
		return fmt.Errorf("pool %q not found", name)
	}
	return nil
}

// ValidatePoolDelete checks that a pool has no active topic mappings.
// When force is true, the mapping check is skipped.
func ValidatePoolDelete(name string, existing map[string]config.PoolConfig, topicMappings map[string][]string, force bool) error {
	if err := ValidatePoolName(name); err != nil {
		return err
	}
	if _, exists := existing[name]; !exists {
		return fmt.Errorf("pool %q not found", name)
	}
	if force {
		return nil
	}
	for topic, pools := range topicMappings {
		if slices.Contains(pools, name) {
			return fmt.Errorf("pool %q has active topic mapping %q: drain or remap first, or use force", name, topic)
		}
	}
	return nil
}

// ValidateTopicPoolMapping checks that all referenced pools exist.
func ValidateTopicPoolMapping(topic string, poolNames []string, existingPools map[string]config.PoolConfig) error {
	if err := ValidateTopicName(topic); err != nil {
		return err
	}
	if len(poolNames) == 0 {
		return fmt.Errorf("topic %q must map to at least one pool", topic)
	}
	for _, name := range poolNames {
		if _, exists := existingPools[name]; !exists {
			return fmt.Errorf("topic %q references non-existent pool %q", topic, name)
		}
	}
	return nil
}
