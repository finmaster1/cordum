# LLM chat observability contract

This page defines the runtime logging and tracing contract for the
informational-only `cordum-llm-chat` service.

## Structured log format

`cordum-llm-chat` emits application logs as JSON to stderr. Docker Compose may
prefix each line with the service name (`llm-chat-1 | `), but the payload after
that prefix must parse as one JSON object per line.

Every JSON log record includes the common `slog` fields:

- `time`
- `level`
- `msg`
- `component`

The `component` value for the chat service is `llm-chat-server`.

## Safe correlation keys

Logs for a chat session or request should use these bounded, non-secret
correlation keys:

| Field | Meaning | Source |
| --- | --- | --- |
| `session_id` | Chat session identifier. | Session store / `X-Chat-Session-Id`. |
| `user_principal` | Authenticated principal forwarded by the gateway. | Trusted `AuthContext`; never raw spoofable headers. |
| `tenant` | Tenant identifier forwarded by the gateway. | Trusted `AuthContext`. |
| `trace_id` | Request trace/correlation identifier. | W3C `Traceparent` trace id, `X-Trace-Id`, `X-Request-Id`, `X-Correlation-Id`, or session-id fallback. |

`trace_id` is deliberately available on INFO-level session logs so operators can
reconstruct a session even when DEBUG/token-delta logs are sampled out.

## Redaction and forbidden values

The chat service must not log:

- prompts or raw user messages
- LLM token deltas
- API keys, JWTs, bearer tokens, or license tokens
- raw `X-API-Key`, `Authorization`, or `CORDUM_API_KEY` values

The logger uses Cordum's redacting slog handler and the llm-chat correlation
helpers sanitize header-derived values before logging. Values that resemble
Bearer tokens, `X-API-Key`, `CORDUM_API_KEY`, JWTs, OpenAI-style `sk-` keys,
AWS AKIA keys, private-key markers, or 64+ hex key material are redacted before
they reach structured logs.

## Probe contract

`scripts/ops-probes/probe-01.sh` is the regression probe for this contract. It:

1. collects `llm-chat` Docker logs,
2. strips the Compose prefix,
3. requires every payload line to parse as JSON,
4. counts safe correlation fields, and
5. runs a secret-pattern scan.

When a secret-like value is detected, the probe reports only:

```text
MATCH <file>:line=<n> pattern=<label>
```

It must not echo the matched secret value into CI evidence or Moe step notes.

Expected success evidence:

```text
secret_scan=zero_hits
json_lines_ok=true
json_validation=PASS
RESULT=PASS
```

## OpenTelemetry tracing

`cordum-llm-chat` uses Cordum's shared `core/infra/otel` bootstrap. Tracing is
off by default; enable it explicitly in an owned environment:

```bash
docker compose --profile observability up -d jaeger
LLMCHAT_OTEL_ENABLED=true \
LLMCHAT_OTEL_EXPORTER_OTLP_ENDPOINT=jaeger:4317 \
LLMCHAT_OTEL_TRACES_SAMPLER_ARG=1.0 \
docker compose up -d llm-chat
```

Compose exposes the Jaeger UI at `http://127.0.0.1:16686` and OTLP receivers on
loopback only (`127.0.0.1:4317` for gRPC, `127.0.0.1:4318` for HTTP). Production
installations should normally point `OTEL_EXPORTER_OTLP_ENDPOINT` at the
customer collector, not expose Jaeger publicly.

### Trace propagation and span names

A single chat request should preserve one W3C trace across:

1. the gateway handler / reverse proxy (`traceparent` injection),
2. `cordum-llm-chat` WebSocket connect/disconnect,
3. the per-turn assistant loop,
4. the OpenAI-compatible Ollama/vLLM call,
5. Redis session lifecycle reads/writes, and
6. chat session audit emission.

Expected llm-chat span names:

| Span | Kind | Safe attributes |
| --- | --- | --- |
| `chat.ws.connect` | server | `chat.session_id`, `chat.tenant`, `chat.user_principal` |
| `chat.ws.disconnect` | internal | same bounded correlation keys as connect |
| `chat.turn` | internal | session/principal/tenant + `llm.backend` |
| `llm.inference` | client | `llm.backend`, `llm.model`, approximate prompt/completion token counts |
| `llm.inference.health` | client | backend/model only |
| `chat.session.read` / `chat.session.write` | client | Redis operation + bounded session metadata |
| `chat.audit.emit` | internal | audit event type/action + tenant/session id |
| `llmchat.knowledge.load` | internal | knowledge-pack token counts and configured max |

The token-count attributes are approximate observability counters only; they are
not used for billing or policy decisions.

### Trace redaction contract

Trace attributes must be bounded and must not contain:

- raw prompts or assistant deltas,
- API keys, JWTs, bearer tokens, license tokens, or `CORDUM_API_KEY`,
- raw `Authorization` or `X-API-Key` headers, or
- unbounded transcript/session text.

`chat.audit.emit` injects the active OTEL `trace_id` into the audit event `Extra`
map so audit-chain entries can be correlated with Jaeger and structured logs.
It does not copy prompt text or model output into span attributes.

### Probe 3 evidence

`scripts/ops-probes/probe-03.sh` verifies static OTEL wiring and, when
`LLMCHAT_JAEGER_QUERY_URL` is configured, queries Jaeger and fails closed unless
required operation names are present.

Example:

```bash
LLMCHAT_JAEGER_QUERY_URL='http://127.0.0.1:16686/api/traces?service=cordum-llm-chat-smoke&lookback=1h&limit=20' \
bash scripts/ops-probes/probe-03.sh
```

Expected evidence:

```text
jaeger_query=ok
jaeger_operation_chat.ws.connect=present
jaeger_operation_chat.turn=present
jaeger_operation_llm.inference=present
jaeger_operation_chat.audit.emit=present
RESULT=PASS
```

## Related docs

- [`ops-review.md`](ops-review.md) records the senior ops review that filed the
  structured-log and trace-propagation follow-ups.
- [`ops-runbook.md`](ops-runbook.md) is the customer-facing day-2 runbook.
- [`production-readiness.md`](production-readiness.md) captures Ollama-default
  production readiness evidence.
