---
sidebar_position: 50
title: LLM chat knowledge pack
---

# LLM chat knowledge pack

The `cordum-llm-chat` service injects two locally-sourced knowledge blobs into the LLM's system prompt at boot time, so the chat assistant can answer questions grounded in Cordum's full API surface and operator-facing documentation. Without this pack the LLM only sees the MCP `tools/list` catalog — useful but narrow.

The pack runs as a **substituter pipeline** that fills `{{api_summary}}` and `{{cordum_io_summary}}` placeholders inside the system prompt. Both substituters read **local files only** — there are no HTTP fetches to `cordum.io` at runtime. This matches the epic's zero-external-egress posture: tenant data never leaves the cluster, and the chat assistant never makes outbound calls except to the local vLLM and the Cordum gateway.

## Configuration

All knobs are read from environment variables at process start; invalid values fall back to the documented defaults with a `slog.Warn` so the operator notices but the service stays operational.

| Env var | Default | Effect |
|---|---|---|
| `LLMCHAT_KNOWLEDGE_PACK_ENABLED` | `true` | Master switch. When `false`, the placeholders pass through unchanged via the phase-4 prompt loader. Useful for debugging or when running against a stripped image without the curated content mounted. |
| `LLMCHAT_KNOWLEDGE_PACK_BUDGET` | `65536` | Per-blob token cap. The substituter truncates output beyond `budget * 4` bytes (≈ 4 bytes/token rule of thumb) and emits a `budget_truncated` log line. **Never disable this** — exceeding the LLM's context window pushes real conversation messages out. |
| `LLMCHAT_OPENAPI_PATH` | `/etc/cordum-llm-chat/openapi.yaml` | Path to the OpenAPI 3 spec. Compose mounts this from `cordum/docs/api/openapi/cordum-api.yaml`; Helm/Kustomize via a `ConfigMap`. |
| `LLMCHAT_CORDUM_IO_PATH` | `/etc/cordum-llm-chat/cordum-io` | Directory (or glob) of curated markdown files. Compose mounts this from `cordum/docs-site/docs/`; Helm/Kustomize via a `ConfigMap`. |

## Mount points

### Docker Compose

Add the bind-mounts and env vars to the `cordum-llm-chat` service entry once phase-7 packaging adds the service stanza:

```yaml
services:
  cordum-llm-chat:
    # ...
    volumes:
      - ./docs/api/openapi/cordum-api.yaml:/etc/cordum-llm-chat/openapi.yaml:ro
      - ./docs-site/docs:/etc/cordum-llm-chat/cordum-io:ro
    environment:
      LLMCHAT_KNOWLEDGE_PACK_ENABLED: "true"
      LLMCHAT_KNOWLEDGE_PACK_BUDGET: "65536"
      LLMCHAT_OPENAPI_PATH: /etc/cordum-llm-chat/openapi.yaml
      LLMCHAT_CORDUM_IO_PATH: /etc/cordum-llm-chat/cordum-io
```

### Kubernetes (Kustomize)

Use `configMapGenerator` so kustomize re-reads source files at apply-time and the pod rolls when content changes (the generator hash-suffixes the ConfigMap name):

```yaml
configMapGenerator:
  - name: cordum-llm-chat-knowledge
    files:
      - openapi.yaml=docs/api/openapi/cordum-api.yaml
      - cordum-io/concepts/architecture.md=docs-site/docs/concepts/architecture.md
      # ... one entry per curated file ...
```

Mount on the deployment:

```yaml
volumeMounts:
  - name: knowledge
    mountPath: /etc/cordum-llm-chat
    readOnly: true
volumes:
  - name: knowledge
    configMap:
      name: cordum-llm-chat-knowledge
```

Apply with `kubectl apply -k deploy/k8s/local` — kustomize regenerates the ConfigMap from the latest source files and triggers a rolling restart automatically.

## Curated subset

