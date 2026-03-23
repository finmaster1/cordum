package workflow

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

// ExprSandboxConfig controls expression evaluation limits.
type ExprSandboxConfig struct {
	// MaxExprLength is the maximum expression string length in bytes.
	// Expressions exceeding this are rejected before parsing.
	MaxExprLength int

	// MaxResultItems caps ForEach and array-producing expressions.
	MaxResultItems int

	// EvalTimeout is the maximum time for a single expression evaluation.
	EvalTimeout time.Duration

	// AllowedVars are the top-level variable names accessible in expressions.
	AllowedVars []string
}

// DefaultSandboxConfig returns production-safe defaults.
func DefaultSandboxConfig() ExprSandboxConfig {
	return ExprSandboxConfig{
		MaxExprLength:  4096,
		MaxResultItems: 10000,
		EvalTimeout:    100 * time.Millisecond,
		AllowedVars:    []string{"ctx", "input", "steps", "item", "env"},
	}
}

// SandboxConfigFromEnv loads sandbox configuration from environment variables,
// falling back to defaults for any unset or invalid values.
func SandboxConfigFromEnv() ExprSandboxConfig {
	cfg := DefaultSandboxConfig()

	if v := os.Getenv("WORKFLOW_EXPR_MAX_LENGTH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxExprLength = n
		} else {
			slog.Warn("invalid WORKFLOW_EXPR_MAX_LENGTH, using default", "value", v, "default", cfg.MaxExprLength)
		}
	}

	if v := os.Getenv("WORKFLOW_EXPR_TIMEOUT_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.EvalTimeout = time.Duration(n) * time.Millisecond
		} else {
			slog.Warn("invalid WORKFLOW_EXPR_TIMEOUT_MS, using default", "value", v, "default", cfg.EvalTimeout)
		}
	}

	if v := os.Getenv("WORKFLOW_EXPR_MAX_RESULT_ITEMS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxResultItems = n
		} else {
			slog.Warn("invalid WORKFLOW_EXPR_MAX_RESULT_ITEMS, using default", "value", v, "default", cfg.MaxResultItems)
		}
	}

	return cfg
}

// blockedPatterns are string literals that should never appear in workflow
// expressions. These indicate probing or injection attempts.
var blockedPatterns = []string{
	"import", "require", "reflect", "unsafe", "runtime", "os.exit",
	"exec", "__proto__", "constructor", "prototype",
}

// defaultSandbox is the package-level sandbox config, initialized once.
var defaultSandbox = DefaultSandboxConfig()

// SetSandboxConfig replaces the package-level sandbox configuration.
func SetSandboxConfig(cfg ExprSandboxConfig) {
	defaultSandbox = cfg
}
