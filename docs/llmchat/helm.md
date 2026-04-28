# LLM Chat Assistant — Helm + Compose deployment

Cordum ships the LLM chat assistant as a local, zero-egress service. The default backend is **Ollama + Qwen2.5-Coder-3B** on CPU. vLLM + Qwen3-Coder-30B is preserved as an explicit GPU opt-in.

## Docker Compose default

Plain Compose starts the default informational chat path:

```bash
export CORDUM_API_KEY=$(openssl rand -hex 32)
export REDIS_PASSWORD=$(openssl rand -hex 16)
docker compose up -d
```

Default services include:

- `ollama` — local OpenAI-compatible inference at `127.0.0.1:11434` on the host and `http://ollama:11434/v1` inside Compose.
- `llm-chat` — `cordum-llm-chat` HTTP service on `:8090`, pointed at Ollama by default.

The default path does not pull or start the vLLM image.

## Compose opt-in vLLM GPU backend

For GPU-equipped customers wanting the larger model:

```bash
export LLMCHAT_OPS_BACKEND=vllm-gpu
export LLMCHAT_BASE_URL=http://qwen-inference:8000/v1
export LLMCHAT_MODEL=qwen3-coder
docker compose --profile gpu up -d
```

The `qwen-inference` service remains pinned to the vLLM v0.16.0 digest and binds its host port to `127.0.0.1:8000:8000`. The container listens on the bridge interface so `llm-chat` can reach it by Docker DNS; the host exposure remains loopback-only.

## Development override

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
```

The dev override replaces the default Ollama service with a small local OpenAI-compatible Python mock. This keeps dashboard/chat health checks testable without downloading model weights.

## Helm default

The Helm chart defaults to CPU-local Ollama:

```bash
helm upgrade --install cordum ./cordum-helm \
  -n cordum --create-namespace \
  --set secrets.apiKey=$(openssl rand -hex 32) \
  --set redis.auth.password=$(openssl rand -hex 32)
```

Default value:

```yaml
inference:
  backend: ollama-cpu
```

Default render includes `ollama-inference` and does not render `qwen-inference`.

## Helm opt-in vLLM GPU backend

```bash
helm upgrade --install cordum ./cordum-helm \
  -n cordum --create-namespace \
  --set secrets.apiKey=$(openssl rand -hex 32) \
  --set redis.auth.password=$(openssl rand -hex 32) \
  --set inference.backend=vllm-gpu \
  --set qwenInference.gpu.nodeSelector.accelerator=nvidia
```

GPU requirements:

- NVIDIA GPU node with the NVIDIA device plugin installed.
- H100-class hardware is the primary target for the FP8 model; A100 is supported but slower/no native FP8. Expect ~40 GB memory budget for the Qwen3-Coder-30B FP8 path.
- `qwen-inference` Service type remains `ClusterIP`; do not expose inference via `NodePort` or `LoadBalancer`.

## External endpoint override

External OpenAI-compatible endpoints are opt-in only. Set `LLMCHAT_BASE_URL` in Compose or `llmChat.externalBaseUrl` in Helm intentionally, and document the egress/security review for that deployment.
