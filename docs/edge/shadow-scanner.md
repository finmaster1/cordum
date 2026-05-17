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

| Sibling    | Status  | Adds |
|------------|---------|------|
| EDGE-141   | DONE    | Server-side finding store + `/api/v1/edge/shadow-agents` lifecycle APIs (`detected → resolved/suppressed`). |
| EDGE-142   | WORKING | Remediation-hint generator (still observe-mode). |
| EDGE-143   | DESIGN  | [K8s + CI shadow detector design doc](kubernetes-ci-shadow-detector-design.md) (design only, awaiting human signoff). |
| EDGE-143.5 | DONE    | Store extensions — §10.1 fields, §10.2 filters, §10.5 Redis indexes + per-finding retention. See §9.1 below. |
| EDGE-143.1 | DONE    | Kubernetes shadow detector library — 9 signal extractors per design doc §7.1, observe-only. See §9.2 below. |

### 9.1 EDGE-143.5 — store extensions (shipped)

Adds the additive store surface from the EDGE-143 design doc §10. All
changes are backward-compatible — EDGE-141 records without these
fields continue to round-trip and surface in lists.

**23 new fields on `ShadowAgentFinding`** (§10.1) — `source_type`
(enum `local|kubernetes|ci|network`, defaults to `local` on read),
`source_id`, `cluster_id`, `namespace`, `workload_kind`,
`workload_name`, `pod_uid`, `ci_provider` (enum
`github_actions|gitlab_ci|jenkins|buildkite|circleci|other`), `repo`
(composite-indexed with `ci_provider`), `ref`, `workflow_id`,
`job_id`, `run_id`, `runner_id`, `tenant_source`, `principal_source`,
`signal_set` (≤16 entries, `[a-z0-9_]{1,32}`), `confidence` ([0,1]),
`first_seen`, `last_seen`, `false_positive_reason`, `exception_id`,
`retention_class` (enum `shadow_short|shadow_default|shadow_long`).

**11 new query filters on `GET /api/v1/edge/shadow-agents`** (§10.2)
— `source_type`, `cluster_id`, `namespace`, `ci_provider`, `repo`
(requires `ci_provider`), `signal` (repeatable any-of, capped at 16),
`confidence_min`, `first_seen_after`, `last_seen_before`,
`exception_id`, `include_managed_skip` (default `false`; when `false`,
findings carrying `false_positive_reason` are excluded). Combined
filters apply AND semantics across dimensions; `signal` applies IN
semantics within the dimension.

**4 new Redis indexes** (§10.5) — `edge:shadow:index:source:<source>`,
`edge:shadow:index:cluster:<cluster_id>`,
`edge:shadow:index:repo:<provider>:<org/repo>`,
`edge:shadow:index:signal:<signal>`. These are **NOT tenant-scoped**
(per the Q7 binding governor ruling — store-level federation, not
detector-level): multiple tenants share the same ZSET on these keys,
and tenant isolation is enforced at read time. Cross-tenant index
entries are **skipped** during a tenant's query, never deleted —
deletion would be cross-tenant data loss.

**Per-finding retention** (§10.5) — `retention_class` drives terminal
TTL when the finding transitions to `resolved` or `suppressed`. Empty
class falls back to the store's `terminalRetention` (default 90d) so
EDGE-141 records keep their existing lifecycle.

| Retention class  | Default TTL | Env var override                       |
|------------------|-------------|----------------------------------------|
| `shadow_short`   | 7 days      | `CORDUM_EDGE_SHADOW_RETENTION_SHORT`   |
| `shadow_default` | 90 days     | `CORDUM_EDGE_SHADOW_RETENTION_DEFAULT` |
| `shadow_long`    | 365 days    | `CORDUM_EDGE_SHADOW_RETENTION_LONG`    |

Env vars use Go `time.ParseDuration` syntax (`7d` is **not** supported;
use `168h`). Zero or negative values cause the gateway to fail at
startup, matching the EDGE-141 "positive durations only" convention.

**Backward-compatibility (§10.4):** legacy EDGE-141 findings (no
`source_type` set) read back with `source_type="local"` defaulted
on read; the `GET ?source_type=local` query falls back to the broad
tenant index + post-filter so legacy rows still surface.

