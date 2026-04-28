# LLM Chat Observability + Ops Senior Review

Task: `task-8eab552b`
Status: **IN PROGRESS**
Reviewer: Moe worker `worker-54cf`
Last updated: 2026-04-28

## Scope note (2026-04-28 informational-only pivot)

The LLM chat assistant is now an **informational-only** Cordum docs/API helper.
It does not call MCP tools, does not submit jobs, and does not mutate state.
This review therefore keeps day-2 observability for chat sessions, admin review,
redaction, metrics, logs, alerts, and stable informational chat frames, while
marking retired chat→MCP/tool-call/approval-frame surfaces as superseded by the
pivot unless they still exist for backwards-compatibility.

Production default inference is Ollama/OpenAI-compatible local inference. vLLM
remains an opt-in GPU profile; dashboards and probes should label the active
backend and keep vLLM-specific panels as opt-in.

## Executive summary

Classification after probes 1-12 (2026-04-28) plus the probe-3 follow-up rerun:
**0 P0**, **5 open P1**, **1 resolved P1**, **1 P2**, and no
secret/metric-cardinality P0s. P1 follow-ups filed: `task-848f003a` (probe 1
structured JSON logs), `task-0e73db35` (probe 3 OTEL/Jaeger; **resolved by
2026-04-28 rerun**), `task-83b72a46` (probe 4 admin audit/search),
`task-68a01f28` (probe 5 protocol v1), `task-7ee2d5ab` (probe 10 usage
counters), `task-53317462` (probe 11 debug dump). Probe 4 also records a P2
detail-routing check for `/copilot/sessions`.

Outcome counts after task-0e73db35: **7 PASS/static-pass probes** (2, 3, 6, 7,
8, 9, 12), **5 open FAIL/P1 probes** (1, 4, 5, 10, 11), **0 BLOCKED**, **0
P0**. Log-redaction grep result: probe 1 secret scan returned zero hits against
sampled llm-chat logs; probe 12 found no INFO/WARN token-delta log spam. Jaeger
trace evidence: `out/llmchat-otel-smoke/jaeger-traces.json` with trace ID
`763604b70c676385f5eddb945d0ee313` (including `chat.audit.emit`) and the
task-0e73db35 rerun of `probe-03.sh` requiring `chat.ws.connect`,
`chat.turn`, `llm.inference`, and `chat.audit.emit`.

| Probe | Surface | Verdict | Evidence |
|---|---|---:|---|
| 1 | Structured logs + redaction | **FAIL (P1)** | `out/llmchat-ops/probe-01/evidence.txt` |
| 2 | Prometheus metrics + cardinality | **PASS** | `out/llmchat-ops/probe-02/evidence.txt` |
| 3 | Trace propagation / Jaeger | **PASS (follow-up task-0e73db35)** | `out/llmchat-ops/probe-03/evidence.txt`, `out/llmchat-otel-smoke/jaeger-traces.json` |
| 4 | Admin session viewer + audit | **FAIL (P1)** | `out/llmchat-ops/probe-04/evidence.txt` |
| 5 | Chat frame protocol stability | **FAIL (P1)** | `out/llmchat-ops/probe-05/evidence.txt` |
| 6 | Ops runbook | **PASS** | `docs/llmchat/ops-runbook.md` |
| 7 | Grafana dashboard | **PASS (static); import not run** | `cordum-helm/dashboards/llm-chat.json` |
| 8 | SIEM export | **PASS (static); live sinks not run** | `out/llmchat-ops/probe-08/evidence.txt` |
| 9 | Alert rules | **PASS (static)** | `cordum-helm/alerts/llm-chat.yaml` |
| 10 | Cost / usage visibility | **FAIL (P1)** | `out/llmchat-ops/probe-10/evidence.txt` |
| 11 | Admin debug dump | **FAIL (P1)** | `out/llmchat-ops/probe-11/evidence.txt` |
| 12 | Log sampling / volume bounds | **PASS (static); live load not run** | `out/llmchat-ops/probe-12/evidence.txt` |

