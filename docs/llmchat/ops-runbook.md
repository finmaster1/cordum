# LLM Chat Production-Readiness Ops Runbook

Task: `task-e363a7fa` — production readiness + vLLM config verification + failure modes + rolling upgrade.

## Interim CPU operating mode

Static vLLM configuration verification is documented separately in
`docs/llmchat/vllm-config-verification.md` and passed for compose/Helm
config. This runbook covers the runtime failure-mode harness under
`tests/ops/`.

Scope decision (2026-04-26): Yaron directed the team to **"switch to CPU
LLM model for now"**. That is the formal interim narrowing for
`task-a5d09fad` and the ops-harness follow-up to `task-e363a7fa`.

Interim evidence model:

- CPU mode is for QA and air-gapped, low-throughput evaluation only.
  Production deployments still require the GPU vLLM path.
- The GPU/k8s tier-matrix probes are formally **DEFERRED** to a follow-up
  GPU/k8s staging task: probe 06 (Tier 1 H100 capacity), probe 07 (GPU
  OOM / at-capacity), probe 12 (rolling upgrade), probe 13 (HF cache
  loss), probe 14 (Tier 2 AWQ), and probe 15 (Tier 3 A100).
- The remaining 12 probes are in scope for a dedicated CPU vLLM stack:
  probes 01-05, 08-11, and 16-18.
- The local compose/dev path that uses the Python qwen-inference mock is
  still not valid evidence for destructive or performance probes.
- `task-a5d09fad` now tracks the CPU-mode staging work; `task-e363a7fa`
  must not be marked complete until CPU live evidence exists or its DoD
  is formally narrowed.

Do not interpret CPU latency or throughput as a production claim. The
per-probe verdict table below remains blocked until the CPU live run
lands and is updated with concrete evidence paths.

## How to run

Non-destructive local syntax/scaffold check:

```bash
bash tests/ops/llmchat_run_all.sh
```

Required live-evidence mode (must fail if any probe still skips):

```bash
LLMCHAT_OPS_REQUIRE_LIVE=1 LLMCHAT_OPS_LIVE=1 CORDUM_API_KEY=<redacted> bash tests/ops/llmchat_run_all.sh
```

Destructive probes additionally require `LLMCHAT_OPS_ALLOW_DESTRUCTIVE=1` and must run only on dedicated staging. Hardware-specific probes require their explicit opt-ins (`LLMCHAT_OPS_K8S_LIVE=1`, `LLMCHAT_OPS_TIER2_LIVE=1`, `LLMCHAT_OPS_TIER3_LIVE=1`).

Evidence is written under `out/llmchat-ops/<probe-id>/evidence.txt` plus per-probe JSONL/metric artifacts.

## Probe matrix

| # | Probe | Domain | Marker | Current severity | Evidence path |
|---|---|---|---|---|---|
| 1 | vLLM cold start | Lifecycle | `gpu-nightly destructive` | BLOCKED | `out/llmchat-ops/llmchat_probe_01_cold_start/evidence.txt` |
| 2 | vLLM crash mid-request | Lifecycle | `gpu-nightly destructive` | BLOCKED | `out/llmchat-ops/llmchat_probe_02_vllm_crash/evidence.txt` |
| 3 | Long tool conversation qwen3_xml sanity | Stability | `gpu-nightly` | BLOCKED | `out/llmchat-ops/llmchat_probe_03_long_tool_conversation/evidence.txt` |
| 4 | Redis partition | Network/storage | `compose-nightly destructive` | BLOCKED | `out/llmchat-ops/llmchat_probe_04_redis_partition/evidence.txt` |
| 5 | NATS partition | Network/storage | `compose-nightly destructive` | BLOCKED | `out/llmchat-ops/llmchat_probe_05_nats_partition/evidence.txt` |
| 6 | Tier 1 H100 capacity | Capacity | `gpu-nightly H100` | BLOCKED | `out/llmchat-ops/llmchat_probe_06_tier1_capacity/evidence.txt` |
| 7 | GPU OOM / at-capacity | Capacity | `gpu-nightly destructive` | BLOCKED | `out/llmchat-ops/llmchat_probe_07_gpu_oom/evidence.txt` |
| 8 | Prefix caching amortization | Capacity | `gpu-nightly` | BLOCKED | `out/llmchat-ops/llmchat_probe_08_prefix_caching/evidence.txt` |
| 9 | Session TTL expiry | Lifecycle | `compose-nightly` | BLOCKED | `out/llmchat-ops/llmchat_probe_09_session_ttl/evidence.txt` |
| 10 | Tool-call budget exhaustion | Stability | `gpu-nightly` | BLOCKED | `out/llmchat-ops/llmchat_probe_10_token_budget/evidence.txt` |
| 11 | Repeat-call detector | Stability | `gpu-nightly` | BLOCKED | `out/llmchat-ops/llmchat_probe_11_repeat_call/evidence.txt` |
| 12 | Rolling upgrade | Kubernetes | `k8s-nightly destructive` | BLOCKED | `out/llmchat-ops/llmchat_probe_12_rolling_upgrade/evidence.txt` |
| 13 | HF cache PVC loss | Network/storage | `gpu-nightly destructive` | BLOCKED | `out/llmchat-ops/llmchat_probe_13_hf_cache_loss/evidence.txt` |
| 14 | Tier 2 AWQ | Hardware tier | `tier2-manual destructive` | BLOCKED | `out/llmchat-ops/llmchat_probe_14_tier2_awq/evidence.txt` |
| 15 | Tier 3 A100 | Hardware tier | `tier3-manual destructive` | BLOCKED | `out/llmchat-ops/llmchat_probe_15_tier3_a100/evidence.txt` |
| 16 | Concurrent isolation/load leak | Stability | `gpu-nightly long-running` | BLOCKED | `out/llmchat-ops/llmchat_probe_16_concurrent_isolation/evidence.txt` |
| 17 | Graceful shutdown | Lifecycle | `compose-nightly destructive` | BLOCKED | `out/llmchat-ops/llmchat_probe_17_graceful_shutdown/evidence.txt` |
| 18 | Redis backpressure | Stability/network | `compose-nightly destructive` | BLOCKED | `out/llmchat-ops/llmchat_probe_18_backpressure/evidence.txt` |

