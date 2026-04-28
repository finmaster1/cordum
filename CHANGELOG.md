# Changelog

All notable changes to this project will be documented in this file.
Format follows [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Added

#### LLM Chat Assistant — Senior ops review evidence (task-8eab552b)
- **`docs/llmchat/ops-review.md`** — 12-probe observability and day-2 operations review for the informational-only/Ollama-default chat assistant. Verdict summary: 0 P0, 6 P1 follow-ups filed, 1 P2 product-routing check, Prometheus cardinality bounded, log secret scan clean, and live Grafana/Jaeger/sink evidence explicitly marked not run where no owned environment was configured.
- **`scripts/ops-probes/`** — reproducible probe harness for structured logs, metrics cardinality, trace/Jaeger wiring, admin session viewer audit/search, protocol versioning, runbook coverage, Grafana JSON, SIEM export, alert rules, per-tenant usage counters, debug dumps, and log sampling.
- **`docs/llmchat/ops-runbook.md`**, **`docs/llmchat/protocol-versioning.md`**, **`cordum-helm/dashboards/llm-chat.json`**, and **`cordum-helm/alerts/llm-chat.yaml`** — customer-facing ops runbook, protocol v1/v2 plan, Grafana dashboard, and baseline alert rules.
- **P1 follow-ups filed**: task-848f003a (JSON structured logs), task-0e73db35 (OTEL/Jaeger traces), task-83b72a46 (admin viewer audit/search), task-68a01f28 (chat frame `v: 1`), task-7ee2d5ab (per-tenant usage API), task-53317462 (redacted debug dump/support bundle).

#### LLM Chat Assistant — Governance senior review (task-931eaea2)
- **`docs/llmchat/governance-review.md`** — 13-probe senior governance review of the chat-assistant dogfooding pitch ("we govern AI workforces including our own copilot"). Probes cover identity (CAP SDK round-trip + idempotent boot, dashboard Jobs visual parity, AllowedTools scope-first deny), audit (mcp.tool_invocation event coverage, chain integrity under chat-driven load with measured `/api/v1/audit/verify` p99, scheduler-restart durability), gating (safety-kernel deny on malformed bundle, approval gate wire-through with WS approval_required frame + retry, default policy bundle enforcement, defense-in-depth zero-trust), and tenancy + delegation (data-classification scope, JTI revocation in Redis, cross-tenant isolation). Verdict tally: **1 PASS / 4 PARTIAL PASS / 7 BLOCKED / 1 DEFERRED**. The two filed P0 blockers (`task-5b755f42` audit/query handler, `task-f13505cc` dashboard agent_id parity) both DONE. Remaining gaps captured as P1/P2 follow-ups: F1 (no `cordumctl agent set-scope` CLI), F7 (`config/llmchat/policy-default.yaml` not auto-loaded), audit-verify endpoint concurrency scaling (p99 jumps 10× at 20 parallel), deterministic-LLM stub for QA, DataClassifications metadata not surfaced in `/api/v1/agents`, Redis TLS client path inside `cordum-redis-1`, `cordumctl tenant` subcommand inventory.
- **Cross-links** — bi-directional CAP cross-linking per epic rail #5: [`cap/docs/agent-registration.md`](https://github.com/cordum-io/cap/blob/main/docs/agent-registration.md) (new, see cap CHANGELOG) documents the `AgentClient.Register/Lookup/SetScope` wrappers with the chat-assistant bootstrap as the reference consumer; the public docs site's [`concepts/agent-protocol`](https://cordum.io/docs/concepts/agent-protocol) and [`concepts/audit`](https://cordum.io/docs/concepts/audit) pages now point at the governance review evidence.

#### LLM Chat Assistant — WCAG 2.5.5 touch targets (task-f2507515)
- **`dashboard/src/components/chat-assistant/ChatWidget.tsx`** + **`ChatComposer.tsx`** — Close and send buttons enlarge to 44 × 44 px at the `≤sm` breakpoint (mobile fullscreen overlay) via `h-11 w-11 sm:h-7 sm:w-7` and `h-11 w-11 sm:h-9 sm:w-9` respectively. Icons stay `h-4 w-4`; only the button container expands so desktop perceived weight is preserved. Satisfies WCAG 2.5.5 (Target Size — Enhanced AAA, 44 px) and Apple HIG minimum hit-target. Asserted in `ChatWidget.test.tsx` § "responsive layout + WCAG 2.5.5 touch targets" via literal-substring class matchers (drift-resistant against future `md:`/`lg:` swaps that would skip tablet portrait at 600-768 px).

#### LLM Chat Assistant — UX & accessibility senior review (task-7dd1af21)
- **`docs/llmchat/ux-review.md`** — 16-probe UX & accessibility review of the Gmail-style chat assistant. Code-review evidence + reproducer commands for each probe. Verdict counts: 5 PASS, 4 PARTIAL, 3 FAIL filed as P1 follow-ups, 4 DEFERRED-TO-LIVE-STACK (gated by task-6a8680fc + task-1e6d21fc), 1 DEFERRED-TO-HUMAN cluster (gated by task-a5d09fad), 1 DEFERRED-PER-DECISION i18n (task-530874ea). Cross-cutting blockers section names every gating task ID.
- **`dashboard/src/components/chat-assistant/ChatWidget.a11y.test.tsx`** — axe-core (wcag2a + wcag2aa, critical+serious filter via the existing `assertNoSeriousAxeViolations` helper) on five widget states: header button only, panel open empty, populated transcript with tool_call+tool_result, ApprovalInlinePrompt rendered, panel open in dark mode. All five green; full chat-assistant suite (4 files / 20 tests) passes.
- **P1 follow-ups filed**: task-7ff6765f (probe 6 — sessionStorage vs localStorage / tab-close resume mismatch), task-f2507515 (probe 11 — chat widget no full-screen overlay at 375 px), task-47de92ef (probe 13 — empty-state suggestion chips missing).

#### LLM Chat Assistant — Phase 11 (tool-call eval harness + model-version-bump protocol)
- **`tests/eval/`** — Go-based tool-call eval harness (`//go:build eval`, no CI run by default) with 30 YAML golden cases across 6 categories (`read_only`, `filtered_reads`, `preapproved_mutations`, `approval_gated_mutations`, `multi_turn`, `guardrail_triggers`; minimum 5 each). The runner walks `tests/eval/cases/**/*.yaml`, drives each case through `POST /api/v1/chat`, and scores tool-call accuracy + JSON-schema-Lite arg validity + summary substring hits + forbidden-call violations + budget overruns. Per-case `CaseResult` JSON + aggregate `EvalSummary` written under `tests/eval/results/<run_id>/`.
- **Baseline + diff comparator** (`tests/eval/compare.go`) — diffs two `EvalSummary` JSON files and emits a markdown report flagging regressions (`>5%` per-case degradation), improvements, new failures, and removed cases. The 5% threshold is the default merge-blocking budget per task rail #6; explicit waivers in the bump PR description override it (documented in `docs/llmchat/model-version-bump.md`).
- **Default-tag regression tests** (`tests/eval/yaml_test.go` + `case_test.go`) — verify every YAML case parses, the file name matches `case.name`, the 5-per-category minimum is met, names are unique, and the JSON-schema-Lite matcher correctly handles type sentinels (`int`, `float`, `str`, `bool`, `array`, `object`), nested objects, literal values, and missing/extra keys. Runs in `go test ./...` so a malformed case fails CI before any vLLM call.
- **`.github/workflows/llmchat-eval.yml`** — `workflow_dispatch` trigger (manual ad-hoc, with optional PR-comment posting via `peter-evans/create-or-update-comment@v4`) plus optional nightly cron (08:00 UTC) for drift detection. 90-min timeout. NOT promoted to required check in v1 — promotion requires GPU-budget decision per task rail #1 + #3.
- **`docs/llmchat/model-version-bump.md`** — 5-step protocol (stage → eval → review diff → update baseline + pin in SAME PR → deploy + monitor 24h) with waiver discipline + v2 considerations.
- **`tests/eval/cases/SCHEMA.md`** — author-facing contract for adding new cases (top-level fields, JSON-schema-Lite matcher rules, category guidance, anti-patterns).

#### LLM Chat Assistant — Phase 10 (vLLM config drift CI gate)
- **`tools/scripts/vllm_config_lint.sh`** + **`vllm_helm_lint.sh`** + **`vllm_lint_common.sh`** — bash lint scripts that hard-fail on `qwen-inference` config drift in `docker-compose.yml` / `docker-compose.release.yml` and the rendered `cordum-helm` chart. Encoded rules: model-must-match-tier (Tier 1 FP8 default, Tier 2 AWQ via `CORDUM_LLMCHAT_TIER=2`), `--tool-call-parser qwen3_xml` mandatory + `hermes` / `qwen3_coder` disallowed, `--max-model-len 131072`, `--kv-cache-dtype fp8`, `--enable-prefix-caching` present, `127.0.0.1:8000:8000` host port mapping (loopback boundary; `0.0.0.0:8000:8000` and bare `8000:8000` rejected), healthcheck `start_period: 300s`. Helm-side adds `qwen-inference` Service.type=ClusterIP and the 5 mandatory `qwenInference` values keys. Each FAIL prints `[FAIL] <file>:<line> rule=<name> — <explanation>` so contributors can map any failure to a specific check in 30 seconds.
- **`tools/scripts/vllm_config_lint_test.sh`** — negative-case driver: builds 5 fixture composes (hermes parser injection / wildcard ports / missing `--kv-cache-dtype` / wrong `start_period` / bare ports), runs each 3× to catch flakes, asserts the lint rejects each with the right rule name. Positive case runs the lint against the actual current `docker-compose*.yml` to satisfy task rail #5 (this task's PR must itself pass lint against phase-7 deliverables).
- **`.github/workflows/vllm-config-lint.yml`** — new CI job; runs on PRs that touch `docker-compose*.yml`, `cordum-helm/**`, or `tools/scripts/vllm_*`. Installs Helm v3.20 + mikefarah `yq` v4, runs the test driver, then the compose + helm lints. 5-min timeout. Branch-protection promotion is a follow-up after a 7-run / 7-day soak.
- **`docs/llmchat/ci-lint.md`** — operator doc covering the per-rule rationale, Tier 1 vs Tier 2 codepaths, local invocation, and the rule-change workflow.

#### LLM Chat Assistant — Phase 9 (integration + e2e tests + CI workflow)
- **`core/llmchat/testutil/mockvllm`** — script-driven `httptest.Server` that speaks the OpenAI-compat streaming surface `core/llmchat/provider_openai.go` expects. Each call to `/v1/chat/completions` advances a turn counter and replays the next scripted `Turn` as SSE frames terminated by `data: [DONE]`. `NewServerHealthy` exposes only `/v1/models` for readiness-only paths. Used by every llmchat integration test so CI never needs a GPU.
- **`core/llmchat/integration_provider_mockvllm_test.go`** (build tag `integration`) — exercises the real `OpenAIProvider` SSE parser through `mockvllm` end-to-end. Two scenarios: (a) text-only turn round-trip asserts assembled deltas + `Done=true` chunk; (b) full Agent loop with `mockvllm` + fake MCP, asserts `tool_call` + `tool_result` + `final` frame ordering and that the agent makes exactly 3 mockvllm calls (tool-call dispatch, end-signal, summary).
- **`core/llmchat/integration_live_stack_test.go`** (build tag `integration`, gated on `CORDUM_INTEGRATION=1`) — three live-stack probes against a running cordum gateway: (1) `TestLiveStack_MCPRoundtripEmitsAuditEvent` asserts a `cordum_list_jobs` MCP call grows the `/api/v1/audit/verify` event count and the chain stays valid; (2) `TestLiveStack_CaseA_PreapprovedSubmit` submits a $40 demo-mock-bank transfer via `cordum_submit_job` (preapproved per epic rail #4) and asserts audit growth + chain validity; (3) `TestLiveStack_CaseB_ApprovalGatedResume` submits a $200 transfer (require-approval per phase-8 policy bundle), drives `cordum_approve_job` against the gate, and resumes via `POST /api/v1/approvals/{id}/approve`.
- **`.github/workflows/llmchat-integration.yml`** — new CI job mirroring `demo-mock-bank-e2e.yml` conventions: per-run-generated API key + Redis password, generated TLS certs via `cordumctl generate-certs`, `docker compose --profile demo up -d`, condition-based `/api/v1/status` polling, `cordumctl pack install demo/mock-bank/pack`, then `go test -tags=integration ./core/llmchat/... -count=3 -timeout 10m -v`. On failure, captures gateway/scheduler/safety-kernel/workflow-engine/mock-bank-worker/redis/nats logs + `compose ps`. Workflow runs on push:main + pull_request:main; branch-protection promotion is a follow-up after a 14-run / 7-day soak per Phase 8 rail.

#### LLM Chat Assistant — Phase 8 (default policy bundle + system prompt + operator/customer docs)
- **Default policy bundle** (`config/llmchat/policy-default.yaml`) — importable via the existing `POST /api/v1/policy/bundles` flow. Scopes the `chat-assistant` agent to 14 read-only tools + `cordum_query_policy` + `cordum_submit_job` (preapproved) + 4 approval-gated mutators (`cordum_approve_job`, `cordum_reject_job`, `cordum_cancel_job`, `cordum_trigger_workflow`). Data classifications: `[public, internal]`. Heavily commented for admin readers; no top-level `description` key (epic rail #3).
- **Production system prompt** (`config/llmchat/system-prompt.md`) — loaded by the phase-4 `core/llmchat/prompt.go` PromptLoader at boot. Grounds the LLM in Cordum domain primitives (workflow / run / job / approval / agent identity / pack / audit chain), names every available tool by its exact `core/mcp/tools.go` constant string, and pins the six guardrails (no invented IDs, audit-before-explain-denial, never echo secrets, ambiguous-amount clarification, no-loop, one-mutation-per-turn). Embeds `{{api_summary}}` + `{{cordum_io_summary}}` placeholders consumed by the phase-3 knowledge-pack substituters.
- **Customer-facing docs** under `docs/llmchat/`:
  - `overview.md` — extended with high-level architecture (mermaid diagram), security model summary, who-sees-what RBAC matrix, license entitlement defaults (Enterprise on, Community off — epic rail #3). Existing API reference preserved underneath.
  - `provider-config.md` (new) — full env-var table with all four two-pass-sampling knobs (`LLMCHAT_TOOL_TEMPERATURE=0.3`, `LLMCHAT_TOOL_TOP_P=0.9`, `LLMCHAT_SUMMARY_TEMPERATURE=0.7`, `LLMCHAT_SUMMARY_TOP_P=0.8`) + per-turn budget knobs + the correct vLLM command form + an explicit "do NOT use" anti-example block listing `qwen3_coder` / `hermes` / `--host 0.0.0.0` host port mapping.
  - `policy-bundle-default.md` (new) — import / widen / narrow recipes + a strong "do not promote tools to preapproved" warning.
  - `hardware-tiers.md` (new) — Tier 1 (H100, production), Tier 2 (RTX 5090 / PRO 6000 — design-partner preview, NOT production-validated), Tier 3 (A100, supported but slower), Unsupported (<24 GB VRAM, CPU). VRAM budget + concurrent-session target + model-swap recipe per tier.
  - `troubleshooting.md` (new) — 7 entries; entry #1 is the `!!!!!!!!` infinite-stream symptom pointing at `qwen3_xml` parser config (the single most-Googled failure mode for this model).
- **License entitlement** `LLMChatAssistant` documented across all the operator-facing pages — Enterprise default true, Community default false (epic rail #3).

#### LLM Chat Assistant — Phase 6 (Gmail-style dashboard widget)
- **Floating chat widget** (`dashboard/src/components/chat-assistant/`) — `ChatWidget`, `ChatStream`, `ToolCallCard`, `ApprovalInlinePrompt`, `ChatComposer`, and `ChatHeaderButton`. The panel mounts once in `AppShell` outside `<Routes>` so it survives navigation between pages exactly like Gmail's chat pane. Animations respect `prefers-reduced-motion`; the panel is `role="complementary"` (not `dialog` — it is persistent, not modal) and Esc-to-close is wired but suppressed when typing in the composer.
- **Header button visibility gate** — the icon in the top bar renders only when (a) the `llmChatAssistant` feature flag is on, (b) the `LicenseEntitlements.features.llm_chat_assistant` flag is true, and (c) `/api/v1/chat/healthz` is returning 200. Any failure hides the button entirely (no greyed-out state) per the epic rail "users never see a broken chat UI".
- **Availability hook** (`hooks/useChatAssistantAvailability.ts`) — polls `/chat/healthz` every 10s, never fires on Community-tier deployments to avoid noisy 401s. Returns a tagged union `{available, reason}` with `unentitled` / `unauthorized` / `vllm_down` / `redis_down` / `unknown` reasons.
- **Session hook** (`hooks/useChatAssistantSession.ts`) — manages a single WebSocket to `/api/v1/chat/ws` using the existing `cordum-api-key.<base64>` subprotocol auth. Exponential backoff 1s/2s/4s/8s capped, 5-failure cap before surfacing `status='closed'`.
- **Session-scoped store** (`state/chatAssistant.ts`) — new zustand store distinct from the existing run-keyed `state/chat.ts`. Persists panel-open state and session id via `sessionStorage` (NOT `localStorage` — chat state is principal-bound and must not survive sign-outs); messages stay in memory only.
- **Admin sessions index** (`pages/settings/ChatAssistantSessionsPage.tsx`) — admin-only page at `/settings/chat-sessions` listing all chat sessions with cursor pagination; rows link to the existing `/copilot/sessions/:sessionId` detail page rather than duplicating transcript rendering.
- **Default MSW handlers** for `/chat/healthz`, `/chat/sessions`, and `/chat/sessions/:id` so any test that mounts `<AppShell />` runs cleanly without per-test setup.
- **Feature flag** `VITE_LLM_CHAT_ASSISTANT` — default `false`. Operators flip it explicitly per environment so the widget can dark-launch independent of the license entitlement state.

#### LLM Chat Assistant — Phase 5 (HTTP transports + admin session viewer)
- **Chat HTTP surface** (`core/llmchat`) — added `POST /api/v1/chat`, SSE fallback `GET /api/v1/chat/stream`, and WebSocket `GET /api/v1/chat/ws`, all gated by the `llm_chat_assistant` entitlement with stable `feature_unavailable` errors when disabled.
- **WebSocket frame contract** — pinned seven frame types (`user`, `assistant_delta`, `tool_call`, `tool_result`, `approval_required`, `final`, `error`) with optional `session_id`; tool-result frames get a defense-in-depth redaction pass before leaving the service.
- **Approval resume** — added an approval resumer for `sys.approvals.>` events. Resolved approvals replay the pending tool call with the session delegation token; rejected approvals inject `tool_result{is_error:true, tool_result:"denied by human reviewer"}` and let the LLM narrate.
- **Admin session viewer** — added `GET /api/v1/chat/sessions` and `GET /api/v1/chat/sessions/{session_id}` plus new RBAC capability `chat.read_all`; tenant admins are scoped to their tenant and cross-tenant detail lookups return 404.
- **Audit lifecycle events** — WebSocket connect/disconnect emit `chat.session_started` and `chat.session_closed` through the existing `SIEMEvent` chain path; schema documented in `docs/llmchat/overview.md`.

#### LLM Chat Assistant — Phase 7 (Docker Compose + Helm packaging)
- **Docker Compose** (`docker-compose.yml`, `docker-compose.dev.yml`, `docker-compose.release.yml`) — new `qwen-inference` and `llm-chat` services gated by `profiles: [llmchat]` so the default `make dev-up` stack stays GPU-free. `qwen-inference` runs `vllm/vllm-openai:latest` with the exact 10-flag form (FP8 model, `qwen3_xml` parser, 131072 context, FP8 KV cache, prefix-caching, gpu-mem-util 0.9, host port loopback `127.0.0.1:8000:8000`). The vLLM process binds inside the container on the bridge interface so `llm-chat` can reach `http://qwen-inference:8000/v1` while the host exposure remains loopback-only. 300s healthcheck `start_period` for the cold load. `llm-chat` uses `depends_on.qwen-inference.condition: service_started` to avoid deadlocking on the 5-minute weight load. Named volume `qwen_hf_cache` persists 30GB FP8 weights across restarts. The dev overlay adds `cordum/llm-chat:dev` and resets GPU reservations; the release overlay adds TLS-mandatory chat service settings.
- **Helm chart** (`cordum-helm/`) — new `templates/deployment-llm-chat.yaml`, `templates/deployment-qwen-inference.yaml`, `templates/service-llm-chat.yaml`, `templates/service-qwen-inference.yaml`, and `templates/configmap-llm-chat.yaml`. Both services are gated by `.Values.{llmChat,qwenInference}.enabled` (default true). `Service.type: ClusterIP` on both — `qwen-inference` is never exposed externally; `llm-chat` reaches it via in-cluster DNS. External-vLLM mode supported via `llmChat.externalBaseUrl` + `qwenInference.enabled=false`. PVC for HF weight cache (default 50Gi) survives rollouts.
- **`cordum-helm/values.yaml`** — appended full `llmChat` (image, resources, sampling 0.3/0.9 tool + 0.7/0.8 summary, externalBaseUrl) and `qwenInference` (model FP8 + tier2AwqModel reference, qwen3_xml parser, maxModelLen 131072, kvCacheDtype fp8, prefix-caching, gpu nodeSelector + tolerations, hfCache size + storageClass) sections.
- **CI** — `.github/workflows/docker.yml` and `docker-main.yml` matrices extended with `{name: llm-chat, service: cordum-llm-chat}`; the published image at `ghcr.io/cordum-io/cordum/llm-chat` matches the path referenced in compose (slash-separator per project_quickstart_ghcr_path.md).
- **`docs/llmchat/helm.md`** (new) — install/upgrade commands, GPU node prereqs, external-vLLM mode, disable, all 3 hardware tiers (H100 / RTX 5090 AWQ / A100), parser pinning rationale, network exposure boundary, HF cache PVC.

#### LLM Chat Assistant — Phase 2b (read-only Cordum REST API client)
- **`core/llmchat/apiclient.go`** — read-only HTTP client hitting `<gateway>/api/v1/*`. Exposes `ListJobs` (typed `[]model.JobRecord`), `GetJob`, `ListBundles`, `GetBundle`, `ListPolicies`, `GetAuditChain` (typed `*sdk/client.AuditVerifyResult`). All methods are GET; the package contains zero `http.MethodPost|Put|Patch|Delete` constants and a unit test (`apiclient_readonly_test.go`) source-greps the package to fail any PR that adds one. Mutations remain on the MCP path (mcpclient.go) for ApprovalGate + ToolInvocationAuditor governance.
- **Auth hierarchy** mirrors mcpclient: per-call `Authorization: Bearer <delegation-token>` REPLACES the service-account `X-API-Key` (rail #3 — service key never leaks into per-session reads).
- **Bounded retry**: 4xx → no retry, surface typed `*ApiUnauthorizedError`/`*ApiForbiddenError`/`*ApiNotFoundError`/`*ApiClientError`; 5xx + transport errors → exponential backoff (default base 500ms, cap 8s, 3 attempts) → `*ApiServerError`. Bearer token never appears in slog fields.

#### LLM Chat Assistant — Phase 3 (session store + delegation tokens + agent bootstrap)
- **Redis session store** (`core/llmchat/session.go`) — chat sessions persisted at `chat:session:{id}` with 24h sliding TTL refreshed on every AppendMessage. FIFO cap at 50 messages bounds memory under long-lived "Gmail-style" sessions where users return for days. Pinned key format consumed by phase-5 WS handler + admin session viewer.
- **Per-session delegation tokens** (`core/llmchat/delegation.go`) — every chat session mints a child EdDSA JWT (chain depth 1, 15-minute default TTL) via `POST /api/v1/agents/{id}/delegate`. CallTool then carries the JWT in `Authorization: Bearer ...`; the service-account `X-API-Key` is OMITTED on those paths so it never leaks into the per-user tool-call audit trail. `ForSession` auto-refreshes when a token is within 60s of expiry. `Revoke` calls `POST /api/v1/agents/revoke-delegation` with the JTI on session close.
- **Idempotent chat-assistant bootstrap** (`core/llmchat/bootstrap.go`) — first-boot: `capsdk.AgentClient.Lookup("chat-assistant", tenant)`. Hit → reuse + verify scope match (rejects divergent identities, never silently accepts). Miss → `capsdk.AgentClient.Register` with the canonical AllowedTools surface (8 read tools + cordum_query_policy + 5 mutating tools), then `capsdk.AgentClient.SetScope` with PreapprovedMutatingTools=[cordum_submit_job] EXACTLY (widening requires policy-bundle update post-ship per epic rail #4). Refuses to proceed when multiple chat-assistant registrations are queued. The CAP SDK calls hit the same gateway control-plane endpoints (POST/GET/PUT `/api/v1/agents`) the MCP bridge uses, so audit + approval-gate behavior is byte-identical to any other Cordum agent identity registration.
- **Bootstrap migrated to CAP SDK AgentClient** — `core/llmchat/bootstrap.go` now depends on a slim `AgentRegistry` interface satisfied by `capsdk.AgentClient` (`cap/sdk/go/agent.go`, shipped in cap PR #44 commit `aad9445`). The pre-GA MCP `cordum_register_agent` / `cordum_set_agent_scope` fallback path is deleted entirely (`feedback_no_backwards_compat`). `cmd/cordum-llm-chat/main.go` constructs the `capsdk.AgentClient` from the existing gateway URL + service API key + tenant config and removes the `mcpAdapter` wrapper.
- **Gateway agent endpoint round-trips `preapproved_mutating_tools`** (`core/controlplane/gateway/handlers_agents.go`) — `updateAgentRequest` and `agentResponse` now carry `PreapprovedMutatingTools []string`; `handleUpdateAgent` persists the field into `store.AgentIdentity`. Previously the gateway silently dropped the field that `core/mcp/bridge_mutating.go` and the new CAP SetScope wrapper send, breaking deterministic-revoke semantics. Additive change with no migration required.
- **CAP module workspace replace** — `go.mod` adds `replace github.com/cordum-io/cap/v2 => ../cap` so cordum builds against the unmerged cap branch carrying `agent.go`. The replace will be removed and the require pin bumped once cap publishes the new tag.
- **Chat lifecycle SIEM action constants** (`core/audit/siem_actions.go`) — centralizes `chat.session_started`, `chat.session_closed`, and `chat.bootstrap_registered` so the bootstrap path and phase-5 websocket/session handlers share one action-string source of truth. **WIRE-FORMAT CHANGE:** `SIEMEvent.Action` for chat-assistant bootstrap is now `chat.bootstrap_registered` (was `cap.agent_registered`); SIEM consumers filtering on the old literal must update.
- New env vars on `cordum-llm-chat`: `LLMCHAT_CHAT_ASSISTANT_AGENT_ID` (REQUIRED), `LLMCHAT_TENANT` (optional), `LLMCHAT_DELEGATION_TTL_SECONDS=900` (default 15min).
- Bootstrap runs synchronously on startup under a 30s deadline; failure → exit 1 with structured error log (no silent partial registrations).

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

- **dashboard llm-chat (task-47de92ef)** — chat empty-state suggestion chip text now matches the DoD verbatim ("show denied jobs today" / "list my active workflows" / "what policies apply to billing?"). Each chip `<button>` gains an explicit `aria-label` of the form "Send suggestion: <text>" so screen readers announce the action plus the payload rather than just the chip text. New `dashboard/src/components/chat-assistant/ChatStream.test.tsx` asserts text, aria-label, click → onSuggestionClick wiring, and disabled-state.
- **dashboard llm-chat (task-7ff6765f)** — `useConfigStore.logout()` now invokes `resetChatAssistantStore()`, wiping the in-memory chat-assistant state and the persisted `cordum-chat-assistant` localStorage key on every sign-out and 401-on-license. Previously the chat session pointer survived sign-out, which could surface a prior operator's transcript to the next user on a shared workstation. Stale sessionStorage-era comment block in `dashboard/src/state/chatAssistant.ts` updated to describe the actual localStorage + sign-out-clear contract. New `dashboard/src/state/__tests__/chat-assistant-logout-reset.test.ts` covers state-clear, localStorage-clear, and idempotency.
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
  remain under `/api/v1/mcp/*`. See
  [`docs/cleanup/openapi-legacy-audit.md`](docs/cleanup/openapi-legacy-audit.md)
  `Audit re-verification 2026-04-23` for the ground-truth timeline.

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
