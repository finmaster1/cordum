# Cordum Edge — local shadow-agent scanner (`cordumctl shadow scan`)

EDGE-140's opt-in local scanner detects likely-unmanaged coding-agent
configurations and processes on a developer or CI host and emits
redacted observe-mode findings. It is a P3 (post-P0) capability,
deliberately scoped narrower than full Shadow IT discovery: scope is the
three first-party MCP clients (Claude Code, Codex, Cursor), a small set
of known agent process names, and a small set of known agent-credential
env-var names. See `docs/security/agentd-keychain.md` for the
defence-in-depth keychain story; see `docs/edge/managed-settings-deploy.md`
for the enterprise enforcement path.

## 1. Overview

`cordumctl shadow scan` walks three local detection sources and produces
a JSONL stream of findings:

1. Config-file presence under the user's home directory.
2. Process-name matches in the host process table (via
   `github.com/shirou/gopsutil/v3/process`).
3. Environment-variable name matches in the calling process's env.

The scanner is **observe-mode only**. It performs zero remediation
actions: no file modification, no process termination, no subprocess
spawning. Operators who want enforcement should route through
`cordumctl edge managed-settings` instead. The scanner is designed for
inventory awareness and CI advisory checks, not gating.

## 2. Trust and privacy boundary

| The scanner CAN | The scanner does NOT |
|-----------------|----------------------|
| Read JSON / TOML structural fields from known config paths. | Read command-line strings, env-var values, prompt text, or any non-schema field of a config. |
| Record server names, transport types, and endpoint hostnames. | Record full URLs, paths, tokens, or any value matching a credential-shape regex. |
| Record process executable names and PIDs. | Record process command-lines, environments, open files, network sockets, or working directories. |
| Record the **names** of known credential env vars. | Record the **values** of those env vars. |
| Emit findings to stdout JSONL or a mode-0600 output file. | Send findings off-host. (`EDGE-141` will add a server-side store; this task only writes locally.) |
| Refuse to follow symlinks (privacy + TOCTOU hardening). | Cross filesystem boundaries through arbitrary user-provided paths. |

Redaction is applied **at extraction time**, not as a post-process
filter. `RedactConfigSummary` only ever reads recognised structural
fields; even if a malicious config injected a value matching a secret
regex into one of those fields, the summary regex-strips the 8 supported
secret-shape patterns before emission and bounds the result to ≤2048
bytes.

## 3. Opt-in gate

The scanner refuses to enumerate anything unless the caller explicitly
opts in. The CLI honours two equivalent signals:

```sh
cordumctl shadow scan --enable-shadow-scan   # explicit flag
CORDUM_EDGE_SHADOW_SCAN_ENABLED=true cordumctl shadow scan   # env-var
```

Either signal is sufficient. Default invocation prints
`shadow scan disabled by default; use --enable-shadow-scan to opt in`
and exits 0 — the polite-no-op is a feature, not a failure, so CI
pipelines can include the command unconditionally.

`true`, `1`, and `yes` are all recognised values for the env-var. Any
other value (including `false`, `0`, empty string) is treated as
opt-out.

## 4. Detection sources

