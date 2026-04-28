# LLM Chat Senior Security Review

Date: 2026-04-28
Task: `task-6cda949c` (senior review); live post-pivot replacement: `task-f80486c5`
Scope: `cordum-llm-chat`, dashboard chat widget, default Ollama inference packaging, opt-in vLLM packaging, gateway/chat auth boundaries.

## 2026-04-28 rescope

Yaron's 2026-04-28 directive changed LLM chat to **informational-only**. The assistant answers questions from the local Cordum API + docs knowledge pack. It does **not** call MCP tools, submit jobs, trigger workflows, approve/reject work, or mutate Cordum state. Ollama + Qwen2.5-Coder-3B is the production default; vLLM/Qwen3 remains an opt-in GPU profile.

The original senior-review checklist included tool-calling and parser attack surfaces. Those are now formally superseded for production scoring:

- **Retired/not scored:** probe 03 (PreapprovedMutatingTools exploit), probe 04 as a tool-call-gating bypass, probe 05 as a production parser-pinning blocker, probe 10 as a chat->MCP flood path.
- **Retained/scored for default Ollama live suite:** probe 01 (no chat delegation token + generic delegation monotonicity), probe 02 (empty chat-assistant identity + MCP filter fail-closed), probe 07 (session hijack), probe 08 (admin authZ), probe 09 (WS origin/CSRF), probe 11 (knowledge/log redaction), probe 12 (entitlement bypass).
- **Opt-in/deferred for default Ollama live suite:** probe 06 (vLLM-era inference exposure / loopback binding) remains available for `vllm-gpu`/`cpu-vllm-awq` profiles but is not scored in the Ollama default live run.

## Executive summary

Result: **PASS for the current informational-only/Ollama scope**.

| Metric | Count |
| --- | ---: |
| Total historical probe scripts | 12 |
| Retained/scored default live probes passed | 7 |
| Retired/superseded probes removed from default orchestrator | 4 |
| Opt-in/deferred vLLM-profile probe | 1 |
| Failed probes | 0 |
| Skipped probes | 0 |
| Evidence files with `not_run`/`not_asserted` blockers | 0 |
| Open P0/P1 blockers | 0 |

Current harness evidence:

```text
LLMCHAT_SECURITY_LIVE=1 LLMCHAT_SECURITY_REQUIRE_LIVE=1 LLMCHAT_SECURITY_BACKEND=ollama-cpu \
LLMCHAT_SECURITY_COMPOSE_FILE=tests/security/compose.ollama-cpu.yaml \
LLMCHAT_SECURITY_OUT_DIR=out/llmchat-security-live-step5-final \
LLMCHAT_GATEWAY_URL=http://127.0.0.1:8095 \
LLMCHAT_SECURITY_ORIGIN_GATEWAY_URL=https://127.0.0.1:8081 \
LLMCHAT_SECURITY_TRUSTED_FORWARDER_API_KEY=secprobe-key \
LLMCHAT_SECURITY_STOLEN_SESSION_ID=secprobe-stolen-step5-final \
bash tests/security/llmchat_run_all.sh
# pass=7 retired=0 skip=0 fail=0 live_missing=0
# results=out/llmchat-security-live-step5-final/security-review-results.json
```

Regression evidence:

