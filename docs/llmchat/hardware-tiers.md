# LLM Chat Hardware Tiers

The default chat assistant backend is CPU-friendly. GPU tiers are optional and only needed for customers who explicitly choose the larger vLLM model.

| Tier | Backend | Hardware | Model | Status |
|---|---|---|---|---|
| Default | `ollama-cpu` | Any Docker/Kubernetes host with 4 GB+ RAM | Qwen2.5-Coder-3B Q4 ctx32k | Production default |
| GPU Tier 1 | `vllm-gpu` | H100-class GPU, ~40 GB VRAM budget | Qwen3-Coder-30B FP8 | Opt-in |
| GPU Tier 2 | `vllm-gpu` | RTX 5090 / PRO 6000 class, 24–48 GB VRAM | Qwen3-Coder-30B AWQ swap | Design-partner / preview |
| GPU Tier 3 | `vllm-gpu` | A100, no native FP8 | FP8 path supported but slower/fallback behavior expected | Opt-in |

## Default CPU install

```bash
docker compose up -d
# or
helm upgrade --install cordum ./cordum-helm \
  --set secrets.apiKey=<key> \
  --set redis.auth.password=<password>
```

## Opt-in GPU install

```bash
LLMCHAT_OPS_BACKEND=vllm-gpu \
LLMCHAT_BASE_URL=http://qwen-inference:8000/v1 \
LLMCHAT_MODEL=qwen3-coder \
docker compose --profile gpu up -d
```

```bash
helm upgrade --install cordum ./cordum-helm \
  --set inference.backend=vllm-gpu \
  --set qwenInference.gpu.nodeSelector.accelerator=nvidia
```

The vLLM path remains local and private; keep the inference service loopback-only in Compose and ClusterIP-only in Kubernetes.
