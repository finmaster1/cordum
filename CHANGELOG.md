# Changelog

All notable changes to this project will be documented in this file.
Format follows [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Added

#### Docs — EDGE-143 design doc for Kubernetes / CI shadow detection (2026-05-17, task-de50a293)

- `docs/edge/kubernetes-ci-shadow-detector-design.md` (new) — design-only architecture & privacy specification for cluster-scope and CI-scope shadow-agent detection. Extends the EDGE-140 local-host scanner (`docs/edge/shadow-scanner.md`) and the in-flight EDGE-141 server-side finding store + EDGE-142 remediation generator to Kubernetes pods and CI runners (GitHub Actions, GitLab CI, Jenkins, Buildkite, CircleCI). Specifies: (a) detection signals — pod/namespace/workload metadata, workflow/job/runner metadata, direct provider-traffic hostname/category/counts only, never payload; (b) tenant/principal mapping precedence chains (pod label → namespace label → cluster config → SA config → quarantine; OIDC claim → repo map → workspace map → quarantine); (c) data minimization with extraction-time redaction + EDGE-140 secret-shape regex strip + 2048-byte cap; (d) proposed additive `ShadowAgentFinding` field set (`source_type`, `cluster_id`, `namespace`, `workload_kind/name`, `pod_uid`, `ci_provider`, `repo`, `workflow_id/job_id/run_id`, `runner_id`, `signal_set`, `confidence`, `first_seen/last_seen`, `false_positive_reason`, `exception_id`, `retention_class`) with backward-compat default `source_type="local"` for existing rows; (e) exception API `POST /api/v1/edge/shadow/exceptions`; (f) three rollout modes (observe-only default, warn = webhook to operator pipeline, enforce = ADR-gated future); (g) remediation classes mapped to EDGE-142 (`attach_mcp_gateway`, `attach_edge_session`, `deploy_managed_settings`, `route_via_llm_proxy`, `register_ci_workflow`, `declare_exception`, `resolve_manually`) with mandatory preview + backup/change-ticket gate on every mutating action; (h) false-positive controls for fork PRs, dependabot/renovate, ephemeral runners, managed-but-late heartbeat, telemetry gaps; (i) security/privacy review checklist (14 PR-template items); (j) 8 open questions blocking implementation tasks. **No production cluster code, no dashboard surface, no Cordum Jobs per agent action, no customer-cluster mutation, no TLS interception, no payload or secret capture by default** — design is observe-first, P3, and requires human signoff before any follow-up implementation task (EDGE-143.1 through EDGE-143.10 proposed) is filed.
- `docs/edge/shadow-scanner.md` — §9 Roadmap updated: EDGE-141/142 status refreshed from PLANNING to WORKING, EDGE-143 row links to the new design doc. The legacy assertion that EDGE-143 would design the P3 dashboard surface is corrected to note the dashboard is intentionally deferred (per the new design doc §17, the dashboard is its own future task family with its own ADR).

#### Dashboard — EDGE-105 MCP lane on Edge Session detail (2026-05-17, task-a04699dc)

- `dashboard/src/components/timeline/lanes/MCPLane.tsx` (new) — the MCP timeline lane mounted below the existing P0 hook timeline on `EdgeSessionDetailPage`. Classifies each `AgentActionEvent` into Servers / Tools / Approvals / Failures categories and renders one row per MCP-relevant event with a distinct icon + decision badge + relative timestamp. Click expands an inline inspector body. Honors the global `<MotionConfig reducedMotion="user">` wrapper.
- `dashboard/src/components/timeline/inspector/MCPInspector.tsx` (new) — six-field row-expand body: upstream server (`event.labels.mcp_server` with `agentProduct` fallback), tool name, decision (StatusBadge), approval link (router `Link` to `/approvals/<approvalRef>` when set), redacted args (`inputRedacted`), redacted result (`labels.result_redacted`). Renders an artifact-pointer chip with sha256-short when `event.artifactPtrs[0]` is present; external link uses `rel="noopener noreferrer"`.
- `dashboard/src/state/mcpLaneFilters.ts` (new) — Zustand slice + URL parse/serialize helpers for the chip-toggle filter state. The URL query `?mcp_lane=servers,tools,approvals,failures` shares the filter across operators; invalid tokens are silently dropped, and an empty parse falls back to the default all-active state.
- `dashboard/src/lib/redaction.ts` (new) — client-side defense-in-depth sanitizer. `sanitizeMCPField` trusts the `<name>_redacted` suffix verbatim (server-side redaction is authoritative); if only a bare sensitive field (`prompt`, `tool_input`, `result`, `args`, etc.) is present it returns the stable placeholder `"[redacted by client sanitizer]"`. `sanitizeMCPPayload` serializes the redacted blob when any `_redacted` key is present, else walks the raw payload and replaces sensitive field values.
- `dashboard/src/pages/EdgeSessionDetailPage.tsx` — three-line ADD only: import `MCPLane` and mount `<MCPLane events={events} />` between the existing P0 timeline section and `EdgeArtifactsPanel`. Zero modifications to the P0 hook timeline, `groupEdgeEvents`, or `TimelineGroupRow`; task rail #2 ("do not duplicate P0 timeline components; extend reusable lanes") honored.
- Tests (22 new): `MCPLane.test.tsx` (10) covers all event-kind rendering, the chip-toggle filter, the empty state, the bare-leak defense, the trusted `_redacted` pass-through, approval-link navigation, the artifact-pointer chip, the strict axe-core gate, the XSS escape, and URL parsing. `MCPInspector.test.tsx` (3) covers six-field render and absent-data fallbacks. `redaction.test.ts` (9) covers the field- and payload-level sanitizer contracts.
- `docs/mcp-server.md` — new "Dashboard MCP Lane" section documenting the lane surface, filter chip UX, inspector layout, redaction contract, empty state, and accessibility posture.

#### Tools — Self-test harness for the EDGE-068 argv-only exec lint (2026-05-17, task-c000a477)

- `tools/scripts/lint_no_secret_log.test.sh` (new, executable) — exercises Phase 4 of `lint_no_secret_log.sh` against three fixture corpora under `tools/scripts/testdata/lint_no_secret_log/`:
  - `phase4_pass/` — argv-only `exec.Command` plus the `go test -c` false-positive defense (the `-c` flag here is the go-test compile flag, not a shell command flag).
  - `phase4_fail/` — eight shell-spawn patterns: `sh -c`, `/bin/sh -c`, `bash -c`, `cmd /C`, `cmd.exe /c`, `powershell -Command`, a multi-line `exec.CommandContext` split across source lines (exercises the awk paren tracker), and `/usr/bin/sh -c` (absolute path).
  - `phase4_exception/` — the `cmd/cordumctl/doctor.go:878-883` runtime.GOOS branch with `// no-shell-exec-lint: operator-confirmed doctor repair only` markers, plus a minimum-shape inline marker.
  Plus T11 default-tree invariant: with `LINT_SCAN_ROOTS_OVERRIDE` unset, the lint must still exit 0 on the real `cmd/` and `core/` trees so the fixture corpus cannot bleed into the production scan.
- `tools/scripts/lint_no_secret_log.sh` — Phase 4 only: new `LINT_SCAN_ROOTS_OVERRIDE` env var (colon-separated dirs) replaces the default `cmd/` + `core/` find roots when set. Used exclusively by the test harness. Unset preserves the historical scan behaviour bit-for-bit.
- `docs/no-shell-exec-lint.md` (new) — documents the argv-only convention, why hook-boundary subprocesses must not route through a shell, the `// no-shell-exec-lint: <reason>` marker shape and required reason text, the current exception list (`cmd/cordumctl/doctor.go:880,882`), and the procedure for adding a new exception (architect review + marker + exception-list update in the same commit).
- `.github/workflows/ci.yml` — `go-test` job now runs `bash tools/scripts/lint_no_secret_log.test.sh` immediately after `lint_no_secret_log.sh` so a regression that silently weakens the guard fails CI.

### Changed

#### Dashboard — Jobs + Job Detail refresh: unified search, fold I/O into Overview, remove Policy Trace tab (2026-05-16, task-cafacca3)

- `dashboard/src/pages/JobsPage.tsx` — main search input placeholder expanded to "Search jobs (ID, topic, pool, tenant, session, run, trace)"; the underlying `filtered` predicate now matches `pool`, `tenant`, and `getJobParentRefs(j).sessionId` alongside the prior ID/topic/trace/run fields so users don't need to open the advanced filter bar to find jobs by those fields.
- `dashboard/src/pages/JobDetailPage.tsx` — tabs reduced from 5 to 2 (Overview + Audit Chain). Inputs + Outputs tab content (Context BlobViewer + Result BlobViewer) folded into Overview as `CollapsibleSection` rows below `AgentExecutionsPanel`. Policy Trace tab removed entirely (unused per task description); `GovernanceTimeline` import deleted from this page — the component file stays because `RunDetailPage.tsx` still consumes it. Legacy `?tab=inputs|outputs|policy-trace` deep-links gracefully migrate to Overview via the activeTab derivation so bookmarks don't 404.
- Tests: 4 obsolete tab-click tests (Inputs/Outputs/Overview-clears-tab-param + the 5-label tab-set assertion) deleted as the assertions test removed UI. New replacement asserts exactly 2 tabs (Overview + Audit Chain) and that GovernanceTimeline does not mount. One pre-existing test (`does not double-print ctx.run_id`) refined from page-wide assertion to the GenericContext-curated-row invariant — the new Context BlobViewer in Overview legitimately echoes raw ctx in JSON.

#### Dashboard — Agent Fleet consolidated from 4 tabs to 2 (2026-05-16, task-083581ca)

- `dashboard/src/pages/AgentsPage.tsx` — `topTabs` reduced 4→2 (Fleet Overview + Identities). Pool Topology absorbed into Fleet Overview as a segmented view-mode toggle (Table / By Pool) with `?view=by-pool` query-state persistence. Agent Registry deleted as redundant — its worker table duplicated Fleet Overview's; the standalone `AgentRegistryTab` function and the `useWorkers` import removed (~75 LOC). Identity Directory renamed to Identities (same content + EntitlementGate wrapper).
- Backward-compatibility migration: mount-time `useEffect` rewrites legacy `?tab=pools|registry|identity` query params via `setSearchParams(..., { replace: true })` so existing bookmarks resolve to the new tab+view combination (pools → fleet+view=by-pool; registry → fleet; identity → identities).
- `dashboard/src/pages/AgentsPage.test.tsx` — 2 new assertions: exactly `{Fleet Overview, Identities}` top-level tabs render; `{Pool Topology, Agent Registry, Identity Directory}` labels are absent. 3/3 PASS post-impl.

#### Dashboard — Edge promoted to top-level sidebar section; redundant Dead Letters entry removed (2026-05-16, task-266f21ad)

- `dashboard/src/components/layout/AppShell.tsx` — APP_SHELL_NAV_SECTIONS now ships 6 sections (Run, **Edge**, Govern, Catalog, Audit, Settings). The new `Edge` section sits between Run and Govern and contains: `Edge Sessions` (`/edge/sessions`), `Edge Approvals` (`/edge/approvals`), `Edge Audit` (`/edge/audit`). The Edge Sessions item moved out of Run so the Edge subsystem has visible breadth in the IA instead of being buried as one item in Run.
- `Dead Letters` sidebar entry removed from the Audit section. DLQ content has been folded into JobsPage as `?status=dlq` since task-0bcb9411; the sidebar entry was a redirect-stub that added navigation noise without a dedicated destination. `/dlq` URL still redirects via `App.tsx::DlqRouteRedirect` so bookmarked links resolve.
- `dashboard/src/App.tsx` — added 2 Navigate redirect routes:
  - `/edge/approvals` → `/approvals?lane=edge`
  - `/edge/audit` → `/audit?event_type_prefix=edge`
  Pathname-prefix routing (`findActiveSection`) keeps Edge highlighted at click time; post-redirect the section reflects WHERE the user IS (Run/Approvals or Audit), an accepted UX trade-off vs introducing a query-aware section matcher.
- `dashboard/src/pages/ApprovalsPage.tsx` — reads `?lane` from the URL; when `lane=edge`, filters approvals to those where `decisionSummary.source.startsWith("edge")`. Today's /approvals feed primarily carries gateway approvals; edge-source approvals populate the filtered view once edge sources are routed through the global feed.
- `dashboard/src/pages/AuditLogPage.tsx` — adds an `event_type_prefix` query state via nuqs; when set, filters events client-side by `e.action.startsWith(prefix)`. Same dependency on a future audit-feed wiring for the prefix to surface non-zero results; the URL contract is wired now.
- Tests: `dashboard/src/components/layout/AppShell.test.tsx` (updated existing 3 assertions + added 4 new) and `dashboard/src/App.routing.test.ts` (added 3 redirect-route source-regex assertions including `/dlq` preservation). 44/44 PASS post-impl.

### Added

#### EDGE-104 — cordumctl mcp attach/preview/rollback for Claude Code, Codex, and Cursor (2026-05-16, task-9351f243)

- `cmd/cordumctl/mcp_attach.go` + `mcp.go` — new `cordumctl mcp <preview|attach|rollback>` verbs alongside existing `pending|approve|reject|tools|keygen|upstream`. `attach` requires `--apply` to write; without it falls through to a preview run so operators see exactly what would change.
- `cmd/cordumctl/mcp_attach_common.go` — shared `AttachAdapter` interface + lifecycle helpers (`PreviewAttach`, `ApplyAttach`, `RollbackAttach`). Backup-on-modify via `<path>.bak.<unix_ms>` with deterministic newest-first restore. Atomic writes via tempfile + `os.Rename` (cross-fs fallback to copy+remove). Mode `0600` on written files. Secret redaction in preview output masks `sk-*`, `ghp_*`/`gho_*`, `Bearer <token>` patterns before any stdout write.
- `cmd/cordumctl/mcp_adapter_claude_code.go` — targets `~/.claude.json`, user-scope `mcpServers` map. HTTP/SSE entries render as `{type, url}`; stdio as `{command, args}`. Preserves all sibling keys (project list, history, theme) verbatim via `map[string]any` round-trip + stable-sorted re-serialize.
- `cmd/cordumctl/mcp_adapter_codex.go` — targets `~/.codex/config.toml`. Hand-rolled byte-level splicer rather than adding a TOML library dep: locates `[mcp_servers.<id>]` blocks, replaces in place for an existing gateway entry, appends with a blank-line separator for a new one. Operator-authored comments + whitespace in non-mcp_servers sections are byte-preserved. HTTP gateways render as a stdio invocation of `cordumctl mcp proxy --endpoint <URL>` since Codex's documented MCP transport is stdio-only.
- `cmd/cordumctl/mcp_adapter_cursor.go` — targets `~/.cursor/mcp.json`. Same JSON-merge primitive as Claude Code; only the stdio entry shape differs (Cursor docs require explicit `type: "stdio"`).
- Schema provenance constants on every adapter (`<Client>SchemaURL` + `<Client>SchemaDate`, fetched 2026-05-16 from current docs). Exposed via `AttachSchemaProvenance(client)` for audit scripts.
- Tests: `cmd/cordumctl/mcp_attach_test.go` — 11 test functions × 3 clients (`claude_code`, `codex`, `cursor`) covering preview missing/existing/malformed, apply creates-new/backs-up-existing, rollback restores/missing-backup-fails, secret redaction, idempotency, per-platform default path resolution (Linux + Windows home shapes), and dispatcher-level `attach` without `--apply` writes nothing.
- Attach is the convenience/adoption path; for enterprise enforcement use the managed-settings flow (`cordumctl edge managed-settings export`, EDGE-150). The canonical cordum-gateway upstream config comes from the EDGE-101 registry (`core/edge/mcp_upstream_registry.go`).
- Docs: `docs/mcp-server.md` § "Attach Commands (EDGE-104)" covers subcommand surface, per-client paths, backup semantics, rollback, secret redaction guarantees, and schema-version tracking.

#### EDGE-103 reopen #1 — approval-required payload + Edge mint dual-write (2026-05-16, task-968d6646)

- `core/mcp/registry.go` — `ApprovalRequired` struct extended with
  `ApprovalRef`, `ArgsHash`, `ExpiresAt`, `PolicySnapshot`, `RetryHint`
  (all `omitempty`). Resume authority is now `ApprovalRef`; `ApprovalID`
  stays for backward-compat SIEM correlation only. Doc comment makes the
  contract explicit so client SDKs branch on the right handle.
- `core/mcp/approval_hold.go` — new exported `BuildMCPApprovalBinding(tenant, server, params, policySnapshot) (actionHash, inputHash)` centralises the hash tuple that mint + consume MUST agree on. Both sides call this helper so a refactor cannot accidentally drift one path's hashing and surface `args_mismatch` on every legitimate retry. `ProcessApprovalClaim` rewired to use the helper.
- `core/controlplane/gateway/mcp_gate.go` — `gatewayApprovalGate` gains `edgeStore` / `policySnapshot` / `serverName` fields wired via new `WithEdgeApprovalMint`. `gate.Check` now populates the full `ApprovalRequired` contract and dual-writes an `EdgeApproval` alongside the legacy `MCPApprovalStore` record when `mcp.CallMetadata` carries `SessionID`/`ExecutionID` — the Edge ref becomes the resume handle. Falls back to legacy-only (approval_ref == approval_id, resumable via retry-with-same-args) when Edge metadata is absent so HTTP MCP transit without an `EdgeSession` keeps working.
- `core/controlplane/gateway/handlers_mcp.go` — `wireMCPApprovalGate` calls `gate.WithEdgeApprovalMint(s.edgeStore, s.mcpPolicySnapshotFunc(), mcpPolicyServerName)` when `s.edgeStore` is non-nil so production boot exercises the dual-write path automatically.
- Tests: `core/controlplane/gateway/mcp_gate_test.go:TestGate_ApprovalRequiredCarriesResumeMetadata` (new field assertions), `core/mcp/approval_hold_test.go:TestBuildApprovalClaimRequest_MintAndConsumeProduceMatchingHashes` + `TestBuildMCPApprovalBinding_StripsApprovalRef` (helper determinism + `_approval_ref` strip), `core/mcp/approval_hold_test.go:TestProcessApprovalClaim_TypedConflictKind` extended with `consumed`/`tuple_mismatch`/`cross_tenant`/`rejected` subtests (all 7 production `ApprovalConflictKind` values), `core/controlplane/gateway/mcp_policy_boot_test.go:TestMCPProdBoot_ApprovalHoldWiredWhenFlagOn` (HasApprovalHold==true regression guard for QA's prior dead-path issue), and `core/mcp/server_approval_hold_e2e_test.go` (4 new JSON-RPC E2E tests: -32099 envelope contract, -32096 args_mismatch lifecycle, consume-once + args-strip dispatch, gateway-misconfigured on missing CallMetadata).

#### EDGE-101 - MCP upstream server registry (2026-05-16, task-fb11aa72)

- New `/api/v1/edge/mcp/upstreams` registry API plus `cordumctl mcp upstream`
  subcommand for managing approved upstream MCP servers. Entries store
  name/transport/endpoint-or-command/tenant/auth-secret-ref/labels/risk/enabled
  metadata and keep disabled records visible to admins by default.
- Validation rejects raw secrets (`secret://` refs only), unsafe strict-mode
  local endpoints, shell-metacharacter `stdio` commands, tenant/name key escapes,
  and enterprise-strict entries that are not on the MCP allowlist.
- Updates write a 30-day backup of the previous registry record before overwrite,
  with collision-resistant backup keys for rapid consecutive updates.
- Builds on the EDGE-100 MCP gateway skeleton; EDGE-104 attach/preview commands
  and EDGE-105 dashboard surfaces consume this registry. Structured gateway logs
  emit `mcp-upstream-<op>` outcomes without secret refs or values.

#### REQUIRE_HUMAN threshold routing in safety kernel (2026-05-16, task-96f931fe reopen #1)

- Supersedes the rejected `cf40ce81` (deleted in `75ed120d`, 1138 LOC
  removed). Rejected predecessor placed 1350 LOC in
  `core/policy/actiongates/*` (forbidden by governor amendment
  `comment-e58c8328`) and used a 3-output model with an
  EducationalContext carrier that does not exist in the architecture
  (carved out by architect amendment `comment-79a9e609`). This entry
  describes the replacement implementation.
- New `core/infra/config/safety_policy.go` `RequireHumanThreshold`
  struct (`MinSeverityForDeny string`, `MinConfidenceForDeny float32`,
  `DowngradeWhenPromptOnly bool`) wired as `SafetyPolicy.RequireHuman`.
  Zero value preserves legacy DENY-on-match behavior; operators opt in
  per-tenant via YAML.
- Safety-kernel input-rule dispatch in
  `core/controlplane/safetykernel/kernel.go` now consults the threshold
  inside the matched-rule loop. A `deny`-authored rule downgrades to
  `pb.DecisionType_DECISION_TYPE_REQUIRE_HUMAN` when any of three
  conditions hold (logical OR): finding severity below the floor,
  finding confidence below the floor, or
  `DowngradeWhenPromptOnly && input.Action == nil`. Action-bound
  high-severity high-confidence DENYs are unchanged — the
  "unchanged from today" branch the architect amendment carved out.
- 2-output model only. No new `pb.DecisionType` value, no
  EducationalContext field, no `input_text`-derived trust source.
  Audit / dashboard / approval-store paths consume the downgraded
  decisions through the existing `REQUIRE_HUMAN` surface.
- `core/controlplane/safetykernel/input_policy.go` carries the
  `shouldDowngradeDenyToRequireHuman` helper + `severityRank` ordinal
  mapping. 9 GREEN unit tests in
  `core/controlplane/safetykernel/decision_threshold_test.go`:
  5 FP scenarios (defensive `/etc/passwd`, `rm -rf` mention,
  API-key rotation, approval-token logging, metadata-service
  education) assert `DENY → REQUIRE_HUMAN`, plus action-bound
  stays-DENY guard, zero-threshold legacy guard, rule-tier severity
  floor precedence, and severityRank mapping. `-count=3` flake check
  clean.
- Structured log `input rule matched` now records the resolved
  `outputDecision` field so operators see at a glance when a
  `deny`-authored rule routed to `require_human` instead.
- Docs: `docs/safety/decision-thresholds.md` rewritten end-to-end to
  reflect the 2-output dial, the routing table, the 5 FP scenarios,
  the anti-patterns (no session-metadata carrier, no `input_text`
  trust), and the implementation references.
- DoD #6 (AgentShield holdout regression) is **deferred**:
  `agentshield-benchmark` is not our repo per
  `[[feedback_dont_touch_agentshield_benchmark]]`; verifying real
  numeric over-refusal reduction requires running the AgentShield
  regression against a gateway built from this commit which is a
  separate operational task. Functional correctness is verified by the
  9 unit tests above + the full `safetykernel` suite passing
  (`go test ./core/controlplane/safetykernel/... -count=1`).

#### EDGE-102 follow-up — Wire MCPServer.WithPolicyGate at gateway boot (2026-05-16, task-e9d9a37d, bundles task-3d5c4f37)

- Gateway boot path (`core/controlplane/gateway/handlers_mcp.go`
  `startMCPRuntimeFromConfig`) now calls `MCPServer.WithPolicyGate` +
  `WithApprovalHold` against real backing stores when the new
  `mcp.policy_gate_enabled` config flag is true. Default false per
  `feedback_dont_change_deployment_defaults` — operators opt in
  explicitly per deploy; missing config key leaves the gate off.
- New `core/controlplane/gateway/mcp_policy_boot.go` introduces two
  small production adapters that the EDGE-102 surface previously
  routed through noop fallbacks:
  - `edgeStoreEventEmitter` adapts the existing `*edgecore.RedisStore`
    (the same instance `s.edgeStore` Edge events already land on) to
    `mcp.EventEmitter` — `mcp.tool.pre` / `mcp.tool.post` /
    `mcp.tool.failed` events now persist alongside hook + LLM events
    on one canonical stream.
  - `productionArtifactStore` adapts the existing `artifacts.Store`
    (the Redis-backed pointer store the export bundler reads) to
    `mcp.ArtifactStore` — oversized redacted MCP-request/response
    payloads land in artifact storage with content-addressed SHA-256
    pointers carrying tenant/session/execution/event labels for the
    dashboard's evidence-export pivot.
- Approval-hold consume path (`_approval_ref` arg, EDGE-103) now
  resolves claims through `edge.RedisStore.ClaimApproval` with a
  `PolicySnapshot` closure sourced from `loadPolicyBundles` so the
  consume-side snapshot matches mint-side (closes the c530c1c0
  ServerName + snapshot guard from PR #276).
- Bundled scope from task-3d5c4f37: ALLOW_WITH_CONSTRAINTS gate
  decisions now propagate the `_constraints` map through to the
  emitted pre + post + failed events. `AgentActionEvent.Constraints`
  field added (omitempty so legacy ALLOW events keep their wire
  shape); `newPostEvent` derives `Decision` via
  `mapPolicyDecisionToEdge` so an AWC verdict records
  `Decision=constrain` rather than degrading to `allow` on the post
  event. Shape mirrors `core/edge/agentd EvaluateResponse.Constraints`
  per the "No parallel subsystems" epic rail.
- Operator-facing boot log:
  `slog.Info("mcp.policy_gate wired", server_name=cordum.builtin, policy_gate_active, approval_hold_active, emitter, artifact_store)`
  when the flag is on; `slog.Info("mcp.policy_gate skipped", reason)`
  when off. One greppable line per acceptance criterion #4.
- `logToolCallDecision` now records `slog.Int("constraint_count", ...)`
  so AWC bursts are greppable from the live log stream; constraint
  VALUES are never logged (CLAUDE.md security rail).
- New `MCPServer` accessors `HasPolicyGate() / PolicyServerName() /
  PolicyEventEmitter() / PolicyArtifactStore() / HasApprovalHold()`
  double as boot-log inputs and stable surfaces for boot-wiring
  tests / future dashboard health probes.

#### EDGE-140 — Local shadow-agent scanner observe mode (2026-05-16, task-74ac5153)

- New `core/edge/shadow/` package implements an opt-in P3 local scanner
  that detects likely-unmanaged Claude Code / Codex / Cursor MCP
  configurations, known agent process names, and known agent-credential
  env-var names. Observe-mode only: zero enforcement actions, zero
  filesystem mutation, zero subprocess invocation. The static-source
  TestScannerRefusesEnforcement guard greps the package for
  `os.Remove` / `os.WriteFile` / `os.Rename` / `os.RemoveAll` /
  `exec.Command` / `"os/exec"` and fails the build if any appears in a
  non-test file (task rail #2 'no enforcement').
- Privacy boundary: the scanner reads only structural JSON / TOML
  fields (`mcpServers` keys + transport + endpoint hostname) — never
  command-lines, env-var values, prompt content, or any field outside
  the recognised schema. Defence-in-depth `RedactConfigSummary`
  regex-strips 8 secret-shape patterns (`sk-`, `sk-ant-`, `ghp_`,
  `gho_`, `xoxb-`, `Bearer`, `BEGIN PRIVATE KEY`, `BEGIN CERTIFICATE`)
  and bounds output to ≤2048 bytes.
- Managed-config skip: configs carrying the EDGE-150
  `CORDUM_EDGE_MANAGED_POLICY_MODE=enterprise-strict` invariant emit
  `Finding{Status:"managed_skip"}` rather than a shadow flag, so
  enterprise-managed fleets are not drowned in false-positive alerts
  (DoD #4 'managed config not flagged').
- New `cordumctl shadow scan` subcommand wires the scanner with three
  CLI flags: `--enable-shadow-scan` (opt-in), `--output <path>` (mode
  0600 JSONL output; default stdout), `--tenant` / `--principal`
  (override attribution fields). The env-var
  `CORDUM_EDGE_SHADOW_SCAN_ENABLED=true|1|yes` is honoured as an
  equivalent opt-in. Default invocation (no flag, no env) prints a
  polite no-op message and exits 0 so CI pipelines can include the
  command unconditionally.
- Cross-platform process enumeration via
  `github.com/shirou/gopsutil/v3 v3.24.5` (plus 7 indirect transitives
  including go-ole, plan9stats, perfstat, m1cpu, go-sysconf, numcpus,
  wmi); test seam `WithProcessLister` injects a mock so unit tests are
  OS-independent. Symlink-attack hardening via `os.Lstat` (refuses to
  follow); large-config OOM hardening via `io.LimitReader` at 1 MiB
  cap with `Status:"partial"` reporting.
- Threat model + operator runbook: `docs/edge/shadow-scanner.md`
  documents the trust boundary, opt-in gate, detection sources, finding
  schema, managed-config skip semantics, and operational guidance.
  Explicitly NO dashboard surface in this task (task rail #3 'Shadow
  Agents were cut from P0'); future EDGE-141 adds the server-side
  finding store, EDGE-142 the remediation-hint generator, EDGE-143 the
  P3 dashboard design.

#### EDGE-100 — P1 MCP Gateway service skeleton (2026-05-16, task-0ffcac35)

- New `core/mcp/gateway_skeleton.go` exposes `RegisterGatewayRoutes(mux, deps)` +
  `NewGateway(deps)` and four `*Gateway` handlers (HandleHealth, HandleConfig,
  HandleUpstream, HandleClientConnect) for the per-tenant
  `/api/v1/mcp/gateway/*` route family. Reuses `edge.Store` for evidence
  persistence (no parallel store) and surfaces tenant resolution +
  gateway-enable lookup as injectable function fields on `GatewayDeps` so the
  API gateway can plug in its existing `resolveTenant` /
  `requireTenantAccess` / `auth.FromRequest` helpers without inventing
  wrapper types.
- `core/infra/config/mcp.go` now owns `MCPPolicy`, including
  `GatewayEnabled bool`, `UpstreamServers []UpstreamServerConfig`, and
  `AllowedUpstreams []string`. Default `false` ships fail-closed per DoD #1;
  EDGE-101 will populate the runtime upstream registry and wire per-tenant
  enable lookup from the config snapshot. Existing single-server MCP
  endpoints preserved; gateway-mode is strictly additive.
- `core/controlplane/gateway/gateway.go` adds `mcpGatewayHandlers(s)` helper
  that constructs the gateway against `s.edgeStore` (or substitutes a
  503 `gateway_unavailable` stub when the store is unavailable) and
  registers all four routes through the existing `s.registerRoute` +
  `s.instrumented` pipeline. Route table + OpenAPI coverage tests both
  pass.
- Event kinds `mcp.server.connected` + `mcp.server.failed` (pre-existing
  at `core/edge/event.go:51-52`) are now emitted only through
  store-supported EdgeSession + AgentExecution evidence roots with the
  resolved tenant + principal — never from body claims (task rail #3).
  Connect-success creates EdgeSession + AgentExecution + connected event.
  The P1 failed-event case is GatewayEnabled=true with no upstream
  configured; bootstrap/store failures before an AgentExecution exists are
  logged structurally instead of being claimed as orphan events.
- New OpenAPI operations: `getMcpGatewayHealth`, `getMcpGatewayConfig`,
  `postMcpGatewayUpstream`, `postMcpGatewayClientsConnect`.
- New tests: `core/mcp/gateway_skeleton_test.go` and
  `core/mcp/gateway_skeleton_redis_test.go` cover health,
  config-redaction, disabled-default zero records, no-upstream failed-event
  persistence via real RedisStore, tenant attribution, missing tenant,
  append-failure handling, and no orphan failure events; plus
  `core/edge/event_mcp_server_test.go` (2 wire-string constants).
- Dashboard surfacing of gateway events deferred to EDGE-105
  (task-a04699dc) — no dashboard files touched in this task.
- EDGE-101 (upstream registry) + EDGE-104 (real client attach) + EDGE-105
  (dashboard surface) build on this contract.

#### EDGE-152 — cordum-agentd keychain + service bootstrap hardening (2026-05-15, task-00320a80)

- New `core/edge/keychain` package wraps the host OS-native credential
  store (macOS Keychain, Linux Secret Service / libsecret, Windows
  Credential Manager) behind a small `Keyring` interface with a mock
  for tests and a `LoadSecret` helper that selects between strict and
  dev bootstrap policies. Backed by `github.com/zalando/go-keyring
  v0.2.8`.
- `cmd/cordum-agentd` now sources `CORDUM_AGENTD_NONCE` +
  `CORDUM_API_KEY` from the OS keychain at startup before `LoadConfig`
  consumes the env map. Strict mode
  (`CORDUM_AGENTD_STRICT=true` or `CORDUM_EDGE_POLICY_MODE=enterprise-strict`)
  fails closed with a `BOOTSTRAP-FAIL:` diagnostic when the keychain
  is unavailable or unprovisioned; dev mode emits a structured warn
  and falls back to the env value (redacted in logs). The pre-existing
  `redactForStderr` / strict-mode codepath is unchanged. Closes the
  EDGE-031 P0 tradeoff where same-user `ps -E`, `/proc/<pid>/environ`,
  and shell history could expose runtime credentials.
- Service-manager templates: `tools/scripts/launchd/com.cordum.agentd.plist`
  (user-mode launchd), `tools/scripts/systemd/cordum-agentd.service`
  (systemd `--user`, hardened with `NoNewPrivileges` /
  `ProtectSystem=strict` / `SystemCallFilter`), and
  `tools/scripts/windows/cordum-agentd-service.xml` (WinSW). All three
  carry only `CORDUM_AGENTD_STRICT=true` + log level — **no
  secret-bearing env entries**. Provisioning helpers
  `tools/scripts/agentd-install/install.sh` (macOS / Linux) and
  `install.ps1` (Windows) read secret values through sealed prompts
  (`stty -echo` / `Read-Host -AsSecureString`) so values never appear
  on the operator's command line or in shell history.
- Adversarial fixture
  `tools/scripts/agentd-install/synthetic-test/run.sh` provisions
  synthetic test-only secrets, starts cordum-agentd in strict mode,
  and `grep -F`s stdout / stderr / journald / committed unit files for
  the verbatim synthetic bytes. Non-zero exit on any leak.
- Threat model + ops runbook: `docs/security/agentd-keychain.md`
  documents the per-platform mapping, strict/dev mode matrix, trust
  boundary (PREVENTS env-table exposure, shell history, settings.json
  carrying secrets; DOES NOT PREVENT root keychain dump, memory dump,
  social engineering), key-rotation ritual, and structured-log audit
  schema (`keychain.load`, `keychain.env_fallback`,
  `keychain.load.miss`, `keychain.load.unavailable`).
- Dashboard surface for agentd bootstrap status is deferred to a
  sibling task; this work touches no `cordum/dashboard/` files. Sibling
  enterprise-hardening series: EDGE-150 (managed-settings, this
  Unreleased section) and EDGE-151 (binary signing/notarization).

#### EDGE-151 — Hook and agentd binary signing/notarization (2026-05-15, task-909be4cb)

- New `tools/sign/` Go package implements release-time binary integrity:
  pure-Go OpenPGP detached-signature verification over a cosign-compatible
  `SHA256SUMS` manifest, per-binary SHA-256 recomputation in constant time
  via `crypto/subtle`, manifest-path-traversal rejection, and a build-time
  pinned `PinnedReleaseFingerprint` (`-ldflags '-X
  github.com/cordum/cordum/tools/sign.PinnedReleaseFingerprint=<hex>'`)
  that defeats single-file `cordum-release.pub.asc` substitution.
- Release CI (`.github/workflows/release.yml`) ships a two-tier scheme on
  every `v*` tag: Tier 1 always-on GPG-signed `SHA256SUMS` manifest
  produced via `tools/sign/cmd/manifest-cli`, and Tier 2 OS-native code
  signing (Apple Developer ID `codesign --options runtime --timestamp
  --deep --strict` + `xcrun notarytool submit --wait` for darwin
  amd64/arm64; Windows Authenticode `signtool sign /tr digicert /td
  sha256 /fd sha256` for windows amd64) gated on
  `github.repository == 'cordum-io/cordum'` and the relevant secrets.
  Forks without OS-native secrets degrade to Tier 1 with a `::warning::`
  banner and an unsigned-but-hashed manifest. The 5-platform × 3-binary
  matrix (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64,
  windows/amd64 × cordum-hook, cordum-agentd, cordum-claude) is
  cross-compiled on `ubuntu-latest` with `CGO_ENABLED=0`.
- PR-level validation (`.github/workflows/binaries-pr-validation.yml`)
  runs `tools/sign` unit tests + `make release-local` + synthetic
  tampered / unsigned scenarios via `tools/scripts/install_test.sh`,
  plus a key-extension grep guard that fails on any private-key block
  outside `tools/test-keys/TEST-ONLY-*`. The PEM pattern is constructed
  at runtime so the workflow file does not self-match.
- Pre-activation gate in `tools/scripts/install.{sh,ps1}` curls or
  reads the release-dir, imports the trusted pubkey into an ephemeral
  GNUPGHOME, refuses on `unsigned manifest` / `gpg signature invalid` /
  `hash mismatch <name>` / `release pubkey fingerprint <got> does not
  match pinned <want>` / `manifest path traversal` / `post-activation
  hash mismatch` / `codesign verify failed`. Atomic same-fs `mv` with
  cross-fs `cp+rename` fallback, SHA-256 recomputed AFTER move
  (defence-in-depth against sig-then-swap race), `chmod +x`. Dev mode
  via `--dev-allow-unsigned` (`-DevAllowUnsigned` on PowerShell)
  accepts only `tools/test-keys/TEST-ONLY-*` material whose UID carries
  the literal `TEST-ONLY` marker and whose fingerprint is not equal to
  the production pin. Audit-event JSON line per outcome emitted to
  stderr: `{event, hash, path, sig_scheme, fingerprint, reason,
  exit_code}` — no secrets, no full paths.
- `make release-local` (alias `tools/scripts/release-local.sh`) produces
  a host-local dev release in `bin/release-local/` signed by the
  committed TEST-ONLY key under `tools/test-keys/TEST-ONLY-release.*`.
  The TEST-ONLY keypair is regenerable via `tools/test-keys/gen.sh
  [--deterministic]`. Existing hook/agentd fail-closed runtime path
  unchanged — EDGE-151 is release-time integrity only and does not
  modify any file under `cmd/cordum-hook/` or `cmd/cordum-agentd/`
  (task rail #1).
- Threat model + operator runbook: `docs/security/binary-signing.md`
  enumerates what the gate prevents (transit tampering, non-root local
  substitution, accidental corruption) and what it does NOT prevent
  (full-root coordinated swap, GitHub Actions secret compromise,
  Developer ID / Authenticode `.pfx` leak, downgrade attack — OUT OF
  SCOPE per sibling `EDGE-151-DOWNGRADE`, build-environment supply
  chain). Documents pubkey pinning via `-ldflags`, the dual-sign
  rotation procedure, and the BINARY-VERIFY-FAIL triage table.
- Production pubkey provisioning is deferred until Yaron lands the GPG
  release secret triple (`GPG_RELEASE_KEY_PRIVATE`,
  `GPG_RELEASE_KEY_PASSPHRASE`, `RELEASE_FINGERPRINT`); until then the
  install path operates in dev mode only. Dashboard surfacing of
  binary-verify outcomes is deferred to sibling `EDGE-151-DASHBOARD`;
  this work touches no `cordum/dashboard/` files.

#### EDGE-150 — Enterprise managed-settings deployment automation (2026-05-15, task-ebed169a)

- New `cordumctl edge managed-settings <export|verify|rollback-template>`
  subcommand renders `managed-settings.json` + `managed-mcp.json` payloads
  for managed Claude Code workstations, validates a deployed file against
  the 14 enterprise invariants, and re-renders the template atomically for
  synthetic-test rollback. The CLI is operator/MDM-script invoked;
  Cordum never calls Jamf, Intune, or any other MDM API directly.
  (`cmd/cordumctl/edge_managed_settings.go`,
  `cmd/cordumctl/edge_managed_settings_test.go`).
- `core/edge/claude/managed_settings_verify.go` exposes
  `VerifyManagedSettings`/`VerifyManagedSettingsFromPath`, a pure-function
  drift detector that enforces every invariant baked into the template
  generator (`allowManagedHooksOnly`, `disableBypassPermissionsMode`,
  the six required hook families, the three managed-mode env vars,
  `CORDUM_AGENTD_URL` shape, and a serialised-form scan that rejects
  nonce/API-key markers). Bounded by an `io.LimitReader` cap to prevent
  OOM on hostile input.
- `cordumctl edge doctor` adds a `managed_settings_compliance` check
  driven by `--managed-settings-path` /
  `CORDUM_EDGE_MANAGED_SETTINGS_PATH`. Empty path → `skip` so
  non-enterprise hosts do not see a spurious failure; missing file,
  parse error, or any drift → `fail` with a redacted detail line.
- New end-to-end deployment runbook
  `docs/edge/managed-settings-deploy.md` covering the macOS/Jamf
  worked example, Windows/Intune (Settings Catalog + OMA-URI + file),
  Linux/WSL Ansible, post-deploy verification, drift-detection
  cadences, MDM-orchestrated rollback (with the explicit "synthetic
  test fixture only" disclaimer for the CLI rollback path), upgrade
  flow, and operator troubleshooting. Cross-linked from
  `docs/edge/README.md`, `docs/edge/managed-settings-template.md`,
  `docs/edge/cordumctl-edge-doctor.md`, and `docs/edge/cli.md`.

### Fixed

#### Audit Log dashboard surfaces the full SIEM feed (2026-05-15, task-00b82b90)

- The Audit Log page was sourcing only `/policy/audit` (policy-bundle subset:
  rule edits, bundle deployments, signature events) and silently omitting
  every other SIEM-chained event family: MCP tool invocations and approvals,
  Edge action attempts and approvals, worker handshakes and trust changes,
  output policy decisions, delegation lineage, auth events. Operators using
  the dashboard could not see the actions they most needed to audit.
- New `GET /api/v1/audit/events` (`handlers_audit_events.go::handleListAuditEvents`,
  route registered at `gateway.go` § 2.7.2) walks the per-tenant Redis Stream
  populated from NATS `sys.audit.export` via `audit.Chainer`. Cursor
  pagination (opaque Redis Stream IDs), default page 100, hard cap
  `MaxAuditEventsLimit = 200`. Filters: `event_type`, `severity`, `from`/`to`
  (RFC3339), `search` (lowercase substring across action / event_type /
  agent_id / job_id / identity / reason). `auth.PermAuditRead` permission gate.
  Strict tenant scoping via `resolveTenant` + `requireTenantAccess`. 503
  `audit_chainer_not_installed` when the chainer is absent. Defense-in-depth
  `redactExtraSecrets` strips keys matching `(?i)secret|token|password|api[_-]?key|private[_-]?key`
  before serialization. Every successful read emits an `audit.read.events`
  meta-event to the same tenant's stream, closing the audit-of-audit loop.
- OpenAPI spec extended with `getAuditEvents` operation + `AuditEvent` /
  `AuditEventsEnvelope` schemas (`docs/api/openapi/cordum-api.yaml`); orval
  regenerated `useGetAuditEvents` (`dashboard/src/api/generated/audit-export/`).
- Dashboard rewired: `AuditLogPage.tsx` now calls `useGetAuditEvents`,
  with a new `mapEvent` translating the SIEM shape (`identity || agent_id`
  → actor, event_type prefix → resource family, `extra.resource_id` chain
  → resourceId, `reason` → detail, `seq` direct from the chained event) to
  the existing on-screen `AuditEvent` row shape. Event-type filter dropdown
  expanded to include Safety / Policy / MCP / Edge / Worker / Topic / Auth /
  Delegation / License / Action-gates families.
- `useAuditEvents` hook in `src/hooks/useAuditEvents.ts` translates 503 →
  human-readable banner ("Audit chain not installed — contact your
  operator"). The pre-existing `useAudit` hook remains in use for the
  policy-bundle drilldown, correlation, and export paths.
- New reference doc: `docs/audit/list-api.md` (contract, tenant scoping,
  cursor stability under concurrent appends, 503 condition, redaction
  defense-in-depth, distinction from `/audit/verify` and `/policy/audit`).
- Tests: 10 backend subtests in `handlers_audit_events_test.go` covering
  permission gating, tenant scoping, cursor pagination stability,
  event_type/time-range filters, 503 condition, secret redaction with a
  full-body regex assertion, bounded limit clamp, empty-stream
  empty-response, and meta-audit emission. Plus
  `TestHandleAuditEvents_HeavyFilterPagesForward` regression for the
  heavy-filter cursor-forward gap (adversarial-review finding).
  Dashboard suite extended with `useAuditEvents.test.ts` (9 cases now
  including two infinite-pagination regressions), `transform.test.ts`
  `mapAuditEvent` describe block (5 cases), and
  `AuditLogPage.siem.test.tsx` (render-level SIEM coverage with
  NuqsTestingAdapter + MSW).
- Reopen #1 fix (QA finding): Audit Log page now consumes
  `useInfiniteAuditEvents` (new TanStack `useInfiniteQuery` variant in
  `src/hooks/useAuditEvents.ts`) so tenants with >200 SIEM events can
  reach older records via the server's `next_cursor`. A "Load more"
  button below the table fetches the next page; the running-count line
  shows "Showing N events · more available" when the cursor is
  non-empty. The page bundles all loaded pages into a single
  client-side flat list for filter + render. Trailing-whitespace lint
  on `docs/audit.md` (CRLF line endings from a pre-existing block plus
  the new cross-link) resolved by normalising the file to LF.

### Added

#### EDGE-103 — MCP approval hold and resume (2026-05-15, task-968d6646)

- `core/mcp/approval_hold.go` (NEW) — `ProcessApprovalClaim` helper that inspects `tools/call` arguments for a server-reserved `_approval_ref` field, atomically consumes the stored approval via `edge.RedisStore.ClaimApproval`, and returns a typed `ApprovalClaimOutcome` carrying `*edge.ApprovalConflictError` on a fail-closed lifecycle conflict. The `_approval_ref` field is stripped BEFORE the upstream tool handler ever sees the payload; the input hash is bound to the stripped form so a caller-side mutation between hold and resume produces an `args_mismatch` denial.
- `core/mcp/server.go` — new JSON-RPC code `-32096` (`approval_lifecycle_error`) distinct from `-32099` (initial `approval_required`). `handleToolsCall` now calls `ProcessApprovalClaim` BEFORE invokeTool when `WithApprovalHold(deps)` is wired. On any `*edge.ApprovalConflictError` the server returns `-32096` with `error.data {kind, approval_ref, reason}`. The snake_case `kind` enum (`not_found / rejected / expired / consumed / args_mismatch / policy_mismatch / tuple_mismatch / self_approval / cross_tenant`) matches the Go `edge.ApprovalConflictKind` so the wire format and the typed error never drift.
- `core/edge/approval_store.go` — new `ApprovalConflictKind` enum + `ApprovalConflictError{Kind, Reason}` wrapping `ErrApprovalConflict` (existing `errors.Is(err, ErrApprovalConflict)` callers keep working). New `ApprovalClaimRequest.CallerAgentID` field for store-level self-approval defense in depth (refused when matching `approval.Requester` OR `approval.ResolverID`).
- `core/edge/store_redis.go` — new `RedisStore.approvalMaxTTL` field + `WithApprovalMaxTTL(d time.Duration) StoreOption`. EnqueueApproval clips a caller-supplied `ExpiresAt` longer than `(createdAt + max)` to that ceiling so a malicious or buggy caller cannot park an approval indefinitely. Non-positive value disables clipping (legacy behaviour preserved).
- `core/edge/approval_store_redis.go` — replaced `approvalClaimMatches` with `classifyApprovalClaimMismatch` returning typed `ApprovalConflictKind`. Self-approval check evaluated FIRST (attacker-surface priority) so a self-approval attempt cannot be masked by simultaneously mutating a benign field. `ClaimApproval` uses `newApprovalConflict(Kind, reason)` so the JSON-RPC layer can dispatch on `errors.As(*ApprovalConflictError)`.
- New documentation: `docs/edge/mcp-approval-hold.md` (protocol overview, JSON-RPC error catalogue + `kind` enum, args canonicalization, policy-snapshot binding, TTL bounds, consume-once, self-approval defense-in-depth at 3 layers, cross-tenant rationale, audit-event schemas).

#### EDGE-102 — MCP tool-call policy gate (2026-05-15, task-032e01fa)

- MCP `tools/call` requests now route through the Edge action-policy pipeline before upstream forwarding. New entry point `core/mcp.InvokeToolWithPolicy` wires `EvaluateToolCall` → `actiongates.Pipeline` (tenant → file → url → mcp → mutation → provenance) → upstream tool handler, with retry dedupe keyed on `<server>|<tool>|<event_id>`. The MCP server activates the policy path via `MCPServer.WithPolicyGate(server, deps)`; un-wired servers fall through to legacy direct dispatch for dev/test.
- New events on `edge.LayerMCP`: `mcp.tool.pre`, `mcp.tool.post`, `mcp.tool.failed`. Each carries `session_id` / `execution_id` / `tenant_id` / `principal_id` from `CallMetadata`, the redacted argument set, and an `artifact_pointer` when the redacted payload exceeds 64 KiB OR contains a high-severity credential family.
- `core/mcp/argument_redactor.go`: redaction regex set extended with `sk-` (Anthropic-style API keys) and `gh[opusr]_` (GitHub PAT/oauth/user/server/refresh tokens). Existing AKIA / sk_live_ / JWT / PEM coverage preserved. Field-name list (password / api_key / token / secret / private_key / authorization / etc.) unchanged.
- New defense-in-depth completeness check `verifyRedactionCompleteness`: after the configured redactor runs, the output is re-scanned for the high-severity sentinel set; if any pattern survives (rule misconfig, partial match, hostile stub), `EvaluateToolCall` returns `redaction_failed` and emits no event. Contract: no raw credential ever lands in a Redis-persisted audit row even when the upstream rule set is incomplete.
- New `core/mcp.CanonicalActionHash(tenant, server, tool, target_path) string` — exported SHA-256 over the normalized tuple that identifies an MCP tool call for approval-lifecycle binding. `BuildActionDescriptorFromToolCall` now extracts the first matching `path` / `file_path` / `target_path` / `filepath` arg and normalizes backslash → forward slash before setting `descriptor.TargetPath`, so Windows and POSIX callers operating on the same logical file converge on a single approval key.
- `core/mcp.MaxToolCallArgsBytes = 1 MiB` hard cap on serialized args; enforced on `json.Marshal` byte length (multibyte UTF-8 cannot smuggle past via lower rune count). Inline event budget is `edge.MaxInputRedactedBytes` = 64 KiB; oversized redacted payloads are written to `edge.ArtifactStore` with a 4 KiB inline summary plus pointer; small payloads with a high-severity finding also get an artifact pointer so forensics retain the full redacted context.
- `REQUIRE_HUMAN` decisions route through the existing `gatewayApprovalGate` with precedence: MCP invariants (always wins) > preapproval lookup > `MCPApprovalStore.ClaimPreApproved` > `EnqueueMCPApproval`. The bridge's pre event carries `decision = require_approval` so the audit trail records the gating point even when resolution is async.
- New documentation: `docs/edge/mcp-tool-policy.md` (gate inputs, verb classification, request flow, decision semantics, redaction field list, artifact-pointer thresholds, tenant isolation contract).

#### Dashboard Phase 5e — per-route error boundaries with route-scoped fallback (2026-05-09, task-adc04293)

- `dashboard/src/components/RouteErrorFallback.tsx`: route-scoped error UI composed from existing `ErrorBanner` primitive plus a Bug-icon mailto "Report bug" link. The mailto body URL-encodes the route name, error message, full stack, and user-agent so the bug report is actionable on first read.
- `dashboard/src/components/RouteBoundary.tsx`: thin wrapper that pairs `ErrorBoundary` with `RouteErrorFallback`, using `useLocation().pathname` as the boundary's `resetKey` so the user navigating away from a broken route auto-clears the boundary state.
- `dashboard/src/components/ErrorBoundary.tsx`: extended with an optional `fallback?: (props: { error, reset }) => ReactNode` render prop. When supplied, the boundary defers to the consumer's UI instead of rendering its built-in "Something went wrong" full-page card. Public type `ErrorBoundaryFallbackProps` is exported for downstream typings.
- `dashboard/src/App.tsx`: every leaf-page route is now wrapped in `<RouteBoundary name="…">`, surfacing a route-specific error UI instead of the generic fallback. Pure-redirect routes (`<Navigate>`-returning components) are not wrapped — they don't render UI. The outermost `ErrorBoundaryWrapper` is retained as the last-resort safety net for AppShell-/Suspense-level render failures that bypass any specific Route.
- Tests: 12 new tests added (5 covering the ErrorBoundary primitive's fallback + reset behavior; 3 integration tests exercising RouteBoundary for throw/navigate-clears/Retry semantics; 4 covering RouteErrorFallback's title + Retry + mailto + generic-message fallback). One pre-existing `App.copilot-session-route.test.tsx` registration-guard regex was loosened from `/element=\{<CopilotSessionPage\b/` to `/<CopilotSessionPage\b/` so the per-route boundary wrap doesn't break a future-friendly drift check.
- DoD #1 (designed empty states across 9 list pages) was fully met by existing code per the inventory in step-1: AgentsPage / JobsPage / ApprovalsPage / AuditLogPage / EdgeSessionsPage / TopicsPage / SchemasPage / PacksPage / govern/QuarantinePage all already render `<EmptyState icon=… title=… description=… action=…>` from `components/ui/EmptyState`. No page redesign needed; the audit table is captured in the task's complete_step note for step-2.

#### Dashboard 5 — Bundles surface: list + detail-with-tabs (2026-05-09, task-220d263a)

- `dashboard/src/pages/policies/BundlesPage.tsx`: filter bar + DataTable list at `/policies/bundles` with scope/search nuqs URL state, status dot, "+ New bundle" CTA. Row-click navigates to detail page. (Step 3, by worker-6b22 commit `22da3212`.)
- `dashboard/src/pages/policies/BundleDetailPage.tsx`: detail page at `/policies/bundles/:id` with the unified `Tabs` primitive and 4 panels — Rules / Versions / Deployments / Diff. Active tab in URL via nuqs (`?tab=rules|versions|deployments|diff`). Each tab is a separate lazy-loaded chunk. Per-tab status badge + back link in PageHeader. (Step 4, worker-6b22 `f064a55b`.)
- `BundleRulesTab.tsx`: rules-in-bundle list reusing the Rules surface row format; "Add rule…" + "+ New rule in this bundle" affordances. (Step 5, worker-6b22 `f064a55b`.)
- `BundleVersionsTab.tsx`: vertical timeline of versions newest-first with author + commit hash + rule-count delta; per-row "Compare with…" picker that sets `?tab=diff&from=...&to=...`. (Step 6, worker-6b22 `f064a55b`.)
- `BundleDeploymentsTab.tsx`: scope×version matrix consuming Backend 2's `GetActiveDeployment` grouping. Click-to-Promote (empty cells) / click-to-Rollback (active cells) gated by `ConfirmDialog`. New `useDeployBundle` + `useRollbackBundle` mutations call Backend 2's `POST /policy/bundles/:id/deploy` + `/rollback`. a11y: row/columnheader scoping + per-cell aria-labels naming the action verb + scope. (Step 7 by worker-c1cf, commit `fa13f3ad`.)
- `BundleDiffTab.tsx`: read-only Monaco DiffEditor side-by-side comparison of two version snapshots' YAML-serialized rule sets, with a summary row "X added · Y removed · Z modified" computed from id-keyed comparison. nuqs URL state `?from=&to=` for deep-link compatibility with the Versions-tab "Compare with…" picker; pickers when unset. DiffEditor lazy-loaded so monaco-editor only ships when the Diff tab is activated. (Step 8 by worker-c1cf, commit `6e3d5a72`.)
- New `dashboard/src/hooks/useBundle.ts` exporting `useBundle/useBundleVersions/useBundleDeployments`; `dashboard/src/hooks/useBundleVersion.ts` (single-version fetch); `useDeployBundle.ts` + `useRollbackBundle.ts` (mutations with cache-invalidation + toast wiring).
- New MSW defaults in `dashboard/src/test-utils/handlers.ts` for every Backend 2 endpoint the new tabs consume (`/policy/bundles`, `/:id`, `/:id/versions`, `/:id/versions/:version`, `/:id/deployments`, `/:id/deploy`, `/:id/rollback`).
- Page tests covering DoD #5: `BundlesPage.test.tsx` (list + filter URL), `BundleDetailPage.test.tsx` (tab navigation), `BundleDeploymentsTab.test.tsx` (matrix + ConfirmDialog wiring), `BundleDiffTab.test.tsx` (Monaco props + summary). DASHBOARD VERIFICATION RAIL: tsc 0 / vitest 238 files / 2052 tests / build 0.
- Note: deployment-timeline Gantt visualization is task Dashboard 6 (`task-7b2862f8`); the matrix is the v1 view.

#### Dashboard verification rail finalization (2026-05-09, task-347388c0 reopen #1)

- Updated `dashboard/docs/process/rail-vitest-green-verification.md` with the approved-rail excerpts post Yaron 2026-05-09 sign-off. Three rails now live in `project.globalRails.customRules` (verbatim text in the doc): DASHBOARD VERIFICATION RAIL (prop-8cc95268), DASHBOARD QA REJECTION FORMAT (prop-5a162a16), and PRE-SUBMIT DOD CHECKLIST (Yaron-2026-05-09). PENDING block annotated with the resolution; original placeholder retained as audit trail. Field-correction note added: rails live in `project.globalRails.customRules`, NOT `allRails.global` (the latter is empty in fetched contexts; the prior reopen mistakenly cited that field).
- Closes the task-347388c0 reopen #1 from QA: DoD #1 (rail exists) + #3 (visible to architects via moe.get_context) verified directly by architect-697e (chat msg-223382a3).

#### Dashboard 1 — Policy Studio foundation routes + type adapters (2026-05-09, task-5d354964)

- Three new lazy-loaded routes wire up the Policy Studio v3 IA per epic-d9a6c0a1 spec: `/policies` (PoliciesPage — Rules surface), `/policies/bundles` (BundlesPage), `/policies/decisions` (DecisionsPage). Each shell renders only `PageHeader` + `EmptyState` from existing primitives — Dashboards 2/5/8 fill the bodies.
- `/govern/overview` becomes a redirect handler (`GovernOverviewRedirect` inline in App.tsx). Reads `?tab=` + `?mode=` and navigates to the new IA: input/output/velocity → `/policies?type=…`; bundles → `/policies/bundles`; scope → `/policies/bundles?view=scope`; evaluation → `/policies/decisions` (preserves `mode`); unknown/missing → `/policies` (Rules default). The existing `/govern/<tab>` PolicyTabRedirect chains through here, so all legacy bookmarks stay valid.
- New `src/lib/policy-studio/` package adds 4 type adapters used across the unified shapes: `ruleTypeLabel` + `ruleTypeIcon` (lucide), `decisionTypeLabel`, `decisionTone` (5-tone palette), `edgeModeLabel`. Co-located test exhaustively iterates every `Object.values(Enum)` variant so missing enum values fail at test time.
- Stale `GovernPolicyOverviewPage` lazy import dropped (TS6133); component file retained on disk for Dashboard 11 cut-over deletion.
- No new shared primitives. No new colors. Routing remains on `react-router-dom` per v2.5 rail.

#### Phase 5d — bundle-size visualizer + soft CI gate (2026-05-09, task-50bbfd7d)

- Installed `rollup-plugin-visualizer@7.0.1` and wired it into `dashboard/vite.config.ts` so every `pnpm run build` emits `dist/stats.html` (treemap, raw + gzip + brotli). `emitFile: true` keeps the file inside `dist/`.
- New `dashboard/scripts/parse-bundle-stats.mjs` reads `dist/assets/*.js` and prints a per-chunk markdown table (raw + gzip + brotli) with totals and the initial-chunk highlight. Soft thresholds (warn-only; always exits 0): initial ≤ 400 KB raw / 120 KB gzip, total ≤ 3100 KB raw / 950 KB gzip — sized ~25-30% above the 2026-05-09 baseline (initial 305 KB / 92 KB gzip; total 2533 KB / 759 KB gzip captured in `dashboard/docs/code-hygiene-sweep.md`).
- `.github/workflows/ci.yml` `dashboard-test` job now: (a) declares `pull-requests: write` permission, (b) runs `pnpm run build` after vitest, (c) computes the bundle-size markdown via the parser, (d) uploads `dist/stats.html` + the markdown as the `dashboard-bundle-stats` artifact (14-day retention), (e) on `pull_request` events posts the markdown via `peter-evans/create-or-update-comment@v4` with `body-includes: "<!-- bundle-size-report -->"` so subsequent pushes update the same comment instead of appending. Main pushes still build + parse but skip the comment step.
- `dashboard/CLAUDE.md` "Bundle size (Phase 5d)" section documents the threshold values, how to read `dist/stats.html` locally, and where to find the baseline.

#### Backend 1.5 — policy-studio yaml additions on dashboard branch + regenerated dashboard TS (2026-05-09, task-e38d99a5)

- `docs/api/openapi/cordum-api.yaml`: surgically extracted Backend 1's 14 unified Rule/Decision/Bundle schemas (lines 10989–11256 of `origin/policy-studio-backend`) and appended them to dashboard branch's yaml so the orval pipeline can regenerate dashboard TS for downstream Dashboard 1+ tasks. Bumped `info.version` `2026-05-09.2` → `2026-05-09.3` (next-after worker-dac4's /policy/audit enrichment .2). worker-dac4's existing /policy/audit edits at lines 1, 3266, 9876, 9883 untouched (disjoint regions).
- `dashboard/src/api/generated/`: regenerated via `pnpm run generate-api`. 17 new TS files surface the unified shapes — `model/{rule,ruleType,ruleStatus,ruleScope,ruleScopeKind,decision,decisionType,decisionSource,bundle,bundleVersion,bundleMetadata,edgeMode,auditMetadata,traceStep}.ts` plus 3 sub-schemas (`ruleMatch`, `ruleDecide`, `traceStepConstraints`). 502 existing files received only the `OpenAPI spec version` comment bump (no semantic changes).
- Bridges Backend 1 (`origin/policy-studio-backend` PR) → Dashboard 1+ track. Per Yaron 2026-05-09 directive (commit/push, no merge until full epic done), the PR opens against `dashboard` and stays in REVIEW; the regenerated types live as commits on dashboard branch and are consumable immediately by parallel Dashboard tasks.

#### Phase 5c command palette recent jobs/agents (2026-05-09, task-095927f8)

- `dashboard/src/components/CommandPalette.tsx` now appends two dynamic groups to the `cmd+k` palette: **Recent Jobs** (top 50, label `${topic} · ${id-prefix}`, deep-link `/jobs/:id`, keywords include id/topic/status/capabilities) and **Recent Agents** (top 50, label `name||id`, deep-link `/agents/:id`, keywords include id/name/pool/status/capabilities). Data sourced from existing `useJobs({ limit: 50 })` and `useWorkers()` React Query hooks; cache-aware so palette open shows last-known recent without an extra fetch.
- Existing static commands (Navigate / Govern / Settings) render unchanged; new sections render below them.
- Fuzzy keyword match is now case-insensitive (`k.toLowerCase().includes(q)`) so mixed-case dynamic keywords (job ids, names) match consistently with the lowercase static keywords.
- `?` keybind to open `KeyboardShortcutsHelp` modal already wired via `dashboard/src/hooks/useKeyboardShortcuts.ts:102-108` with `isEditableTarget` guard at L77-83 (skips when target is INPUT/TEXTAREA/SELECT/contentEditable). Confirmed during inventory; no code change needed for that DoD item.
- New tests: 3 render-based cases in `CommandPalette.recent.test.tsx` covering the two dynamic groups + the empty-state path.

#### Phase 4 drift sweep follow-up #2 closure (2026-05-09, task-82593815)

- `dashboard/src/pages/DesignSystemConvergence.test.ts` extended with a comprehensive sweep test using `import.meta.glob('./**/*.tsx', { query: '?raw', import: 'default', eager: true })` (test files excluded at the glob level). Asserts `RAW_CONTROL_RE` (/<(input|select|textarea)\b/) does not match across `src/pages/**/*.tsx` outside the documented carve-out set. Forward-compatible — catches new pages that introduce raw native form controls without manual list maintenance.
- Companion regression detector: `carve-out pages still hold raw controls` test asserts `LoginPage` + `RunDetailPage` continue to contain raw controls; if a future migration accidentally rewrites them, this test fails forcing a coordinated docs + carve-out-set update rather than silent drift.
- Carve-outs documented inline (linking to `mem-df8a90aa` and `dashboard/docs/design-system-audit.md`): `LoginPage` (native HTML form for browser autofill / password manager interop on auth surface) + `RunDetailPage` (workflow-run console exempted, DoD-3 register).
- DoD #1 (23 pages migrated) confirmed by re-grep at HEAD: zero pages outside the carve-outs contain raw controls — the migration completed via parallel-worker activity (commits prior to claim). No code changes required for batch migration steps.

#### Phase 5a a11y test gate (2026-05-09, task-bf55ddbd; reopen #1 fix)

- `renderWithProviders` (`dashboard/src/test-utils/render.tsx`) accepts an opt-in `runAxe: true` option that returns a `Promise<RenderWithProvidersResult>` and asserts **zero WCAG 2 A/AA violations of any impact** on the rendered container. Strict gate per DoD: only `color-contrast` is disabled (jsdom can't composite backdrop-filter; Lighthouse CI / Phase 5b owns color-contrast). Existing callers without `runAxe` stay synchronous and unchanged. Optional `axeMode: "light" | "dark"` selects the theme axe runs against.
- New `dashboard/src/pages/NotFoundPage.test.tsx` page test opts in via `runAxe: true` to demonstrate the strict gate at the page level — NotFoundPage renders fully synchronously (no async data) so the post-render axe pass exercises the actual customer DOM, not a loading skeleton.
- `dashboard/src/components/UserMenu.test.tsx` first test also opted in (additional component-level coverage; UserMenu's idle render is axe-clean).
- New `dashboard/eslint.a11y.config.mjs` — narrow flat config that escalates the gate-relevant jsx-a11y rules (alt-text, ARIA correctness, heading-has-content, anchor-has-content, iframe-has-title) to `error`. `pnpm run lint:a11y` rewritten to point at this config; cross-platform safe (replaces the broken JSON-arg form that failed under PowerShell shell-quoting).
- `dashboard/src/components/ui/Card.tsx` `CardTitle` refactored to render `<h3>{children}</h3>` (was self-closing with spread props) so `jsx-a11y/heading-has-content` can statically verify content. No behavior change.

#### Workflow governance overlay fields (2026-05-08, task-913b6c6c)

- `WorkflowStep.policy_gate?: "allow" | "deny" | "require_approval"` — optional design-time policy hint surfaced on the WorkflowStudio governance overlay before any run. Validated at workflow-save time; unset means "no hint" (overlay defers to runtime safety decision).
- `WorkflowRunStep.audit_hash?: string` — optional 64-char hex audit-chain event hash for the step's job. Populated by run-record builders that join `StepRun.JobID → SIEMEvent.JobID → SIEMEvent.EventHash`. Populate-strategy left to producers; unset for skipped/upstream-failed steps.
- OpenAPI: `info.version` bumped `2026-04-21.2` → `2026-05-08.2` (reopen #1: `2026-05-08.1` shipped only `WorkflowStep.policy_gate`; `2026-05-08.2` adds `RunStepStatus.audit_hash` so the dashboard governance overlay's audit-hash chip data path is fully contracted). `WorkflowStep.policy_gate` enum + description added under `components/schemas/WorkflowStep`. `RunStepStatus.audit_hash` (nullable string, audit-chain event hash) added under `components/schemas/RunStepStatus`.

#### Runtime WorkflowRunStep.AuditHash population (2026-05-08, task-a45b8eb1)

- New `audit.StepHashSink` interface + `audit.WithStepHashSink` ConsumerOption: optional dependency the audit consumer uses to back-fill `StepRun.AuditHash` after a successful `chainer.Append`. Architectural choice per Option A scope-split (eventual-consistency write-back from the audit-consumer goroutine; the workflow engine cannot synchronously compute the chain hash because `PrevHash` depends on the chain head held by the chainer).
- Post-Append hook in `core/audit/consumer.go handle()`: when chain Append succeeds AND a sink is wired AND the SIEMEvent has both `EventHash` and `JobID`, the consumer calls `sink.UpdateAuditHash(ctx, jobID, eventHash)`. Sink errors are non-fatal (logged + swallowed) — the audit chain entry is durable; the workflow store can be back-filled offline if the sink misses transiently.
- `workflow.RedisStore` now implements the sink: run writes maintain a `StepRun.JobID → run/step` Redis lookup, `UpdateAuditHash` idempotently persists the first audit-chain hash on matching top-level and nested step records, and pending hashes are applied on the next run write if the audit event arrives before the step's JobID index.
- Gateway audit pipeline wiring passes the workflow store into both NATS consumer mode (`audit.WithStepHashSink`) and direct/chain-only senders, so new workflow runs populate the dashboard governance overlay's audit-hash chip as soon as their SIEMEvent is appended to the chain. Skipped/upstream-failed/no-entry steps remain unset.

### Fixed

#### UpdateRun lost-update race for concurrent AuditHash writes (2026-05-09, task-a45b8eb1 reopen #2)

- `core/workflow/store_redis.go`: replaced the legacy two-phase Lua-then-Go-merge `UpdateRun` body with an atomic Lua script that performs GET-merge-SET as a single Redis command. The script walks the persisted run's StepRuns (recursively, including `children`) and forwards any populated `audit_hash` into the new payload's StepRuns whose `audit_hash` is empty for the same `job_id`, then SETs the merged payload. This closes the lost-update race the previous reopen left open: a stale UpdateRun whose caller marshaled before a concurrent `UpdateAuditHash` succeeded would otherwise have erased the just-written hash on its SET. With merge baked into the Lua, the GET inside the script always sees the current persisted state, so the race window collapses to zero. Index updates remain in a separate idempotent pipeline (cluster-safe, eventual-consistency tolerant). Removed the now-redundant Go-side `mergePersistedAuditHashes` helper and its tree-walk subroutines; pending-hash recovery (via `wf:run:pending_audit_hash:<jobID>` keys) still runs Go-side for the case where the audit event lands before the run/step is persisted at all.
- `core/workflow/store_redis_test.go`: added two regressions. `TestRedisStoreUpdateRunPreservesAuditHashAcrossInFlightRace` reproduces the exact race state — the audit consumer wrote the hash atomically while the caller's UpdateRun payload was built without it — and asserts that the SET preserves the hash and lands the caller's status mutation. `TestRedisStoreUpdateRunPreservesAuditHashUnderConcurrentInFlightAuditWrite` exercises 200 iterations of concurrent `UpdateRun + UpdateAuditHash` goroutines on the same run/job and asserts the hash survives in every iteration regardless of goroutine ordering. Both tests fail deterministically on a Lua-merge-disabled control implementation, proving they catch the lost-update path.

#### Backend 6 — pack `metadata.aliases` + safety-policy `constraints` extensions (2026-05-10, task-e4e9489c)

- **Pack manifest**: `metadata.aliases: []string` is now optional and additive. When set, topic / pools-patch / timeouts-patch namespace checks accept `job.<id>.*` AND `job.<alias>.*` for each declared alias. Regex `^[a-z][a-z0-9_-]{1,30}$`, max 8 entries, duplicates rejected. Unblocks the CordClaw pack owning `job.openclaw.*` topics under `metadata.id: cordclaw` (task-1e446868). Existing packs without aliases keep validating under the strict prefix rule. See `docs/operations/pack-aliases.md`.
- **Safety-policy `constraints`** schema (`core/infra/config/schema/safety_policy.schema.json`) now permits three additional fields under `rule.constraints`:
  - `max_output_bytes` (integer, 0–16 777 216 / 16 MiB upper bound enforced to prevent OOM via misconfigured large outputs).
  - `allowed_destinations` ([]string — write-target allowlist).
  - `redact_patterns` ([]string — regex patterns redacted before downstream emission).
  Schema retains `additionalProperties: false`; typos still fail. Existing policy fragments without these fields validate unchanged. See `docs/operations/safetykernel-constraints.md`.
- Both changes are additive only — no schema-version bump on either surface. CLI + gateway validators (`cmd/cordumctl/pack.go`, `core/controlplane/gateway/packs/validate.go`) are mirror-updated; pools-patch and timeouts-patch validators thread aliases through. Sibling invariants (pack-id regex, schema-id prefix, workflow-id prefix, pool-name prefix) are unchanged — only the topic namespace check honors aliases. Tests cover alias regex, count cap, duplicates, back-compat, and the new constraint bounds.

#### Cordum Edge P0 (2026-04-30)

EDGE epic shipped 32 P0 tasks for the Compliance Firewall surface — local
hook + agentd + Gateway Edge APIs + Safety Kernel evaluate + approvals +
artifact pointers + dashboard Edge Sessions. P0 final acceptance signed off
under EDGE-032 on 2026-04-30; product, API, CLI, and demo docs are at
[docs/edge/README.md](docs/edge/README.md) with the new-engineer
30-minute walkthrough at [docs/quickstart-edge.md](docs/quickstart-edge.md).

- EDGE-001: P0 architecture decisions and acceptance gate lock
- EDGE-002: Edge data model contracts and JSON schemas
- EDGE-003: Redis Edge store for sessions, executions, events, and indexes
- EDGE-004: Edge redaction and stable input hashing helpers
- EDGE-005: Gateway Edge session and execution APIs
- EDGE-006: Gateway Edge event write, batch, and read APIs
- EDGE-007: edge.event WebSocket stream envelope
- EDGE-008: Deterministic edge action classifier and policy input mapper
- EDGE-008.5: Post-review hardening from PR #243 senior review
- EDGE-008.6: Classifier shell allowlist inversion and adversarial safety tests
- EDGE-008.7: Edge API error shape, event idempotency, and OpenAPI contract
- EDGE-008.7.1: atomic event append + idempotency completion
- EDGE-008.7.2: action_hash determinism — sort RiskTags + add InputHash to approval CAS match
- EDGE-008.8: ADR-010 agentd transport reconciliation
- EDGE-009: Gateway edge evaluate API with Safety Kernel integration
- EDGE-010: Edge policy templates and simulation fixtures
- EDGE-011: Edge approval lifecycle store and Gateway APIs
- EDGE-012: Approval retry and optional inline wait contract
- EDGE-012.1: bind /api/v1/edge/approvals/{ref}/wait to original requester principal
- EDGE-012.2: bind /api/v1/edge/approvals list results to requester principal
- EDGE-013: Edge artifact pointers and evidence export bundle
- EDGE-014: Edge audit events, metrics, and structured logging
- EDGE-015: cordum-hook binary core contract
- EDGE-015.1: cordum-hook DefaultHookTimeout (10s) exceeds Claude's documented 5s deadline
- EDGE-016: Claude Code hook input/output mapper
- EDGE-017: cordum-agentd session manager, heartbeat, and local socket
- EDGE-017.1: cordum-agentd nonce externalization for trusted launchers
- EDGE-017.2: heartbeat OnStatus persists detached from shutdown — final state can be overwritten
- EDGE-017.3: hook receipt event split from decision event on shutdown — half-written audit record
- EDGE-017.4: agentd loopback nonce written to settings.json plaintext — same-user impersonation
- EDGE-017.4.1: Remove deprecated agentd ?nonce= query-param accept after one release
- EDGE-017.5: agentd state-store Windows ACL hardening
- EDGE-018: agentd evaluate client, caching, approvals, and fail modes
- EDGE-018.1: agentd evaluator request coalescing for concurrent identical hooks
- EDGE-019: cordumctl edge claude launch wrapper
- EDGE-020: Claude settings and enterprise managed-settings generators
- EDGE-021: cordumctl edge doctor and local diagnostics
- EDGE-022: Dashboard Edge API types, hooks, and stream invalidation
- EDGE-023: Dashboard Edge Sessions list page
- EDGE-024: Dashboard Edge Session detail timeline and event inspector
- EDGE-025: Dashboard Edge approvals drawer and artifacts panel
- EDGE-026: Dashboard Agent Executions panels on Job and Workflow Run detail
- EDGE-027: Local fake-hook E2E for P0 acceptance
- EDGE-028: Backend integration tests and regression suite
- EDGE-029: Edge docs — product, API, config, CLI, and demo
- EDGE-030: Demo polish and operator runbook
- EDGE-031: Security review and threat-model closure for P0
- EDGE-032: P0 final acceptance, demo signoff, and release readiness

#### Lighthouse CI gate for /login (epic-252d2c07 Phase 5b)

CI now runs Lighthouse against the unauth `/login` surface on every PR
and posts performance / accessibility / best-practices / SEO scores as a
PR comment.

- **`@lhci/cli` 0.15.1** added to `dashboard/devDependencies`.
- **`dashboard/lighthouserc.json`** — desktop preset, 3 runs averaged,
  `temporary-public-storage` upload target. All assertions `warn`-mode
  (perf ≥ 0.7, a11y ≥ 0.9, best-practices ≥ 0.85, SEO off) — no
  PR-blocking yet.
- **`.github/workflows/ci.yml` new `lhci-login` job** — PR-only,
  `continue-on-error: true`, `pull-requests: write` permission for
  comment posting. Reads `.lighthouseci/manifest.json` to format a
  markdown score table via `actions/github-script@v7`.
- **Local run**: `pnpm run lhci` from `dashboard/` (uses
  `start-server-and-test` to boot `vite preview` and tear it down
  cleanly after `lhci autorun`).

Authenticated-surface lhci (HomePage / JobsPage / AuditLogPage / etc.)
deferred to follow-up task **task-63603c2e** (cookie-bridge + test
credentials required).

#### OpenAPI /policy/audit enrichment (epic-252d2c07 follow-up to task-55f813b3)

Closes the spec drift between the gateway handler and the OpenAPI spec for
`GET /api/v1/policy/audit`:

- 9 query params declared (`limit`, `offset`, `action`, `agent_id`,
  `after`, `before`, `search`, `rule_id`, `type`) matching the
  gateway handler at
  `core/controlplane/gateway/handlers_policy_bundles.go:805`.
- Response shape changed from bare `PolicyAuditEntry[]` to a typed
  `PolicyAuditEnvelope` (`{items, total, has_more, offset}`) matching
  the actual handler payload.
- `PolicyAuditEntry` schema enriched from 7 fields to 25 fields
  (existing 7 stay; 4 of them now `deprecated: true` since the backend
  doesn't populate them; 18 backend-only fields added — `resource_*`,
  `actor_id`, `role`, `auth_source` (new `AuthSource` enum),
  `agent_*`, `bundle_ids` plural, `reason`, `decision`,
  `matched_rule`, `policy_version`, `extra`, `snapshot_before` /
  `snapshot_after`, `created_at`).
- `info.version` bumped to `2026-05-09.2`.

Dashboard: `dashboard/src/pages/AuditLogPage.tsx` swapped from a manual
`get<AuditResponse>('/policy/audit?...')` call to the regenerated
`useGetPolicyAudit` hook. The previous `PolicyAuditEntry &
Record<string, unknown>` bridge intersection (added in task-55f813b3
step-7) is removed; the page now consumes the typed shape directly.

#### Cordum Edge P0 cleanup (2026-05-03)

Post-acceptance cleanup batch on top of P0 ship — Docker stack reproducibility,
PRD/roadmap freshness, docs visibility split (public + private subtrees),
PR #243 reviewer follow-ups, fanout bounds, typed-error refactor, hook output
parser alignment, dashboard inspector visibility, gateway approval auto-consume,
and a Windows-specific settings rendering bug.

- EDGE-029.1: PRD.md and PRD_ROADMAP.md freshness sweep for post-2026-04-30 P0 progress
- EDGE-033: Full Docker stack build + end-to-end validation + user-facing run instructions
- EDGE-034: Fix all PR #243 workflow failures (feature/cordum-edge-p0)
- EDGE-035: Address all open reviewer comments on PR #243
- EDGE-036: Split cordum/docs into public + private subtrees with explicit visibility policy
- EDGE-037: Bound Edge execution fanout and large-session deletion cleanup
- EDGE-038: Replace Edge gateway string-matched validation errors with typed errors
- EDGE-039: cordum-hook output parser alignment + EDGE-033 fake-hook E2E continuation to 5/5 PASS
- EDGE-040: Edge session shows 0 events in dashboard timeline despite running status — investigate hook→agentd→Gateway event write-back gap
- EDGE-041: user_prompt_submit + PreToolUse content invisible in dashboard inspector — align hook mapper output with dashboard `_redacted` suffix convention
- EDGE-042: Gateway evaluate does not match freshly-approved approval to new evaluate request — consume retry returns DENY+approval_ref instead of ALLOW
- EDGE-043: EDGE-042 design gaps — rejected/expired approvals silently re-enqueue, 4-page scan limit, no auto-consume unit test
- EDGE-044: EDGE-041 _redacted suffix rename broke Safety Kernel policy matching — every PreToolUse defaults to DENY post-rename
- EDGE-045: cordumctl edge claude renders hook command path with stripped backslashes on Windows — settings.json gets `.bincordum-hook` instead of `./bin/cordum-hook.exe`
- EDGE-046: redactHookBoundaryString over-redacts any prompt containing the substring "secret" — wipes whole value to literal "<redacted>" instead of preserving content
- EDGE-047: cordumctl edge claude UX — 9-flag invocation defeats "drop in front of Claude Code" product story; ship `cordumctl edge init` scaffold + `./cordum.yaml`/`~/.cordum/config.yaml` config auto-discovery + `cordum-claude` standalone shortcut + `--print-config` diagnostic
- BUG-001: TestCollectCopilotSessionDecisionsPaginatesPastUnrelatedTenantDecisions returns len(decisions)=0 want 1 — pre-existing fail in core/controlplane/gateway from PR #233

#### Docs visibility split (EDGE-036)
- Split `cordum/docs/` into PUBLIC (root) + PRIVATE (`docs/internal/`)
  subtrees per `docs/visibility-policy.md`. 26 internal-only audit /
  threat-model / SEC-issue-draft / cleanup-sweep / decision-log /
  acceptance-evidence files moved under `docs/internal/` via `git mv`
  (history preserved). PUBLIC docs cite only PUBLIC docs; verification
  gates documented in `docs/visibility-policy.md`. Banner at
  `docs/internal/INTERNAL.md`.

#### Cordum Edge P0 documentation and Claude Code launcher
- Added Edge product, API, configuration, CLI, and demo documentation for the P0
  Compliance Firewall surface (`docs/edge.md`, `docs/edge-claude-code.md`,
  `docs/demo-edge-claude.md`, and `docs/edge/`).
- Documented `cordumctl edge claude`, local `cordum-hook`/`cordum-agentd`
  behavior, generated settings, approval retry UX, fail modes, token tradeoffs,
  and the developer-wrapper vs enterprise-managed-settings boundary.

#### Control Plane Boundary Hardening
- **Topic registry** (`GET/POST/DELETE /api/v1/topics`) — canonical source of truth for registered topics with pool, schema, pack, and status metadata
- **Submit-time topic validation** — unknown topics rejected with 400 at both gateway and scheduler boundaries; known topics with zero workers stay valid (degraded, `ErrNoWorkers` retry)
- **Submit-time schema enforcement** — job payloads validated against topic's input schema via JSON Schema draft-07. Modes: `SCHEMA_ENFORCEMENT=enforce|warn|off` (default `warn`)
- **Worker credential store** (`GET/POST/DELETE /api/v1/workers/credentials`) — hashed tokens (argon2id) for worker attestation. Modes: `WORKER_ATTESTATION=enforce|warn|off` (default `off`)
- **Worker readiness handshake** — scheduler filters on `ready == true` when `WORKER_READINESS_REQUIRED=true`. Workers must send handshake with `ready_topics` before receiving jobs. Unknown workers allowed (absence ≠ not ready).
- **Dashboard TopicsPage** — unified view of topics with pool, schema, pack, active workers, and degraded indicators
- **cordumctl topic** subcommands: `list`, `create`, `delete`
- **cordumctl worker credential** subcommands: `create`, `list`, `revoke`
- **SDK client methods** — `ListTopics`, `CreateTopic`, `DeleteTopic`, `ListWorkerCredentials`, `CreateWorkerCredential`, `RevokeWorkerCredential`
- **ADR-009** — Architecture Decision Record for canonical `TopicRegistration`, `WorkerCredential`, `WorkerSnapshot` types
- **Pack manifest schema bindings** — `inputSchema`/`outputSchema` fields on pack topic declarations, validated at install time

#### CAP v2.9.0 Integration
- Upgraded CAP dependency from v2.8.6 to v2.9.0
- `Agent.Start()` now publishes handshake automatically in Go, Python, and Node SDKs — all 36+ workers get handshake at startup with zero code changes
- `Heartbeat.auth_token` (field 18) for worker attestation
- `Handshake.ready_topics` (field 6) for readiness declaration
- `publishHandshake()` added to Python and Node SDKs (previously Go-only)
- Migrated all deprecated `SystemAlert` fields (`Level`, `Component`, `Code`) to structured replacements (`Severity`, `SourceComponent`, `ErrorCodeEnum`)

#### Output Policy System
- Two-phase output safety scanning: fast sync metadata checks on scheduler hot path + deeper async content checks over dereferenced result payloads
- gRPC `OutputPolicyService.CheckOutput` contract in `core/protocol/proto/v1/output_policy.proto`
- Output decisions: `ALLOW`, `QUARANTINE`, `REDACT` with typed findings (`secret_leak`, `pii`, `injection`)
- Scanner framework (`core/controlplane/safetykernel/scanners.go`) with configurable output scanners via `config/output_scanners.yaml`
- `OUTPUT_QUARANTINED` job terminal state in scheduler engine
- Output quarantine UX in dashboard: quarantine badge, remediation drawer, artifact panel
- `output_rules` section in safety policy YAML for topic/capability/content-pattern matching

#### MCP Server
- `cmd/cordum-mcp/` — MCP server binary with stdio and HTTP/SSE transport modes
- MCP gateway bridge (`core/controlplane/gateway/gateway_mcp.go`) for tool execution and resource resolution
- MCP data bridge for context/result blob resolution
- MCP tools reference documentation (`docs/mcp-tools-reference.md`)
- MCP resources reference documentation (`docs/mcp-resources-reference.md`)
- MCP server setup guide (`docs/mcp-server.md`) with Claude integration instructions

#### Workflow Engine — New Step Types
- **Switch step**: conditional branching with match expressions and default fallthrough
- **Parallel step**: concurrent execution with configurable max concurrency and failure strategy
- **Loop step**: iterate over arrays/ranges with break conditions
- **Transform step**: JSONPath/template-based data transformation between steps
- **Storage step**: read/write workflow-scoped key-value storage
- **Sub-workflow step**: invoke nested workflows with input/output mapping

#### Dashboard — Complete Rebuild (215 tasks across 12 epics)
- **Foundation**: AppShell layout, sidebar navigation (9+1 items), route system, theme, Zustand state management
- **Command Center** (`/`): Overview page with system metrics, active jobs, agent status, recent activity
- **Agent Fleet** (`/agents`): Worker pool management, heartbeat monitoring, capacity visualization
- **Jobs** (`/jobs`): Job list with filters/search, job detail page, state machine visualization, job submission drawer, artifact panel, memory panel
- **Workflows** (`/workflows`): Workflow list, DAG builder with visual canvas, node config panel, run visualization with real-time overlay, step detail panel
- **Safety Policies** (`/policies`): Policy Studio with visual rule builder, condition group builder, policy simulator with explain results, policy history timeline, bundle editor
- **Approvals** (`/approvals`): Approval cards inbox with urgency indicators, detail panel, bulk approve/reject actions, badge count in sidebar
- **Audit Trail** (`/audit`): Event stream with real-time updates, advanced filters, timeline visualization, PDF/CSV export, audit reports
- **Dead Letter Queue** (`/dlq`): DLQ message list, detail view, retry/purge actions, badge count in sidebar
- **Packs** (`/packs`): Marketplace catalog browser, pack detail view, install/uninstall from UI
- **Settings** (`/settings`): Sub-route layout (config, health, keys, users, MCP), system health tab, users tab with password management, effective config panel, locks panel, setup checklist
- **Schemas**: Schema list, detail view, JSON schema editor
- Cmd+K command palette for quick navigation
- WebSocket streaming (`/api/v1/stream`) for real-time job/workflow updates
- Cross-tab sync for auth state via `useCrossTabSync` hook
- URL-based filter persistence via `useUrlFilters` hook

#### Dashboard — Feature Gaps
- Job submit drawer with topic/prompt/labels form
- Memory panel for job context inspection
- Output quarantine UX with remediation drawer
- Workflow builder sidebar with node palette
- Parallel node and loop node visual components
- Run visualization with real-time step status overlay
- Settings MCP page for MCP server configuration
- Change password section in settings
- Effective config panel showing merged configuration
- Locks panel for distributed lock inspection
- Setup checklist for initial platform configuration

#### SIEM Audit Export
- Buffered audit event exporter with async batching and retry
- Webhook exporter with HMAC-SHA256 signatures and custom headers
- Syslog exporter with RFC 5424 formatting over TCP/UDP
- Datadog log intake exporter (v2 API) with site mapping
- CloudWatch Logs exporter with AWS Signature V4
- `NewExporterFromEnv()` factory for env-var-based backend selection
- Env vars: `CORDUM_AUDIT_EXPORT_TYPE`, `CORDUM_AUDIT_EXPORT_WEBHOOK_URL`, `CORDUM_AUDIT_EXPORT_SYSLOG_ADDR`, `CORDUM_AUDIT_EXPORT_DD_API_KEY`, `CORDUM_AUDIT_EXPORT_DD_SITE`

#### Auth Endpoints
- User/password authentication system separate from API keys
  - `CORDUM_USER_AUTH_ENABLED` to enable user store (Redis-backed with bcrypt)
  - `CORDUM_ADMIN_USERNAME`, `CORDUM_ADMIN_PASSWORD`, `CORDUM_ADMIN_EMAIL` for bootstrap
  - `POST /api/v1/users` endpoint for user creation (admin only)
  - `POST /api/v1/auth/password` endpoint for password changes
- `POST /api/v1/auth/login` — unified login for API keys and user credentials
- `POST /api/v1/auth/logout` — session termination
- Unified login page in dashboard with single card layout
- Enterprise badge for SSO features
- User auth settings in docker-compose.yml and Helm chart

#### Documentation
- `docs/output-policy.md` — Output safety scanning operator guide
- `docs/workflow-step-types.md` — All 12 step types with YAML examples
- `docs/api-reference.md` — Comprehensive REST endpoint reference (105+ endpoints)
- `docs/safety-kernel.md` — Deep reference for input policy, MCP filters, overlays, cache, signatures
- `docs/scheduler-internals.md` — State machine, output policy integration, reconciler, saga, routing
- `docs/dashboard-guide.md` — All dashboard pages, workflows, keyboard shortcuts
- `docs/configuration-reference.md` — Complete config schema, overlay system, env vars master table
- `docs/mcp-server.md` — MCP server modes and Claude integration
- `docs/mcp-tools-reference.md` — MCP tool catalog with schemas and examples
- `docs/mcp-resources-reference.md` — MCP resource catalog with URI templates
- `docs/websocket-streaming.md` — WebSocket protocol, auth, events, reconnection
- `docs/grpc-services.md` — gRPC service reference
- `docs/sdk-reference.md` — SDK reference for gateway client, worker runtime, testing
- `docs/k8s-deployment.md` — Kubernetes deployment guide
- `docs/troubleshooting.md` — Common issues and debug commands
- `docs/production-gate.md` — Production readiness gate script
- `docs/pack.md` — Expanded with development workflow, testing, marketplace publishing, worker registration
- ADR: Output policy architecture decision (`docs/adr/005-output-policy-architecture.md`)
- Tutorials: `docs/tutorials/langchain-guard.md`

#### Packs & Marketplace
- Pack development workflow documentation (create → develop → test → build → verify → publish)
- Pack policy simulation tests (`cordumctl pack verify`)
- Marketplace catalog browser in dashboard
- Pack install/uninstall from dashboard UI

#### Infrastructure
- `.goreleaser.yml` for release builds
- `tools/scripts/production_gate.sh` — pre-deploy verification script
- OpenAPI/Swagger UI in `docs/api/openapi/`
- `cordum-rest.yaml` OpenAPI spec

### Fixed

- **edge (EDGE-041)** — cordum-hook mapper now renames every Claude `tool_input` field with a `_redacted` suffix on the wire so the dashboard inspector renders action context. `command` → `command_redacted`, `file_path` → `file_path_redacted`, `old_string` → `old_string_redacted`, `tool_response` → `tool_response_redacted`, etc. Unknown / version-drifted Claude fields fall through to a `tool_input_redacted` bucket so evidence never silently drops content. Builds on c951048d (UserPromptSubmit `prompt_redacted`); classifier accepts both the new and bare keys via multi-alias `inputStringAny` for historical-event compatibility. The dashboard sanitizer (`dashboard/src/api/transform.ts isUnsafeEdgeKey`) trusts only `_redacted`-suffixed keys as defense-in-depth — bare names were silently stripped, leaving `Redacted summary {}` empty in the EdgeEventInspector.
- **edge (EDGE-039)** — agentd evidence event now uses a fresh `agentd-` prefixed event_id instead of reusing the gateway-written `hook.policy_decision` event_id; the old behavior collided in `loadEventByIDInTx` and rejected the events/batch flush with 409 → 503 to hook → empty stdout. Cache-hit path already cleared resp.EventID; fresh-success path was the gap.
- **edge (EDGE-042)** — gateway `/api/v1/edge/evaluate` auto-consumes a reusable approval at the REQUIRE_APPROVAL branch when the request has no explicit `approval_ref`. cordum-hook cannot carry approval_ref across Claude tool retries; without auto-consume the approval-flow gate's "consume" call returned a fresh REQUIRE_APPROVAL with a new approval_ref instead of ALLOW. Lookup paginates the principal-status index (tuple index is SRem'd on consume) and routes through `consumeEdgeEvaluateApproval`.
- **edge (EDGE-044)** — restored the live Edge fake-hook E2E to 5/5 PASS after the EDGE-041 `_redacted`-suffix rename caused fresh-binary-vs-stale-image divergence. Source code at HEAD (post-EDGE-041) classifies renamed keys correctly via existing `inputStringAny` multi-alias coverage; live regression came from the `cordum/api-gateway:dev` Docker image being built before EDGE-041 landed and still using the pre-rename classifier — fresh `cordum-hook`/`cordum-agentd` binaries produced `file_path_redacted`/`command_redacted` keys that the stale gateway image read as missing, falling through to default-deny. Fix is operational: new `make edge-rebuild-e2e` target rebuilds local Edge binaries AND the api-gateway image in lockstep before strict-mode E2E runs; `docs/LOCAL_E2E.md` flags the rebuild as MANDATORY before `CORDUM_INTEGRATION=1`. Five new classifier regression tests in `core/edge/classifier_test.go` lock the renamed-key contract at the unit layer so any future alias gap surfaces in the focused suite, not in the live e2e.
- **scheduler (task-625b2ed1)** — fixed a latent nil-deref in `buildCompensationRequest` (saga.go). The inline `proto.Clone(base).(*pb.JobRequest)` lacked the ok-check every sibling clone site had; on a proto.Clone type-assertion failure the next line would dereference nil and panic the scheduler mid-compensation. Migration to the new `core/protocol/protoutil.CloneJobRequest` helper enforces the ok-check at every call site. Operator impact: none in the happy path; the failure path now returns a wrapped error instead of crashing.
- **audit (task-8db173c5)** — `SyslogExporter.Close` now logs at Warn when the underlying `net.Conn.Close` returns an error (fields: `network`, `address`, `error`). Previously the error was returned opaquely to the `BufferedExporter` close cascade where it could be absorbed silently, masking half-open sockets and TCP-stack fsync failures. Returned-error contract is unchanged.
- **gateway (task-1d4e6b4c bug #2)** — WebSocket `SetReadDeadline` errors at connection setup (`handleStream`, `handleJobStream`) are now propagated: on failure the handler logs at Warn, sets the disconnect state, closes the ws, and returns. Previously the error was discarded and the read loop ran with no deadline, so the server waited indefinitely for a frame that never arrived.
- **gateway (task-1d4e6b4c bug #3)** — `revalidateWSAuthWithRetry` now surfaces the last transient error after 3 exhausted retries instead of returning nil. A NATS/Redis outage during revalidation previously kept a potentially-revoked session alive for the full 2-minute revalidation window; callers already branch on `err != nil` and will close the connection, letting the dashboard auto-reconnect. `ctx.Done()` still returns nil — caller-initiated shutdown is not a failure.
- **safety-kernel (task-681f83cd)** — `shadowTimeout` now actually bounds the per-submission shadow evaluation loop. The `context.WithTimeout` return was previously discarded; captured + plumbed through `evalShadowSafely` with a `ctx.Err()` check at bundle-iteration top.

#### Critical
- **NATS reconnect** — Safety kernel and scheduler re-subscribe to `sys.config.changed` on NATS reconnect. Previously degraded silently to 30s polling on network partition.
- **Config scope corruption** — `SetWithRetry` now deep-merges config updates, preserving existing keys. Policy bundles no longer silently wiped by pools config pushes. Startup migration moves stale bundles to correct scope.
- **E2E TLS job dispatch** (`task-73bc2227`) — fixed `tools/scripts/e2e_test.sh` Phase 4 on TLS compose stacks. The script now auto-detects `./certs/ca/ca.crt`, uses `tls://` / `rediss://`, passes `NATS_TLS_CA` / `NATS_TLS_CERT` / `NATS_TLS_KEY` plus `REDIS_TLS_CA` / `REDIS_TLS_CERT` / `REDIS_TLS_KEY` to `examples/hello-worker-go`, installs `./examples/hello-worker-go/pack/pack.yaml` so `job.hello-pack.echo` is registered before submit, treats missing Phase 4 readiness/completion as hard failures while parsing the canonical `/api/v1/workers` `items` response, and the gateway `unknown_topic` response now includes tenant-filtered `registered_topics` (capped at 20) plus `topics_endpoint`.
- **cordumctl topic registration** — `pack install` now registers topics in topic registry; `pack uninstall` removes them. Fixes #171.
- **cordumctl lock release** — `runPackInstall` and `runPackUninstall` return errors instead of `os.Exit(1)`, ensuring deferred lock release fires on all error paths.
- **Safety Kernel NATS subscription** — subscribes to `sys.config.changed` for immediate policy reload (was poll-only with 30s delay).
- **cordumctl JSON tags** — `packTests` structs now have `json:"..."` tags matching YAML tags, fixing silent registry data corruption.

#### High
- **Panic recovery** — all NATS subscription callbacks wrapped with `defer recover()` + stack trace logging
- **Readiness filter** — unknown workers allowed (absence ≠ not ready), preventing new worker traffic starvation
- **Credential cache** — async refresh in NATS handler (prevents scheduler throughput collapse), merge-on-failure (prevents stale cache)
- **Rollback reporting** — cordumctl rollback errors tracked and returned, non-zero exit on partial rollback
- **Approval stale_request false negative** — single-step approval workflows no longer get auto-invalidated as `stale_request` immediately after `POST /approve`. The gateway approve endpoint now locks the current `HashJobRequest(req)` into `SafetyDecisionRecord.JobHash`, and `scheduler.checkSafetyDecision` preserves a prior `JobHash` from gateway submit instead of clobbering it with a post-effective-config mutation hash; hash-fence store read failures retry without publishing instead of falling through the input fail-open path. This is a bug fix, not an API contract change; clients that only observed the spurious `invalidate_stale_request` path should now see the benign approval succeed again. Follow-up to commit `297937c7` and guard task `task-035cdc8e`.
- **Dashboard memory leaks** — duplicate WebSocket, IntersectionObserver, CSV blob URL timing
- **Dashboard error handling** — LoginPage 4xx, RunDetailPage chat error, PackDetailPage null state
- **Dashboard a11y** — focus traps on modals, aria-labels on stats, localStorage try-catch
- **Security logging** — `slog.Info`/`slog.Warn` for credential and topic operations
- **Input validation** — array length limits (max 100 items, 128 chars), URL encoding on dynamic links
- **lodash** — CVE-2026-4800, CVE-2026-2950 fixed upstream in 4.18.0; bumped to 4.18.1 as the latest safe release.

#### System Audit Bug Fixes (25 tasks)
- Gateway: Fixed SSRF in marketplace URL validation — added private IP filtering for RFC 1918/loopback/link-local addresses
- Gateway: Hardened public path matching to prevent auth bypass on path variations
- Gateway: Rate limit middleware now runs after API key authentication (was running before, allowing bypass)
- Gateway: Error responses sanitized to prevent internal stack trace leakage
- Scheduler: Fixed per-run mutex for concurrent engine execution
- Scheduler: Fixed reconciler race conditions in timeout handling
- Scheduler: Fixed pending replayer edge cases
- Workflow engine: Fixed stale closure bugs using `useRef` pattern
- Workflow engine: Fixed dependency array triggers in hooks
- Config: Fixed safety policy schema validation
- Memory store: Fixed job store edge cases in concurrent access
- Metrics: Fixed metric registration and labeling
- `bufio.Scanner.Err()` checked after scan loops across codebase

#### Dashboard-to-Backend Integration Bug Fixes
- Transform layer handles API contract mismatches between backend `{scope, data}` wrapper and frontend flat expectations
- Policy bundle detail mapping: parse rules from YAML content instead of hardcoding `rules: []`
- Visual rule builder: use shared `usePolicyBundle()` hook instead of local bypass
- `resolvePublishTargets()`: fixed `secops/` prefix requirement so pack bundles can publish

### Changed

- **core: extracted Unix-timestamp → RFC3339 formatter into `core/infra/timeutil` (task-e396a874)** — 5 inline formatters migrated: `FormatUnixAuto` (handlers_chat.go magnitude cascade) + typed `FromSeconds`/`FromMillis`/`FromMicros`/`FromNanos` for compile-time-known units. Byte-for-byte identical output; empty string on `ts<=0` preserved per site.
- **core: extracted `proto.Clone((*pb.JobRequest))` guard-pattern into `core/protocol/protoutil.CloneJobRequest` (task-625b2ed1)** — 4 inline call sites migrated to one helper with typed ok-check + nil guard. See the paired `Fixed` entry for the latent saga.go:322 nil-deref this closed. JobMetadata clone sites in saga.go not migrated (different type, separate follow-up if drift emerges).
- **gateway: removed packs_compat.go + policy_compat.go (task-a828e179)** — 233 lines of pure-alias shims deleted. Every caller (~40 files) now imports `core/controlplane/gateway/packs` or `core/controlplane/gateway/policybundles` directly and uses the fully-qualified `packs.PascalCase` / `policybundles.PascalCase` shape. `resolveAgentForAudit` moved to `handlers_agents.go`. Internal refactor; no public API change.
- **core: extracted Redis CAS retry loop into `core/infra/redisutil/Retry`** — 4 production call sites (gateway keystore_redis RevokeKey + mcp_approvals Consume/Resolve/SweepExpired) now share a single retry primitive with `WithMaxAttempts`/`WithKeys` options and an `ErrMaxAttemptsExceeded` sentinel. Behavior byte-equivalent. Closes task-c7e419d8.
- **core: unified JobRequest canonicalisation into `core/protocol/reqhash`** — single `Canonical` + `Hash` helper shared by scheduler, gateway, and store; five bare `protojson.Unmarshal` sites in `core/infra/store/job_store.go` now pass `DiscardUnknown: true`. See release notes for the Redis WATCH/MULTI atomic-store decision. Closes task-090ab6af.
- Auth: Login endpoint supports both user credentials and API keys
- Auth: AuthConfig includes `user_auth_enabled` and `saml_enterprise` fields
- Scheduler: Output policy integration in dispatch pipeline
- Workflow engine: Support for 6 new step types alongside existing job/fan-out/condition/delay/approval/notify
- Dashboard: Sidebar navigation consolidated to 9+1 items (removed /context, /pools, /system, /trace, /tools)
- Dashboard: Routes reorganized under new page structure
- Safety kernel: Policy fragments from config service merged with file/URL policy on load/reload

#### CAP v2.5.2 Protocol Integration
- Upgraded CAP protocol dependency from v2.0.19 to v2.5.2 (both `go.mod` and `sdk/go.mod`)
- All NATS-connected services publish `Handshake` on `sys.handshake` at startup for capability discovery (gateway, scheduler, workflow-engine; workers via SDK runtime)
- `SystemAlert` now includes `severity` enum, `error_code_enum`, `source_component`, `details` map, and `trace_id` (deprecated string fields still populated)
- `JobResult` error codes use structured `ErrorCode` enum alongside string `error_code` for backward compatibility
- Bus-layer validation rejects malformed `JobRequest`/`JobResult` messages using CAP SDK helpers (`validation_rejections_total` metric)
- Scheduler handles `BusPacket{Handshake}` in its message switch, updating the worker registry with component capabilities
- Dashboard displays structured error code badges on job detail page and enhanced alert severity in audit log
- Added conformance test fixtures for all 8 CAP packet types with signature verification
- SDK runtime exposes `ValidateJobRequest`, `ValidateJobResult`, `Handshake`, `ComponentRole`, `ErrorCode`, `AlertSeverity` types

### Removed

- Retired the `cordum-enterprise` repo — all enterprise features (SSO/SAML, SCIM, advanced RBAC, SIEM export, legal hold, velocity rules, agent identity) now ship in cordum core behind license entitlements; separate repo archived on GitHub. Closes task-b7c6c2f1. See release notes for full surface list.
- Removed the legacy OpenAPI sidecars `docs/api/openapi/cordum-rest.yaml`
  and `docs/api/openapi/cordum.swagger.json`. `docs/api/openapi/cordum-api.yaml`
  is now the single canonical OpenAPI 3 spec, `make openapi` is a pure
  Redocly validation pass, and the local/public Swagger UI wrappers now load
  only that canonical spec. Also removed the legacy prefixed MCP transport
  aliases `/api/v1/mcp/{sse,message,status}`; MCP transport is now exposed
  only at `/mcp/{sse,message,status}` while MCP governance REST endpoints
  remain under `/api/v1/mcp/*`. The internal OpenAPI legacy audit
  (`Audit re-verification 2026-04-23`) holds the ground-truth timeline
  (Cordum engineering).

### Security
- **WebSocket quarantine-redaction fail-closed (task-1d4e6b4c bug #1)** — the filter that strips `ResultPtr` + `ArtifactPtrs` from DENIED `JobResult` packets before broadcasting to WebSocket subscribers previously FAILED OPEN on `proto.Clone` type-assertion failure AND on the defensive `cloned.GetJobResult() == nil` branch, returning the original unredacted packet. Redis-stored result payloads may contain PII, user prompts, secrets, or model outputs; the filter now fails CLOSED: returns nil on any failure, `enqueueBusPacket` drops the broadcast, `cordum_gateway_ws_quarantine_redaction_drops_total` increments, and an error is logged with `job_id` + `trace_id`. The next state-change event arrives in the normal stream cadence.
- Session tokens: Replaced timestamp-based tokens with `crypto/rand` (was only 53 bits entropy)
- HSTS: Added `Strict-Transport-Security` headers
- Brute-force protection: Added login attempt rate limiting
- Password policy: Enforced minimum complexity requirements
- Docker healthchecks: Added health endpoints to all container services
- Kubernetes: Fixed dashboard deployment manifest
- Kubernetes: Added egress network policies
- Redis: Configured persistence for production durability
- Gosec findings mitigated across codebase (G117 suppressions for intentional secret/password fields)
- OIDC host validation fix

### Tests
- Added test coverage for `core/audit/` package: config, datadog, exporter, syslog (4 new test files)
- Added workflow engine tests: loop, parallel, storage, sub-workflow, switch, transform step types
- Added output policy tests: engine output, safety client, protobuf
- Added dashboard component tests: Badge, CollapsibleSection, ComboboxInput, ConfirmDialog, Drawer, HighlightText, TagInput
- Added dashboard hook tests: useApprovals, useAudit, useAuth, useAuthConfig, useCrossTabSync, useDLQ, useJobs, useKeyboardShortcuts, useMemory, useOutputPolicy, useOutputRules, usePacks, usePageTitle, usePermission, usePolicies, useRunStream, useSchemas, useSettings, useSetupStatus, useStatus, useToast, useUrlFilters, useWorkers, useWorkflows
- Added dashboard lib tests: api, audit-filters, audit-report, export, format, logger, pdfExport, policy-yaml, runtime-config, settingsSchemas, status, utils
- Added dashboard state tests: config, toast, ui
- Added dashboard page tests: DLQPage, SettingsLayout, SetupChecklist, SystemHealthTab, UsersTab
- Added API client and transform layer tests

### Dashboard UI Polish Wave (2026-04-25)
- **Soft UI Evolution** — Button/Card/Tabs primitives migrated to `rounded-xl` + `duration-[var(--duration-soft)]` (250ms); `--shadow-soft`, `--shadow-soft-hover`, `--radius: 0.75rem` design tokens consumed at call sites; regression-pinned by `dashboard/src/components/ui/SoftUiEvolution.test.ts`
- **Per-row motion stagger** on Jobs / Audit / Agents tables via `motion.tbody` + `motion.tr` with `staggerChildren: 0.04` and `useReducedMotion`-honoring item variants
- **Staggered motion entry** on PolicyOverviewPage + SimulatorPage matching `HomePage.tsx:317-350` idiom
- **MotionConfig** — global `<MotionConfig reducedMotion="user">` wrapper at `App.tsx:201` so all `motion.*` descendants honor `prefers-reduced-motion`
- **RunDetailPage step-list a11y** — `role="listbox"` parent + `role="option"` items with `tabIndex=0`, `aria-selected`, `aria-label`, `onKeyDown` (Enter + Space, `preventDefault`), focus-visible ring
- **axe-core a11y gate** — automated accessibility test suite (`*.a11y.test.tsx`) covering HomePage, PolicyOverviewPage, SettingsHubPage in light and dark modes; `aria-pressed` added to toggle-state buttons (live-mode, etc.)
- **`useAdminLocks` role gate** — `enabled: useIsAdmin()` short-circuits the 5s `/admin/locks` poll for non-admin users (was emitting 720 `403`s/hour and a silent blank LockInspector card); LockInspector now renders an `EmptyState` admin-required card on `!isAdmin`
- **`useDelegations` test fix** — race-prone unhandled rejection silenced via no-op `mutation.catch(() => {})` immediately after `mutateAsync`; rollback path now asserts `setQueryData(allKey/agentKey, seeded)` explicitly
- **`mapJobRecord` origin refs** — `BackendJobRecord` interface declares `workflow_run_id`, `labels`, and `metadata` as optional fields; `mapJobRecord` forwards them onto the returned `Job` so the JobsPage list `OriginPill` correctly renders Run/Session pills (was always falling through to Direct because the list-mapper stripped the fields that `mapJobDetail` adds back)
- **`backdrop-filter` `@supports` fallback** — Safari `<14` and iOS Safari `<14` now render `.glass-panel` / `.glass-sidebar` / `.glass-header` with opaque `var(--card)` background; PostCSS/Tailwind autoprefixer auto-extended the rule to cover `-webkit-backdrop-filter`
- **GovernanceVerificationPage** at `/govern/verification` (admin-gated via `RequireRole`); routing guard test added so missing `<Route>` registrations fail tsc + vitest
- **`instrument-card` sweep** — 5 Policy Studio routes + ApprovalDetailPage / SettingsMcpPage / RunDetailPage internal info blocks adopted the shared instrument-card primitive
- **Dashboard 12-col Bento Grid** — BundleDetailPage + JobDetailPage + AgentDetailPage refactored to `grid-cols-1 lg:grid-cols-12` with framer-motion staggered tile entry; RunDetailPage explicitly *exempted* (full-viewport 3-pane console shell is non-bento by design — see `dashboard/docs/design-system-audit.md` § DoD-3 exemptions)
- **Brand identity** — favicons + logo refresh

### Strategic Decisions (2026-04-25)
- **Dashboard i18n DEFERRED post-Visa** (task-530874ea) — zero `useTranslation`/`i18next`/`FormattedMessage`/`t(` adoption today; ~1500-key migration cost weighed against zero current external-customer demand and the project_strategic_direction "governance depth over breadth" rail. Follow-up `task-8c4cdcaf` filed in BACKLOG for post-Visa revisit.
- **LLM epic backlogged** (epic-ac495830) — same logic: not the right priority pre-Visa.

### Process Rails (proposed; pending human approval)
- `prop-8cc95268` — DASHBOARD VERIFICATION RAIL: tasks touching `cordum/dashboard/` MUST run `tsc --noEmit` + `npx vitest run` + `npm run build` and paste each summary line into the final `complete_step` note before `complete_task`. Docker-build-success is NOT a substitute (Vite bundles through type errors; the rail closes that loophole).
- `prop-5a162a16` — DASHBOARD QA REJECTION FORMAT: QA must cite the first failing gate and, for vitest failures, the first new failing test as `<describe> > <it> (<path>:<line>)`.

## [v0.3.0] - 2026-01-31
- Protocol/SDK: bump CAP to v2.0.19 across core + SDK modules.
- SDK: `sdk/runtime` now wraps CAP runtime (typed handlers + pointer hydration).
- SDK: add CAP bus helpers for progress/cancel/heartbeats + direct worker subjects.
- Examples: migrate workers to CAP runtime + direct-subject subscriptions.
- Breaking: legacy `sdk/runtime` worker API removed; use `runtime.Agent` + CAP worker helpers.

## [v0.2.0] - 2026-01-26
- Scheduler: add durable saga/compensation handling with reverse-stack rollback for fatal failures.
- Scheduler: add compensation idempotency keys and saga rollback metrics.
- Protocol: align job status handling with CAP v2.0.16 (FAILED_FATAL/FAILED_RETRYABLE).
- Workflow engine: treat FAILED_FATAL as terminal and FAILED_RETRYABLE as retryable.
- Security/docs: updated control-plane docs/wiki for saga semantics and CAP changes.
- Tests: added coverage for saga manager, safety kernel cache/URL validation, protobuf + grpc stubs, and Redis idempotency flows.

## [v0.1.4] - 2026-01-25
- Security: remove default API keys; deployments must supply `CORDUM_API_KEY`.
- Security: fail-closed API auth; enforce `X-Tenant-ID`; require policy signatures when enforcement is enabled.
