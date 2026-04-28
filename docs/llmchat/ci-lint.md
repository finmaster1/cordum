# LLM Chat inference config lint

The vLLM config lint scripts protect the **opt-in** GPU backend from unsafe drift. Production uses Ollama/Qwen2.5-Coder-3B unless an operator explicitly chooses GPU.

- `tools/scripts/vllm_config_lint.sh` checks the Compose vLLM service when it is present under the `gpu` profile.
- `tools/scripts/vllm_helm_lint.sh` renders Helm with `--set inference.backend=vllm-gpu` before checking the vLLM deployment.

Current enforced rules for the opt-in vLLM path:

| Rule | Why |
|---|---|
| model matches the selected tier | Avoids accidentally serving an unsupported or oversized model. |
| `--max-model-len 131072` | Preserves the validated Qwen3-Coder context budget for GPU users. |
| `--kv-cache-dtype fp8` | Keeps the H100 memory budget within target. |
| `--enable-prefix-caching` | Keeps multi-turn informational Q&A efficient. |
| `--disable-log-requests` | Prevents prompt/request bodies from being logged. |
| loopback/ClusterIP exposure only | Keeps inference private to the local host/cluster. |

Informational-only chat no longer requires vLLM tool-call parser flags. Legacy parser names such as `hermes` and `qwen3_coder` remain forbidden so stale tool-calling overrides cannot drift back into deployment manifests.
