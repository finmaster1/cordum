package claude

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const goldenManagedSettingsPath = "testdata/settings/managed-settings.json"

func TestVerifyManagedSettingsPassesGolden(t *testing.T) {
	data, err := os.ReadFile(goldenManagedSettingsPath)
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	res, err := VerifyManagedSettings(data)
	if err != nil {
		t.Fatalf("VerifyManagedSettings returned error on golden: %v", err)
	}
	if !res.OK {
		t.Fatalf("golden fixture must verify OK; drifts=%+v", res.Drifts)
	}
	if len(res.Drifts) != 0 {
		t.Fatalf("golden fixture must report 0 drifts; got %d: %+v", len(res.Drifts), res.Drifts)
	}
}

func TestVerifyManagedSettingsDetectsAllInvariantViolations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		mutate    func(map[string]any)
		wantField string
	}{
		{
			name:      "allowManagedHooksOnly_false",
			mutate:    func(m map[string]any) { m["allowManagedHooksOnly"] = false },
			wantField: "allowManagedHooksOnly",
		},
		{
			name:      "allowManagedMcpServersOnly_false",
			mutate:    func(m map[string]any) { m["allowManagedMcpServersOnly"] = false },
			wantField: "allowManagedMcpServersOnly",
		},
		{
			name:      "disableBypassPermissionsMode_wrong_literal",
			mutate:    func(m map[string]any) { m["disableBypassPermissionsMode"] = "warn" },
			wantField: "disableBypassPermissionsMode",
		},
		{
			name: "missing_PreToolUse_hook",
			mutate: func(m map[string]any) {
				hooks, _ := m["hooks"].(map[string]any)
				delete(hooks, "PreToolUse")
			},
			wantField: "hooks.PreToolUse",
		},
		{
			name: "missing_PostToolUse_hook",
			mutate: func(m map[string]any) {
				hooks, _ := m["hooks"].(map[string]any)
				delete(hooks, "PostToolUse")
			},
			wantField: "hooks.PostToolUse",
		},
		{
			name: "missing_PostToolUseFailure_hook",
			mutate: func(m map[string]any) {
				hooks, _ := m["hooks"].(map[string]any)
				delete(hooks, "PostToolUseFailure")
			},
			wantField: "hooks.PostToolUseFailure",
		},
		{
			name: "missing_UserPromptSubmit_hook",
			mutate: func(m map[string]any) {
				hooks, _ := m["hooks"].(map[string]any)
				delete(hooks, "UserPromptSubmit")
			},
			wantField: "hooks.UserPromptSubmit",
		},
		{
			name: "missing_ConfigChange_hook",
			mutate: func(m map[string]any) {
				hooks, _ := m["hooks"].(map[string]any)
				delete(hooks, "ConfigChange")
			},
			wantField: "hooks.ConfigChange",
		},
		{
			name: "missing_FileChanged_hook",
			mutate: func(m map[string]any) {
				hooks, _ := m["hooks"].(map[string]any)
				delete(hooks, "FileChanged")
			},
			wantField: "hooks.FileChanged",
		},
		{
			name: "env_fail_closed_not_true",
			mutate: func(m map[string]any) {
				env, _ := m["env"].(map[string]any)
				env["CORDUM_AGENTD_FAIL_CLOSED"] = "false"
			},
			wantField: "env.CORDUM_AGENTD_FAIL_CLOSED",
		},
		{
			name: "env_managed_policy_mode_wrong",
			mutate: func(m map[string]any) {
				env, _ := m["env"].(map[string]any)
				env["CORDUM_EDGE_MANAGED_POLICY_MODE"] = "observe"
			},
			wantField: "env.CORDUM_EDGE_MANAGED_POLICY_MODE",
		},
		{
			name: "env_managed_hooks_only_not_true",
			mutate: func(m map[string]any) {
				env, _ := m["env"].(map[string]any)
				env["CORDUM_EDGE_MANAGED_HOOKS_ONLY"] = "false"
			},
			wantField: "env.CORDUM_EDGE_MANAGED_HOOKS_ONLY",
		},
		{
			name: "env_agentd_url_empty",
			mutate: func(m map[string]any) {
				env, _ := m["env"].(map[string]any)
				env["CORDUM_AGENTD_URL"] = ""
			},
			wantField: "env.CORDUM_AGENTD_URL",
		},
		{
			name: "env_agentd_url_carries_query",
			mutate: func(m map[string]any) {
				env, _ := m["env"].(map[string]any)
				env["CORDUM_AGENTD_URL"] = "http://127.0.0.1:8765/v1/edge/hooks/claude?token=abc"
			},
			wantField: "env.CORDUM_AGENTD_URL",
		},
		{
			name: "env_carries_hook_nonce",
			mutate: func(m map[string]any) {
				env, _ := m["env"].(map[string]any)
				env["CORDUM_AGENTD_HOOK_NONCE"] = "deadbeef"
			},
			wantField: "env.CORDUM_AGENTD_HOOK_NONCE",
		},
		{
			name: "env_carries_anthropic_api_key",
			mutate: func(m map[string]any) {
				env, _ := m["env"].(map[string]any)
				env["ANTHROPIC_API_KEY"] = "sk-test-not-real"
			},
			wantField: "env.ANTHROPIC_API_KEY",
		},
		{
			name: "serialized_form_carries_bearer_marker",
			mutate: func(m map[string]any) {
				env, _ := m["env"].(map[string]any)
				env["CUSTOM_DEBUG"] = "Authorization: Bearer leaked-token"
			},
			wantField: "serialized.sensitive_marker",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bundle := mustGenerateGoldenBundle(t)
			var doc map[string]any
			if err := json.Unmarshal(bundle.ManagedSettingsJSON, &doc); err != nil {
				t.Fatalf("unmarshal generated managed settings: %v", err)
			}
			tc.mutate(doc)
			mutated, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				t.Fatalf("marshal mutated doc: %v", err)
			}
			res, err := VerifyManagedSettings(mutated)
			if err != nil {
				t.Fatalf("VerifyManagedSettings returned parse error: %v", err)
			}
			if res.OK {
				t.Fatalf("expected drift for %q but result was OK", tc.wantField)
			}
			if !driftsContainField(res.Drifts, tc.wantField) {
				t.Fatalf("expected drift on field %q; got drifts=%+v", tc.wantField, res.Drifts)
			}
		})
	}
}

