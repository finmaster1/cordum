package claude

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
)

// maxManagedSettingsFileBytes caps the size we will read from a managed
// settings file. Operators upload these via MDM/Jamf/Intune; legitimate
// payloads are well under 50 KiB. The 1 MiB ceiling protects the verify
// path from OOM if an attacker swaps the file for an arbitrarily large one.
const maxManagedSettingsFileBytes int64 = 1 << 20

// Severity strings exported alongside ManagedSettingsDrift so consumers can
// branch on them without importing internal constants.
const (
	managedSettingsDriftCritical = "critical"
	managedSettingsDriftHigh     = "high"
)

// ManagedSettingsDrift describes a single invariant violation discovered by
// VerifyManagedSettings. Field names use dotted/bracketed paths
// ("hooks.PreToolUse[0].command") to point operators at the broken node.
type ManagedSettingsDrift struct {
	Field    string `json:"field"`
	Got      string `json:"got"`
	Want     string `json:"want"`
	Severity string `json:"severity"`
}

// ManagedSettingsVerifyResult is the outcome of a single verify call. OK is
// true only when Drifts is empty. Source is opaque metadata describing where
// the verified bytes came from (e.g. "managed-settings.json"); callers use
// it for diagnostics only.
type ManagedSettingsVerifyResult struct {
	OK     bool                   `json:"ok"`
	Drifts []ManagedSettingsDrift `json:"drifts"`
	Source string                 `json:"source"`
}

// VerifyManagedSettings parses a managed-settings.json payload and reports
// every Cordum Edge invariant violation. A nil error indicates the payload
// was structurally valid JSON; check ManagedSettingsVerifyResult.OK to
// learn whether the contents matched the enterprise contract.
func VerifyManagedSettings(data []byte) (ManagedSettingsVerifyResult, error) {
	var doc managedSettingsDocument
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		// Allow extra/unknown fields by retrying without strict mode so a
		// future Claude Code update that introduces a new optional key does
		// not break verify; only structural decode errors propagate.
		dec2 := json.NewDecoder(strings.NewReader(string(data)))
		if err2 := dec2.Decode(&doc); err2 != nil {
			return ManagedSettingsVerifyResult{}, fmt.Errorf("parse managed-settings.json: %w", err2)
		}
	}
	drifts := collectManagedSettingsDrifts(doc, data)
	return ManagedSettingsVerifyResult{
		OK:     len(drifts) == 0,
		Drifts: drifts,
		Source: "managed-settings.json",
	}, nil
}

// VerifyManagedSettingsFromPath opens path with a 1 MiB cap and runs
// VerifyManagedSettings on the contents. Returns a non-nil error for IO
// failures, oversized files, or unparseable JSON.
func VerifyManagedSettingsFromPath(path string) (ManagedSettingsVerifyResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return ManagedSettingsVerifyResult{}, fmt.Errorf("open managed-settings.json: %w", err)
	}
	defer func() { _ = f.Close() }()
	limited := io.LimitReader(f, maxManagedSettingsFileBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return ManagedSettingsVerifyResult{}, fmt.Errorf("read managed-settings.json: %w", err)
	}
	if int64(len(data)) > maxManagedSettingsFileBytes {
		return ManagedSettingsVerifyResult{}, fmt.Errorf("managed-settings.json exceeds %d bytes", maxManagedSettingsFileBytes)
	}
	res, err := VerifyManagedSettings(data)
	if err != nil {
		return ManagedSettingsVerifyResult{}, err
	}
	res.Source = path
	return res, nil
}

// requiredManagedHookEvents is the canonical set of hook events that must be
// present (each with at least one non-empty command). Order is preserved for
// deterministic drift reporting.
var requiredManagedHookEvents = []string{
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"UserPromptSubmit",
	"ConfigChange",
	"FileChanged",
}