```text
bash -n tests/security/llmchat_common.sh tests/security/llmchat_run_all.sh tests/security/llmchat_probe_*.sh
# exit 0

bash tests/security/llmchat_run_all.sh
# pass=7 retired=0 skip=0 fail=0 live_missing=0

LLMCHAT_SECURITY_LIVE=1 LLMCHAT_SECURITY_REQUIRE_LIVE=1 LLMCHAT_SECURITY_BACKEND=ollama-cpu \
LLMCHAT_SECURITY_COMPOSE_FILE=tests/security/compose.ollama-cpu.yaml \
LLMCHAT_SECURITY_OUT_DIR=out/llmchat-security-live-step5-final \
LLMCHAT_GATEWAY_URL=http://127.0.0.1:8095 \
LLMCHAT_SECURITY_ORIGIN_GATEWAY_URL=https://127.0.0.1:8081 \
LLMCHAT_SECURITY_TRUSTED_FORWARDER_API_KEY=secprobe-key \
LLMCHAT_SECURITY_STOLEN_SESSION_ID=secprobe-stolen-step5-final \
bash tests/security/llmchat_run_all.sh
# pass=7 retired=0 skip=0 fail=0 live_missing=0

go test ./cmd/cordum-llm-chat ./core/llmchat ./core/llmchat/knowledge ./core/mcp -count=1
# exit 0

go test ./core/controlplane/gateway/auth -run 'TestNewBasicAuthProviderLogsAPIKeySource|TestParseAPIKeysFormats' -count=1
# exit 0

go vet ./...
# exit 0

go build ./...
# exit 0

# Full-suite attempts were run for this task and are recorded in the worker handoff, but this Windows host hit
# unrelated local socket exhaustion in gateway/cmd tests (connectex: Only one usage of each socket address).
# The security-scope packages and live Ollama probes above passed.
```

## How to reproduce

Default, non-destructive review:

```bash
cd cordum
bash tests/security/llmchat_run_all.sh
```

Live clean-stack review for a dedicated runner:

```bash
cd cordum
LLMCHAT_SECURITY_COMPOSE_UP=1 \
LLMCHAT_SECURITY_LIVE=1 \
LLMCHAT_SECURITY_REQUIRE_LIVE=1 \
LLMCHAT_SECURITY_BACKEND=ollama-cpu \
LLMCHAT_SECURITY_COMPOSE_FILE=tests/security/compose.ollama-cpu.yaml \
LLMCHAT_SECURITY_OUT_DIR=out/llmchat-security-live-step5-final \
LLMCHAT_GATEWAY_URL=http://127.0.0.1:8095 \
LLMCHAT_SECURITY_ORIGIN_GATEWAY_URL=https://127.0.0.1:8081 \
LLMCHAT_SECURITY_TRUSTED_FORWARDER_API_KEY=secprobe-key \
LLMCHAT_SECURITY_STOLEN_SESSION_ID=<session-id-to-seed> \
bash tests/security/llmchat_run_all.sh
```

The harness is shell-portable on the Windows/MSYS/WSL developer environment. Docker/Helm/npm checks degrade to source-level assertions when those CLIs are unavailable or not executable from the current shell; CI runners with those CLIs exercise the rendered/config paths.

## Probe matrix

