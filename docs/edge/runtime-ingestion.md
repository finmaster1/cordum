# Edge Runtime Event Ingestion

> Status: **EDGE-144 design + skeleton**. Disabled by default. No collector daemon
> ships in this milestone — only the bounded ingest contract a future
> Tetragon/Falco/eBPF sidecar adapter will speak.

## What this is

Runtime-layer ingestion lets a trusted sidecar (Tetragon, Falco, an in-cluster
eBPF collector, etc.) feed bounded, redacted process / file / network /
DNS telemetry into the existing Edge evidence pipeline as
`edge.AgentActionEvent` records carrying `layer = "runtime"`.

The skeleton in this milestone defines:

- a stable wire envelope (`RuntimeEventEnvelope`, `RuntimeBatch`) clients
  upload,
- a deterministic mapper into existing `AgentActionEvent` kinds,
- a disabled-by-default gateway endpoint that validates auth, tenant access,
  source identity, parent session/execution, batch caps, and redaction
  before persisting through the same `edge.Store.AppendEvents` path the
  hook / MCP surfaces use,
- a sampling/drop policy that keeps Redis writes bounded.

## What this is **not** (non-goals)

- No eBPF / Tetragon / Falco collector binary ships from this task.
- No enforcement: runtime events are recorded, not blocked.
- No parallel store, no parallel NATS subject, no parallel Redis stream.
- No dashboard UI in this milestone.
- No Cordum Job mapping for individual runtime events (see epic rail
  "Do not model individual Claude Code/tool actions as Cordum Jobs").
- No raw transcript, raw argv, raw file contents, raw packet payloads, raw
  DNS response bodies, raw HTTP request bodies, raw headers, raw secrets.

## Event kind mapping

Runtime telemetry maps onto the five existing `edge.EventKind` constants
declared in `core/edge/event.go`:

| Runtime envelope `kind`   | `AgentActionEvent.Kind`         | Notes                                                  |
| ------------------------- | ------------------------------- | ------------------------------------------------------ |
| `runtime.process.exec`    | `EventKindRuntimeProcessExec`   | One event per exec/clone-exec; bounded summary only.   |
| `runtime.file.read`       | `EventKindRuntimeFileRead`      | Path / basename redacted; never file contents.         |
| `runtime.file.write`      | `EventKindRuntimeFileWrite`     | Path / basename redacted; never file contents.         |
| `runtime.network.connect` | `EventKindRuntimeNetworkConnect`| Destination host / IP prefix / port / protocol only.   |
| `runtime.dns.query`       | `EventKindRuntimeDNSQuery`      | Truncated, redacted qname; never response bodies.      |

Every mapped event is stamped with:

- `Layer = LayerRuntime`,
- `Decision = DecisionRecorded` (the existing `recorded` decision used for
  observe-only evidence — never `allow` / `deny`),
- `Status = ActionStatusOK` unless the source-side observation explicitly
  marked it `failed` / `degraded`,
- `RuleTier = ""` (runtime telemetry is not a policy gate decision),
- `Seq` assigned by the store on append (callers never invent sequence
  numbers).

Unknown / non-runtime kinds are rejected at the adapter boundary. New
kinds require a code change here plus the matching `EventKind` constant.

## Wire envelope

```jsonc
{
  "source_id":     "tetragon-cluster-a",
  "batch_id":      "uuid-v4",
  "events": [
    {
      "tenant_id":     "tenant-1234",
      "session_id":    "edge-session-abc",
      "execution_id":  "edge-exec-xyz",
      "source_event_id": "stable-hash-or-source-uid",
      "observed_at":   "2026-05-17T13:42:11.123Z",
      "kind":          "runtime.process.exec",
      // Optional bounded, redacted summaries:
      "process": {
        "executable_basename": "curl",
        "executable_sha256":   "abcdef...",
        "argument_count":      3
      },
      "file":    { "operation": "read",  "path_redacted": "/tmp/[REDACTED]" },
      "network": { "host_redacted": "[REDACTED].example",
                   "ip_prefix":     "10.0.0.0/24",
                   "port":          443,
                   "protocol":      "tcp" },
      "dns":     { "qname_redacted": "[REDACTED].svc.cluster.local",
                   "qtype":          "A" },
      "labels":      { "node": "node-7" },
      "metadata":    { "container_runtime": "containerd" },
      "artifact_ptrs": []
    }
  ]
}
```