### Current pre-probe findings from exploration

- Runtime logs from `llm-chat` are not pure JSON; they are text-prefixed
  slog lines. Probe 1 must verify whether that is still true for the current
  image and classify severity.
- Metrics are live and bounded by allowlists in `core/llmchat/metrics.go`, but
  metric names still contain legacy `tool`/`vllm` terminology.
- OpenTelemetry/Jaeger wiring was absent during the original review; follow-up
  `task-0e73db35` now initializes OTEL in `cordum-llm-chat`, propagates
  `traceparent` from the gateway proxy, and instruments the surviving
  informational-chat path.
- Admin session list/detail routes enforce permission and tenant scope, but no
  `chat.admin_session_viewed` SIEM event constant/emission was found.
- `/settings/chat-sessions` exists, but search and chat-specific detail routing
  need verification; current rows navigate to `/copilot/sessions/:sessionId`.
- Chart path in this repo is `cordum-helm/`; plan references to `helm/cordum/`
  are stale.

## Cardinality ceiling

| Metric family | Labels | Expected max series | Enforcement |
|---|---|---:|---|
| `chat_sessions_active` | none | 1 | `core/llmchat/metrics_test.go` |
| `chat_tool_calls_total` | `tool` allowlist + `unknown` | 21 legacy/back-compat series | `normalizeTool` allowlist |
| `chat_approval_required_total` | none | 1 legacy/back-compat series | no labels |
| `chat_vllm_latency_seconds` | histogram `le` only | 12 histogram series | fixed buckets |
| `chat_token_budget_used_total` | none | 1 | no labels |
| `chat_errors_total` | `kind` allowlist | 8 series | `normalizeErrorKind` allowlist |

Session IDs, principals, tenants, prompt text, tokens, trace IDs, and error
messages must never be metric labels.

## Probe 1 — Structured logs + secret redaction

**Expected:** llm-chat logs are structured, machine-parseable, include safe
correlation fields (`session_id`, `user_principal`, `tenant`, `trace_id`) where
applicable, and never leak tokens, API keys, JWTs, PEM material, or prompts at
INFO.

**Procedure:** `scripts/ops-probes/probe-01.sh` captures logs, validates JSON
or records the non-JSON format as evidence, and runs the shared secret-pattern
scanner.

**Actual:** `probe-01.sh` ran from Git Bash/MSYS against the auto-detected chat log service (`llm-chat` in the current post-pivot compose config; `llm-chat` in earlier profile configs). It captured recent log lines and the secret scanner returned zero hits. JSON validation
failed on every sampled line because the service emitted text-prefixed slog
output, for example a `LLM-CHAT-SERVER INFO llmchat/agent: turn_start ...`
rather than a JSON object. The lines also use `principal=` instead of the
required `user_principal` key and do not include `trace_id` in the sampled
application logs.

**Verdict:** **FAIL (P1).** Secret redaction passed for the sampled log dump,
but the structured-log requirement is not met. This is a day-2 operability gap:
log processors cannot reliably parse fields or enforce field-level redaction.

**Evidence:** `out/llmchat-ops/probe-01/evidence.txt`

**Findings / tasks:** File/track a follow-up before final handoff if this task
continues to REVIEW: initialize JSON slog for `cordum-llm-chat` and standardize
safe correlation keys (`session_id`, `user_principal`, `tenant`, `trace_id`).
Follow-up task-848f003a resolves this finding and the live probe should be
rerun before re-reviewing task-8eab552b.

## Probe 2 — Metrics + cardinality

**Expected:** `/metrics` returns the required chat metric families, every label
is bounded, and no session-like or secret-like value appears in labels.

**Procedure:** `scripts/ops-probes/probe-02.sh` fetches Prometheus text format,
checks required families, counts label combinations per family, and scans label
values for UUID/session/token shapes.

