---
sidebar_position: 1
title: Quickstart
slug: /
---

# Quickstart

Get Cordum running locally in under 5 minutes (plus a one-time ~5 minute LLM model pull on first boot).

## Prerequisites

- Docker and Docker Compose, with **at least 8 GB Docker memory** (Docker Desktop default 4 GB is not enough for the chat assistant)
- Go 1.24+ (for building from source)
- Node.js 20+ (for the dashboard)

No GPU required for the default profile.

## Start Cordum

```bash
git clone https://github.com/cordum-io/cordum.git
cd cordum
make dev-up
```

This starts all platform services (API Gateway, Scheduler, Safety Kernel, Workflow Engine, Context Engine, NATS, Redis) plus the **Ollama** profile of the LLM chat assistant: Ollama serving `qwen2.5-coder:7b-instruct-q4_K_M` (~4.5 GB resident, no GPU needed). First boot blocks ~3-5 minutes on the model pull; cached for subsequent boots.

If you have a GPU and want the higher-quality Qwen3-Coder-30B-FP8, use `make dev-up-gpu` instead. See [docs/llmchat/ollama-runtime.md](https://github.com/cordum-io/cordum/blob/main/docs/llmchat/ollama-runtime.md) for the profile matrix.

## Verify

```bash
curl http://localhost:8081/health
```

You should see `{"status":"ok"}`.

## Submit Your First Job

```bash
curl -X POST http://localhost:8081/api/v1/jobs \
  -H 'Content-Type: application/json' \
  -H 'X-Tenant-ID: default' \
  -d '{
    "topic": "job.default",
    "prompt": "Hello, Cordum!"
  }'
```

## Next Steps

- [Installation Guide](/getting-started/installation) for production deployments
- [Architecture](/concepts/architecture) to understand how Cordum works
- [Safety Kernel](/concepts/safety-kernel) for governance policies
