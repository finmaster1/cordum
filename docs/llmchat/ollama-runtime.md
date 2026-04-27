# Ollama Runtime — CPU-friendly LLM Chat Profile

> **Status**: default for `make dev-up` since 2026-04-27.
> **Plan**: `~/.claude/plans/goofy-tickling-hartmanis.md`.

## What this is

A third inference profile for the Cordum LLM Chat Assistant that runs
without a GPU. The two pre-existing profiles (`llmchat` GPU/FP8 and
`llmchat-cpu` vLLM/AWQ) remain available; this one is **additive**.

| Profile | Inference runtime | Model | Memory floor | Activates with |
|---|---|---|---|---|
| `llmchat-ollama` (default) | Ollama 0.5+ | `qwen2.5-coder:7b-instruct-q4_K_M` | ~5 GB resident | `make dev-up` or `make dev-up-ollama` |
| `llmchat-cpu` | vLLM CPU + AWQ | `Qwen3-Coder-30B-A3B-Instruct-AWQ` | 16-24 GB | `make dev-up-cpu` |
| `llmchat` | vLLM GPU + FP8 | `Qwen3-Coder-30B-A3B-Instruct-FP8` | ~36 GB VRAM (H100) | `make dev-up-gpu` |

## Why Ollama + Qwen2.5-Coder-7B

Selected over the alternatives in `~/.claude/plans/goofy-tickling-hartmanis.md`:

- **Ollama 0.5+** emits OpenAI streaming `delta.tool_calls[]` natively, which
  is what `core/llmchat/provider_openai.go` already consumes — zero parser
  rewrite. Older Ollama tags (<0.5) embed tool calls in the `content` field
  as JSON strings; the chat-loop dispatcher would silently drop them, so we
  pin a 0.5+ floor in `tools/scripts/ollama_pin_digest.sh` and assert the
  contract in `tests/ops/llmchat_probe_19_ollama_cold_start.sh`.
- **Qwen2.5-Coder-7B-Q4_K_M** is Apache-2.0, ships in the same family as
  the GPU profile (Qwen3-Coder-30B-FP8) so prompt patterns and tool-call
  format stay portable, and fits inside an 8 GB Docker host with margin.
  3B variants struggle on multi-tool prompts; 7B is the smallest size
  that reliably chains the 19-tool MCP surface.
- **Llama-3.2-3B**, **Phi-3.5-mini**, **Hermes-3** were rejected for
  license restrictions, untested tool-call quality against this codebase,
  or custom format requiring a parser rewrite. See the plan file for the
  full rejection table.

## Hardware sizing

| Docker memory available | Recommended model | Override |
|---|---|---|
| ≥ 8 GB | `qwen2.5-coder:7b-instruct-q4_K_M` (default) | none — `make dev-up` |
| 5-7 GB | `qwen2.5-coder:3b-instruct-q4_K_M` (smaller) | `LLMCHAT_MODEL=qwen2.5-coder:3b-instruct-q4_K_M make dev-up` |
| < 5 GB | not supported | use the public Cordum Cloud demo, or run on a beefier host |

The 4 GB Docker default that ships with Docker Desktop on macOS / Windows
is **not enough** even for the 3B model under realistic chat load.
Increase Docker Desktop memory to 8 GB before reporting OOM bugs.

## Bring-up

```bash
git clone https://github.com/cordum-io/cordum
cd cordum
cp .env.example .env   # CORDUM_API_KEY auto-generates if blank
make dev-up
```

First boot blocks for **3-5 minutes** on the model pull (single 4.5 GB
download, cached in the `ollama_models` named volume). Subsequent boots
cold-start in ~30 seconds. Check progress:

```bash
docker compose logs -f ollama
docker compose exec ollama ollama list   # qwen2.5-coder:7b-instruct-q4_K_M  4.5 GB
```

Verify the chat is wired correctly:

```bash
curl -s http://127.0.0.1:11434/v1/models | jq '.data[].id'
curl -s http://127.0.0.1:8090/readyz   # llm-chat-ollama
# Then open http://127.0.0.1:8080 — chat icon visible in header.
```

## Switching between profiles

The three profiles are mutually exclusive at runtime; `make dev-down`
before switching:

```bash
make dev-down
make dev-up-gpu      # vLLM + Qwen3-Coder-30B-FP8 (requires NVIDIA Docker runtime)
make dev-down
make dev-up-cpu      # vLLM + Qwen3-Coder-30B-AWQ (requires 16-24 GB RAM)
make dev-down
make dev-up-ollama   # Ollama + Qwen2.5-Coder-7B (this profile, default)
```

The named volumes are kept separate (`qwen_hf_cache` for FP8,
`qwen_cpu_hf_cache` for AWQ, `ollama_models` for Ollama) so switching
does not invalidate the others' caches.

## Switching from vLLM-CPU (the broken-on-4GB-host migration)

If you tried `make dev-up-cpu` on a host with <16 GB and got
`Too large swap space. 4.0 GiB out of the 3.82 GiB total CPU memory`,
the migration is one command — but reset the vLLM cache first; it is
~30 GB and unrelated to Ollama:

```bash
make dev-down
docker volume rm cordum_qwen_cpu_hf_cache  # ~30 GB reclaimed
make dev-up   # = make dev-up-ollama (default)
```

## Tool-call contract

Every Ollama tag is checked against the OpenAI streaming format the
`core/llmchat/provider_openai.go` SSE parser requires:

- Each `data: {…}` SSE frame may carry partial `choices[].delta.tool_calls[]`
- Arguments stream as JSON-string fragments (assembled by the caller)
- Terminal frame has `finish_reason: "tool_calls"`

`tests/ops/llmchat_probe_19_ollama_cold_start.sh` POSTs
`/v1/chat/completions` with one trivial tool and asserts at least one
SSE frame contains `delta.tool_calls`. Run it manually to confirm a new
Ollama tag is safe to pin:

```bash
LLMCHAT_OPS_BACKEND=ollama-cpu LLMCHAT_OPS_LIVE=1 \
  bash tests/ops/llmchat_probe_19_ollama_cold_start.sh
```

If this probe fails after an Ollama bump, **revert the pin** in
`tools/scripts/ollama_pin_digest.sh` — the new tag is silently breaking
tool dispatch for every chat user.

## Troubleshooting

### `ollama` healthcheck stays unhealthy past `start_period: 600s`

The model pull stalled. Check the container logs:

```bash
docker compose logs --tail 100 ollama | grep -E 'pulling|error'
```

Common causes:
- **Slow link** to `registry.ollama.ai`. The pull is one large blob;
  there is no resume protocol. Re-run `make dev-up` to retry.
- **Disk full** on the `ollama_models` volume. Free space and retry.
- **Image digest drift** — the pinned tag was re-pushed without a
  coordinated bump. Run `bash tools/scripts/ollama_pin_digest.sh 0.5.7`
  and update the `image:` line in `docker-compose.yml`.

### Chat replies are garbled / model invents tool names

Q4_K_M quantization on a 7B model has a real quality ceiling. Two knobs:

1. **Lower temperature**: `LLMCHAT_TOOL_TEMPERATURE=0.1` (default 0.3).
   Reduces tool-name hallucination at the cost of less varied prose.
2. **Switch to a larger model** if you have RAM headroom:
   `LLMCHAT_MODEL=qwen2.5-coder:14b-instruct-q4_K_M` (~9 GB resident).
   Update `docker-compose.yml` to bump the `mem_limit` on the `ollama`
   service to `12G` first.

### `llm-chat-ollama` /readyz keeps returning 503

The provider HealthCheck probes `/v1/models`. If Ollama responds 200 but
`llm-chat` /readyz is still 503, the service-account API key is missing.
Check:

```bash
docker compose exec llm-chat-ollama env | grep -E 'CORDUM_API_KEY|LLMCHAT_'
```

`CORDUM_API_KEY` must be non-empty. If `.env` is missing or empty, the
compose `${CORDUM_API_KEY:?CORDUM_API_KEY_not_set}` substitution fails
loud — you'll see it in `docker compose up -d` output before the chat
container starts.

### Concurrent chat sessions block each other

`OLLAMA_NUM_PARALLEL=1` is set deliberately. CPU generation thrashes
under concurrent decoding. For multi-user scenarios, the GPU profile is
the answer; this profile is for community-tier single-user dev work.

## Production posture

This profile is **not** recommended for production. Specifically:

- `OLLAMA_NUM_PARALLEL=1` caps throughput at one generation at a time
- Q4_K_M on 7B has measurable quality regression vs the GPU FP8 profile
- The 4.5 GB resident model on a single-pod Deployment is not
  horizontally scalable; the PVC is `ReadWriteOnce`

For production, set `qwenInference.enabled=true` in Helm values and pair
with at least one H100-class GPU node. The Helm `inference.runtime`
default is `vllm`, so a `helm install` without overrides preserves the
GPU posture.

## Helm-side toggle

```yaml
# values-ollama.yaml — community-tier dev cluster
qwenInference:
  enabled: false
ollamaInference:
  enabled: true
llmChat:
  enabled: true
  externalBaseUrl: ""   # ignored when ollamaInference.enabled=true
```

```bash
helm upgrade --install cordum cordum-helm -f cordum-helm/values.yaml -f values-ollama.yaml
```

The chart auto-points `LLMCHAT_BASE_URL` at the in-cluster Ollama
Service when `ollamaInference.enabled=true` and
`qwenInference.enabled=false`.

## Related

- `~/.claude/plans/goofy-tickling-hartmanis.md` — the plan file Yaron
  approved on 2026-04-27.
- `docs/llmchat/helm.md` — the GPU/FP8 production-tier guide. Refer
  there for hardware-tier sizing and the AWQ/Tier-2 swap.
- `docs/llmchat/supply-chain.md` — the digest-pin upgrade procedure.
  When you bump the Ollama pin, follow the same convention.
- `tests/ops/llmchat_probe_19_ollama_cold_start.sh` — the cold-start
  contract gate. Make sure it passes before promoting a new Ollama tag.