## Per-probe operating notes

### Probe 1: vLLM cold start

- Script: `tests/ops/llmchat_probe_01_cold_start.sh`
- Marker: `gpu-nightly destructive`
- Expected defense / recovery: Requires real FP8 vLLM; asserts /readyz 503/200 and dashboard health hide/show timing.
- Evidence path: `out/llmchat-ops/llmchat_probe_01_cold_start/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 2: vLLM crash mid-request

- Script: `tests/ops/llmchat_probe_02_vllm_crash.sh`
- Marker: `gpu-nightly destructive`
- Expected defense / recovery: Requires real streaming vLLM; asserts structured WS error and retry after restart.
- Evidence path: `out/llmchat-ops/llmchat_probe_02_vllm_crash/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 3: Long tool conversation qwen3_xml sanity

- Script: `tests/ops/llmchat_probe_03_long_tool_conversation.sh`
- Marker: `gpu-nightly`
- Expected defense / recovery: Requires real Qwen tool calling; asserts 15 turns, >=20 tool calls, no !!!!!!!!.
- Evidence path: `out/llmchat-ops/llmchat_probe_03_long_tool_conversation/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 4: Redis partition

- Script: `tests/ops/llmchat_probe_04_redis_partition.sh`
- Marker: `compose-nightly destructive`
- Expected defense / recovery: Requires dedicated live compose; disconnects Redis and checks degraded readyz/recovery.
- Evidence path: `out/llmchat-ops/llmchat_probe_04_redis_partition/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 5: NATS partition

- Script: `tests/ops/llmchat_probe_05_nats_partition.sh`
- Marker: `compose-nightly destructive`
- Expected defense / recovery: Requires dedicated live compose; disconnects NATS and verifies audit chain.
- Evidence path: `out/llmchat-ops/llmchat_probe_05_nats_partition/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 6: Tier 1 H100 capacity

- Script: `tests/ops/llmchat_probe_06_tier1_capacity.sh`
- Marker: `gpu-nightly H100`
- Expected defense / recovery: Requires H100; records 16/32 session latency and vLLM metrics.
- Evidence path: `out/llmchat-ops/llmchat_probe_06_tier1_capacity/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 7: GPU OOM / at-capacity

- Script: `tests/ops/llmchat_probe_07_gpu_oom.sh`
- Marker: `gpu-nightly destructive`
- Expected defense / recovery: Requires H100; starts 50 long-context sessions and checks structured overload errors.
- Evidence path: `out/llmchat-ops/llmchat_probe_07_gpu_oom/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 8: Prefix caching amortization

- Script: `tests/ops/llmchat_probe_08_prefix_caching.sh`
- Marker: `gpu-nightly`
- Expected defense / recovery: Requires vLLM metrics; asserts prefix cache hit rate >30%.
- Evidence path: `out/llmchat-ops/llmchat_probe_08_prefix_caching/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 9: Session TTL expiry

- Script: `tests/ops/llmchat_probe_09_session_ttl.sh`
- Marker: `compose-nightly`
- Expected defense / recovery: Requires live stack/Redis; expires session keys and checks graceful resume error.
- Evidence path: `out/llmchat-ops/llmchat_probe_09_session_ttl/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 10: Tool-call budget exhaustion

- Script: `tests/ops/llmchat_probe_10_token_budget.sh`
- Marker: `gpu-nightly`
- Expected defense / recovery: Requires real tool calling; expects tool_calls_budget_tripped/wall-clock guard.
- Evidence path: `out/llmchat-ops/llmchat_probe_10_token_budget/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 11: Repeat-call detector

