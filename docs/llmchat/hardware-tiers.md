# LLM Chat Assistant — Hardware tiers

The chat assistant runs Qwen3-Coder-30B locally via vLLM. GPU choice
sets your concurrent-session budget and your inference throughput.
Pick the tier that matches your fleet.

> **Interim CPU QA mode (2026-04-26):** Per Yaron's direction to
> "switch to CPU LLM model for now", Cordum can run a dedicated CPU vLLM
> stack for QA and air-gapped low-throughput evaluation while the GPU/k8s
> staging matrix is deferred. This does **not** change the production
> recommendation: production deployments still require GPU-backed vLLM.
> Ops probes 06, 07, 12, 13, 14, and 15 remain deferred to the GPU/k8s
> follow-up; probes 01-05, 08-11, and 16-18 are the CPU-mode evidence
> set for `task-a5d09fad`.

| Tier | GPU | VRAM | Quant | Concurrent sessions | Status | Cost (illustrative) |
|---|---|---|---|---|---|---|
| **Tier 1 — recommended production** | H100 | 80 GB | FP8 native | 16 (comfortable) → 32 (stress ceiling) | Production | AWS p5.xlarge ≈ $5/hr |
| **Tier 2 — smaller deployments** | RTX 5090 / PRO 6000 | 24-48 GB | INT4 AWQ (`QuantTrio/Qwen3-Coder-30B-A3B-Instruct-AWQ`) | 4-8 | **Design-partner / preview** | Self-hosted |
| **Tier 3 — supported, slower** | A100 | 80 GB | INT8 / BF16 fallback | 8-16 | Production | AWS p4d.24xlarge |
| **Unsupported** | <24 GB VRAM, CPU | — | — | 0 | — | — |

## Tier 1 — H100 80GB (recommended production)

- **Model:** `Qwen/Qwen3-Coder-30B-A3B-Instruct-FP8` (default in
  `cordum-helm/values.yaml.qwenInference.model`).
- **VRAM budget:** FP8 weights ~34-36 GB at runtime; with
  `--gpu-memory-utilization 0.9` that leaves ~36-37 GB for KV cache.
- **Concurrent-session target:** 16 comfortable, 32 stress ceiling.
- **Throughput:** native FP8 tensor cores → fast prefill + decode.
- **Cost:** AWS `p5.xlarge` (~$5/hr on-demand) is the baseline
  reference. Cheaper alternatives exist (RunPod, Lambda Labs) at
  similar throughput per dollar.

This is the path we recommend for any production deployment with
real concurrent users. Tier 2 and Tier 3 exist to unblock smaller
fleets or brownfield A100 deployments — not as primary targets.

## Tier 2 — RTX 5090 / PRO 6000 (design-partner preview)

**Status: design-partner / preview.** This tier is in active
customer trials but is not yet production-validated by the Cordum
team. Customers running this tier should expect rougher edges than
Tier 1 + 3 (GPU driver compatibility, longer cold-start times,
narrower concurrent-session budgets). File issues against the
LLM Chat epic if you hit problems.

- **Model:** swap to `QuantTrio/Qwen3-Coder-30B-A3B-Instruct-AWQ`.
  AWQ is INT4 weight-only quantization, ~16-18 GB at runtime,
  designed for Ada/Blackwell consumer cards that lack native FP8
  tensor cores.
- **VRAM budget:** AWQ weights ~16-18 GB; `--gpu-memory-utilization
  0.9` on a 24 GB RTX 5090 leaves only ~3-5 GB headroom for KV
  cache. PRO 6000 (48 GB) is more comfortable.
- **Concurrent-session target:** 4-8 sessions on RTX 5090; 8-16 on
  PRO 6000. Push above this and KV cache pressure tanks throughput.
- **Throughput:** 988-1207 tok/s aggregate with FP8 KV cache + AWQ
  weights (measured on RTX 5090; Blackwell architecture). Tool-call
  accuracy parity with Tier 1.
- **Swap recipe** (compose):
  ```bash
  # In docker-compose.dev.yml or values.yaml override
  qwen-inference:
    command:
      - --model
      - QuantTrio/Qwen3-Coder-30B-A3B-Instruct-AWQ
      # ...rest of flags identical to Tier 1
  ```
- **Swap recipe** (helm):
  ```bash
  helm upgrade cordum cordum-helm/ \
    --set qwenInference.model=QuantTrio/Qwen3-Coder-30B-A3B-Instruct-AWQ
  ```

## Tier 3 — A100 80GB (supported, slower)

A100 has no native FP8 tensor cores; vLLM falls back to INT8 or
BF16. Throughput is meaningfully lower than H100, especially at
high concurrency. The chat assistant works correctly — it's just
slower.

- **Model:** `Qwen/Qwen3-Coder-30B-A3B-Instruct-FP8` works
  (vLLM dequantizes at load time) OR drop the `-FP8` suffix and
  use BF16. BF16 is more memory-hungry (~64 GB at 128k context)
  but bypasses the FP8 dequant overhead.
- **VRAM budget:** BF16 weights ~64 GB at runtime; KV cache fights
  for the remaining ~12 GB → tight ceiling.
- **Concurrent-session target:** 8-16 sessions. Reduce
  `--max-model-len` to 65536 if you need more concurrency at the
  cost of long-tool-conversation capacity.
- **Throughput:** ~50% of H100. Acceptable for ops teams; not
  recommended for chat-heavy workloads.
- **Cost:** AWS `p4d.24xlarge` is the standard reference. Most
  brownfield A100 fleets will already have hardware allocated.

## Unsupported

- **GPUs with <24 GB VRAM.** AWQ at INT4 is the smallest model we
  ship and even that needs ~16-18 GB plus headroom. Cards smaller
  than 24 GB will OOM at startup or thrash KV cache.
- **CPU inference.** Qwen3-Coder-30B is impractical to serve from
  CPU — single-token latency is multiple seconds. Not a supported
  path.

## Picking a tier

| You have | Use |
|---|---|
| 1+ H100 nodes | Tier 1 |
| RTX 5090 / PRO 6000 dev box | Tier 2 (preview) |
| A100 fleet | Tier 3 |
| L4 / T4 / RTX 4070 | Not supported — use Tier 2 hardware or external vLLM |
| No GPU | External vLLM mode (`LLMCHAT_BASE_URL`) — see `helm.md` |

## Multi-GPU + scaling

Single-GPU is the supported topology for v1. Multi-GPU with vLLM
tensor parallelism is on the roadmap but not yet validated end-to-end
with the chat-assistant agent loop. If you need >32 concurrent
sessions on a single host, file a feature request.

Horizontal scale (multiple `cordum-llm-chat` pods sharing one
`qwen-inference` backend) works today — set `llmChat.replicas` in
the helm chart. The Redis session store is shared across replicas.