Field rules:

- `tenant_id`, `session_id`, `execution_id`, `source_event_id`,
  `observed_at`, and `kind` are **required**. Missing any → 400
  `invalid_request`.
- `argument_count` is required for `runtime.process.exec`; raw `argv` is
  forbidden. The adapter rejects any envelope that contains a key named
  `argv`, `args`, `command_line`, `cmdline`, `env`, `environment`,
  `file_content`, `file_contents`, `packet`, `payload`, `body`,
  `request_body`, `response_body`, `headers`, `header`, `cookie`,
  `cookies`, `secret`, `secrets`, `token`, `tokens`, `password`,
  `passwords`, `api_key`, `apikey`, `private_key`, `dns_response`, or
  `response`.
- Path / host / qname fields go through the existing edge redactor
  (`edge.RedactValue` with `RedactionHashBoth`) before persistence.
- Any large evidence (full PCAP, raw script body, full exec trace) must
  use `artifact_ptrs` with the existing `artifact:` / `edge-artifact:`
  URI schemes; the adapter never inlines raw bytes.

## Batching, sampling, retention

Constants on the adapter (`core/edge/runtimeingest`):

| Setting                                | Value      | Why                                                                 |
| -------------------------------------- | ---------- | ------------------------------------------------------------------- |
| `MaxRuntimeBatchEvents`                | 256        | Bounded make() for CodeQL; matches batch size of existing endpoints.|
| `MaxRuntimeEnvelopeBytes`              | 4096       | Per-event redacted size cap; mirrors `MaxInputRedactedBytes` shape. |
| `MaxRuntimeBatchBodyBytes`             | 1 MiB      | Body limit before JSON decode; mirrors edge maxBody convention.     |
| `MaxRuntimeRedactedStringBytes`        | 256        | Hard cap on per-field redacted string length.                       |
| `DefaultRuntimeSampleRateNumerator`    | 1          | 100% accepted when sampling disabled.                               |
| `DefaultRuntimeSampleRateDenominator`  | 1          |                                                                     |

Sampling: when a source-config seam enables sampling (out of scope for
this skeleton; designed-only), the adapter uses
`fnv64a(source_id|kind|source_event_id) % denominator < numerator` so:

- retries from the same source land in the same accept/drop bucket
  (deterministic),
- two replicas of the same collector cannot accidentally double-count a
  single observation,
- the gateway response surfaces `dropped_count` per batch so operators
  can observe pressure without exporting raw events.

Persistence path: the adapter **never** writes to Redis or NATS directly.
It builds `[]edge.AgentActionEvent`, validates each event via the
existing `AgentActionEvent.Validate()`, and persistence goes through
`edge.Store.AppendEvents` — the same atomic MULTI/EXEC pipeline used by
`POST /api/v1/edge/events/batch`. Per-execution event caps
(`ErrExecutionEventCapExceeded`) and parent-validation TOCTOU protections
inherited automatically.

Retention is inherited from the existing Edge session retention policy
(see `docs/edge/retention.md`); runtime events do not introduce a new
retention class. Large bodies belong in artifact storage, which has its
own `RetentionClass` controls.

## Tenant + source authentication

The endpoint is gated by the existing Edge auth stack — exactly the same
as the hook / MCP / evaluate endpoints:

1. Gateway middleware: API key / OIDC / mTLS via the standard request
   pipeline.
2. `requireEdgePermissionOrRole(PermJobsWrite, "admin", "user")` — RBAC
   parity with the events endpoints; viewer cannot ingest.
3. `edgeTenantFromRequest` — `X-Tenant-ID` header is mandatory; if the
   body carries a different tenant id we 403 `tenant_mismatch`.
4. `source_id` validation — the adapter rejects any envelope whose
   `source_id` is absent, whitespace-only, exceeds the redactor's
   string cap, or matches a secret-shaped pattern. A future config seam
   may add an allowlist; the skeleton enforces shape only.
5. Parent validation — `validateEdgeEventParents` confirms the
   `session_id` + `execution_id` exist for this tenant and that the
   execution's `SessionID` matches the envelope. Cross-tenant /
   cross-session events are rejected before any append.

No new auth provider, no new tenant header, no new API key path.

