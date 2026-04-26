# LLM Chat Assistant — Governance Senior Review

Dogfooding QA for `cordum-llm-chat`: the chat copilot is itself a Cordum agent, so its tool calls MUST traverse the same policy / approval / audit pipeline as any other MCP client. This page records the senior-review evidence for the 13 governance probes specified in `task-931eaea2`.

> **Scope note:** task-931eaea2's plan calls for 13 probes covering identity, audit, gating, and tenancy. Several pre-execution findings (recorded in step-2 of the task plan) require corrections to the probe procedures before execution; see [Pre-execution findings](#pre-execution-findings) below.

## Status summary

| | Identity (1, 2, 7) | Audit (3, 4, 11) | Gating (5, 6, 12, 13) | Tenancy (8, 9, 10) |
|-|-|-|-|-|
| Pass | _tbd_ | _tbd_ | _tbd_ | _tbd_ |
| Fail | _tbd_ | _tbd_ | _tbd_ | _tbd_ |
| Blocked | _tbd_ | _tbd_ | _tbd_ | _tbd_ |

Audit-chain integrity (DoD #2): `/api/v1/audit/verify` → _tbd_
Chain-verify p99 latency under chat load (DoD #3): _tbd_ ms (budget ≤ 10ms regression)
P0 / P1 / P2 follow-up tasks filed: _tbd_

## Pre-execution findings

Recorded in `task-931eaea2` step-2 (worker-e2a9, 2026-04-26). Each finding affects probe procedure; do not execute the affected probe without applying the correction.

| # | Finding | Affected probe(s) | Severity | Action |
|---|---------|-------------------|----------|--------|
| F1 | `cordumctl agent set-scope` does NOT exist (no CLI command) | 7, 13 | P1 | Use CAP SDK control-plane endpoint (see `core/llmchat/bootstrap.go:295-300`) instead of CLI |
| F2 | `cordumctl license generate --tier enterprise` does NOT exist | step-1(e) | P2 | Skip; chat-assistant entitlement already active in this stack |
| F3 | `cordumctl run <id>` is actually `cordumctl run start <workflow_id>` | 6 | P2 | Update probe procedure |
| F4 | Probe 1 acceptance criteria references `cap.agent_registered` SIEM event but the actual constant is `chat.bootstrap_registered` (`core/audit/siem_actions.go:9`) | 1 | P2 | Update probe procedure to grep for `chat.bootstrap_registered` |
| F5 | Audit query endpoint is `/api/v1/audit/query` (NOT `/api/v1/audit/events` as the task description says) | 3 | P2 | Update probe procedure |
| F6 | Dashboard `JobsPage.tsx:700-756` does NOT render an `agent_id` column. Visual parity DoD cannot pass without dashboard work. | 2 | P0 | File dashboard task to add agent_id column to Jobs table |
| F7 | `config/llmchat/policy-default.yaml` is NOT auto-loaded; operator must POST to `/api/v1/policy/bundles` after first stack boot | 12 | P1 | Either auto-load on chat-assistant boot OR document the manual step in the deployment runbook + add a smoke check |
| F8 | **`/api/v1/audit/query` endpoint returns 404** — gateway never registers it (`core/controlplane/gateway/gateway.go:1192-1207` only registers `/audit/export*`, `/audit/verify`, `/audit/legal-hold*`). MCP `audit_query` tool at `core/mcp/bridge_readonly.go:245` calls this non-existent endpoint and would fail at runtime. | 1, 3, 4, 5, 11 | **P0** | Either implement the gateway handler OR change the MCP bridge to use a different mechanism (e.g. policy/audit, legal-hold export). Verified live: `curl https://localhost:8081/api/v1/audit/query?type=chat.bootstrap_registered → 404`, `audit/verify → 200`. |

## Probe template

Each probe section follows this template:

```
## Probe N — <title>
**Expected:** <criteria>
**Procedure:** <steps>
**Actual:** <to be filled>
**Verdict:** [PASS|FAIL|BLOCKED]
**Evidence:** <links to logs, audit query results, screenshots>
**P0/P1 task filed:** <task ID if FAIL>
```

---

## Probe 1 — CAP SDK agent-identity round-trip
**Expected:** chat-assistant registers via `core/auth/delegation` + `cap/sdk/go/agent.go` on first boot (idempotent). On first boot, an audit event for the registration is emitted with `agent_id=chat-assistant@<tenant>`, `risk_tier=medium`, `preapproved_mutating_tools=[cordum_submit_job]`. On restart, no duplicate registration. Every subsequent tool call carries the CAP-tagged AgentIdentity in the audit record.

**Procedure (corrected per F4):**
1. `docker compose restart cordum-llm-chat` and capture timestamp `T0`.
2. `cordumctl audit query --type chat.bootstrap_registered --since T0 --limit 5` (or hit `/api/v1/audit/query?type=chat.bootstrap_registered&since=T0`).
3. Restart again at `T1`; query `/api/v1/audit/query?type=chat.bootstrap_registered&since=T1` → expect zero new events.
4. Drive a chat-driven `cordum_list_jobs` call; query `/api/v1/audit/query?type=mcp.tool_invocation&since=<call-time>` and assert the audit row carries `agent_id=chat-assistant@<tenant>` field.

**Actual (worker-e2a9, 2026-04-26T17:20Z):**
- (a) chat-assistant agent IS registered in `/api/v1/agents`: 1 entry (no duplicate ⇒ idempotent boot worked at least once across the stack lifetime), `id=d2315a95-7b08-40a1-8bdc-7b96858f41e6`, `risk_tier=medium` ✓ (matches plan spec), `owner=system`, `allowed_tools=20` (matches `config/llmchat/policy-default.yaml`), `preapproved_mutating_tools=['cordum_submit_job']` ✓ (matches epic rail #4).
- (b) Restart for idempotency check: `docker restart cordum-llm-chat-1` failed with `Error response from daemon: Cannot restart container cordum-llm-chat-1: error while creating mount source path '/run/desktop/mnt/host/d/Cordum/cordum-llm-debug/config/llmchat': mkdir /run/desktop/mnt/host/d/Cordum/cordum-llm-debug: file exists`. Environmental issue (orphan mount from another worker session); does NOT invalidate (a) but blocks fresh-boot test.
- (c) Audit emission verification BLOCKED by **F8** — `/api/v1/audit/query` returns 404. Gateway only registers `/audit/export*`, `/audit/verify`, `/audit/legal-hold*` (`gateway.go:1192-1207`). The MCP `audit_query` tool at `bridge_readonly.go:245` calls a non-existent endpoint.
- (d) Chain integrity: `GET /api/v1/audit/verify` → `{"status":"ok"}` (response is just `{status}`; no `valid`/`chain_depth`/`verified_count` fields).

**Verdict:** PARTIAL PASS for (a), BLOCKED on (b) by Docker mount issue, BLOCKED on (c) by F8.
**Evidence:** Live API responses captured in step-4 worker note. Code refs: `core/audit/siem_actions.go:9` (constant exists), `core/llmchat/bootstrap.go:126,190` (boot + emission). `core/controlplane/gateway/gateway.go:1192-1207` (audit routes registered — no /query).
**P0/P1 task filed:** F8 to be filed in step-8 triage as P0 (audit query plumbing broken).

---

---

## Probe 2 — Chat-driven calls in dashboard Jobs + Audit pages
**Expected:** Open `/jobs` in tab A, chat widget in tab B; submit "$40 transfer in demo-mock-bank"; the job appears in `/jobs` with an `agent_id=chat-assistant@<tenant>` indicator and is clickable to a detail page showing full lineage.

**Procedure:** _BLOCKED ON F6_ — Jobs page does not render agent_id column. File a dashboard task to add the column (or surface agent_id in another visible way) before this probe can complete.

**Actual (worker-e2a9, 2026-04-26T17:20Z):** Re-confirmed F6 by re-reading `dashboard/src/pages/JobsPage.tsx:700-756`. Columns rendered: Status, Job ID, Topic, Origin, Safety Decision, Attempts, Updated. NO agent_id column. The dogfooding pitch ("appears in Jobs page like any other Cordum agent" — task rail #6) cannot be visually confirmed without dashboard work.
**Verdict:** BLOCKED
**Evidence:** Code survey only; no live test possible.
**P0/P1 task filed:** F6 → step-8 P0.

---

## Probe 3 — Every MCP call produces a `mcp.tool_invocation` SIEMEvent
**Expected:** Run 100 diverse chat messages covering all 22 MCP tools. Audit count of `mcp.tool_invocation` events grows by exactly 100. Zero drops.

**Procedure (corrected per F5):**
1. Capture baseline count: `before=$(curl -sk "$BASE/api/v1/audit/query?type=mcp.tool_invocation&limit=1" | jq '.total')`.
2. Drive 100 chat turns via the mockvllm script harness (phase-9 fixture).
3. Capture: `after=$(curl -sk "$BASE/api/v1/audit/query?type=mcp.tool_invocation&limit=1" | jq '.total')`.
4. Assert `after - before == 100`.

**Actual (worker-e2a9, 2026-04-26T17:25Z):** BLOCKED hard by F8 — neither `/api/v1/audit/query` nor `/api/v1/audit/events` exist in the gateway (gateway.go:1192-1207 only registers /audit/export*, /audit/verify, /audit/legal-hold*). Cannot count `mcp.tool_invocation` events via the documented HTTP path. Workaround paths: (a) audit-export to a webhook/syslog/Datadog backend then grep externally (production-realistic but requires backend setup), (b) implement the gateway handler (architectural fix), (c) query Redis audit store directly (out-of-band, brittle).
**Verdict:** BLOCKED on F8 (P0).
**Evidence:** Live confirmation: `curl https://localhost:8081/api/v1/audit/query?type=mcp.tool_invocation → 404`.
**P0/P1 task filed:** F8 → step-8 P0.

---

## Probe 4 — Chain integrity across chat-driven load
**Expected:** Run probe 3 + concurrent governance + security stress; `GET /api/v1/audit/verify` returns `status=ok`, no chain breaks. Document chain depth + verify p99 latency.

**Procedure:**
1. Start probe 3 in background.
2. Concurrently run integration_case_a + integration_case_b stress fixtures.
3. After completion, `curl -sk "$BASE/api/v1/audit/verify?tenant=default" | jq '.status, .valid'`.
4. Capture `time` of 3 successive verify calls; record p99.

**Actual (worker-e2a9, 2026-04-26T17:25Z):** PARTIAL PASS.
- Chain integrity: `status=ok, total_events=10000, verified_events=10000, gaps=[], retention_window_hours=168, first_seq=1, last_seq=10000`. Hash chain is intact across 10000 events.
- Baseline serial latency (5 calls): 199, 206, 215, 222, 241 ms — p99 ≈ 241ms.
- 20 concurrent calls: p50=1660ms, p95=2325ms, p99=2354ms (overall wall-clock 2690ms for 20 parallel).
- DoD #3 budget says "≤ p99 10ms regression" under chat load. The verify operation re-hashes 10K events on each call — absolute latency is 199-2354ms range; the 10ms regression budget is unmeasurable here without (a) chat-induced load (blocked by F8 — cannot drive chat-MCP calls and count audit emission), AND (b) a proper baseline-vs-load comparison harness. The verify endpoint's poor concurrency scaling (10× from baseline at 20 parallel) is itself a finding worth filing — verify is not designed for hot-path / load-test usage.
- Chat-driven load probe component (probe 3 backbone) BLOCKED by F8.

**Verdict:** PARTIAL PASS (chain integrity verified ok); regression-budget portion BLOCKED by F8.
**Evidence:** Live curl outputs in step-5 worker note. Verify endpoint's concurrency scaling characterized.
**P0/P1 task filed:** F8 (P0); also recommend P1 follow-up "audit-verify endpoint concurrency scaling — investigate whether re-hashing 10K events per call is the intended hot-path design".

---

## Probe 5 — Safety kernel actually gates
**Expected:** A chat-driven mutating call that fails the default policy (e.g. `cordum_update_policy_bundle` with a malformed bundle) returns canonical `-32099 approval_required` OR `-32000 policy_denied`; audit event has `decision=SafetyDeny` + the exact rule_id.

**Procedure:**
1. Craft chat message that maps to `cordum_update_policy_bundle` with bundle missing required patterns.
2. Capture MCP error code + audit row.

**Actual:** _tbd_
**Verdict:** _tbd_
**Evidence:** _tbd_
**P0/P1 task filed:** _tbd_

---

## Probe 6 — Approval gate wire-through
**Expected:** Chat user says "approve job-abc"; `cordum_approve_job` fires; WS emits `approval_required` frame; programmatic accept; original call retries; `approval_granted` audit event chained to the original via trace_id.

**Procedure (corrected per F3):**
1. Pre-create pending approval ($200 mock-bank transfer).
2. Chat user requests approval; capture WS frame.
3. POST `/api/v1/approvals/{id}/approve` as admin.
4. Verify retry of original + audit chain via trace_id.

**Actual:** _tbd_
**Verdict:** _tbd_
**Evidence:** _tbd_
**P0/P1 task filed:** _tbd_

---

## Probe 7 — AgentIdentity AllowedTools enforcement (scope-first deny)
**Expected:** Narrow chat-assistant's AllowedTools to read-only; chat "submit a $40 transfer" returns scope-filter error BEFORE policy bundle; audit reason=`agent_identity_scope_deny`.

**Procedure (corrected per F1):**
1. Snapshot current AgentIdentity (`GET /api/v1/agents/<id>`).
2. Use the CAP SDK control-plane endpoint to set scope to read-only (no `cordum_submit_job`). See `core/llmchat/bootstrap.go:295-300` for the exact call shape.
3. Chat user requests submit; capture MCP error + audit row.
4. Restore scope.

**Actual (worker-e2a9, 2026-04-26T17:20Z):** BLOCKED by F1 (no `cordumctl agent set-scope` CLI command) AND F8 (cannot confirm audit reason via /audit/query). The static code path is correct (scope-first ordering verified at `core/mcp/registry.go:274,303`), but the runtime evidence cannot be captured without (a) a CLI / API workaround for narrowing scope, AND (b) the audit query endpoint to read back the deny reason.
**Verdict:** BLOCKED
**Evidence:** Order confirmed by code: scope filter at `core/mcp/registry.go:274` runs before approval gate at `core/mcp/registry.go:303`.
**P0/P1 task filed:** F1 + F8 → step-8 P0/P1.

---

## Probe 8 — Data classification scope deny
**Expected:** chat-assistant tagged `[public, internal]`; a query touching `pii` (e.g. "list all users with emails") is scope-denied; audit records the attempt with classification=`pii`.

**Procedure:**
1. Confirm chat-assistant DataClassifications via `GET /api/v1/agents/<id>`.
2. Drive chat query that maps to a pii-classified tool.
3. Capture MCP error + audit row classification field.

**Actual:** _tbd_
**Verdict:** _tbd_
**Evidence:** _tbd_
**P0/P1 task filed:** _tbd_

---

## Probe 9 — Session-scoped delegation revocation
**Expected:** Close a chat session → JTI revoked in Redis → reusing the captured token on `/api/v1/jobs` (or another delegation-protected endpoint) returns 401 with `reason=delegation_revoked`.

**Procedure:**
1. Open chat session; capture JTI from session metadata (Redis `chat:session:{id}` HGET, or admin tool).
2. Close session via WS close frame.
3. Verify Redis `delegation:revoked:{jti}` key exists (`core/auth/delegation/revocation.go:14, 84`).
4. Reuse token on a write endpoint; expect 401 + `reason=delegation_revoked`.
5. **Adversarial extension (per step-10 self-review):** restart Redis after step 3, repeat step 4 — confirm revocation persists.

**Actual:** _tbd_
**Verdict:** _tbd_
**Evidence:** _tbd_
**P0/P1 task filed:** _tbd_

---

## Probe 10 — Cross-tenant isolation
**Expected:** Open chat sessions as tenant-A user and tenant-B user; tenant-B "list jobs" does NOT return tenant-A's jobs; audit shows tenant-scoped filter.

**Procedure:**
1. `cordumctl tenant create A` and `B` (or use existing test tenants).
2. Open two chat sessions, one per tenant.
3. Tenant-A submits job-A; tenant-B asks "list jobs".
4. Assert tenant-B response does NOT contain job-A.
5. **Adversarial extension:** repeat with concurrent submit + list to test race.

**Actual:** _tbd_
**Verdict:** _tbd_
**Evidence:** _tbd_
**P0/P1 task filed:** _tbd_

---

## Probe 11 — Audit chain survives scheduler restart
**Expected:** Long-running chat session; mid-session `docker compose restart cordum-scheduler`; in-flight tool calls either complete OR get audit-logged with `reason=aborted`; chain integrity preserved.

**Procedure:**
1. Start mockvllm script with 5-second sleeps between turns.
2. After 30s, `docker compose restart cordum-scheduler`.
3. Wait for completion; verify all tool calls accounted for in audit (completed + aborted = total).
4. Run `/api/v1/audit/verify` → expect status=ok.

**Actual (worker-e2a9, 2026-04-26T17:25Z):** DEFERRED.
- The "completed vs aborted = total" accounting requires per-call audit visibility, which is blocked by F8 (cannot list `mcp.tool_invocation` events by type/since).
- Restarting `cordum-scheduler-1` in the SHARED dev stack would disrupt other workers concurrently working on this stack (multiple parallel agents per the chat log). Cannot run the destructive part of this probe without a dedicated stack.
- Static prerequisite: chain integrity is verified intact (10000/10000 verified, no gaps) — see Probe 4 evidence.

**Verdict:** DEFERRED on F8 + shared-stack risk.
**Evidence:** N/A — would re-use Probe 4 verify output post-restart.
**P0/P1 task filed:** F8 (P0); also recommend dedicated test stack for destructive probes.

---

## Probe 12 — Policy bundle default enforced
**Expected:** `config/llmchat/policy-default.yaml` is loaded AND ENFORCED.

**Procedure (corrected per F7):**
1. Confirm file present at `config/llmchat/policy-default.yaml`.
2. Confirm chat-assistant AgentIdentity reflects the file's allow_tools (already verified in step-1: 20 allowed_tools match).
3. Drive a chat call to a tool NOT in `allow_tools` → expect scope-deny.
4. **Note:** the file is NOT auto-loaded into `/api/v1/policy/bundles`. Either (a) deploy step posts the file via `cordumctl policy bundle import`, OR (b) we depend on AgentIdentity scope to enforce. Both paths must be documented.

**Actual:** _tbd_
**Verdict:** _tbd_
**Evidence:** chat-assistant AgentIdentity has 20 allowed_tools matching the file (verified step-1).
**P0/P1 task filed:** _tbd_

---

## Probe 13 — Zero-trust verification (defense in depth)
**Expected:** Even if AgentIdentity scoping is misconfigured, safety kernel is the last line and denies unauthorized mutations.

**Procedure (corrected per F1):**
1. Use CAP SDK to set chat-assistant AllowedTools to wildcard (or remove constraints).
2. Drive an unauthorized mutation (e.g. `cordum_uninstall_pack`).
3. Confirm SAFETY KERNEL still denies via policy bundle (audit row decision=SafetyDeny).
4. Restore AgentIdentity scope.

**Actual:** _tbd_
**Verdict:** _tbd_
**Evidence:** _tbd_
**P0/P1 task filed:** _tbd_

---

## Adversarial self-review checklist (step-10)

Each item below must be addressed before final commit.

- [ ] (1) Probe 7: scope-narrowing applied atomically (no TOCTOU window where chat could submit before scope narrowed)?
- [ ] (2) Probe 9: revocation persists across Redis restart (durability test)?
- [ ] (3) Probe 10: isolation holds under concurrent submit + list race?
- [ ] (4) Probe 4: audit verify ran AFTER buffer flush (5s)?
- [ ] (5) Probe 13: confirmed safety kernel denies (vs. scope filter denying first)?
- [ ] (6) Probe 11: in-flight delegation tokens preserved across scheduler restart, or invalidated (failure mode)?
- [ ] (7) Residual state cleaned (test agents, test tenants, test sessions)?
- [ ] (8) No real PII or API keys leaked into evidence files?
- [ ] (9) Any temporary scope override fully reverted?

## Cross-links

- [ ] cordum-site/docs-site governance concept page → link to this review
- [ ] cap/docs agent-registration page → link to chat-assistant phase-3 evidence