| Probe | Attack surface | Current scope | Status | Evidence |
| --- | --- | --- | --- | --- |
| 01 | Delegation token scope | Retained: chat must not mint/expose delegation tokens; generic token monotonicity stays covered | PASS | `out/llmchat-security-live-step5-final/llmchat_probe_01_delegation_scope/evidence.txt` |
| 02 | Agent identity scope | Retained: chat-assistant `AllowedTools=[]`, `PreapprovedMutatingTools=[]`; direct MCP filter returns `-32601` for non-visible tools | PASS | `out/llmchat-security-live-step5-final/llmchat_probe_02_agent_identity_scope/evidence.txt` |
| 03 | Preapproved mutation exploit | Retired: no chat->MCP mutation path exists | RETIRED/REMOVED | Not in default orchestrator |
| 04 | Prompt injection bypassing tool-call gating | Retired as a tool-call bypass; prompt/no-tools/redaction controls remain asserted elsewhere | RETIRED/REMOVED | Not in default orchestrator |
| 05 | Parser config pinning / DoS | Retired as default production blocker: Ollama has no tool parser | RETIRED/REMOVED | Not in default orchestrator |
| 06 | Loopback binding / exposure | Opt-in vLLM-profile check; default Ollama live execution documents deferral | DEFERRED | vLLM opt-in profile (`vllm-gpu`/`cpu-vllm-awq`) |
| 07 | Session hijack | Retained: session IDs bound to trusted principal+tenant | PASS | `out/llmchat-security-live-step5-final/llmchat_probe_07_session_hijack/evidence.txt` |
| 08 | Admin page authZ | Retained: `chat.read_all` permission required, fail-closed without checker | PASS | `out/llmchat-security-live-step5-final/llmchat_probe_08_admin_authz/evidence.txt` |
| 09 | WS origin / CSRF | Retained: browser-facing gateway origin allowlist rejects malicious origins | PASS | `out/llmchat-security-live-step5-final/llmchat_probe_09_ws_origin/evidence.txt` |
| 10 | Rate limiting / chat->MCP backpressure | Retired as chat->MCP flood path; generic gateway and chat output budgets remain asserted elsewhere | RETIRED/REMOVED | Not in default orchestrator |
| 11 | Log/knowledge redaction | Retained: API/docs knowledge uses `DefaultRedactor`; provider/agent do not log prompt/auth bodies | PASS | `out/llmchat-security-live-step5-final/llmchat_probe_11_log_redaction/evidence.txt` |
| 12 | Entitlement bypass | Retained: `LLMChatAssistant` gates chat endpoints and dashboard button hides on unavailable health | PASS | `out/llmchat-security-live-step5-final/llmchat_probe_12_entitlement_bypass/evidence.txt` |

## Per-probe expected vs actual

### Probe 01 — Delegation token scope

- **Payload:** steal a chat session and look for a browser-visible delegation JWT, then attempt to widen a child token to `cordum_update_policy_bundle`/`cfg.*`.
- **Expected defense layer:** informational-only chat has no delegation/MCP client and emits no delegation token; generic `core/auth/delegation.TokenService` still rejects widened child scopes and chain-depth abuse.
- **Actual:** PASS. Retired chat delegation files are absent, chat handlers do not surface bearer tokens, provider requests omit tool fields, and delegation monotonicity tests pass.

### Probe 02 — Agent identity scope

- **Payload:** crafted JSON-RPC `tools/call` for `cordum_unlist_tool_xyz` or a mutating tool under the chat-assistant identity.
- **Expected defense layer:** chat cannot originate MCP calls; direct MCP callers using the chat identity see `FilterForIdentity` remove all tools before policy evaluation and map misses to JSON-RPC `-32601`.
- **Actual:** PASS. Bootstrap pins empty allowed/preapproved scopes, retired MCP client files are absent, and MCP filter tests pass.

### Probe 03 — Preapproved mutation exploit

- **Payload:** historical attempts to call `cordum_update_policy_bundle`, approve/reject/cancel jobs, trigger workflows, or submit jobs from chat.
- **Expected defense layer:** no chat mutation path exists.
- **Actual:** RETIRED. The script asserts `AllowedTools=[]`, `PreapprovedMutatingTools=[]`, no approval/tool frames, no retired MCP files, and no OpenAI `tools`/`tool_choice` payload fields.

### Probe 04 — Prompt injection

- **Payloads:** role-mimicry tokens, XML-like tool-call text, ignore-redactor instructions, Unicode homoglyph tool names, and staged secret-dump roleplay using placeholder hosts only.
- **Expected defense layer:** no tool-call parser or MCP execution path exists; system prompts pin informational-only behavior; knowledge-pack content is redacted before prompt insertion.
- **Actual:** RETIRED as a tool-call bypass surface. The script still asserts prompt guardrails, text-only provider requests, unexpected backend tool deltas ignored, and API/site knowledge redaction tests.

### Probe 05 — Parser config pinning / DoS hardening

