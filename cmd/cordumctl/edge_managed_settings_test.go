package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testManagedMCPGatewayURL = "https://mcp.cordum.example/mcp"
	testManagedLLMProxyURL   = "https://llm-proxy.cordum.example"
	testManagedAPIKeyHelper  = "/opt/cordum/bin/cordum-agentd claude api-key-helper"
)

func TestManagedSettingsExportWritesBothFiles(t *testing.T) {
	dir := t.TempDir()
	code, stdout, stderr := runManagedSettingsForTest(t, exportArgs(dir)...)
	if code != 0 {
		t.Fatalf("export exit=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, name := range []string{"managed-settings.json", "managed-mcp.json"} {
		path := filepath.Join(dir, name)
		if !strings.Contains(stdout, "wrote "+filepath.ToSlash(path)) && !strings.Contains(stdout, "wrote "+path) {
			t.Fatalf("stdout missing wrote line for %s; got %q", name, stdout)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Size() == 0 {
			t.Fatalf("%s is empty", path)
		}
	}
}

func TestManagedSettingsExportRefusesOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runManagedSettingsForTest(t, exportArgs(dir)...); code != 0 {
		t.Fatalf("first export failed: code=%d stderr=%s", code, stderr)
	}
	code, _, stderr := runManagedSettingsForTest(t, exportArgs(dir)...)
	if code != 2 {
		t.Fatalf("second export expected exit 2 (refuse-overwrite); got %d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "refusing to overwrite") {
		t.Fatalf("stderr missing refuse-overwrite message; got %q", stderr)
	}
}

func TestManagedSettingsExportAllowsOverwriteWithForce(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runManagedSettingsForTest(t, exportArgs(dir)...); code != 0 {
		t.Fatalf("first export failed: code=%d stderr=%s", code, stderr)
	}
	args := append(exportArgs(dir), "--force")
	code, _, stderr := runManagedSettingsForTest(t, args...)
	if code != 0 {
		t.Fatalf("force overwrite expected exit 0; got %d stderr=%s", code, stderr)
	}
}

func TestManagedSettingsExportRejectsSensitiveHookCommand(t *testing.T) {
	dir := t.TempDir()
	args := exportArgs(dir)
	args = replaceFlag(args, "--hook-command", "/opt/cordum/bin/cordum-hook --token sk-test-not-real-secret")
	code, _, stderr := runManagedSettingsForTest(t, args...)
	if code != 2 {
		t.Fatalf("sensitive hook command expected exit 2; got %d stderr=%s", code, stderr)
	}
	if strings.Contains(stderr, "sk-test-not-real-secret") {
		t.Fatalf("stderr leaked the literal sensitive value: %q", stderr)
	}
	if !strings.Contains(stderr, "sensitive") {
		t.Fatalf("stderr missing sensitive-rejection message; got %q", stderr)
	}
}

func TestManagedSettingsExportRequiresMandatoryFlags(t *testing.T) {
	cases := []struct {
		name   string
		remove string
	}{
		{name: "missing_output", remove: "--output"},
		{name: "missing_mcp_gateway_url", remove: "--mcp-gateway-url"},
		{name: "missing_llm_proxy_base_url", remove: "--llm-proxy-base-url"},
		{name: "missing_api_key_helper_command", remove: "--api-key-helper-command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			args := removeFlag(exportArgs(dir), tc.remove)
			code, _, stderr := runManagedSettingsForTest(t, args...)
			if code != 2 {
				t.Fatalf("expected exit 2; got %d stderr=%s", code, stderr)
			}
		})
	}
}

func TestManagedSettingsVerifyPassesGolden(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runManagedSettingsForTest(t, exportArgs(dir)...); code != 0 {
		t.Fatalf("export prep failed: code=%d stderr=%s", code, stderr)
	}
	path := filepath.Join(dir, "managed-settings.json")
	code, stdout, stderr := runManagedSettingsForTest(t, "verify", "--path", path)
	if code != 0 {
		t.Fatalf("verify exit=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "ok:") {
		t.Fatalf("stdout missing ok marker; got %q", stdout)
	}
}

func TestManagedSettingsVerifyJSONFlagEmitsEnvelope(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runManagedSettingsForTest(t, exportArgs(dir)...); code != 0 {
		t.Fatalf("export prep failed: code=%d stderr=%s", code, stderr)
	}
	path := filepath.Join(dir, "managed-settings.json")
	code, stdout, _ := runManagedSettingsForTest(t, "verify", "--path", path, "--json")
	if code != 0 {
		t.Fatalf("verify --json exit=%d stdout=%s", code, stdout)
	}
	var env struct {
		OK     bool `json:"ok"`
		Drifts []struct {
			Field    string `json:"field"`
			Got      string `json:"got"`
			Want     string `json:"want"`
			Severity string `json:"severity"`
		} `json:"drifts"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("decode envelope: %v; stdout=%q", err, stdout)
	}
	if !env.OK {
		t.Fatalf("envelope OK should be true for golden; got drifts=%+v", env.Drifts)
	}
	if env.Source == "" {
		t.Fatalf("envelope Source must be populated; got empty")
	}
}

func TestManagedSettingsVerifyDetectsDrift(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runManagedSettingsForTest(t, exportArgs(dir)...); code != 0 {
		t.Fatalf("export prep failed: code=%d stderr=%s", code, stderr)
	}
	path := filepath.Join(dir, "managed-settings.json")
	if err := flipManagedSettingsBool(path, "allowManagedHooksOnly", false); err != nil {
		t.Fatalf("inject drift: %v", err)
	}
	code, stdout, _ := runManagedSettingsForTest(t, "verify", "--path", path)
	if code != 1 {
		t.Fatalf("verify drift expected exit 1; got %d stdout=%s", code, stdout)
	}
	if !strings.Contains(stdout, "allowManagedHooksOnly") {
		t.Fatalf("stdout missing drifted field name; got %q", stdout)
	}
}

func TestManagedSettingsVerifyMissingFileExits2(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	code, _, stderr := runManagedSettingsForTest(t, "verify", "--path", missing)
	if code != 2 {
		t.Fatalf("missing-file expected exit 2; got %d stderr=%s", code, stderr)
	}
}

func TestManagedSettingsVerifyMalformedFileExits2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(path, []byte("{ not valid"), 0o600); err != nil {
		t.Fatalf("write broken fixture: %v", err)
	}
	code, _, stderr := runManagedSettingsForTest(t, "verify", "--path", path)
	if code != 2 {
		t.Fatalf("malformed expected exit 2; got %d stderr=%s", code, stderr)
	}
}

func TestManagedSettingsUnknownSubcommandExits2(t *testing.T) {
	code, _, stderr := runManagedSettingsForTest(t, "frobnicate")
	if code != 2 {
		t.Fatalf("unknown subcommand expected exit 2; got %d stderr=%s", code, stderr)
	}
}

func runManagedSettingsForTest(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("CORDUM_API_KEY", "")
	var stdout, stderr bytes.Buffer
	code := runEdgeManagedSettingsCmd(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func exportArgs(dir string) []string {
	return []string{
		"export",
		"--output", dir,
		"--mcp-gateway-url", testManagedMCPGatewayURL,
		"--llm-proxy-base-url", testManagedLLMProxyURL,
		"--api-key-helper-command", testManagedAPIKeyHelper,
		"--hook-command", "/opt/cordum/bin/cordum-hook",
	}
}

func flipManagedSettingsBool(path, key string, value bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	doc[key] = value
	patched, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(patched, '\n'), 0o600)
}