**Actual:** `probe-02.sh` fetched `http://127.0.0.1:8090/metrics` successfully
(~12 KB). Required families were present: `chat_sessions_active`,
`chat_tool_calls_total`, `chat_approval_required_total`,
`chat_vllm_latency_seconds`, `chat_token_budget_used_total`, and
`chat_errors_total`. Cardinality remained under the documented ceiling: active
sessions=1, approval_required=1, token_budget=1, errors=8, tool_calls=21, and
vLLM latency histogram emitted fixed bucket/count/sum series. The script found no
forbidden `session_id`, principal, tenant, token, prompt, trace, UUID, JWT,
Bearer, or `sk-*` values in labels.

**Verdict:** **PASS.** Cardinality is bounded and enforceable. The metric names
still include legacy `tool`/`vllm` terminology; under the informational-only
Ollama default this should be documented as compatibility naming or cleaned up in
follow-up, but it does not create unbounded cardinality.

**Evidence:** `out/llmchat-ops/probe-02/evidence.txt`

**Findings / tasks:** Optional naming follow-up only; no P0/P1 for cardinality.

## Probe 3 — Trace propagation / Jaeger

**Expected:** a single chat message has one trace across browser, gateway,
llm-chat, inference backend, audit, and any retained downstream services.
Under informational-only scope, MCP/scheduler/worker spans are retired unless a
legacy tool path is deliberately exercised.

**Procedure:** `scripts/ops-probes/probe-03.sh` records trace IDs from logs and
queries the configured Jaeger/OTEL endpoint when present.

**Actual:** The original review found no llm-chat OpenTelemetry/Jaeger exporter
configuration. Follow-up `task-0e73db35` added the missing instrumentation:
`cordum-llm-chat` initializes the shared OTEL provider, the gateway reverse
proxy injects W3C `traceparent`, and llm-chat emits bounded spans for
`chat.ws.connect`, `chat.ws.disconnect`, `chat.turn`, `llm.inference`,
`chat.session.read`/`chat.session.write`, `chat.audit.emit`, and
`llmchat.knowledge.load`. The Jaeger smoke queried the OTLP-exported trace and
captured operations `chat.ws.connect`, `chat.ws.disconnect`, `chat.turn`,
`llm.inference`, and `chat.audit.emit` in trace
`763604b70c676385f5eddb945d0ee313`.

**Verdict:** **PASS (resolved by task-0e73db35).** The surviving
informational-chat path now has OTEL propagation and Jaeger-verifiable spans.
The retired chat→MCP/scheduler/worker span expectations remain out of
production-default scope after the informational-only pivot.

**Evidence:** `out/llmchat-ops/probe-03/evidence.txt`,
`out/llmchat-otel-smoke/jaeger-traces.json`, and Jaeger UI/query URL pattern:
`http://127.0.0.1:16686/api/traces?service=cordum-llm-chat-smoke&lookback=1h&limit=20`.

**Findings / tasks:** `task-0e73db35` resolves the probe-3 P1. Keep the
dedicated trace probe in the ops harness so regressions fail closed when a
configured Jaeger endpoint is supplied.

## Probe 4 — Admin session viewer + audit

**Expected:** admin-only session list/detail works, supports pagination and
search, shows read-only transcripts, and emits a SIEM/audit event for each
admin view.

**Procedure:** `scripts/ops-probes/probe-04.sh` exercises list/detail APIs where
credentials exist; browser evidence is attached separately.

**Actual:** Static probe found the backend and dashboard pieces for a basic
admin viewer: `HandleListSessions`, `HandleGetSession`, `chat.read_all`, cursor
pagination, tenant field, and `user_principal` mapping are present. Live API
verification was not run in this pass because `LLMCHAT_OPS_LIVE=1` and an API
key were not supplied to the probe. The probe found **zero** concrete
`chat.admin_session_viewed`/admin-view SIEM action hits, and no search query UI
or API parameter for user/tenant/session_id search. It also flags a P2 routing
risk: the settings table currently links through `/copilot/sessions`, which may
not be the intended chat-assistant read-only transcript path.