func TestVerifyManagedSettingsCrossPlatformPathVariants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		opts ManagedSettingsOptions
	}{
		{
			name: "linux",
			opts: ManagedSettingsOptions{Platform: "linux", HookCommand: "/usr/local/bin/cordum-hook"},
		},
		{
			name: "macos path with spaces",
			opts: ManagedSettingsOptions{Platform: "darwin", HookCommand: "/Applications/Cordum Edge/cordum-hook"},
		},
		{
			name: "windows path with spaces",
			opts: ManagedSettingsOptions{Platform: "windows", HookCommand: `C:\Program Files\Cordum\cordum-hook.exe`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.opts.HookTimeout = DefaultHookTimeout
			tc.opts.AgentdURL = "http://127.0.0.1:8765/v1/edge/hooks/claude"
			tc.opts.MCPGatewayURL = "https://mcp.cordum.example/mcp"
			tc.opts.LLMProxyBaseURL = "https://llm-proxy.cordum.example"
			tc.opts.APIKeyHelperCommand = "/opt/cordum/bin/cordum-agentd claude api-key-helper"
			bundle, err := GenerateManagedSettingsTemplate(tc.opts)
			if err != nil {
				t.Fatalf("generate %s bundle: %v", tc.name, err)
			}
			res, err := VerifyManagedSettings(bundle.ManagedSettingsJSON)
			if err != nil {
				t.Fatalf("verify %s bundle: %v", tc.name, err)
			}
			if !res.OK {
				t.Fatalf("verify %s expected OK; drifts=%+v", tc.name, res.Drifts)
			}
		})
	}
}

func TestVerifyManagedSettingsFromPathRejectsMissingFile(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	if _, err := VerifyManagedSettingsFromPath(missing); err == nil {
		t.Fatalf("expected error for missing path %q, got nil", missing)
	}
}

func TestVerifyManagedSettingsFromPathRejectsHugeFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.json")
	// 2 MiB of bytes — exceeds the 1 MiB io.LimitReader cap.
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 2*1024*1024), 0o600); err != nil {
		t.Fatalf("write huge fixture: %v", err)
	}
	if _, err := VerifyManagedSettingsFromPath(path); err == nil {
		t.Fatalf("expected error for oversized file; got nil")
	}
}

func TestVerifyManagedSettingsRejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	if _, err := VerifyManagedSettings([]byte("{ not valid json")); err == nil {
		t.Fatalf("expected parse error for malformed JSON; got nil")
	}
}

func mustGenerateGoldenBundle(t *testing.T) ManagedSettingsBundle {
	t.Helper()
	bundle, err := GenerateManagedSettingsTemplate(ManagedSettingsOptions{
		HookCommand:         "/opt/cordum/bin/cordum-hook",
		HookTimeout:         5 * time.Second,
		AgentdURL:           "http://127.0.0.1:8765/v1/edge/hooks/claude",
		MCPGatewayURL:       "https://mcp.cordum.example/mcp",
		LLMProxyBaseURL:     "https://llm-proxy.cordum.example",
		APIKeyHelperCommand: "/opt/cordum/bin/cordum-agentd claude api-key-helper",
		Platform:            "linux",
	})
	if err != nil {
		t.Fatalf("generate golden bundle: %v", err)
	}
	return bundle
}

func driftsContainField(drifts []ManagedSettingsDrift, field string) bool {
	for _, d := range drifts {
		if d.Field == field {
			return true
		}
		// Allow callers to assert a prefix (e.g. "hooks.PreToolUse"
		// matches "hooks.PreToolUse[0].command") so future invariants
		// can use sub-paths without breaking existing assertions.
		if strings.HasPrefix(d.Field, field+".") || strings.HasPrefix(d.Field, field+"[") {
			return true
		}
	}
	return false
}