## Disabled-by-default behavior

The endpoint is gated by the env var
`CORDUM_EDGE_RUNTIME_INGEST_ENABLED`. Acceptable enable values:
`"true"`, `"1"`, `"yes"` (case-insensitive). Default (empty / anything
else) → 503 `service_unavailable` with `code = "service_unavailable"`,
no Redis writes, no SIEM forwarding, no side effects.

503 (rather than 404) was chosen so:

- the route still appears in OpenAPI and the gateway router, so
  operators can probe readiness without restart cycles;
- a future managed-settings rollout can toggle the flag on a subset of
  fleets without redeploying;
- this mirrors the existing convention used by services that exist but
  are intentionally not yet serving traffic.

## Response shape

```jsonc
{
  "accepted_count": 12,
  "dropped_count":  3,
  "dropped":        [
    { "source_event_id": "abc...", "reason": "sampled_out" }
  ],
  "request_id": "..."
}
```

Partial acceptance: the adapter rejects an entire batch only when the
batch itself is malformed (oversize body, bad JSON, oversize element
count). Once decode succeeds, per-event errors (`invalid_request`,
`tenant_mismatch`, `not_found` for missing parent, `request_too_large`
for over-size redacted body) abort the whole batch with the first
typed error — no partial Redis writes. Sampling drops are counted but
do not error.

Error mapping (all via the existing edge envelope):

| Condition                                 | HTTP | `code`                  |
| ----------------------------------------- | ---- | ----------------------- |
| Endpoint disabled                          | 503  | `service_unavailable`   |
| Missing / empty / oversize source_id       | 400  | `invalid_request`       |
| Tenant header missing                      | 400  | `tenant_required`       |
| Tenant header / body mismatch              | 403  | `tenant_mismatch`       |
| RBAC / license denial                       | 403  | `access_denied`         |
| Body > MaxRuntimeBatchBodyBytes             | 413  | `request_too_large`     |
| Decoded events > MaxRuntimeBatchEvents      | 413  | `request_too_large`     |
| Empty batch                                 | 400  | `invalid_request`       |
| Unknown / non-runtime `kind`                | 400  | `invalid_request`       |
| Forbidden raw-field key present             | 400  | `raw_payload_rejected`  |
| Per-event redacted size > MaxRuntime…Bytes  | 413  | `request_too_large`     |
| Missing parent session                      | 404  | `not_found`             |
| Missing parent execution                    | 404  | `not_found`             |
| Execution-session mismatch                  | 400  | `execution_session_mismatch` |
| Cross-tenant artifact pointer               | 400  | `artifact_pointer_invalid` |
| Per-execution event cap exceeded            | 429  | `event_cap_exceeded`    |
| Store unavailable                           | 503  | `store_unavailable`     |
| Anything else                               | 500  | `internal_error`        |

## Rollout

1. **EDGE-144** (this task): design + disabled-by-default skeleton +
   tests. Production behavior: 503 on the route, zero Redis writes.
2. **Operational metrics** (later task): add Prometheus counters in
   the existing `core/edge` recorder for accepted / dropped / rejected
   per source.
3. **Collector adapter** (later task): ship a Tetragon / Falco /
   in-cluster eBPF sidecar that speaks this wire format. Enforcement
   would belong to a separate epic; runtime ingest is observe-only.
4. **Dashboard surface** (later task): once the data is flowing, a
   dashboard panel under the existing Edge timeline can join on
   `Layer = runtime`. **No** dashboard work happens in EDGE-144.

## Why this shape

Three constraints drove the design:

1. **Privacy.** Runtime telemetry is the highest-leakage layer. The
   schema deliberately does not include argv, file contents, packet
   payloads, DNS responses, request bodies, or headers — and the
   adapter rejects envelopes that try to smuggle them under disguised
   keys. Anything with raw evidence requires an artifact pointer with
   its own retention controls.
2. **No flood.** The schema is opt-in (disabled by default), capped per
   batch and per byte, deterministic-sampling-aware, and persists
   through the existing per-execution event cap. There is no parallel
   bus.
3. **Reuse-before-build.** Every operation (auth, tenant gate, parent
   validation, redaction, append, error envelope, IDs, sequence)
   reuses the existing Edge stack. The runtime layer adds one wire
   schema and one bounded mapper — nothing else.
