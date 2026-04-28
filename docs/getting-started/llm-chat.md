# LLM Chat Assistant quickstart

The chat assistant is informational-only: it answers questions about Cordum docs and APIs, but does not submit jobs, approve work, trigger workflows, or mutate state.

## Default local backend

No GPU is required. Use any Docker host with 4 GB+ RAM:

```bash
docker compose up -d
```

The default starts `ollama` with Qwen2.5-Coder-3B plus `llm-chat` on port 8090.

## Optional GPU backend

For GPU-equipped customers wanting the larger vLLM model:

```bash
LLMCHAT_OPS_BACKEND=vllm-gpu \
LLMCHAT_BASE_URL=http://qwen-inference:8000/v1 \
LLMCHAT_MODEL=qwen3-coder \
docker compose --profile gpu up -d
```

Helm equivalent:

```bash
helm upgrade --install cordum ./cordum-helm \
  --set inference.backend=vllm-gpu
```

This path requires an NVIDIA GPU node (H100 preferred; A100 supported with lower throughput) and roughly a 40 GB VRAM budget for the default FP8 model.