This task explicitly does **not** add a dashboard surface for shadow
findings (task rail #3 'Shadow Agents were cut from P0; do not add P0
nav/page here'). The future P3 dashboard surface is intentionally
deferred; see EDGE-143's design doc §17 for the proposed follow-up task
that designs it.

### 9.2 EDGE-143.1 — Kubernetes shadow detector library (shipped)

Library package at `core/edge/shadow/k8s/`. Vendors `k8s.io/client-go@v0.34.8`
as the **first Kubernetes dependency in cordum** (transitive
`k8s.io/api`, `k8s.io/apimachinery`). Observe-only by design and by
the type system: the detector holds a narrow internal `kubeReader`
interface that declares only `listPods` / `listNamespaces` /
`listServices` / `getServiceAccount` / `listNetworkPolicies`. No
mutating verb (Create/Update/Patch/Delete) is reachable from any code
path in this package, so any future regression that tries to mutate
cluster state is a Go compile error. Design doc reference: design doc
§7 + binding governor ruling
[comment-a17f4f1c on task-de50a293](kubernetes-ci-shadow-detector-design.md).

**9 signal extractors** (design doc §7.1):

| Signal                         | Risk   | Trigger |
|--------------------------------|--------|---------|
| `k8s_heartbeat_missing`        | medium | Pod whose image matches a known agent image but the Cordum heartbeat label is missing. §14 N-consecutive-poll gate (default 3) suppresses single-poll false positives. |
| `k8s_unmanaged_process`        | medium | Pod container `command`/`args` leading-token matches a known agent executable AND no heartbeat label. Only the leading token is captured — never subsequent args. |
| `k8s_unmanaged_mcp_service`    | medium | Service with `port.name ∈ {mcp, mcp-stdio, mcp-sse, mcp-http}` missing the Cordum gateway-adoption label. |
| `k8s_unmanaged_workload`       | medium | Pod owned by a Deployment/DaemonSet/StatefulSet/Job/CronJob/ReplicaSet not on the operator allowlist; emits at owner level so a 100-replica rogue deployment produces ONE finding. |
| `k8s_untrusted_agent_image`    | low    | Pod image registry-prefix not in `ImageRegistryAllowlist` and image name matches an agent token. |
| `k8s_namespace_untenanted`     | low    | Namespace missing tenant label AND containing ≥1 agent-image pod (§14 aggregation guard). |
| `k8s_admission_observed`       | medium | Reserved for operator-supplied admission-log tail; observe-only, never installs a webhook. |
| `k8s_egress_bypass`            | high   | NetworkPolicy with broad egress (`0.0.0.0/0`) not scoped to the operator's LLM proxy allowlist. |
| `k8s_ephemeral_indicator`      | low    | Pod present last scan, gone this scan — **never auto-promoted** to a finding without corroboration per §14. |

**Tenant mapping precedence** (design doc §6.1, recorded in
`tenant_source` field):

1. `pod_label` — pod `cordum.io/tenant-id`.
2. `namespace_label` — namespace `cordum.io/tenant-id`.
3. `cluster_config` — operator-maintained `Config.ClusterTenantMap[ClusterID]`.
4. `sa_config` — pod's service-account annotation `cordum.io/tenant-id`.
5. `quarantine` — terminal default (`cordum.shadow.quarantine`); finding remains actionable because workload identity is still captured.

**Principal mapping precedence** (§6.2, recorded in `principal_source`):

1. `pod_label` → 2. `pod_annotation` → 3. `sa_name` (`sa:<ns>:<name>`) → 4. `unknown`.

**Data minimization** (§5 — enforced at extraction time, not as a
post-filter):

| MAY collect | MUST NOT collect |
|-------------|------------------|
| Pod/Service/Namespace `.name`, `.labels`, `.annotations` (tenancy-purposeful). | Container `Env[].Value` (only `Env[].Name`). |
| Container image registry+name; tag scrubbed via `imageTagSafe` if secret-shape. | Mounted Secret/ConfigMap data bodies. |
| Container `Command[0]` / `Args[0]` leading token via `leadingToken`. | Command-arg values beyond the leading token. |
| OwnerReferences kind+name+UID. | Full URLs with query string. |
| | Raw network payloads. |

Defense-in-depth: every string entering `CreateFindingRequest` runs
through `redactField` (8-pattern regex strip mirroring
`core/edge/shadow/redaction.go:23-36`, plus a 2048-byte cap). The
shadow store applies the same patterns again at write time via
`stripSecretMarkers` on `EvidenceSummary` — two independent strip
passes so a regression on one side does not silently leak.

**Configuration** (`k8s.Config`):

| Field                       | Default | Purpose |
|-----------------------------|---------|---------|
| `ClusterID`                 | (required for prod) | Used as `cluster_id` on every emitted finding + tier-3 tenant lookup key. |
| `ScanInterval`              | 60s     | Cadence for `Run()`-driven polling. |
| `TenantLabelKey`            | `cordum.io/tenant-id` | Label/annotation key consulted by the tenant resolver. |
| `PrincipalLabelKey`         | `cordum.io/principal-id` | Same for principal. |
| `HeartbeatLabelKey`         | `cordum.io/edge-session-id` | Label whose presence marks a pod as Cordum-governed. |
| `GatewayAdoptionLabel`      | `cordum.io/mcp-gateway` | Label whose presence marks a Service as MCP-gateway-attached. |
| `HeartbeatMissedThreshold`  | 3       | §14 N-consecutive-poll gate before `heartbeat_missing` promotes. |
| `KnownAgentImages`          | —       | Allowlisted agent image prefixes (e.g. `anthropic/claude-code`). Set explicitly per operator deployment. |
| `KnownAgentExecutables`     | `claude, codex, cursor, mcp-server, mcp-gateway` | Leading-token allowlist for `unmanaged_process`. |
| `ImageRegistryAllowlist`    | —       | Registry prefixes trusted not to be shadow agents. Set explicitly. |
| `MCPPortNames`              | `mcp, mcp-stdio, mcp-sse, mcp-http` | Service port names that flag an MCP service. |
| `WorkloadAllowlist`         | —       | Workload names exempt from `unmanaged_workload`. |
| `NamespaceAllowlist`        | —       | Namespaces exempt from `namespace_untenanted`. |
| `ClusterTenantMap`          | —       | Tier-3 `cluster_id → tenant_id` map for the default resolver. |
| `LLMProxyEndpoints`         | —       | Endpoints considered safe for egress; broader egress triggers `egress_bypass`. |
| `QuarantineTenantID`        | `cordum.shadow.quarantine` | Terminal tenant when all other tiers fail. |

**Observability** (Observer interface; design doc §13):

```go
type Observer interface {
    RecordFindingEmit(signal, risk string)
    EmitAudit(event audit.SIEMEvent)
}
```

Production wiring backs `Observer` with a Prometheus `CounterVec`
(`cordum_edge_shadow_finding_emit{source_type, signal, risk}`) and the
shared `audit.AuditSender`. Bounded labels only — tenant_id, cluster_id,
namespace, workload_name, pod_uid, repo, run_id are NEVER passed as
metric labels (high cardinality); they live on the persisted finding
and on `audit.SIEMEvent.Extra`. The audit event uses
`Action="shadow_agent.observed"`, `Decision="observed"`,
`EventType="edge.shadow_finding_created"` with severity derived from
risk per §13.2.

**Observe-mode enforcement contract** (design doc §11, ADR-gated
future):

- This task ships ZERO enforce-mode code paths. No
  `ValidatingAdmissionWebhook`, no `kubectl patch`, no `kubectl
  delete`, no auto-label-mutation.
- `CORDUM_EDGE_SHADOW_K8S_ENFORCE` env var name is **reserved** for the
  future enforce-mode hook but is not consulted anywhere today.
- The narrow `kubeReader` interface (`detector.go:79`) is the primary
  enforcement mechanism — adding a mutating verb to the library would
  require widening this interface, which would be reviewable.

**EDGE-141 reuse**: findings are persisted via the existing
`shadow.Store.CreateFinding` API. No parallel finding store, no
parallel ingest pipeline. The Source ID emitted is
`"k8s-detector-" + ClusterID` so SIEM aggregators can correlate.
