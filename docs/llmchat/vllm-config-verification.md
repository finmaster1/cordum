# vLLM opt-in configuration verification

Historical note: the original chat design used vLLM parser/tool-call flags. After the 2026-04-28 informational-only rescope, those flags are retired for chat. This page now verifies only the retained opt-in vLLM deployment invariants.

## Retained checks

| Surface | Required value |
|---|---|
| Compose profile | `qwen-inference` is under `profiles: [gpu]` |
| Image | pinned `vllm/vllm-openai@sha256:4801151759655c57606c844662e5213403c032a62d149c7ce61d615759a821ef` |
| Model | `Qwen/Qwen3-Coder-30B-A3B-Instruct-FP8` unless an explicit tier override is tested |
| Served name | `qwen3-coder` |
| Context | `--max-model-len 131072` |
| KV cache | `--kv-cache-dtype fp8` |
| Prefix cache | `--enable-prefix-caching` |
| Request logging | `--disable-log-requests` |
| Exposure | Compose host port is `127.0.0.1:8000:8000`; Helm Service is `ClusterIP` |

## Commands

```bash
docker compose -f docker-compose.yml --profile gpu config -q
helm template cordum cordum-helm \
  --set secrets.apiKey=dummy \
  --set redis.auth.password=dummy \
  --set inference.backend=vllm-gpu
bash tools/scripts/vllm_config_lint.sh docker-compose.yml docker-compose.release.yml
```