- Script: `tests/ops/llmchat_probe_11_repeat_call.sh`
- Marker: `gpu-nightly`
- Expected defense / recovery: Requires real tool calling; expects repeat_tool_call error.
- Evidence path: `out/llmchat-ops/llmchat_probe_11_repeat_call/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 12: Rolling upgrade

- Script: `tests/ops/llmchat_probe_12_rolling_upgrade.sh`
- Marker: `k8s-nightly destructive`
- Expected defense / recovery: Requires k8s release; rollout restart with active WS sessions and audit verify.
- Evidence path: `out/llmchat-ops/llmchat_probe_12_rolling_upgrade/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 13: HF cache PVC loss

- Script: `tests/ops/llmchat_probe_13_hf_cache_loss.sh`
- Marker: `gpu-nightly destructive`
- Expected defense / recovery: Requires GPU and empty cache volume; records 10-15min recovery.
- Evidence path: `out/llmchat-ops/llmchat_probe_13_hf_cache_loss/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 14: Tier 2 AWQ

- Script: `tests/ops/llmchat_probe_14_tier2_awq.sh`
- Marker: `tier2-manual destructive`
- Expected defense / recovery: Requires RTX 5090/PRO 6000; deploys AWQ and checks >=988 tok/s.
- Evidence path: `out/llmchat-ops/llmchat_probe_14_tier2_awq/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 15: Tier 3 A100

- Script: `tests/ops/llmchat_probe_15_tier3_a100.sh`
- Marker: `tier3-manual destructive`
- Expected defense / recovery: Requires A100; doc caveat statically checked locally, live latency pending.
- Evidence path: `out/llmchat-ops/llmchat_probe_15_tier3_a100/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 16: Concurrent isolation/load leak

- Script: `tests/ops/llmchat_probe_16_concurrent_isolation.sh`
- Marker: `gpu-nightly long-running`
- Expected defense / recovery: Requires real stack + pprof; checks context bleed, RSS/FD/pprof/audit.
- Evidence path: `out/llmchat-ops/llmchat_probe_16_concurrent_isolation/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 17: Graceful shutdown

- Script: `tests/ops/llmchat_probe_17_graceful_shutdown.sh`
- Marker: `compose-nightly destructive`
- Expected defense / recovery: Requires live compose; SIGTERM drains WS sessions and verifies audit chain.
- Evidence path: `out/llmchat-ops/llmchat_probe_17_graceful_shutdown/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

### Probe 18: Redis backpressure

- Script: `tests/ops/llmchat_probe_18_backpressure.sh`
- Marker: `compose-nightly destructive`
- Expected defense / recovery: Requires live compose; pauses Redis and checks bounded memory/recovery.
- Evidence path: `out/llmchat-ops/llmchat_probe_18_backpressure/evidence.txt`
- Current outcome: **BLOCKED** — not run against a real live target in this session.
- Recovery procedure: inspect the evidence file, service logs, vLLM metrics, and audit verify output captured by the script; resolve any P0/P1 product finding; rerun the full harness with `LLMCHAT_OPS_REQUIRE_LIVE=1` before claiming closure.

## Hardware tier reality

- Tier 1 H100: pending live probe 06. No latency or concurrency claim is made from this runbook until p50/p95/p99 and vLLM KV/GPU metrics are captured.
- Tier 2 RTX 5090/PRO 6000 AWQ: pending live probe 14. Do not claim the 988-1207 tok/s budget until probe 14 records a real throughput metric and tool-call eval result.
- Tier 3 A100: probe 15 statically verified `docs/llmchat/helm.md` contains the A100/no-native-FP8/supported-but-slower caveat; live A100 latency remains pending.

## Escalation

- `task-a5d09fad` (HIGH/P0 infra blocker): provision the real GPU/k8s staging matrix so these probes can produce QA-grade live evidence.
- Product P0/P1 tasks should be filed from live probe FAIL evidence, not from local mock skips. Current skips are environment blockers, not product PASS results.

## Adversarial self-review of the harness

- Specific assertions: every live probe checks concrete outcomes (HTTP status, WS error code, frame counts, audit `status: ok`, latency/throughput metrics, pprof artifacts, or bounded RSS/FD drift).
- Fail-closed behavior: `LLMCHAT_OPS_REQUIRE_LIVE=1` turns skips into a nonzero orchestrator exit; destructive probes require explicit opt-in.
- Cleanup: destructive scripts use reconnect/unpause/restore traps where they mutate Docker network state or pause services; HF-cache and rollout probes record restore/remediation evidence.
- Secret handling: scripts require API keys via environment but write only headers/paths/statuses, not raw key values. Probe payloads are operational placeholders, not real attacker hosts.
- Embarrassment check: submitting these skips as PASS would be misleading; this runbook explicitly marks the task BLOCKED pending real staging evidence.
