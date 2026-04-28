# Ollama Runtime — Default CPU LLM Chat Backend

Ollama + Qwen2.5-Coder-3B is the production default for the informational-only chat assistant. It runs locally on CPU, avoids the GPU staging dependency, and keeps tenant data inside the deployment.

| Backend | Runtime | Model | Host budget | How to start |
|---|---|---|---:|---|
| `ollama-cpu` (default) | Ollama | `qwen2.5-coder:3b-instruct-q4_K_M-ctx32k` in packaging | 4 GB+ RAM | `docker compose up -d` / Helm default |
| `vllm-gpu` (opt-in) | vLLM | `qwen3-coder` served from Qwen3-Coder-30B | ~40 GB VRAM | `docker compose --profile gpu up -d` / `--set inference.backend=vllm-gpu` |

## Why Ollama + Qwen2.5-Coder-3B

- Informational Q&A grounded in Cordum docs/API does not need the previous 30B tool-calling stack.
- The 3B Q4 model is small enough for 4 GB Docker/Kubernetes hosts.
- Ollama exposes an OpenAI-compatible API, so `core/llmchat` does not need a second provider implementation.
- The backend is local by default: no runtime internet retrieval and no hosted model egress.

## Compose behavior

Default Compose starts `ollama` and `llm-chat` without profiles. The Ollama container pulls the base 3B model and creates a ctx32k local tag so the prompt can include local knowledge context.

```bash
docker compose up -d
curl -s http://127.0.0.1:11434/api/tags
curl -s http://127.0.0.1:8090/readyz
```

## Helm behavior

Default values render `ollama-inference`:

```yaml
inference:
  backend: ollama-cpu
ollamaInference:
  enabled: true
  model: qwen2.5-coder:3b-instruct-q4_K_M
  servedModelName: qwen2.5-coder:3b-instruct-q4_K_M-ctx32k
  contextLength: 32768
```

## Switching to vLLM

Use vLLM only when a GPU-equipped customer explicitly chooses the larger model:

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
