# LLM Chat model-change protocol

The chat assistant is informational-only. Model changes must be evaluated for answer quality, latency, refusal behavior, and secret-redaction safety against the local knowledge pack.

## Backends

- Default: `ollama-cpu` with Qwen2.5-Coder-3B.
- Opt-in GPU: `vllm-gpu` with Qwen3-Coder-30B.

## Change process

1. Stage the new model in a non-production environment.
2. Run the chat eval harness against `EVAL_LLMCHAT_URL` and the selected backend URL/model.
3. Compare answer-quality summaries and latency against the previous baseline.
4. Update Compose/Helm values in the same PR as the evaluation evidence.
5. Deploy, monitor `/readyz`, `/metrics`, error rate, and chat-session audit events for 24 hours.

For vLLM changes, keep the image pin and supply-chain scan workflow intact. For Ollama changes, update the pin via the Ollama pin script and verify the OpenAI-compatible streaming shape before promotion.
