# LLM Chat Assistant — Informational-Only Governance Review

> **SCOPE REDUCED 2026-04-28** — task-01aaa6bd retired the original chat→MCP tool-calling design under Yaron's directive captured in `project_llm_chat_informational_only` / Moe memory `mem-e6484160`: the LLM chat assistant is now an informational Q&A helper grounded in Cordum API docs + cordum.io content. It does **not** call MCP tools, submit jobs, approve/reject work, trigger workflows, or mutate state.

This page replaces the historical 13-probe tool-calling governance review from `task-931eaea2`. Retired probes are removed rather than left as blocked work because their premise no longer exists. The surviving probes verify the governance surfaces that still matter for informational chat: identity bootstrap, entitlement/auth, dashboard provenance, session isolation, audit-chain continuity, and operational recovery.

## Status summary

| Category | Surviving probes | Current status | Notes |
| --- | --- | --- | --- |
| Identity / entitlement | 1, 2, 7 | STATIC PASS / LIVE RE-RUN RECOMMENDED | Chat-assistant registers as a low-risk identity with empty tool scope; dashboard still shows the chat affordance only when licensed and healthy. |
| Audit / chain integrity | 4, 11 | STATIC PASS / LIVE RE-RUN RECOMMENDED | Chat emits `chat.session_started` / `chat.session_closed`; no tool-invocation audit path remains for chat. |
| Session / tenancy | 9 | STATIC PASS / LIVE RE-RUN RECOMMENDED | Session ownership remains principal+tenant-bound; per-session delegation tokens are gone. |

Totals after scope reduction: **6 surviving probes / 7 retired probes / 0 open P0 from retired tool-calling paths**.

## Retired probes

The following historical probes were deleted from `scripts/governance-probes/` because they depended on chat invoking MCP tools or approval-gated mutations:

| Retired probe | Historical title | Why removed |
| --- | --- | --- |
| 3 | Every MCP call produces an `mcp.tool_invocation` SIEMEvent | Chat no longer calls MCP tools. Other MCP clients still own tool-invocation audit coverage. |
| 5 | Safety kernel actually gates | Chat does not submit jobs or invoke tools, so safety-kernel enforcement is not on the chat path. |
| 6 | Approval gate wire-through | `approval_required` frames and inline approvals were removed from chat. Approval gates remain for existing dashboard/CLI/MCP mutation paths. |
| 8 | Data classification scope deny | Chat has no tool/data scope to exercise; retrieved knowledge redaction is covered by knowledge-pack work. |
| 10 | Cross-tenant isolation through tool calls | Chat no longer lists/submits tenant data via tools; session ownership isolation remains in probe 9. |
| 12 | Policy bundle default enforced | The chat-assistant tool policy bundle is obsolete with `AllowedTools=[]`. |
| 13 | Zero-trust safety defense-in-depth through wildcard scope | Tool scope is empty by construction; wildcard-scope adversarial setup is no longer a supported chat state. |

## Probe template

Each surviving probe keeps the historical evidence format:

```
## Probe N — <title>
**Expected:** <criteria>
**Procedure:** <steps>
**Actual:** <evidence or latest static check>
**Verdict:** [PASS|PARTIAL|BLOCKED|RETIRED]
**Evidence:** <commands, logs, screenshots, task IDs>
**P0/P1 task filed:** <task ID if FAIL>
```

---

## Probe 1 — Chat-assistant identity bootstrap is idempotent and no-tool scoped

**Expected:** `cordum-llm-chat` registers or reuses a `chat-assistant` identity through the CAP SDK control-plane path. The identity is low risk, tenant-scoped, and has `AllowedTools=[]` plus `PreapprovedMutatingTools=[]`.

**Procedure:**
1. Start or restart `cordum-llm-chat`.
2. Query `/api/v1/agents?name=chat-assistant` or the equivalent CAP SDK wrapper.
3. Assert exactly one identity exists for the tenant.
4. Assert `risk_tier=low`, `allowed_tools=[]`, `preapproved_mutating_tools=[]`.
5. Restart again and assert no duplicate registration.

**Actual:** task-01aaa6bd production code changed `core/llmchat/bootstrap.go` so `expectedAllowedTools()` and `expectedPreapprovedMutatingTools()` both return nil/empty, registration uses `RiskTier: "low"`, and existing divergent tool-scoped identities fail closed through `verifyScope`.

**Verdict:** STATIC PASS; live restart re-run recommended in the next clean compose/Helm validation.
**Evidence:** `core/llmchat/bootstrap.go`, task-01aaa6bd step 2 grep/build evidence.
**P0/P1 task filed:** none.

---

## Probe 2 — Dashboard chat affordance is informational and health/entitlement gated

**Expected:** The dashboard chat button appears only when the `LLMChatAssistant` entitlement and chat health checks allow it. The widget does not render tool-call cards, approval prompts, or approval badges. The persistent disclaimer says chat is informational-only and directs state changes to dashboard/CLI paths.

**Procedure:**
1. Run dashboard tests for `ChatHeaderButton`, `ChatWidget`, `ChatStream`, and `useChatAssistantStore`.
2. Grep chat-assistant UI/state/types for retired frame names.
3. Manually verify the widget copy in the browser when a live dashboard is available.

**Actual:** task-01aaa6bd step 3 removed `ApprovalInlinePrompt`, `ToolCallCard`, pending approval badge state, and retired frame parsing. The grep below returns no hits:

```bash
grep -RInE 'approval_required|ApprovalInline|pendingApprovalIds|toolCallId|toolCalls|ToolCallCard|tool_call|tool_result' \
  dashboard/src/components/chat-assistant dashboard/src/state/chatAssistant.ts dashboard/src/types/chatAssistant.ts \
  dashboard/src/hooks/useChatAssistantSession.ts dashboard/src/hooks/useChatAssistantSessions.ts --include='*.ts' --include='*.tsx'
```

**Verdict:** PASS by code/test evidence.
**Evidence:** task-01aaa6bd step 3: targeted vitest 28/28; full `npx vitest run` 1776/1776; tsc and build exit 0.
**P0/P1 task filed:** none.

---

## Probe 4 — Audit chain integrity across informational chat sessions

**Expected:** Chat session lifecycle events (`chat.session_started`, `chat.session_closed`) append to the audit chain without gaps. `/api/v1/audit/verify` remains `status=ok` after normal chat use.

**Procedure:**
1. Start a chat session, send at least one informational prompt, and close the WS.
2. Wait for async audit flushing.
3. Query audit events for session lifecycle actions.
4. Call `/api/v1/audit/verify` and assert `status=ok` / no gaps.

**Actual:** task-01aaa6bd preserves lifecycle auditing in `core/llmchat/chat_handlers.go` and removes only the obsolete `total_tool_calls` metadata. Historical chain verification from task-931eaea2 was `status=ok` on 10,000 events; live re-run should be done after this retirement PR deploys.

**Verdict:** STATIC PASS; live post-deploy re-run recommended.
**Evidence:** `core/llmchat/chat_handlers.go`; historical task-931eaea2 chain verify evidence; task-01aaa6bd step 2 build evidence.
**P0/P1 task filed:** none.

---

## Probe 7 — Empty AgentIdentity tool scope denies chat mutations by construction

**Expected:** The chat-assistant identity cannot invoke any Cordum MCP tool because its registered scope has no allowed tools. Attempts to drive mutations through chat should result in explanatory assistant text, not a tool invocation or approval frame.

**Procedure:**
1. Verify bootstrap scope is empty (`AllowedTools=[]`, `PreapprovedMutatingTools=[]`).
2. Ask chat to submit, approve, reject, cancel, or trigger work.
3. Assert the UI receives only assistant text/error/final frames and no `tool_call` / `approval_required` frame.
4. Query audit for `mcp.tool_invocation` with chat-assistant agent id during the test window; expect zero.

**Actual:** Production chat no longer constructs an MCP client, no longer sends OpenAI tools, and the dashboard parser ignores retired frames. Static greps in task-01aaa6bd step 2 returned zero non-test hits for `mcp\.Client|tools/call|tool_call_id` and zero non-test `approval_required|ApprovalRequired` hits in `core/llmchat` + `cmd/cordum-llm-chat`.

**Verdict:** STATIC PASS; live adversarial prompt re-run recommended.
**Evidence:** task-01aaa6bd steps 2 and 3.
**P0/P1 task filed:** none.

---

## Probe 9 — Session ownership and expiry remain tenant/principal scoped

**Expected:** A persisted session ID can be resumed only by the same principal+tenant. Forged or cross-tenant session IDs are rejected. Idle sessions expire gracefully after the configured TTL. No per-session delegation token is minted, stored, or required.

**Procedure:**
1. Create a session as tenant A / principal A.
2. Try to resume it as tenant B / principal B; expect not-found/forbidden semantics.
3. Expire the Redis `chat:session:{id}` key and attempt resume; expect graceful session-expired/not-found handling.
4. Inspect Redis session metadata; no delegation token/JTI fields should be present.

**Actual:** task-01aaa6bd removed delegation fields from `Session`, Redis metadata fields, `SetDelegation`, and handler-side delegation issuance. `sessionVisibleToUser` still enforces principal+tenant matching before resume.

**Verdict:** STATIC PASS; live Redis TTL re-run recommended.
**Evidence:** `core/llmchat/session.go`, `core/llmchat/chat_handlers.go`; task-01aaa6bd step 2 grep evidence for no non-test delegation hits.
**P0/P1 task filed:** none.

---

## Probe 11 — Audit/session continuity across scheduler or worker restarts

**Expected:** Because informational chat does not route work through scheduler/worker MCP mutation paths, scheduler or worker restarts must not corrupt chat sessions or audit-chain continuity. In-flight chat streams should either complete or return a structured provider/session error; audit chain verification remains green.

**Procedure:**
1. Start an informational chat session and keep the WS open.
2. Restart scheduler or a worker in an isolated stack.
3. Continue the chat session and close it.
4. Verify session lifecycle audit events and `/api/v1/audit/verify`.

**Actual:** Static code shows no scheduler/worker dependency in the chat turn path; chat depends on Redis, the local inference provider, gateway trusted-forwarder auth, and audit chainer. Live destructive restart remains a clean-stack validation item.

**Verdict:** STATIC PASS; destructive live restart deferred to clean stack.
**Evidence:** `cmd/cordum-llm-chat/main.go`, `core/llmchat/agent.go`, `core/llmchat/chat_handlers.go`.
**P0/P1 task filed:** none.

## Cross-links

- Moe memory `mem-e6484160` / `project_llm_chat_informational_only` — authoritative 2026-04-28 scope-reduction directive.
- task-01aaa6bd — implementation task that removed chat tool-calling code paths.
- task-a72bdedf — knowledge-pack ingestion, the primary remaining deliverable for informational Q&A grounding.
- Historical governance task: task-931eaea2. Use it only for audit-chain background; its tool-calling probes are superseded by this document.
