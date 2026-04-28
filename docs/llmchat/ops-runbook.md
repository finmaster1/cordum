# LLM Chat Ops Runbook

Task: `task-8eab552b` — day-2 operations for the informational-only LLM chat assistant.

## Scope and defaults

The LLM chat assistant is an informational Q&A helper grounded in local Cordum API and documentation knowledge packs. It does **not** call MCP tools, submit jobs, approve jobs, or mutate Cordum state. Mutations stay in the dashboard, CLI, and normal API workflows.

Production default inference is local Ollama with Qwen2.5-Coder-3B through the OpenAI-compatible API surface. vLLM/Qwen3-Coder remains an opt-in GPU profile for customers that explicitly choose it. External LLM endpoints are only supported when an operator intentionally sets the documented override.

## Deploy

### Docker Compose

1. Create a production `.env` with at least `CORDUM_API_KEY`, Redis/NATS secrets, and the Enterprise license token/public key that includes `llm_chat_assistant`.
2. Start the default CPU-local chat profile:
   ```bash
   docker compose up -d --build
   ```
3. Confirm the gateway points at the active chat service and that only one chat backend profile is exposed to users.
4. Keep the knowledge-pack mounts read-only: OpenAPI under `/etc/cordum-llm-chat/openapi.yaml` and curated cordum.io docs under `/etc/cordum-llm-chat/cordum-io`.

### Helm

1. Set production guardrails:
   ```yaml
   global:
     production: true
     tls:
       enabled: true
   llmChat:
     enabled: true
   inference:
     backend: ollama-cpu
   ```
2. Supply `secrets.apiKey`, Redis auth, NATS auth, and the Enterprise license through Kubernetes Secrets or the chart's external-secret mechanism.
3. Install or upgrade:
   ```bash
   helm upgrade --install cordum ./cordum-helm -n cordum --create-namespace -f values.prod.yaml
   ```

## Upgrade

1. Read the release notes for chat frame/protocol changes and knowledge-pack format changes.
2. Upgrade inference first only when the new chat service is backwards-compatible with the current OpenAI-compatible API.
3. Upgrade `cordum-llm-chat` before the dashboard if the dashboard expects new health or admin-session fields.
4. Drain or tolerate websocket reconnects. The widget should reconnect, but active users can see a short interruption during pod replacement.
5. Verify `/healthz`, `/readyz`, `/metrics`, and the dashboard chat button before declaring the upgrade complete.

## Rollback

1. Stop new dashboard traffic by temporarily disabling the chat button through the health gate or by rolling the gateway/dashboard to the previous release.
2. Roll back `cordum-llm-chat`:
   ```bash
   helm rollback cordum <REVISION> -n cordum
   ```
3. If the inference backend changed, roll it back after chat is stable.
4. Do not delete Redis `chat:session:*` keys during rollback unless support confirms the transcript data is corrupt; session loss is customer-visible.
5. Re-run the checks in [Check health](#check-health).

## Scale

- `cordum-llm-chat` is horizontally scalable when all replicas share Redis and the gateway/load balancer supports websocket stickiness or reconnects cleanly.
- Ollama CPU capacity is the default bottleneck. Start with one inference container per host and increase only after measuring latency and memory.
- vLLM GPU profile users must size by model memory, KV cache, and concurrency. Keep vLLM `ClusterIP`-only; expose only the chat service/gateway.
- Never use session IDs, users, tenants, prompts, tokens, or trace IDs as Prometheus labels.

## Check health

| Check | Command | Expected |
|---|---|---|
| Gateway status | `curl -k -H "X-API-Key: $CORDUM_API_KEY" https://127.0.0.1:8081/api/v1/status` | 200, Enterprise license lists `llm_chat_assistant` |
| Chat health | `curl -fsS http://127.0.0.1:8092/healthz` | 200 |
| Chat readiness | `curl -fsS http://127.0.0.1:8092/readyz` | 200 with Redis and inference OK |
| Metrics | `curl -fsS http://127.0.0.1:8092/metrics | grep '^chat_'` | chat metric families present |
| Dashboard | Navigate to `/settings/chat-sessions` as admin | Admin session list loads |
| Audit chain | query the audit verification endpoint per `docs/audit-operations.md` | status OK |

## Common alerts

Alert rules ship in `cordum-helm/alerts/llm-chat.yaml`.

- **LLMChatBackendDown** — readiness/metrics are missing for more than 5 minutes. Check the inference container first, then Redis and license state.
- **LLMChatHighErrorRate** — error counter rate exceeds the baseline threshold. Inspect recent `chat_errors_total{kind=...}` values and redacted logs.
- **LLMChatApprovalBacklogHigh** — legacy compatibility alert for deployments that still expose approval-required telemetry. Informational-only default should stay at zero.
- **LLMChatNoSessionsFor30m** — no active sessions for 30 minutes while the service is up. Confirm the dashboard chat button is visible and entitlement is enabled.

## Known issues and workarounds

- Some legacy metrics still include `tool` and `vllm` names for compatibility. Treat them as backend/tooling labels, not as proof that chat may mutate state.
- If `/readyz` reports the inference field as `vllm` while running Ollama, treat it as a backwards-compatible field name; confirm the configured model/backend in deployment values.
- Live debug dumps are not yet implemented. Use admin read-only transcript APIs and redacted logs as the temporary support bundle.
- The current runtime log format may be text-prefixed instead of JSON. Until fixed, configure log processors with a defensive parser and do not rely on field extraction for security controls.

## Escalation matrix

| Severity | Examples | Immediate action | Owner |
|---|---|---|---|
| P0 | Secret/JWT/API key in logs, external inference exposure, entitlement bypass | Disable chat, preserve evidence, page security owner | Security + platform lead |
| P1 | Trace export missing, admin view unaudited, dashboard/alerts not shipping | File follow-up, keep task out of DONE until accepted or scoped | Platform ops owner |
| P2 | Naming drift, dashboard panel no-data cosmetic issue | Track in next ops polish cycle | Dashboard/ops |
| P3 | Documentation wording issue | Fix opportunistically | Docs owner |

## Evidence collection

Run the probe harness from the repository root:

```bash
bash scripts/ops-probes/probe-01.sh
bash scripts/ops-probes/probe-02.sh
bash scripts/ops-probes/probe-03.sh
bash scripts/ops-probes/probe-04.sh
bash scripts/ops-probes/probe-06.sh
bash scripts/ops-probes/probe-07.sh
bash scripts/ops-probes/probe-09.sh
bash scripts/ops-probes/probe-11.sh
bash scripts/ops-probes/probe-12.sh
```

Evidence lands under `out/llmchat-ops/<probe>/evidence.txt`. Redact hostnames, tenant names, and support-ticket identifiers before sharing outside the customer environment.
