# Runtime Ingest Replay Protection

Status: design proposal, not implemented (`task-f5fdb9ea`).

## Decision

Runtime ingest needs replay protection before production enablement, but it is
not a PR #276 ship gate while the endpoint remains opt-in and disabled by
default.

The current runtime ingest gate already enforces the critical auth boundary:

- `CORDUM_EDGE_RUNTIME_INGEST_ENABLED` must be truthy; otherwise the route
  returns `503`.
- The caller must authenticate as a runtime collector and hold
  `PermRuntimeIngest`, `edge.runtime.*`, or `edge.*`.
- The authenticated collector principal must equal `source.source_id`.
- `X-Tenant-ID` is authoritative, and any body `tenant_id` must match it.
- The referenced session and execution must exist in the tenant.
- The session principal and execution worker must match the collector.

Replay protection is defense in depth on top of that boundary. It protects
against duplicate writes from captured requests, stuck collector retries,
load-balanced gateway races, and gateway restart amnesia.

## Current replay gap

Runtime envelopes carry `source_event_id`, and the adapter derives a stable
`event_id` from:

```text
source_id | tenant_id | session_id | execution_id | kind | source_event_id
```

That makes retried mappings deterministic, but normal `AppendEvents` does not
use the event-id index as a pre-write uniqueness guard. A replayed batch can
therefore append duplicate records with the same stable `event_id`. The
event-id index points at the latest sequence, but the earlier duplicate remains
in the execution event list.

## Recommended approach

Add an explicit bounded replay window keyed by authenticated source identity.

1. Extend the runtime ingest batch schema with `nonce`.
   - Use UUIDv7 or random 128-bit values.
   - Keep the existing optional `batch_id` for operator correlation only.
2. On receive, compute a replay key from the authenticated context:

   ```text
   edge:rt:nonce:<tenant_id>:<collector_id>
   ```

   The key must never include raw payload content.
3. Use Redis as the shared cross-instance replay window.
   - `SADD` the nonce.
   - If `SADD` returns `1`, this is the first accepted batch; continue mapping
     and appending events.
   - If `SADD` returns `0`, this is a replay. Return an idempotent success
     response and do not append another copy.
   - Apply a TTL of one hour to bound storage and cover common retry windows.
4. Bound set size.
   - Track approximate cardinality with `SCARD`.
   - Refuse or compact if the per-collector window exceeds the configured cap.
   - Do not retain raw runtime payloads for deduplication.
5. Record observability without leaking nonce values.
   - Count first-seen, replayed, and replay-window-saturated batches.
   - Log tenant, collector, and bounded reason only.

## Legitimate retry semantics

Collector retry after a transient network failure should be safe:

- If the first request committed and the retry arrives inside the window, return
  `200 OK` with the same accepted/dropped counts where feasible, or a documented
  idempotent replay response.
- If the first request reserved the nonce but failed before append, release the
  reservation or store a pending marker with a short TTL so the collector can
  retry.
- If the replay window expired, treat the request as new only if the collector
  supplies a fresh nonce.

The exact response body can reuse Edge idempotency patterns, but it must remain
bounded and must not cache raw runtime event bodies.

## Optional ordering

A monotonic sequence per `(tenant_id, collector_id)` can detect reordering, but
it is not required for replay protection. Add it only if operators need
collector-order guarantees; nonce-based dedup is sufficient for duplicate
batch protection.

## Out of scope for this task

- Implementing the nonce field or Redis window.
- Selecting UUIDv7 versus random 128-bit nonce format.
- Adding response-body replay caching.
- Changing collector credentials, session binding, or source binding.

Those belong in the production-enable implementation task linked from
`task-f5fdb9ea`.
