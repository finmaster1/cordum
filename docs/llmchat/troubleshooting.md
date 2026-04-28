# LLM Chat troubleshooting

## Chat button is hidden

The dashboard hides the chat button when the `LLMChatAssistant` entitlement is disabled or `/api/v1/chat/healthz` is not healthy.

Checks:

```bash
curl -s http://127.0.0.1:8090/healthz
curl -s http://127.0.0.1:8090/readyz
```

## Ollama is still pulling the model

First start may take several minutes on a cold `ollama_models` volume.

```bash
docker compose logs -f ollama
curl -s http://127.0.0.1:11434/api/tags
```

The default served model is `qwen2.5-coder:3b-instruct-q4_K_M-ctx32k`.

## Invalid backend value

`cordum-llm-chat` fails closed for unsupported backend values:

```text
unsupported LLMCHAT_OPS_BACKEND=<value>; allowed: ollama-cpu, vllm-gpu
```

Use `ollama-cpu` for the default CPU backend or `vllm-gpu` for the explicit GPU backend.

## Opt-in vLLM is unreachable

When using the GPU path, make sure all three env vars align:

```bash
LLMCHAT_OPS_BACKEND=vllm-gpu
LLMCHAT_BASE_URL=http://qwen-inference:8000/v1
LLMCHAT_MODEL=qwen3-coder
```

Then verify:

```bash
docker compose --profile gpu ps qwen-inference
curl -s http://127.0.0.1:8000/v1/models
curl -s http://127.0.0.1:8090/readyz
```

## Secret appears in a transcript or log

Treat as a security incident. Disable chat, preserve logs, rotate the exposed credential, and file a P0. The assistant should treat API keys, passwords, bearer tokens, JWTs, kubeconfigs, private keys, and certificates as redacted.