**Verdict:** **FAIL (P1).** Basic admin list/detail scaffolding exists, but the
meta-governance requirement (audit event per admin view) and search requirement
are unmet.

**Evidence:** `out/llmchat-ops/probe-04/evidence.txt`

**Findings / tasks:** File/track follow-up for `chat.admin_session_viewed` SIEM
emission plus admin search by user, tenant, and session ID. Track the
`/copilot/sessions` routing mismatch as P2 unless product confirms it is the
canonical read-only transcript route.

## Probe 5 — Chat frame protocol stability

**Expected:** informational chat frames are schema-pinned. If a version field is
used it is pinned to `v: 1`; unknown versions fail closed. Retired tool-call and
approval frames are not treated as production-default requirements after the
2026-04-28 pivot.

**Procedure:** `scripts/ops-probes/probe-05.sh` checks static frame schema tests
and, in live mode, sends a deliberately unsupported version frame.

**Actual:** Added `docs/llmchat/protocol-versioning.md` with the intended v1
contract and v2 upgrade plan. Static probe found no top-level `json:"v"` frame
field in Go/dashboard chat frame definitions and no `unsupported_protocol_version`
handler string. Live websocket version rejection was not run because
`LLMCHAT_WS_URL`/live mode were not configured.

**Verdict:** **FAIL (P1).** The protocol-versioning document now exists, but the
runtime/client frame contract is not pinned to `v: 1` and unknown versions do not
fail closed.

**Evidence:** `out/llmchat-ops/probe-05/evidence.txt`,
`docs/llmchat/protocol-versioning.md`

**Findings / tasks:** File/track follow-up to add a `v: 1` field to chat frames,
accept/reject client versions explicitly, and return `unsupported_protocol_version`
for unknown versions.

## Probe 6 — Ops runbook

**Expected:** `docs/llmchat/ops-runbook.md` covers deploy, upgrade, rollback,
scale, health checks, alerts, known issues, and escalation for customer ops.

**Procedure:** `scripts/ops-probes/probe-06.sh` checks required headings and
links.

**Actual:** Rewrote `docs/llmchat/ops-runbook.md` from a stale vLLM production-
readiness matrix into a customer-facing informational-chat ops runbook. Static
probe passed: required headings are present (`Deploy`, `Upgrade`, `Rollback`,
`Scale`, `Check health`, `Common alerts`, `Known issues and workarounds`,
`Escalation matrix`), and required operational terms are present (`Ollama`,
Enterprise license, `llm_chat_assistant`, `/healthz`, `/readyz`, `/metrics`,
rollback, scale, P0, redaction). Secret-pattern scan was clean.

**Verdict:** **PASS.** Runbook content now matches the 2026-04-28
informational-only/Ollama-default scope.

**Evidence:** `docs/llmchat/ops-runbook.md`, `out/llmchat-ops/probe-06/evidence.txt`

**Findings / tasks:** None for the static runbook check.

## Probe 7 — Grafana dashboard

**Expected:** `cordum-helm/dashboards/llm-chat.json` ships with panels for
active sessions, backend latency, token budget, errors, and backend health; it
imports with no-data panels instead of errors.

**Procedure:** `scripts/ops-probes/probe-07.sh` validates JSON structure and, in
live mode, records Grafana import evidence.

**Actual:** Added `cordum-helm/dashboards/llm-chat.json`. Static JSON validation
passed with dashboard title `Cordum LLM Chat`, 6 panels, and required metric
coverage for `chat_sessions_active`, `chat_vllm_latency_seconds_bucket`,
`chat_errors_total`, and `chat_token_budget_used_total`. Every panel sets
`noValue: "No data"` for empty-stack rendering. Live Grafana import/screenshot
was not run because `GRAFANA_URL` and `LLMCHAT_OPS_LIVE=1` were not configured.