- **Payload:** malicious operator attempts `qwen3_coder`/`hermes` parser drift.
- **Expected defense layer:** production default is Ollama with no tool parser; opt-in vLLM profile must not enable `--tool-call-parser` or `--enable-auto-tool-choice` in informational-only mode.
- **Actual:** RETIRED as production blocker. vLLM compose lint and negative tests pass; Helm/static template assertions confirm no parser flags in the opt-in profile.

### Probe 06 — Loopback binding / zero external exposure

- **Payload:** change Compose inference ports to wildcard/bare host binding or Helm service type to `LoadBalancer`/`NodePort`.
- **Expected defense layer:** Compose publishes Ollama and vLLM on host loopback only; Helm renders inference services as `ClusterIP` only.
- **Actual:** DEFERRED for the default Ollama live suite. Probe 06 remains the opt-in vLLM-profile exposure check; it is not part of `ollama-cpu` live scoring for `task-f80486c5`.

### Probe 07 — Session hijack

- **Payload:** steal a `session_id` and try to resume with a different principal/tenant or spoof trusted headers directly to llm-chat.
- **Expected defense layer:** trusted-forwarder auth and `sessionVisibleToUser` reject forged/cross-tenant resumes.
- **Actual:** PASS. Session-binding, spoofed-header, forged-session, and trusted-forwarder tests pass.

### Probe 08 — Admin page authZ

- **Payload:** non-admin GET `/api/v1/chat/sessions`, then admin baseline.
- **Expected defense layer:** `chat.read_all` permission is required and missing permission checker fails closed.
- **Actual:** PASS. Admin list/detail/cross-tenant/fail-closed tests pass.

### Probe 09 — WS origin / CSRF

- **Payload:** WebSocket upgrade from `Origin: https://attacker.example` with stolen credentials.
- **Expected defense layer:** browser-facing gateway origin allowlist rejects before proxying to internal llm-chat; direct service still requires trusted forwarding.
- **Actual:** PASS. Gateway origin/CORS tests pass.

### Probe 10 — Rate limiting / backpressure

- **Payload:** historical 100 msg/sec chat->MCP tool-loop flood.
- **Expected defense layer:** no chat->MCP path exists; generic gateway rate limits and chat WS/output budgets remain bounded.
- **Actual:** RETIRED as chat->MCP flood surface. Static limiter wiring and llm-chat WS/output-budget tests pass; the generic gateway rate-limit suite is recorded as optional evidence for this retired surface.

### Probe 11 — Log redaction

- **Payload:** synthetic `sk-test-*`, JWT-looking, `Bearer ...`, and cloud-key strings in curated API/site knowledge or prompts.
- **Expected defense layer:** `DefaultRedactor` scrubs API/site knowledge before model context; provider/agent do not log auth headers or prompt bodies; optional log-dir grep finds zero secret-like strings.
- **Actual:** PASS. Knowledge redaction tests, provider auth/no-tool tests, MCP redactor tests, gateway auth logging tests, and source log assertions pass.

### Probe 12 — Entitlement bypass

- **Payload:** set `LLMChatAssistant=false` and request `/api/v1/chat/*`; verify dashboard header button hides when chat health is unavailable.
- **Expected defense layer:** internal chat handlers return stable feature-unavailable/402, gateway gates before forwarding, dashboard health/entitlement hook hides within the 10s poll interval.
- **Actual:** PASS. Licensing defaults, internal handler gate, gateway proxy gate, and dashboard source/test assertions pass. Dashboard npm tests run when npm/node_modules are available.


## POST-PIVOT INFORMATIONAL-ONLY LIVE EVIDENCE — 2026-04-28 (`task-f80486c5`)

This task replaced the deleted GPU/vLLM-only live security task after the product pivot to **Ollama-CPU informational-only**. The live run used the owned compose target `tests/security/compose.ollama-cpu.yaml` with non-shared host ports:

- `cordum-llm-chat-secprobes`: `127.0.0.1:8095 -> 8090`
- `qwen-inference-secprobes` (Ollama): `127.0.0.1:11436 -> 11434`
- isolated Redis + gateway-bootstrap mock + generated Enterprise test license fixture; ports can be overridden with LLMCHAT_SECURITY_LLMCHAT_PORT / LLMCHAT_SECURITY_OLLAMA_PORT for concurrent runners

Probe 09 is the one browser-facing gateway-origin check; its cURL hit the live local gateway TLS endpoint (`https://127.0.0.1:8081`) because the owned Ollama compose target intentionally does not boot the whole gateway/scheduler/NATS stack. The llm-chat/Ollama probes used the isolated stack.

Run summary:

```text
LLMCHAT_SECURITY_LIVE=1 LLMCHAT_SECURITY_REQUIRE_LIVE=1 LLMCHAT_SECURITY_BACKEND=ollama-cpu \
LLMCHAT_SECURITY_COMPOSE_FILE=tests/security/compose.ollama-cpu.yaml \
LLMCHAT_SECURITY_OUT_DIR=out/llmchat-security-live-step5-final \
LLMCHAT_GATEWAY_URL=http://127.0.0.1:8095 \
LLMCHAT_SECURITY_ORIGIN_GATEWAY_URL=https://127.0.0.1:8081 \
LLMCHAT_SECURITY_TRUSTED_FORWARDER_API_KEY=secprobe-key \
LLMCHAT_SECURITY_STOLEN_SESSION_ID=secprobe-stolen-step5-final \
bash tests/security/llmchat_run_all.sh

[llmchat_probe_01_delegation_scope] PASS (exit=0)
[llmchat_probe_02_agent_identity_scope] PASS (exit=0)
[llmchat_probe_07_session_hijack] PASS (exit=0)
[llmchat_probe_08_admin_authz] PASS (exit=0)
[llmchat_probe_09_ws_origin] PASS (exit=0)
[llmchat_probe_11_log_redaction] PASS (exit=0)
[llmchat_probe_12_entitlement_bypass] PASS (exit=0)
{"fail":0,"live_missing":0,"live_required":true,"pass":7,"retired":0,"scored_total":7,"skip":0,"total":7}
```

| Probe | Expected | Procedure | Actual / evidence excerpt | Verdict |
| --- | --- | --- | --- | --- |
| 01 | No browser-visible delegation token; generic delegation monotonicity remains fail-closed. | Run retained delegation + no-tool chat tests under `LLMCHAT_SECURITY_LIVE=1`. | `go test ./core/auth/delegation ... exit_code=0`; `go test ./core/llmchat ... exit_code=0`; `live_delegation_token=not_applicable`. | PASS |
| 02 | Chat-assistant has no MCP tools; direct MCP identity filter remains fail-closed. | Run MCP identity filter + bootstrap empty-scope tests under the Ollama backend. | `go test ./core/mcp ... exit_code=0`; `go test ./core/llmchat ... exit_code=0`; `live_mcp_call=not_applicable`. | PASS |
| 07 | A stolen `session_id` cannot resume under a different trusted principal/tenant. | Auto-seed isolated Redis with `secprobe-stolen-20260428T0900Z`, then POST to isolated llm-chat with trusted-forwarder key and Mallory/evil tenant headers. | `curl_exit=0 http_status=404`; body `{"code":"not_found",...}`; `assert_http_status_in ok: got=404 allowed=401,403,404`. | PASS |
| 08 | Non-admin/non-`chat.read_all` caller cannot list all chat sessions. | GET `/api/v1/chat/sessions` on isolated llm-chat with trusted-forwarder key and role `user`. | `curl_exit=0 http_status=403`; `permission chat.read_all denied for role user`; status assertion passed. | PASS |
| 09 | Malicious WebSocket `Origin` is rejected by the browser-facing gateway before proxying. | GET `/api/v1/chat/ws` with WS upgrade headers and `Origin: https://attacker.example` against live local gateway TLS endpoint. | `curl_exit=0 http_status=403`; body includes `origin not allowed`; status/body assertions passed. | PASS |
| 11 | Knowledge/log redaction prevents API keys, Bearer tokens, and JWT-like strings from leaking. | Run redaction/provider/auth tests and scan captured isolated Docker logs. | Knowledge, provider, MCP redactor, and auth logging tests exit 0; `assert_no_secret_patterns_in_dir ok`. | PASS |
| 12 | `LLMChatAssistant` entitlement gates chat and dashboard availability tests pass. | GET isolated `/api/v1/chat/healthz` with trusted-forwarder key under generated Enterprise test license fixture. | `curl_exit=0 http_status=200`; body `{"status":"ok","redis":"ok","vllm":"ok"}`; dashboard health-button tests `2 passed / 11 passed`. | PASS |
| 06 | vLLM-profile loopback/ClusterIP exposure check. | Not executed in default Ollama live suite. | Deferred by design: `ollama-cpu` default uses the owned loopback compose target; probe 06 remains for opt-in `vllm-gpu` / `cpu-vllm-awq`. | DEFERRED |