The cordum.io substituter walks the mounted directory recursively for `*.md` and `*.mdx` files. The default mount surfaces these subdirectories:

- **`concepts/`** — architecture, audit, output policy, safety kernel, agent identity, etc. The "what is Cordum" content the LLM needs to explain core primitives.
- **`getting-started/`** — install, quickstart, MCP-with-claude/cursor/vscode walkthroughs. Operator-facing onboarding the LLM can quote from.
- **`operations/`** — troubleshooting, FAQ, configuration, this very file. The runbook content the LLM cites when explaining a denial or failure.

Excluded by default:

- **`api-reference/`** — the `{{api_summary}}` substituter already produces a structured digest from the OpenAPI spec. Including the prose API reference would duplicate content and waste budget.
- **`tutorials/`** — long-form. Better as live links than baked into the prompt.

If you mount a different directory tree, the substituter walks whatever it finds — there is no allowlist beyond the file extension.

## Refresh

The substituter caches each blob for 5 minutes by default (`KnowledgePackLoader` TTL). On Linux/macOS, sending `SIGHUP` to the process invalidates the cache immediately:

```bash
kill -HUP $(pidof cordum-llm-chat)
```

The next chat turn will re-read the source files. On Windows, restart the service — there is no `SIGHUP` signal class.

## Budget tuning

The default budget of 65536 tokens (≈ 256 KB UTF-8) per blob is sized for a 128K-context model with two substituted blobs leaving ample room for the conversation transcript. Considerations when raising or lowering:

- **Raising too high blows the context window.** Two 128K-token blobs would consume the entire context, leaving no room for messages or tool results.
- **Lowering trades knowledge for headroom.** A 16K-token cap shrinks the API summary to roughly the first 60 endpoints; users get less detailed answers about lesser-used routes.
- **Curated content size** is the practical floor — if `concepts/` alone is 80 KB, `LLMCHAT_KNOWLEDGE_PACK_BUDGET=16384` (≈ 64 KB) will truncate it.

Inspect with `wc -c $(find /etc/cordum-llm-chat/cordum-io -name '*.md')` to see the raw size before tuning.

## Logs

The substituter pipeline emits the following structured `slog` events:

| Event | Fields | When |
|---|---|---|
| `budget_truncated` | `token`, `original_bytes`, `budget_bytes` | A blob exceeds the per-blob budget and gets truncated. |
| `openapi_file_missing` | `path` | The OpenAPI spec file does not exist. The `{{api_summary}}` placeholder is replaced with an empty string. |
| `openapi_read_failed` | `path`, `err` | Permission or IO error reading the spec. Soft-fail. |
| `openapi_parse_failed` | `path`, `err` | YAML decode error. Soft-fail. |
| `openapi_no_paths` | `path` | Spec parsed but contains zero `paths:` entries. Soft-fail. |
| `cordum_io_no_files` | `path` | The cordum.io directory matches zero `.md`/`.mdx` files. Soft-fail. |
| `cordum_io_file_skipped` | `path`, `err` | A single curated file is unreadable (permissions, etc.). Skip and continue with the rest. |
| `cordum_io_expand_failed` | `path`, `err` | Path-traversal rejected or glob expansion errored. Soft-fail. |
| `knowledge_pack_warm_failed` | `err` | The boot-time precompute failed. The service starts anyway; subsequent chat turns retry the substituters lazily. |
| `cache_refreshed` | `token` | One token's cached value invalidated by `RefreshAll` (e.g. on `SIGHUP`). |
| `sighup_received` | (none) | A `SIGHUP` signal was received. |

## Audit + dashboard surfaces

This feature is config-only — it changes what the LLM **sees** in its system prompt, not what it **does**. There are no new audit events (the existing `mcp.tool_invocation` audit pipeline already records every tool call regardless of prompt content). There is no dashboard surface (operators tune the pack via env vars and curated content edits).