**Verdict:** **PASS (static), IMPORT NOT RUN.** The dashboard artifact ships and
is structurally valid; final customer evidence still needs an owned Grafana
import or screenshot if the original DoD is enforced literally.

**Evidence:** `out/llmchat-ops/probe-07/evidence.txt`

**Findings / tasks:** No P0/P1 from static validation. Import screenshot remains
manual/dedicated-environment evidence.

## Probe 8 — SIEM export

**Expected:** chat lifecycle events and retained governance events export
through the existing audit sinks. Retired `mcp.tool_invocation` and
`chat.approval_required` paths are not production-default chat requirements.

**Procedure:** `scripts/ops-probes/probe-08.sh` checks constants/tests and, when
sinks are configured, captures webhook/syslog/Datadog/CloudWatch examples.

**Actual:** Static probe found canonical `chat.session_started` and
`chat.session_closed` action constants in `core/audit/siem_actions.go`. It
records `chat.approval_required` as retired under informational-only default and
found `mcp.tool_invocation` outside the default chat path. Existing audit sinks
and tests are present for webhook, syslog, Datadog, and CloudWatch. Unit
serialization/exporter coverage passed:
`go test ./core/audit -run 'Test.*(Webhook|Syslog|Datadog|CloudWatch|SIEMAction)' -count=1`.
End-to-end live sink exports were not run because no sink endpoints were
configured.

**Verdict:** **PASS (static), LIVE SINKS NOT RUN.** Retained chat lifecycle
SIEM format is centralized and exporter tests pass. This does not prove customer
sink delivery without configured webhook/syslog/Datadog/CloudWatch endpoints.

**Evidence:** `out/llmchat-ops/probe-08/evidence.txt`

**Findings / tasks:** No P0/P1 for static retained SIEM export. Live sink capture
remains customer/staging evidence.

## Probe 9 — Alert rules

**Expected:** llm-chat alert rules ship in the Helm chart and validate with
promtool: backend down, high error rate, approval backlog/retired equivalent,
and zero sessions for 30m.

**Procedure:** `scripts/ops-probes/probe-09.sh` validates YAML and runs promtool
when available.

**Actual:** Added `cordum-helm/alerts/llm-chat.yaml` with four alert rules:
`LLMChatBackendDown`, `LLMChatHighErrorRate`, `LLMChatApprovalBacklogHigh`
(legacy compatibility / should stay zero for informational-only), and
`LLMChatNoSessionsFor30m`. Static probe passed required alert/metric/duration
checks (`chat_sessions_active`, `chat_errors_total`,
`chat_approval_required_total`, 5m and 30m durations). `promtool` was not
available in this shell, so promtool validation is recorded as not run.

**Verdict:** **PASS (static).** Alert file exists with expected rule names and
metrics. Promtool validation remains environment-dependent.

**Evidence:** `out/llmchat-ops/probe-09/evidence.txt`

**Findings / tasks:** Optional CI follow-up to run `promtool check rules` where
promtool is installed.

## Probe 10 — Cost / usage visibility

**Expected:** per-tenant chat usage counters exist for ops/billing planning
(tokens in/out, messages, backend calls; tool-call counters only for legacy
compatibility).

**Procedure:** `scripts/ops-probes/probe-10.sh` checks for admin API routes and,
in live mode, verifies per-tenant counters.

**Actual:** Static scan found no `admin/chat/usage`, `ChatUsage`, `chat_usage`,
`tokens_in`, `tokens_out`, or usage-counter implementation in core/gateway/
dashboard code. Hits in the evidence are unrelated license/session-token docs or
ops-review/runbook text. Live `/admin/chat/usage?tenant=...` check was not run
because `CORDUM_API_KEY` and live mode were not configured.

**Verdict:** **FAIL (P1).** Per-tenant chat usage/cost visibility is not
implemented.

**Evidence:** `out/llmchat-ops/probe-10/evidence.txt`

**Findings / tasks:** File/track follow-up for per-tenant chat usage counters and
admin API (messages, tokens in/out, backend calls; legacy tool-call count only if
legacy path remains installed).

