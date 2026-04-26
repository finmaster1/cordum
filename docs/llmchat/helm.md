# LLM Chat Assistant — Helm + Compose deployment

The chat assistant ships as two new services on top of the existing
Cordum stack:

- `llm-chat` — the chat-loop service (Go, ports 8090 HTTP)
- `qwen-inference` — vLLM serving Qwen3-Coder-30B-A3B-Instruct-FP8 (port 8000, OpenAI-compatible)

Both are gated behind feature flags. Default-enabled in the Helm chart;
opt-in via the `llmchat` profile in Docker Compose.

## Docker Compose

The default `make dev-up` flow does NOT bring up the chat assistant —
the `qwen-inference` service requires an NVIDIA GPU and `make dev-up`
runs on every contributor's laptop. Bring it up explicitly:

```bash
COMPOSE_PROFILES=llmchat docker compose up -d
# or
docker compose --profile llmchat up -d
```

This starts `qwen-inference` (loopback-bound on `127.0.0.1:8000:8000`)
and `llm-chat` (HTTP on `:8090`). The first start pulls ~30GB of FP8
weights into the named volume `qwen_hf_cache` and takes 3-5 minutes
to become healthy; subsequent restarts are seconds.

The Compose `qwen-inference` healthcheck has `start_period: 300s` to
cover the cold load. `llm-chat` uses `depends_on.qwen-inference.condition: service_started`
(NOT `service_healthy`) so the chat container starts immediately and
its `/readyz` reports not-ready until vLLM is up — that gates the
dashboard chat button automatically.

## Helm

Install with the GPU-enabled defaults:

```bash
helm install cordum cordum-helm/ \
  --set secrets.apiKey=$(openssl rand -hex 32) \
  --set redis.auth.password=$(openssl rand -hex 32)
```

Both `llmChat.enabled` and `qwenInference.enabled` default to `true`.
The `qwen-inference` Pod requires an NVIDIA GPU node; install the
[nvidia-device-plugin](https://github.com/NVIDIA/k8s-device-plugin)
DaemonSet on your GPU nodes first, and label the nodes to match
`qwenInference.gpu.nodeSelector` (default: `accelerator: nvidia`).

### External vLLM mode

If you have an existing vLLM cluster (or a separate inference team),
point `llm-chat` at it and disable the bundled vLLM:

```bash
helm install cordum cordum-helm/ \
  --set llmChat.externalBaseUrl=https://vllm.internal/v1 \
  --set qwenInference.enabled=false \
  ...
```

The dashboard chat button still gates on `/api/v1/chat/healthz`, so
the UX matches the bundled-vLLM path.

### Disable entirely

For deployments that don't want the chat assistant at all (Community
tier, regulated environments, etc.):

```bash
helm install cordum cordum-helm/ \
  --set llmChat.enabled=false \
  --set qwenInference.enabled=false \
  ...
```

The license entitlement (`LLMChatAssistant` in `core/licensing/license.go`)
is the authoritative gate at the API layer regardless — Compose/Helm
flags are convenience for skipping the resource overhead.

## Hardware tiers

Pick the tier matching your GPU. The default values target Tier 1.

### Tier 1 — H100 80GB (recommended)

- **Model:** `Qwen/Qwen3-Coder-30B-A3B-Instruct-FP8` (default)
- FP8 weights are ~34-36GB at runtime
- `--gpu-memory-utilization 0.9` leaves ~36-37GB headroom for KV cache
- Comfortable concurrency: **16 concurrent copilot sessions**
- Stress ceiling: **32 concurrent**
- AWS instance: `p5.xlarge` or equivalent

### Tier 2 — RTX 5090 / PRO 6000 (Ada/Blackwell consumer cards)

These cards lack native FP8 tensor cores at the H100 level. Swap the
checkpoint to the AWQ quant:

```bash
helm install cordum cordum-helm/ \
  --set qwenInference.model=QuantTrio/Qwen3-Coder-30B-A3B-Instruct-AWQ \
  ...
```

- Throughput budget: **988-1207 tok/s** with FP8 KV cache enabled
- Tool-calling parity with Tier 1 (qwen3_xml parser unchanged)

### Tier 3 — A100 80GB

A100 has no native FP8 tensor cores; vLLM falls back to INT8 / BF16.
The chat assistant is **supported but slower** on A100. For new
deployments where Tier 1 is available, prefer it. For brownfield A100
fleets, the chat assistant works at degraded latency — exact
throughput depends on workload mix.

### Unsupported

- GPUs with <24GB VRAM (FP8 weights don't fit)
- CPU-only inference (Qwen3-Coder-30B is impractical to serve from CPU)

## Parser pinning — DO NOT CHANGE

The vLLM `--tool-call-parser qwen3_xml` flag is **load-bearing**. The
upstream Qwen model card historically recommended `qwen3_coder`, but
that parser produces infinite `!!!!!!!!` token streams on long
tool-heavy conversations. vLLM's own docs supersede the model card
here. `qwen3_xml` is what you want.

If a future contributor opens a PR changing the parser, that PR is
wrong unless it also changes `cordum-helm/values.yaml`'s
`qwenInference.toolCallParser` AND ships a regression test against
the long-tool-conversation failure mode. Phase 10 of this epic
(task-d8000ffb) ships a CI lint that fails any PR mentioning
`qwen3_coder` or `hermes` in the compose / helm files.

## Network exposure boundary

Two-layer defense for the inference endpoint:

- **Compose:** `qwen-inference` ports map `127.0.0.1:8000:8000` —
  loopback only on the host. Other containers reach vLLM via the
  Docker network's intra-network DNS (`http://qwen-inference:8000/v1`).
- **Helm:** `Service.type: ClusterIP` only. Never `LoadBalancer` or
  `NodePort`. The `llm-chat` Pod is the sole legitimate client.
  Inside the container, vLLM binds `0.0.0.0:8000` (so the pod IP is
  reachable from the Service); the Kubernetes Service boundary is the
  defense, not the bind address.

External exposure of the inference endpoint is a P0 finding. The
security review (task-6cda949c) probes for both forms in Compose and
Helm-rendered output.

## HF weight cache

The chart provisions a `PersistentVolumeClaim` for `/root/.cache/huggingface`
when `qwenInference.hfCache.enabled=true` (default, 50Gi). Re-pulling
30GB of FP8 weights on every Pod restart is unacceptable; the PVC
keeps the cache across rollouts.

For ephemeral test deployments where you don't mind the cold-load
penalty, set `qwenInference.hfCache.enabled=false` to fall back to
an `emptyDir`.

## Audit and observability

The packaging itself emits no audit events. The chat-session lifecycle
(`chat.session_started`, `chat.session_closed`, `chat.bootstrap_registered`)
is implemented in phase 5 (task-d47a47ea). Tool invocations through the
MCP path emit `mcp.tool_invocation` events via the existing
`ToolInvocationAuditor`.
