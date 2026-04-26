# LLM Chat Assistant — UX & Accessibility Review

This document captures the senior-engineer UX review of the Gmail-style
chat assistant widget that ships in Cordum 0.1+ (epic `epic-ac495830`).
It pairs verifiable code-review evidence with reproducer steps for the
probes that require a live GPU stack or a human-driven browser session.

## Summary

| Verdict | Meaning |
|---|---|
| **PASS** | Verified at code-review or vitest+axe-core level; behaviour is in the merged code. |
| **PARTIAL** | Static surface verified; live behaviour requires a GPU+browser session. Reproducer included. |
| **DEFERRED-TO-LIVE-STACK** | Cannot evidence without `qwen-inference` healthy + dashboard image rebuilt. Tracked under task-6a8680fc + task-1e6d21fc + the dashboard lockfile sync. |
| **DEFERRED-TO-HUMAN** | Requires a real browser (Playwright not yet wired) or assistive technology (VoiceOver/NVDA). Tracked under task-a5d09fad. |
| **DEFERRED-PER-DECISION** | Project-level decision documented in [task-530874ea](https://) — not a fail. |
| **FAIL — P1 filed** | Real gap; a Moe task captures the fix. |

| Probe | Verdict |
|---|---|
| 1. Persistence across routes | **PASS** (single mount in AppShell, sessionStorage-backed store) |
| 2. Header button availability tracks vLLM | **PASS** (10 s polling, hides on probe failure or unentitled) |
| 3a. Tool-call args deterministic | **PARTIAL** (sampling-mode dispatch verified; live 10× run DEFERRED-TO-LIVE-STACK) |
| 3b. NL summary varies | **DEFERRED-TO-LIVE-STACK** |
| 4. Two-pass sampling env override | **PASS** (env loader covered by unit tests) |
| 5. Inline approval flow | **PASS** code-review; click-to-approve flicker DEFERRED-TO-HUMAN |
| 6. Session resume across tab close | **FAIL — task-7ff6765f** (sessionStorage clears on tab close; plan claimed localStorage) |
| 7. Optimistic send | **PARTIAL** (`applyFrame` is synchronous; visual flicker DEFERRED-TO-HUMAN) |
| 8a. Streaming renders progressively | **PASS** code-review; visual cadence DEFERRED-TO-HUMAN |
| 8b. Cold-vs-warm prefix-caching latency | **DEFERRED-TO-LIVE-STACK** |
| 9. Tool call cards | **PASS** code-review; collapse-expand UX DEFERRED-TO-HUMAN |
| 10. Axe-core zero violations | **PASS** (jsdom; full real-browser axe DEFERRED-TO-HUMAN) |
| 11. Mobile responsive (375 px full-screen) | **FAIL — task-f2507515** (no media-query fullscreen at 375 px) |
| 12. Dark / light theme parity | **PASS** code-review (CSS variables only); pixel contrast DEFERRED-TO-HUMAN |
| 13. Empty-state suggestion chips | **FAIL — task-47de92ef** (welcome hint present, 3 chips not implemented) |
| 14. Error states | **PARTIAL** (error bubble + license-gate hide PASS; vLLM-timeout retry DEFERRED-TO-LIVE-STACK) |
| 15. Admin chat-sessions page | **PASS** route exists; full UX DEFERRED-TO-HUMAN |
| 16. i18n catalog | **DEFERRED-PER-DECISION** ([task-530874ea](#)) |

Probes 1–9, 14, 15 require a healthy `qwen-inference` + rebuilt dashboard image
to be verified end-to-end. Until [task-6a8680fc](#) (vLLM `--disable-log-requests`
crash-loop), [task-1e6d21fc](#) (`/metrics` 404), and the
dashboard lockfile sync land, the live procedures below are documented but
not executed in this review pass.

---

## Probe 1 — Persistence across routes

**Expected**: open widget on `/jobs`, send a message, navigate to `/workflows`,
`/policies`, `/settings/chat-sessions`; widget stays open, session preserved,
no remount flash.

**Code-review evidence**:
- `dashboard/src/components/layout/AppShell.tsx:223` mounts `<ChatWidget />`
  once at the top level, OUTSIDE the `<Routes>` / `<Route>` tree. Route
  changes do not unmount the widget.
- `dashboard/src/state/chatAssistant.ts:216-224` persists `panelOpen` and
  `sessionId` via Zustand `persist` middleware to `sessionStorage`. Store
  reference identity is stable across renders, so React reconciliation does
  not remount the widget DOM tree on a route change.
- `dashboard/src/state/chatAssistant.ts:217` storage key
  `cordum-chat-assistant`.

**Verdict**: **PASS** at architecture / code-review level. Visual confirmation
of "no remount flash" requires a real browser (DEFERRED-TO-HUMAN).

**Reproducer (live)**:
```bash
# from a healthy stack:
docker compose --profile llmchat up -d --build
# open http://127.0.0.1:8080 in a browser, sign in
# click ChatHeaderButton, send "hi"
# navigate /jobs -> /workflows -> /policies -> /settings/chat-sessions
# assert: widget DOM stays mounted, sessionStorage["cordum-chat-assistant"] unchanged
```

---

## Probe 2 — Header button auto-hide tracks vLLM

**Expected**: button absent when vLLM is down; appears within 10 s of
`/api/v1/chat/healthz` returning 200; disappears within 10 s when vLLM stops.

**Code-review evidence**:
- `dashboard/src/hooks/useChatAssistantAvailability.ts:7` `POLL_INTERVAL_MS = 10_000`.
- `dashboard/src/components/chat-assistant/ChatHeaderButton.tsx:14-15` renders
  `null` when feature flag off OR `availability.available === false`.
- `dashboard/src/components/chat-assistant/ChatHeaderButton.test.tsx` asserts
  the four arms of the truth-table (flag off, unentitled license,
  entitled+healthz=200, entitled+healthz=503).

**Verdict**: **PASS**. Hide/show logic is correct; the 10 s cadence matches
the epic rail.

---

## Probe 3 — Two-pass sampling

**Expected (3a)**: tool-call dispatch uses `temperature=0.3 top_p=0.9` and
yields byte-identical args across 10 fresh-session runs of `list my jobs`.
**Expected (3b)**: natural-language summarisation uses `temperature=0.7
top_p=0.8` and produces visibly varied prose across the same 10 runs.

**Code-review evidence**:
- `core/llmchat/provider_openai.go:148-152` selects per-mode sampling:
  ```go
  temp, topP := p.summaryTemperature, p.summaryTopP
  if mode == SamplingModeToolCalls {
      temp, topP = p.toolTemperature, p.toolTopP
  }
  ```
- `cmd/cordum-llm-chat/main.go:43-46` wires defaults
  `0.3 / 0.9 / 0.7 / 0.8`.
- `cmd/cordum-llm-chat/main.go:363-375` loads
  `LLMCHAT_TOOL_TEMPERATURE`, `LLMCHAT_TOOL_TOP_P`,
  `LLMCHAT_SUMMARY_TEMPERATURE`, `LLMCHAT_SUMMARY_TOP_P` via
  `envFloatOrDefault`.
- `cmd/cordum-llm-chat/main_test.go:93-96` exercises override values; lines
  199-202 assert non-float values fail closed.

**Verdict (3a)**: **PARTIAL** — code dispatches the right sampling mode,
but the byte-identical-across-10-runs assertion needs a live vLLM and
fresh sessions to defeat prefix-caching contamination.
**Verdict (3b)**: **DEFERRED-TO-LIVE-STACK** for the same reason.

**Reproducer (live)**:
```bash
# capture 10 fresh sessions
for i in $(seq 1 10); do
  curl -s --resolve cordum-llm-chat:8090:127.0.0.1 \
    -H "X-API-Key: $CORDUM_API_KEY" \
    -X POST https://cordum-llm-chat:8090/api/v1/chat \
    -d '{"message":"list my jobs"}' \
    -H "X-Reset-Session: 1" > "run-$i.json"
done
# tool-call args (probe 3a) — expect identical >=9/10
jq -S '.frames[] | select(.type=="tool_call") | .tool_call.arguments' run-*.json | sort -u | wc -l
# summary text (probe 3b) — expect levenshtein-distinct >=7/10 pairs
jq -r '.frames[] | select(.type=="assistant_delta") | .delta' run-*.json
```

---

## Probe 4 — Two-pass-sampling env override

**Expected**: setting `LLMCHAT_SUMMARY_TEMPERATURE=0.9` at deploy time
takes effect at request time without code change.

**Code-review evidence**: as in Probe 3 — `envFloatOrDefault` reads the four
env vars; `main_test.go:93-110` asserts `cfg.Provider.SummaryTemperature ==
0.72` when env is set to `0.72`. Audit-time confirmation that the override
flows into provider sampling needs the live container.

**Verdict**: **PASS** at code/test level. Container-time confirmation is
gated by [task-6a8680fc](#) (qwen-inference up).

**Reproducer (live)**:
```bash
docker compose exec cordum-llm-chat env | grep '^LLMCHAT_'
# override one knob and re-run probe 3b
docker compose run --rm \
  -e LLMCHAT_SUMMARY_TEMPERATURE=0.9 \
  cordum-llm-chat /usr/local/bin/cordum-llm-chat &
```

---

## Probe 5 — Inline approval flow

**Expected**: `cordum_approve_job` triggers an inline prompt with Approve +
Reject + tool name + args; clicking Approve resumes the original tool call
without leaving chat.

**Code-review evidence**:
- `dashboard/src/components/chat-assistant/ApprovalInlinePrompt.tsx`:
  - Renders only when `approval.status === "pending"` (line 30).
  - Names the defense layer ("Cordum approval gate paused this call",
    line 60) — explicit overreliance affordance per OWASP LLM09.
  - Surfaces the JSON args in a `<pre aria-label="tool call arguments">`
    block (lines 68-73) — the operator sees the literal arguments, not a
    paraphrase.
  - "Verify and approve" button uses
    `aria-label="Approve {tool} — verify arguments first"` (line 84) so
    screen readers cannot mis-announce the destructive action.
  - Approve POSTs to `/approvals/{id}/approve` with `reason: "approved via
    chat assistant"` (line 39); the server WS frame flips approval status
    via `applyFrame` in the store.
- `dashboard/src/state/chatAssistant.ts:156-187` handles `approval_required`:
  appends approval into `pendingApprovalIds`, sets the toolCall.approval
  status to `pending`.
- `ApprovalInlinePrompt.test.tsx` covers defense-layer copy, args
  visibility, the verify-and-approve label, and audit-chain caption.

**Verdict**: **PASS**. The retry-after-approval path requires a live
WS round-trip to confirm visually (DEFERRED-TO-HUMAN).

---

## Probe 6 — Session resume across browser close

**Expected** (per plan): close browser, reopen, click header button →
"full conversation restored from `chat:session:{id}` (Redis) via localStorage
session_id pointer".

**Actual (code-review)**:
- `dashboard/src/state/chatAssistant.ts:218`
  `storage: createJSONStorage(() => sessionStorage)` — uses
  `sessionStorage`, **NOT** `localStorage`.
- `sessionStorage` is **per-tab** — it does NOT survive tab close. Closing
  the browser, or even the tab, clears the session pointer.

**Verdict**: **FAIL — filed as task-7ff6765f (P1)**. The plan's claim is
incorrect: closing the browser does not preserve the session pointer with
the current implementation. Two valid resolutions exist:

1. Switch to `localStorage` (matches the plan), accepting that the chat
   pointer survives across sign-outs — needs an explicit clear on the
   sign-out path.
2. Update the plan / docs to describe the actual sessionStorage-tab-scoped
   behaviour and remove the "close browser" flow from the DoD.

A Moe follow-up task captures the decision.

---

## Probe 7 — Optimistic send

**Expected**: user bubble appears < 50 ms after Enter, before the WS round-trip;
server echo replaces the optimistic bubble seamlessly.

**Code-review evidence**:
- `dashboard/src/hooks/useChatAssistantSession.ts:328-330`
  ```ts
  ws.send(JSON.stringify(frame));
  useChatAssistantStore.getState().applyFrame(frame as ChatFrame);
  ```
  `applyFrame` runs synchronously after `ws.send` — the user bubble is
  rendered before any network round-trip resolves.
- `state/chatAssistant.ts:110-119` deduplicates user frames by `id`, so the
  server echo (same uuid) is a no-op rather than a duplicate.

**Verdict**: **PARTIAL** — synchronous `applyFrame` proves the user bubble
appears optimistically; the "no flicker" assertion needs a real-browser
visual check.

---

## Probe 8 — Streaming + prefix-caching

**Expected (8a)**: `assistant_delta` frames render progressively (not in a
single lump after the final frame); cancel button aborts mid-stream.
**Expected (8b)**: warm prefill < 50 % of cold (vLLM prefix-caching).

**Code-review evidence**:
- `core/llmchat/provider_openai.go:284-328` `stream()` reads the SSE body
  frame-by-frame and emits each `Chunk` to a channel; the WS handler
  forwards each as an `assistant_delta` frame.
- `state/chatAssistant.ts:121-129` appends the delta to the running
  assistant message text (`current.text + frame.delta`), so the React
  re-render happens per frame, not at end.
- `useChatAssistantSession.ts:307` close handler uses code 1000 for unmount
  / cancel — provides the abort path.

**Verdict (8a)**: **PASS** at code-review. Visual cadence DEFERRED-TO-HUMAN.
**Verdict (8b)**: **DEFERRED-TO-LIVE-STACK**. The cold-vs-warm latency
table demands a real GPU-backed vLLM with prefix-caching enabled.

---

## Probe 9 — Tool call cards

**Expected**: each tool call is a collapsible card with name, args preview,
collapsed result JSON.

**Code-review evidence**:
- `dashboard/src/components/chat-assistant/ToolCallCard.tsx` renders the
  card; `ChatStream.tsx:74-77` wires it under each assistant message,
  followed by `<ApprovalInlinePrompt />` if applicable.
- `state/chatAssistant.ts:144-154` patches `tool_result` into the matching
  toolCall and flips `approval.status` to `resolved` if it was pending.

**Verdict**: **PASS** code-review. Collapse-expand interaction
DEFERRED-TO-HUMAN.

---

## Probe 10 — Accessibility (axe-core)

**Expected**: zero axe-core violations on widget closed, open empty, open
with messages, open with approval prompt.

**Evidence**: `dashboard/src/components/chat-assistant/ChatWidget.a11y.test.tsx`
runs `axe.run` (via `assertNoSeriousAxeViolations`) on five states:

1. Header button only (panel closed).
2. Panel open, empty transcript.
3. Panel open with user + assistant + tool_call + tool_result.
4. ApprovalInlinePrompt rendered.
5. Panel open in dark mode.

All five pass with zero critical+serious violations. Tag scope is
`wcag2a` + `wcag2aa`. Note: jsdom does not composite `backdrop-filter`,
so `color-contrast` false-negatives on glass surfaces are tolerated by
the helper — see `test-utils/a11y.ts`.

**Verdict**: **PASS** at jsdom/axe-core level.

**DEFERRED-TO-HUMAN** for full real-browser axe (Chrome/Firefox/Safari),
keyboard-only Tab/Enter/Esc/Cmd-K walkthrough, and VoiceOver + NVDA
announcement-order verification.

---

## Probe 11 — Mobile responsive

**Expected**: 375 px viewport collapses to full-screen overlay; iPad
~768 px stays in panel mode but narrower.

**Actual (code-review)**:
- `ChatWidget.tsx:62-67` className:
  ```
  fixed right-4 z-50 flex flex-col
  top-16 bottom-4 w-[380px] max-w-[calc(100vw-2rem)]
  rounded-2xl border border-cordum/30 bg-surface-0 shadow-soft
  overflow-hidden
  ```
- At 375 px, `max-w-[calc(100vw-2rem)]` = 343 px — the widget is *capped*
  to viewport width minus 2 rem of right-edge gutter, but it is not a
  full-screen overlay. There is no `@media` switch and no responsive
  token.

**Verdict**: **FAIL — filed as task-f2507515 (P1)**. The current mobile
experience is usable (widget shrinks to fit 375 px) but does not match
the spec's "full-screen overlay" requirement. The fix is a small Tailwind
`sm:` / `md:` breakpoint set.

---

## Probe 12 — Dark / light theme

**Expected**: widget respects the dashboard light/dark toggle; WCAG AA
contrast everywhere; no hard-coded hex.

**Code-review evidence**:
- `ChatWidget.tsx`, `ChatHeaderButton.tsx`, `ApprovalInlinePrompt.tsx`,
  `ChatStream.tsx` all use Tailwind tokens that resolve to CSS variables:
  `bg-surface-0`, `bg-surface-1`, `text-foreground`, `text-muted-foreground`,
  `border-cordum/30`, `bg-status-warning/10`, `text-status-error`. No
  literal hex codes appear.
- `ChatWidget.a11y.test.tsx` exercises both light and dark with axe; the
  jsdom result is no critical violations in either mode.

**Verdict**: **PASS** at code-review. Pixel-level contrast measurement on
real-browser computed styles is DEFERRED-TO-HUMAN — jsdom does not
composite layered backgrounds, so axe's `color-contrast` rule cannot be
trusted on glass surfaces (per `test-utils/a11y.ts`).

---

## Probe 13 — Empty state

**Expected**: first-time user sees a welcome message + 3 suggestion chips
(`show denied jobs today`, `list my active workflows`, `what policies apply
to billing?`); clicking a chip sends that message.

**Actual (code-review)**:
- `ChatStream.tsx:29-39` renders only "Ask Cordum" + a single hint
  paragraph: *"Try 'list failing jobs' or 'submit a $40 mock-bank
  transfer.' Mutating actions still go through approvals."*
- No suggestion chips. No click-to-send affordance.

**Verdict**: **FAIL — filed as task-47de92ef (P1)**. The empty state is
functional but does not match the plan's clickable-chips spec.

---

## Probe 14 — Error states

**Expected**: structured error bubble + retry on llm-chat 500; "Chat
requires Enterprise license" on 402; "model took too long, retrying" on
vLLM timeout.

**Code-review evidence**:
- `ChatWidget.tsx:97-102` renders a `<div role=alert-style>` (status-error
  styling) with `<AlertCircle>` icon and `session.error` text whenever
  `useChatAssistantSession.error` is set.
- `useChatAssistantAvailability.ts:93-96` maps a 401/403 healthz response
  to `reason: "unauthorized"`; the widget hides on `!available` regardless
  of reason. License state is composed in
  `useChatAssistantAvailability.ts:60-63`.
- vLLM-timeout retry path is implemented via the WS reconnect loop
  (`useChatAssistantSession.ts:271-286`) with exponential backoff up to
  `MAX_BACKOFF_MS = 8000` and a 5-failure cap surfacing
  `status='closed'` + `error='unable to reach chat service'`.

**Verdict (a)**: **PASS** post-inline-fix — adversarial review caught a
real screen-reader gap: the original error bubble was a plain `<div>` with
no `role` or `aria-live`. Fixed inline in `ChatWidget.tsx` by adding
`role="alert"` + `aria-live="assertive"` and `aria-hidden="true"` on the
decorative icon, so AT users now hear connection errors as soon as they
appear. Visual retry UX DEFERRED-TO-HUMAN.
**Verdict (b)**: **PASS** — the unentitled / 402 path renders nothing
(per epic rail #5: "Users never see a broken chat UI"); the alternative
"Chat requires Enterprise license" treatment would contradict that rail.
Plan and rail diverge — the rail wins. Document accordingly.
**Verdict (c)**: **DEFERRED-TO-LIVE-STACK** for vLLM-timeout retry visual.

---

## Probe 15 — Admin chat-sessions page

**Expected**: `/settings/chat-sessions` admin page lists sessions,
paginates, search by user/tenant, click into a session for read-only
transcript; non-admin → 403 / redirect.

**Code-review evidence**:
- `dashboard/src/App.tsx:188`
  `<Route path="/settings/chat-sessions" element={<ChatAssistantSessionsPage />} />`.
- `dashboard/src/hooks/useChatAssistantSessions.ts` provides the data hooks:
  `GET /chat/sessions` (list, paginated) and `GET /chat/sessions/{id}`
  (detail).

**Verdict**: **PASS** route + data hook exist. Admin gating, pagination
behaviour at 100+ sessions, and read-only transcript UX
DEFERRED-TO-HUMAN.

---

## Probe 16 — Localisation readiness

**Expected**: all widget strings come from an i18n catalog; no English
literals in JSX.

**Project decision**: i18n is **DEFERRED-PER-DECISION** post-Visa, per
[task-530874ea](#) with follow-up [task-8c4cdcaf](#) in BACKLOG.

**Verdict**: **DEFERRED-PER-DECISION** — not a fail. The current widget
contains expected hard-coded strings ("Ask Cordum", advisory disclaimer,
empty-state hint, ApprovalInlinePrompt copy). When the i18n follow-up is
reactivated, this review's findings should be revisited.

---

## Cross-cutting blockers

| ID | Title | Why it gates this review |
|---|---|---|
| task-6a8680fc | vLLM `--disable-log-requests` crash-loop | Live probes 1, 2, 3, 4, 5, 7, 8, 14 cannot run without `qwen-inference` healthy. |
| task-1e6d21fc | llm-chat `/metrics` 404 | Observability prerequisite for ops QA — orthogonal to UX but listed in the LLM epic dependency chain. |
| dashboard lockfile sync | `@emnapi/core@1.10.0` / `@emnapi/runtime@1.10.0` missing in package-lock.json | Prevents the dashboard image rebuild via `docker compose --profile llmchat up -d --build`. |
| task-a5d09fad | GPU/k8s staging for production-readiness probes | The home for browser-only and screen-reader-only DoD items. |

The user directive *fix and unblock* drove this review pass to extract
every probe verdict obtainable from the merged code rather than wait for
the upstream stack. The remaining DEFERRED items are tracked accurately
above; they are not silent gaps.