## Probe 11 — Admin debug dump

**Expected:** an admin can export a redacted support bundle for a chat session:
transcript, frame log, trace/correlation IDs, and zero secrets. Dumps must have
bounded retention or cleanup semantics.

**Procedure:** `scripts/ops-probes/probe-11.sh` checks for endpoint/UI support
and scans any produced dump with the shared secret scanner.

**Actual:** Static search found only documentation references to debug dumps,
including the runbook statement that live debug dumps are not yet implemented.
No `DebugDump`, `support_bundle`, `debug_dump`, session-dump handler, gateway
route, or dashboard UI support was found. Live dump scan was not run because no
`LLMCHAT_DEBUG_DUMP_URL` was available.

**Verdict:** **FAIL (P1).** The debug-dump support bundle DoD is unmet.

**Evidence:** `out/llmchat-ops/probe-11/evidence.txt`

**Findings / tasks:** File/track follow-up for a redacted admin support bundle
endpoint/UI with transcript, frame log, correlation/trace IDs, secret scanning,
and retention/cleanup behavior.

## Probe 12 — Log sampling / volume bounds

**Expected:** high-volume streaming/token-delta logs are sampled or suppressed
at INFO; correlation IDs remain available even when detail logs are sampled out.

**Procedure:** `scripts/ops-probes/probe-12.sh` counts log lines during a small
chat load test and records whether DEBUG-level token deltas are bounded.

**Actual:** `probe-12.sh` captured current auto-detected chat-service logs before and
after the static check. It found no token-delta/chunk log lines and no
`assistant_delta`, `token_delta`, stream-chunk, or chunk-delta log lines at
INFO/WARN. The 10-message load subcheck was **not run** in this pass
(`LLMCHAT_OPS_LIVE=1 LLMCHAT_OPS_RUN_LOAD=1` was not set); therefore the
script records `load_bound_enforced=false` and does not claim the <100-line
under-load bound as live evidence.

**Verdict:** **PASS (static/sampling scan), LIVE LOAD NOT RUN.** No token-delta
log spam is visible in the sampled logs, but final REVIEW should not claim the
full load-bound DoD until the small live load can complete on an owned stack.

**Evidence:** `out/llmchat-ops/probe-12/evidence.txt`

**Findings / tasks:** No immediate P0/P1 from static evidence. Live load evidence
is still required if the task is completed under the original DoD wording.


## Adversarial self-review (step 9)

| # | Attack-minded question | Verdict | Action |
|---:|---|---|---|
| 1 | Does probe 1 catch token shapes beyond `eyJ` JWTs (EdDSA hex, raw API keys without `Bearer`)? | **FIX-APPLIED** | Expanded `scan_for_secret_patterns` to catch `X-API-Key`, `CORDUM_API_KEY`, and 64+ hex material, and to redact those hits in evidence before display. |
| 2 | Does probe 2 count exponential cardinality candidates such as histogram bucket series? | **PASS** | Probe 2 counts `chat_vllm_latency_seconds_bucket`, `_sum`, and `_count` separately; bucket series are fixed (11 buckets) and no unbounded session/principal labels appear. |
| 3 | Does probe 3 cover sad-path traces, not just happy path? | **PASS/FOLLOW-UP-RESOLVED** | `task-0e73db35` adds OTEL spans for happy-path chat turns plus error status on provider/runner/session failures; `probe-03.sh` now fails closed when the configured Jaeger trace lacks required operations. |
| 4 | Does admin-viewer audit identify the exact view (page/cursor/search) for forensics? | **FIX-APPLIED TO FOLLOW-UP** | P1 follow-up `task-83b72a46` now carries a comment requiring page/cursor/search attributes on `chat.admin_session_viewed`. |
| 5 | Does the `v=2` reject test verify close-frame reason, not just close? | **PASS/FOLLOW-UP-SCOPED** | P1 follow-up `task-68a01f28` requires stable `unsupported_protocol_version` error/close behavior; current probe fails until implemented. |
| 6 | Do SIEM tests verify event ordering under concurrent sessions? | **GAP-DOCUMENTED** | Probe 8 is static/exporter-unit evidence only; live sink ordering under concurrent sessions remains unproven until configured sink endpoints exist. |
| 7 | Did promtool validate alert thresholds against actual baselines? | **GAP-DOCUMENTED** | Probe 9 passes static alert presence but records `promtool=not_available`; baseline threshold tuning remains a CI/staging follow-up, not claimed complete. |
| 8 | Does debug-dump testing verify cleanup/retention? | **PASS/FOLLOW-UP-SCOPED** | P1 follow-up `task-53317462` DoD explicitly includes bounded retention/cleanup semantics and secret scanning. |
| 9 | Does log sampling preserve correlation IDs when detail logs are sampled out? | **FIX-APPLIED TO FOLLOW-UP** | Missing `trace_id`/correlation fields are already captured by probe 1 P1; follow-up `task-848f003a` now carries a comment requiring correlation IDs to survive sampling. |

