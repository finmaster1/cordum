# LLM Chat provider configuration

`cordum-llm-chat` reads provider configuration from environment variables at startup. Invalid backend selection fails closed so operators see a clear misconfiguration instead of a silent fallback.

## Backend selection

| Env var | Default | Description |
|---|---:|---|
| `LLMCHAT_OPS_BACKEND` | `ollama-cpu` | Backend identity. Allowed values: `ollama-cpu`, `vllm-gpu`. Invalid values abort startup with `unsupported LLMCHAT_OPS_BACKEND=<value>; allowed: ollama-cpu, vllm-gpu`. |
| `LLMCHAT_PROVIDER` | `openai` | Provider implementation. The local Ollama and vLLM paths both use the OpenAI-compatible API. |
| `LLMCHAT_BASE_URL` | backend-specific | OpenAI-compatible `/v1` root. Default is `http://ollama:11434/v1` for `ollama-cpu`, `http://qwen-inference:8000/v1` for `vllm-gpu`. |
| `LLMCHAT_MODEL` | backend-specific | Default is `qwen2.5-coder:3b-instruct-q4_K_M` in the binary; Compose/Helm set the ctx32k local Ollama tag. vLLM served model name is `qwen3-coder`. |
| `LLMCHAT_API_KEY` | empty | Optional bearer token for external/shared inference endpoints. Local Ollama/vLLM do not require it. |

## Informational-answer sampling

| Env var | Default | Description |
|---|---:|---|
| `LLMCHAT_SUMMARY_TEMPERATURE` | `0.7` | Response temperature for the single informational-answer path. |
| `LLMCHAT_SUMMARY_TOP_P` | `0.8` | Top-p for the informational-answer path. |
| `LLMCHAT_MAX_WALL_CLOCK_PER_TURN` | `60s` | Per-turn wall-clock guardrail. Compose raises this for local CPU inference. |
| `LLMCHAT_MAX_ASSISTANT_BYTES` | `32768` | Maximum assistant output bytes per turn. |

Tool-calling knobs from the original design are retired for chat. Mutations remain in the dashboard, CLI, and normal API workflows.

## Knowledge pack

| Env var | Default | Description |
|---|---:|---|
| `LLMCHAT_KNOWLEDGE_PACK_ENABLED` | `true` | Enables local OpenAPI/docs substitution into the prompt. |
| `LLMCHAT_KNOWLEDGE_PACK_BUDGET` | `65536` | Per-blob token budget used by the knowledge-pack loader. |
| `LLMCHAT_OPENAPI_PATH` | `/etc/cordum-llm-chat/openapi.yaml` | Local OpenAPI spec path. |
| `LLMCHAT_CORDUM_IO_PATH` | `/etc/cordum-llm-chat/cordum-io` | Local curated docs path. |

## Examples

Default Ollama CPU:

```bash
LLMCHAT_OPS_BACKEND=ollama-cpu \
LLMCHAT_BASE_URL=http://ollama:11434/v1 \
LLMCHAT_MODEL=qwen2.5-coder:3b-instruct-q4_K_M-ctx32k
```

Opt-in vLLM GPU:

```bash
LLMCHAT_OPS_BACKEND=vllm-gpu \
LLMCHAT_BASE_URL=http://qwen-inference:8000/v1 \
LLMCHAT_MODEL=qwen3-coder
```
