package keychain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// BootstrapMode selects the LoadSecret failure policy. ModeStrict is the
// production default; ModeDev relaxes the keychain requirement so a
// developer can run cordum-agentd against a checkout without provisioning
// secrets into the OS keychain first.
type BootstrapMode int

const (
	// ModeStrict requires every secret to come from the keychain. A miss
	// or backend-unavailable error surfaces verbatim — agentd must fail
	// closed when it cannot source its boot-time credentials securely.
	ModeStrict BootstrapMode = iota

	// ModeDev permits an env-variable fallback when the keychain has no
	// entry for the requested secret. A structured warning is emitted on
	// fallback (with the secret name but NEVER the value) so an operator
	// can spot accidental dev-mode runs in production.
	ModeDev
)

// String implements fmt.Stringer for structured-log attribute clarity.
func (m BootstrapMode) String() string {
	switch m {
	case ModeStrict:
		return "strict"
	case ModeDev:
		return "dev"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}

// LoadSecret resolves secretName from kr, applying the policy implied by
// mode. The function NEVER logs the returned value; structured logs
// include only the secret name, the mode, and the resolved source
// ("keychain" / "env-fallback").
//
// ModeStrict semantics:
//   - keychain hit                          → (value, nil)
//   - keychain ErrKeyringNotFound           → ("", ErrKeyringNotFound)
//   - keychain ErrKeyringUnavailable/other  → ("", wrapped error)
//   - envFallback is ignored in strict mode and never read.
//
// ModeDev semantics:
//   - keychain hit                          → (value, nil)
//   - keychain ErrKeyringNotFound + envFallback != ""
//     → (envFallback, nil) + warn log
//   - keychain ErrKeyringNotFound + envFallback == ""
//     → ("", ErrKeyringNotFound)
//   - keychain ErrKeyringUnavailable + envFallback != ""
//     → (envFallback, nil) + warn log
//   - keychain ErrKeyringUnavailable + envFallback == ""
//     → ("", wrapped error)
//
// secretName == "" returns ErrKeyringNotFound without touching kr; this
// makes "this secret is not provisioned in this build" a single error
// path callers can switch on.
func LoadSecret(
	ctx context.Context,
	kr Keyring,
	mode BootstrapMode,
	envFallback string,
	secretName string,
	logger *slog.Logger,
) (string, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if secretName == "" {
		return "", ErrKeyringNotFound
	}
	if kr == nil {
		if mode == ModeStrict {
			return "", fmt.Errorf("%w: nil keychain provider", ErrKeyringUnavailable)
		}
		if envFallback != "" {
			warnEnvFallback(logger, secretName, mode, "nil keychain provider")
			return envFallback, nil
		}
		return "", ErrKeyringNotFound
	}

	value, err := kr.Get(ctx, secretName)
	switch {
	case err == nil:
		logger.Debug(
			"keychain.load",
			slog.String("secret_name", secretName),
			slog.String("source", "keychain"),
			slog.String("mode", mode.String()),
		)
		return value, nil

	case errors.Is(err, ErrKeyringNotFound):
		if mode == ModeStrict {
			logger.Error(
				"keychain.load.miss",
				slog.String("secret_name", secretName),
				slog.String("mode", mode.String()),
			)
			return "", ErrKeyringNotFound
		}
		if envFallback != "" {
			warnEnvFallback(logger, secretName, mode, "keychain miss")
			return envFallback, nil
		}
		return "", ErrKeyringNotFound

	default:
		if mode == ModeStrict {
			logger.Error(
				"keychain.load.unavailable",
				slog.String("secret_name", secretName),
				slog.String("mode", mode.String()),
				slog.String("reason", err.Error()),
			)
			return "", err
		}
		if envFallback != "" {
			warnEnvFallback(logger, secretName, mode, "keychain unavailable: "+err.Error())
			return envFallback, nil
		}
		return "", err
	}
}

func warnEnvFallback(logger *slog.Logger, secretName string, mode BootstrapMode, reason string) {
	logger.Warn(
		"keychain.env_fallback",
		slog.String("secret_name", secretName),
		slog.String("source", "env-fallback"),
		slog.String("mode", mode.String()),
		slog.String("reason", reason),
	)
}