## Follow-up task log

| Severity | Task | Probe | Summary |
|---|---|---|---|
| P1 | task-848f003a | 1 | llm-chat runtime logs are text-prefixed slog, not JSON structured logs with required safe correlation keys. |
| P1 (resolved) | task-0e73db35 | 3 | OTEL/Jaeger trace propagation added for gateway → llm-chat → inference → audit/session lifecycle; `probe-03.sh` rerun passes with configured Jaeger query. |
| P1 | task-83b72a46 | 4 | Admin session viewer lacks concrete `chat.admin_session_viewed` audit event and search by user/tenant/session_id. |
| P1 | task-53317462 | 11 | Admin session debug dump/support bundle endpoint/UI is not implemented. |
| P1 | task-68a01f28 | 5 | Chat frame protocol lacks top-level `v: 1` and unknown-version rejection. |
| P1 | task-7ee2d5ab | 10 | Per-tenant chat usage counters/admin API are not implemented. |

## Final verification log

Final full-probe rerun from `D:/Cordum/cordum` using Git Bash/MSYS on 2026-04-28T07:01Z:

- `bash -n scripts/ops-probes/common-fixture.sh scripts/ops-probes/probe-{01,02,03,04,05,06,07,08,09,10,11,12}.sh` -> `BASH_N_EXIT=0`.
- Original probe exits: `01=1`, `02=0`, `03=1`, `04=1`, `05=1`, `06=0`, `07=0`, `08=0`, `09=0`, `10=1`, `11=1`, `12=0`.
- Post-follow-up probe-3 rerun: `LLMCHAT_JAEGER_QUERY_URL='http://127.0.0.1:16686/api/traces?service=cordum-llm-chat-smoke&lookback=1h&limit=20' bash scripts/ops-probes/probe-03.sh` -> pass with required operations present.
- Expected PASS/static-pass probes after task-0e73db35: 2 (metrics/cardinality), 3 (trace propagation/Jaeger with configured query), 6 (runbook), 7 (Grafana JSON static), 8 (SIEM exporter static + audit unit test), 9 (alert rules static), 12 (log sampling static).
- Expected open FAIL/P1 probes with filed follow-ups: 1 (`task-848f003a`), 4 (`task-83b72a46`), 5 (`task-68a01f28`), 10 (`task-7ee2d5ab`), 11 (`task-53317462`).
- Metrics endpoint used by the final rerun: `http://127.0.0.1:8090/metrics`, matching the current post-pivot compose service name `llm-chat`. The probes still support `LLMCHAT_METRICS_URL` and `LLMCHAT_LOG_SERVICE` overrides for older `llm-chat` profile runs.
- `python -m json.tool cordum-helm/dashboards/llm-chat.json` -> pass.
- `git diff --check` on all task-owned docs/scripts/chart artifacts -> clean before the final commit.