// reservedManagedEnvKeys lists env vars that MUST NOT appear in managed
// settings. The bundle generator never emits them; their presence indicates
// an operator hand-edited a secret into the file.
var reservedManagedEnvKeys = []string{
	"CORDUM_AGENTD_HOOK_NONCE",
	"ANTHROPIC_API_KEY",
	"CORDUM_API_KEY",
}

// sensitiveSerializedMarkers are byte sequences that indicate a leaked
// secret regardless of which JSON key carries them. Mirror upstream
// containsSensitiveValue + add explicit auth-header marker so a
// stringified Authorization header in a debug env var still trips.
var sensitiveSerializedMarkers = []string{
	"sk-",
	"ghp_",
	"Authorization: Bearer",
	"AKIA",
}

func collectManagedSettingsDrifts(doc managedSettingsDocument, raw []byte) []ManagedSettingsDrift {
	var drifts []ManagedSettingsDrift
	if !doc.AllowManagedHooksOnly {
		drifts = append(drifts, ManagedSettingsDrift{Field: "allowManagedHooksOnly", Got: "false", Want: "true", Severity: managedSettingsDriftCritical})
	}
	if !doc.AllowManagedMcpServersOnly {
		drifts = append(drifts, ManagedSettingsDrift{Field: "allowManagedMcpServersOnly", Got: "false", Want: "true", Severity: managedSettingsDriftCritical})
	}
	if doc.DisableBypassPermissions != "disable" {
		drifts = append(drifts, ManagedSettingsDrift{
			Field:    "disableBypassPermissionsMode",
			Got:      redactDiagnostic(doc.DisableBypassPermissions),
			Want:     "disable",
			Severity: managedSettingsDriftCritical,
		})
	}
	drifts = append(drifts, requiredHookEventDrifts(doc.Hooks)...)
	drifts = append(drifts, requiredEnvDrifts(doc.Env)...)
	drifts = append(drifts, reservedEnvDrifts(doc.Env)...)
	drifts = append(drifts, agentdURLDrifts(doc.Env)...)
	drifts = append(drifts, serializedMarkerDrifts(raw)...)
	return drifts
}

func requiredHookEventDrifts(hooks map[string][]claudeHookSet) []ManagedSettingsDrift {
	var drifts []ManagedSettingsDrift
	for _, event := range requiredManagedHookEvents {
		sets, ok := hooks[event]
		if !ok || len(sets) == 0 {
			drifts = append(drifts, ManagedSettingsDrift{
				Field:    "hooks." + event,
				Got:      "missing",
				Want:     "at least one command hook",
				Severity: managedSettingsDriftCritical,
			})
			continue
		}
		if !hasNonEmptyCommand(sets) {
			drifts = append(drifts, ManagedSettingsDrift{
				Field:    "hooks." + event + "[*].command",
				Got:      "empty",
				Want:     "non-empty command",
				Severity: managedSettingsDriftCritical,
			})
		}
	}
	return drifts
}

func hasNonEmptyCommand(sets []claudeHookSet) bool {
	for _, set := range sets {
		for _, hook := range set.Hooks {
			if strings.TrimSpace(hook.Command) != "" {
				return true
			}
		}
	}
	return false
}

func requiredEnvDrifts(env map[string]string) []ManagedSettingsDrift {
	required := []struct {
		key      string
		want     string
		severity string
	}{
		{key: "CORDUM_AGENTD_FAIL_CLOSED", want: "true", severity: managedSettingsDriftCritical},
		{key: "CORDUM_EDGE_MANAGED_POLICY_MODE", want: "enterprise-strict", severity: managedSettingsDriftCritical},
		{key: "CORDUM_EDGE_MANAGED_HOOKS_ONLY", want: "true", severity: managedSettingsDriftCritical},
	}
	var drifts []ManagedSettingsDrift
	for _, r := range required {
		got := env[r.key]
		if got != r.want {
			drifts = append(drifts, ManagedSettingsDrift{
				Field:    "env." + r.key,
				Got:      redactDiagnostic(got),
				Want:     r.want,
				Severity: r.severity,
			})
		}
	}
	return drifts
}