| Source | What is recorded |
|--------|------------------|
| Claude Code | `~/.claude/settings.json` — JSON parse → `mcpServers` keys + per-entry `transport` + endpoint hostname. |
| Codex | `~/.codex/config.toml` — minimal regex extract of `[mcp_servers.<name>]` headers + `transport = "..."` + `endpoint = "..."` hostname. |
| Cursor | `~/.cursor/mcp.json` — JSON parse, same shape as Claude Code. |
| Process names | Case-insensitive substring match against `claude-code`, `claude`, `codex`, `cursor`, and the `mcp-*` prefix. PID is recorded; cmdline / environ are not. |
| Env vars | `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `CURSOR_API_KEY` — only the **fact-of-presence** is recorded; values are never read. |

Adding a new source means amending `clientSpecs`, `processMatches`, or
`knownEnvVars` in `core/edge/shadow/scanner.go`. Each addition should
keep the privacy boundary: structural fields only.

## 5. Finding schema

Every finding is one JSON object with the following fields:

```jsonc
{
  "tenant_id":   "string — tenant attribution from --tenant or env",
  "principal_id":"string — principal attribution from --principal or env",
  "hostname":    "string — os.Hostname()",
  "product":     "claude-code | codex | cursor | mcp-server",
  "evidence_type": "config_file | process_name | environment_var",
  "redacted_path": "~/.claude/settings.json | process:claude:1234 | env:ANTHROPIC_API_KEY",
  "redacted_config_summary": "N mcp servers configured (transports: ...; hosts: ...) | empty | ...",
  "risk":           "low | medium | high",
  "remediation_hint": "string — operator-facing one-liner; never imperative",
  "status":         "observed | unreadable | managed_skip | partial",
  "observed_at":    "RFC3339 timestamp"
}
```

`risk` never emits `critical` because enforcement is out of scope for
this task (task rail #2). The most severe shadow observation that can
ship from this package is `high`.

## 6. Managed-config skip

Configs that originated from a Cordum managed-settings deployment carry
the `CORDUM_EDGE_MANAGED_POLICY_MODE=enterprise-strict` invariant
(established by EDGE-150). The scanner recognises this signature and
emits `Finding{Status:"managed_skip"}` instead of flagging the config
as a shadow observation. This satisfies DoD #4 'managed config not
flagged' and prevents enterprise-managed fleets from being drowned in
false-positive shadow alerts.

A managed-skip finding is still emitted — it carries the redacted
summary so operators see the file was inspected and identified as
managed, rather than being silently filtered out.

## 7. Output

Default: stdout, one finding per line as JSON (JSONL). Pipe into `jq`
for ad-hoc filtering or into a SIEM ingester for collection.

`--output <path>` writes to a file opened with `O_WRONLY|O_CREATE|O_TRUNC`
at mode `0o600`. The file is truncated on each scan — these JSONL
outputs are transient artefacts, not long-lived audit logs. If you
need historical retention, pipe the stdout JSONL into an append-only
collector.

`--tenant <id>` and `--principal <id>` override the per-finding
attribution fields. Defaults are empty. Production deployments should
set these from the operator's tenant / principal binding rather than
auto-inferring from process state (cross-tenant safety: the scanner
runs as a local CLI and has no authoritative tenant context).

## 8. Operational guidance

| Use case | Invocation |
|----------|------------|
| Developer-local audit | `cordumctl shadow scan --enable-shadow-scan` |
| CI advisory check | `cordumctl shadow scan --enable-shadow-scan --output shadow-findings.jsonl` |
| Tenant-attributed enterprise audit | `cordumctl shadow scan --enable-shadow-scan --tenant <id> --principal <id>` |
| Pipeline-friendly default (no-op when disabled) | `cordumctl shadow scan` |

**Do not wire shadow scan to enforcement.** The CLI never returns
non-zero based on findings — exit 0 means "scan completed", regardless
of whether findings exist. Wiring shadow scan to a CI gate that fails
the build on any finding would conflict with task rail #2 (no
enforcement) and would create the same false-positive amplification
that the managed-config skip is designed to prevent.

## 9. Roadmap

| Sibling | Status | Adds |
|---------|--------|------|
| EDGE-141 | PLANNING | Server-side finding store + `/api/v1/edge/shadow/findings` ingest |
| EDGE-142 | PLANNING | Remediation-hint generator (still observe-mode) |
| EDGE-143 | PLANNING | K8s + CI shadow detector design doc |

This task explicitly does **not** add a dashboard surface for shadow
findings (task rail #3 'Shadow Agents were cut from P0; do not add P0
nav/page here'). The future P3 dashboard surface will be designed by
EDGE-143 with the full server-side store from EDGE-141 in place.