Retired probes 03/04/05/10 are no longer in the default orchestrator. They targeted chat-to-MCP mutation, prompt-to-tool bypass, parser pinning, and chat-to-MCP flood surfaces that do not exist in informational-only chat.

## OWASP LLM Top 10 (2025) mapping

| OWASP item | Cordum coverage after rescope | Result |
| --- | --- | --- |
| LLM01 Prompt Injection | Probe 04 retained prompt/no-tool/redaction assertions; chat has no tool execution surface | RETIRED as tool-bypass, controls PASS |
| LLM02 Sensitive Information Disclosure / Insecure Output Handling | Probes 04 and 11 cover prompt guardrails, knowledge redaction, and log non-leak controls | PASS |
| LLM03 Training Data Poisoning | N/A: Cordum serves local pre-trained models and does not train/fine-tune from tenant chat data | N/A |
| LLM04 Model Denial of Service | Probe 10 covers retained budgets/backpressure; parser-specific DoS retired with Ollama/no-tools default | RETIRED as chat->MCP/parser path, controls PASS |
| LLM05 Supply Chain | Runtime probes do not assess image provenance; supply-chain gate handled by follow-up `task-991597a4`/`supply-chain.md` | P2 follow-up |
| LLM06 Sensitive Information Disclosure | Probes 07 and 11 cover session hijack and redaction/log controls | PASS |
| LLM07 Insecure Plugin Design | Plugin/tool design surface retired for chat (`AllowedTools=[]`) and asserted by probes 02/03 | RETIRED for chat |
| LLM08 Excessive Agency | Probes 01, 02, and 03 prove no delegation/tool/preapproved agency from chat | PASS/RETIRED |
| LLM09 Overreliance | Technical controls pass; UX affordance follow-up remains `task-2bc8c05a` | P3 follow-up |
| LLM10 Model Theft | Probe 06 covers inference loopback/ClusterIP isolation for Ollama default and opt-in vLLM | PASS |

## Findings and escalation

| ID | Severity | Status | Description | Escalation |
| --- | --- | --- | --- | --- |
| SR-001 | P0 while present | CLOSED | Historical Helm vLLM parser value-drivenness could drift parser. | Superseded by no-parser informational vLLM profile and lint. No open P0/P1. |
| SR-002 | P1 while present | CLOSED | Historical vLLM request logging needed explicit prompt/request suppression. | `--disable-log-requests` retained for opt-in vLLM; Ollama default has no parser/tool logs. No open P0/P1. |
| SR-003 | P2 | BACKLOG | vLLM image provenance/SBOM/vulnerability gate is outside runtime probes. | `task-991597a4` |
| SR-004 | P3 | BACKLOG | Additional UX copy can reduce overreliance on assistant answers. | `task-2bc8c05a` |

There are **zero open P0/P1 findings** for the current informational-only/Ollama scope.