func reservedEnvDrifts(env map[string]string) []ManagedSettingsDrift {
	var drifts []ManagedSettingsDrift
	for _, key := range reservedManagedEnvKeys {
		if _, ok := env[key]; ok {
			drifts = append(drifts, ManagedSettingsDrift{
				Field:    "env." + key,
				Got:      "present",
				Want:     "absent",
				Severity: managedSettingsDriftHigh,
			})
		}
	}
	// Catch any env value that contains a sensitive marker (covers
	// debug/custom keys not in the reserved list).
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if isReservedEnvKey(key) {
			continue
		}
		if containsSensitiveValue(env[key]) {
			drifts = append(drifts, ManagedSettingsDrift{
				Field:    "env." + key,
				Got:      "[REDACTED]",
				Want:     "non-sensitive value",
				Severity: managedSettingsDriftHigh,
			})
		}
	}
	return drifts
}

func isReservedEnvKey(key string) bool {
	return slices.Contains(reservedManagedEnvKeys, key)
}

func agentdURLDrifts(env map[string]string) []ManagedSettingsDrift {
	raw := strings.TrimSpace(env["CORDUM_AGENTD_URL"])
	if raw == "" {
		return []ManagedSettingsDrift{{
			Field:    "env.CORDUM_AGENTD_URL",
			Got:      "empty",
			Want:     "loopback hook URL",
			Severity: managedSettingsDriftCritical,
		}}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return []ManagedSettingsDrift{{
			Field:    "env.CORDUM_AGENTD_URL",
			Got:      redactDiagnostic(raw),
			Want:     "valid URL",
			Severity: managedSettingsDriftCritical,
		}}
	}
	if strings.TrimSpace(parsed.RawQuery) != "" {
		return []ManagedSettingsDrift{{
			Field:    "env.CORDUM_AGENTD_URL",
			Got:      "url with query string",
			Want:     "url without query string",
			Severity: managedSettingsDriftCritical,
		}}
	}
	// The hook URL must be a loopback http(s) endpoint pointed at the
	// per-host /v1/edge/hooks/claude path. A scheme-less value (e.g.
	// "agentd.example.com/v1/...") parses without error but reaches a
	// non-local network; a non-loopback host can be a SSRF target or a
	// silent man-in-the-middle for hook decisions. Enforce all three
	// invariants here so the verify pass refuses to ratify a config that
	// would route enforcement decisions off-box.
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return []ManagedSettingsDrift{{
			Field:    "env.CORDUM_AGENTD_URL",
			Got:      redactDiagnostic(raw),
			Want:     "http(s) loopback URL",
			Severity: managedSettingsDriftCritical,
		}}
	}
	host := parsed.Hostname()
	if !isLoopbackHookHost(host) {
		return []ManagedSettingsDrift{{
			Field:    "env.CORDUM_AGENTD_URL",
			Got:      redactDiagnostic(raw),
			Want:     "loopback host (127.0.0.1, ::1, or localhost)",
			Severity: managedSettingsDriftCritical,
		}}
	}
	if parsed.Path != "/v1/edge/hooks/claude" {
		return []ManagedSettingsDrift{{
			Field:    "env.CORDUM_AGENTD_URL",
			Got:      redactDiagnostic(raw),
			Want:     "path /v1/edge/hooks/claude",
			Severity: managedSettingsDriftCritical,
		}}
	}
	return nil
}

func isLoopbackHookHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}

func serializedMarkerDrifts(raw []byte) []ManagedSettingsDrift {
	body := string(raw)
	for _, marker := range sensitiveSerializedMarkers {
		if strings.Contains(body, marker) {
			return []ManagedSettingsDrift{{
				Field:    "serialized.sensitive_marker",
				Got:      marker,
				Want:     "no leaked secret markers",
				Severity: managedSettingsDriftHigh,
			}}
		}
	}
	return nil
}
