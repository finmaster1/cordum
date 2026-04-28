# cordum-llm-chat production-readiness runbook (Ollama default)

> Scope: task-ce2b4a32, post-2026-04-28 informational-only pivot. The production default is `ollama-cpu` with `qwen2.5-coder:3b-instruct-q4_K_M-ctx32k`; vLLM is opt-in and out of scope here.

## Test environment

- Date: 2026-04-28.
- Stack: local Docker Compose from branch `LLM-in_Dashbord`.
- Docker host reported `DockerMem=8325890048 CPUs=16` during the load probe.
- Important live-stack note: after rebuilding `llm-chat` from the current branch, the persisted `chat-assistant` identity still had pre-pivot tool-calling scope. The current service failed closed until the local agent identity was down-scoped through the supported `PUT /api/v1/agents/{id}` admin API (`allowed_tools=[]`, `preapproved_mutating_tools=[]`, `risk_tier=low`). This is an upgrade-runbook prerequisite for any environment that previously enabled chat tool-calling.

## Failure-mode probes

### FM-1: Ollama down + dashboard chat button hides

**Expected**

- Gateway `/api/v1/chat/healthz` returns 503 while Ollama is stopped.
- Dashboard polling hides the chat button within the 10s health-probe window.
- Restarting Ollama restores `/api/v1/chat/healthz` to 200 without restarting `cordum-llm-chat`.

**Procedure**

1. Verify `/api/v1/chat/healthz` is 200.
2. Load the dashboard and verify the `Open chat assistant` button exists.
3. `docker compose stop ollama`.
4. Re-check `/api/v1/chat/healthz` and run the headless dashboard DOM probe after the poll window.
5. `docker compose start ollama` and re-check health.

**Actual evidence** (`out/llmchat-prod-readiness/step2-ollama-down.txt`)

```text
baseline: HTTP/1.1 200 OK
{"status":"ok","redis":"ok","vllm":"ok"}
baseline dashboard: chatButtons=[{"aria":"Open chat assistant"}]
stop: Container cordum-ollama-1 Stopped
degraded: HTTP/1.1 503 Service Unavailable
{"status":"degraded","redis":"ok","vllm":"fail: llmchat/openai: health GET failed: Get \"http://ollama:11434/v1/models\": dial tcp: lookup ollama on 127.0.0.11:53: no such host"}
degraded dashboard: chatButtons=[]
restored: HTTP/1.1 200 OK
{"status":"ok","redis":"ok","vllm":"ok"}
```

**Verdict**: PASS. The user-facing dashboard hides the entry point when the inference backend is unavailable, and readiness recovers after Ollama restarts.

### FM-2: High concurrent-session load / OOM behavior

**Expected**

- On the default 4GB-shaped Ollama container, memory stays within the model sizing target or the container fails cleanly with an OOM signal captured in Docker state/logs.
- Chat requests fail fast enough for the caller to recover; no process crash or silent hang.

**Procedure**

1. Record `docker info` host memory and `docker stats` for `cordum-ollama-1` and `cordum-llm-chat-1`.
2. Run 50 concurrent direct llm-chat POST sessions using `tests/load/llmchat_load_probe.mjs` with a 20s per-request timeout.
3. Record post-load `docker stats`, `docker inspect ... OOMKilled`, and Ollama log tail.

**Actual evidence** (`out/llmchat-prod-readiness/step2-oom-load.txt`)

```text
DockerMem=8325890048 CPUs=16
before: cordum-ollama-1 20.6MiB / 4GiB; cordum-llm-chat-1 17.38MiB / 512MiB
load: TOTAL=50 TIMEOUT_MS=20000
counts={"TimeoutError":50}; p50=20013ms p95=20034ms p99=20034ms
after: cordum-ollama-1 2.946GiB / 4GiB (73.66%); cordum-llm-chat-1 18.05MiB / 512MiB
inspect: OOMKilled=false RestartCount=0 Status=running ExitCode=0
Ollama logs: repeated "aborting completion request due to client closing the connection" and 200 POST /v1/chat/completions around 19.5-21.9s.
```

**Verdict**: PASS for OOM containment (no OOM, no restart, memory stayed under
the 4GiB container limit). Capacity verdict is **saturated**: 50 concurrent
full-knowledge-pack chats exceed the 20s operator timeout on this CPU host. Use
the sizing section below for the loaded-model baseline and supported
concurrency ceiling; the later single-session baseline measured `3.079GiB`, so
the `3GiB` target should be treated as an approximate lower bound rather than a
hard production limit.

### FM-3: Ollama restart semantics + in-flight WebSocket

**Expected**

- Restarting Ollama during an in-flight WebSocket turn should not crash `cordum-llm-chat`.
- The client should receive either an explicit error frame or a close that the dashboard can reconnect from; indefinite silence is not acceptable.
- Readiness should return to 200 after Ollama is healthy again.

**Procedure**

1. Open a direct trusted-forwarder WebSocket to `ws://127.0.0.1:8090/api/v1/chat/ws` using `tests/load/llmchat_ws_restart_probe.go`.
2. Send one user message.
3. Restart `ollama` while the turn is in flight.
4. Capture frames and post-restart health.

**Actual evidence** (`out/llmchat-prod-readiness/step2-ollama-restart-ws.txt`)

```text
ws_open=ok
sent_message=ok
frame: {"type":"user","session_id":"dcc3b0d6-4931-40d7-b993-f90f1ce82f8f",...}
frame: {"type":"error","session_id":"dcc3b0d6-4931-40d7-b993-f90f1ce82f8f","is_error":true,"error_code":"provider_failed","error_msg":"llmchat/openai: retry exhausted: Post \"http://ollama:11434/v1/chat/completions\": dial tcp: lookup ollama on 127.0.0.11:53: no such host"}
post-restart: HTTP/1.1 200 OK
{"status":"ok","redis":"ok","vllm":"ok"}
ollama: Up ... (healthy)
```

**Verdict**: PASS with characterized user impact. Existing in-flight turns receive an explicit `provider_failed` frame; the service remains healthy after Ollama returns. The dashboard should surface the error and allow retry/reconnect.

### FM-4: Knowledge-pack mount failure refuses start

**Expected**

- Missing OpenAPI/site knowledge files must fail closed at boot; informational-only chat without Cordum docs is a misconfiguration.

**Procedure**

Run a one-off `llm-chat` container with `LLMCHAT_KNOWLEDGE_API_SPEC_PATH=/etc/cordum/missing-openapi.yaml`.

**Actual evidence** (`out/llmchat-prod-readiness/step2-knowledge-mount-failure.txt`)

```text
[LLM-CHAT-SERVER] INFO llm-chat backend active backend=ollama-cpu base_url=http://ollama:11434/v1 model=qwen2.5-coder:3b-instruct-q4_K_M-ctx32k
[LLM-CHAT-SERVER] ERROR cordum-llm-chat: knowledge pack load failed, refusing to start error=read OpenAPI spec /etc/cordum/missing-openapi.yaml: open /etc/cordum/missing-openapi.yaml: no such file or directory
exit_code=1
```

**Verdict**: PASS. The current branch image refuses to start with a missing knowledge-pack mount.

### FM-5: License-entitlement blocked path

**Expected**

- With no Enterprise `LLMChatAssistant` entitlement, chat endpoints return HTTP 402 `feature_unavailable` before invoking the model.

**Procedure**

Start a one-off `llm-chat` container with `CORDUM_LICENSE_TOKEN=` and `CORDUM_LICENSE_PUBLIC_KEY=`, then POST to `/api/v1/chat` with trusted-forwarder headers.

**Actual evidence** (`out/llmchat-prod-readiness/step2-license-blocked.txt`)

```text
startup: cordum-llm-chat listening addr=:18092 ... backend=ollama-cpu ...
POST /api/v1/chat:
HTTP/1.1 402 Payment Required
{"code":"feature_unavailable","error":"request_failed","message":"chat requires Enterprise","status":402}
```

**Verdict**: PASS. The entitlement gate fails closed and does not reach Ollama.

## Rolling upgrade

### Strategy decision

The default Ollama inference Deployment intentionally uses `Recreate`, not
`RollingUpdate`:

```text
helm template cordum cordum-helm --set secrets.apiKey=dummy --set redis.auth.password=dummy --set inference.backend=ollama-cpu --set llmChat.replicas=2
ollama: {"name": "cordum-ollama-inference", "replicas": 1, "strategy": {"type": "Recreate"}}
llm-chat: {"name": "cordum-llm-chat", "replicas": 2, "strategy": "<k8s-default RollingUpdate>"}
```

This is different from the ideal `RollingUpdate maxSurge=1 maxUnavailable=0`
shape because Ollama pulls the model into a single ReadWriteOnce
`ollama_models` PVC at startup and the default CPU profile serializes
generations (`OLLAMA_NUM_PARALLEL=1`). Overlapping two Ollama pods against the
same cache during startup-time `ollama pull` risks a PVC attach/cache-contention
failure. The safe operator posture is:

- run at least two `cordum-llm-chat` replicas for frontend availability;
- keep the default Ollama inference Deployment single-replica/Recreate;
- expect in-flight turns to receive an explicit provider error during the short
  inference restart window (FM-3 above), then retry once readiness returns;
- use an external HA inference endpoint only as an explicit opt-in if true
  zero-downtime model-serving upgrades are required.

### Image-bump render smoke

The chart renders a bumped Ollama image without changing the default backend:

```text
helm template cordum cordum-helm --set secrets.apiKey=dummy --set redis.auth.password=dummy --set inference.backend=ollama-cpu --set ollamaInference.image.tag=0.5.8 --set llmChat.replicas=2
name: cordum-ollama-inference
strategy:
  type: Recreate
image: "ollama/ollama:0.5.8"
```

### Live Kubernetes evidence status

The local dev host has `kubectl` configured for `docker-desktop`, but the API
server was not running during this task:

```text
kubectl config current-context
docker-desktop

kubectl get nodes -o wide
Unable to connect to the server: dial tcp 127.0.0.1:6443: connectex: No connection could be made because the target machine actively refused it.
```

Therefore this runbook records Helm-render evidence plus the live Docker Compose
restart characterization in FM-3. A staging cluster must still run and archive
these commands before a production cut:

```bash
helm upgrade --install cordum ./cordum-helm -n cordum --create-namespace \
  --set secrets.apiKey=<redacted> \
  --set redis.auth.password=<redacted> \
  --set inference.backend=ollama-cpu \
  --set llmChat.replicas=2 \
  --set ollamaInference.image.tag=<next-validated-tag>

kubectl -n cordum rollout status deploy/cordum-llm-chat --timeout=5m
kubectl -n cordum rollout status deploy/cordum-ollama-inference --timeout=10m
kubectl -n cordum rollout history deploy/cordum-llm-chat
kubectl -n cordum rollout history deploy/cordum-ollama-inference
kubectl -n cordum get pods -l app.kubernetes.io/component=llm-chat -w
kubectl -n cordum get pods -l app.kubernetes.io/component=ollama-inference -w
```

**Verdict**: PASS for chart-render validation and operator procedure; live
Kubernetes rollout evidence is explicitly pending a running staging cluster.

## Resource sizing and concurrent-session ceiling

### Host shape

```text
docker info: DockerMem=8325890048 CPUs=16 Server=29.2.0
ollama container limit: 4GiB
llm-chat container limit: 512MiB
```

### Memory baseline

After an Ollama restart, before the model is loaded, memory is tiny:

```text
cordum-ollama-1 CPU=0.00% MEM=14.91MiB / 4GiB MEM%=0.36%
cordum-llm-chat-1 CPU=0.00% MEM=12.17MiB / 512MiB MEM%=2.38%
```

A direct, small Ollama request to the default ctx32k local tag succeeds and
loads the model:

```text
POST http://127.0.0.1:11434/v1/chat/completions
model=qwen2.5-coder:3b-instruct-q4_K_M-ctx32k
status=200 elapsed_ms=8014
cordum-ollama-1 CPU=0.00% MEM=3.079GiB / 4GiB MEM%=76.98%
cordum-llm-chat-1 CPU=0.00% MEM=12.16MiB / 512MiB MEM%=2.38%
```

**Verdict**: the loaded-model baseline is close to, but slightly above, the
3GiB sizing target on this Docker Desktop host (`3.079GiB` observed). It stays
well under the 4GiB container limit. Treat `3GiB` as an approximate lower bound
for the ctx32k model; operators should allocate at least 4GiB to the Ollama
container, as the default Compose/Helm profile does.

### Full llm-chat path concurrency

The host-driven probe uses the real `cordum-llm-chat` `/api/v1/chat` path, so
it includes retrieval/system-prompt expansion and model generation. That is the
right SLO surface for the user-facing product, but it is substantially slower
than a bare Ollama prompt.

| Concurrent sessions | Timeout | Outcome | p50 | p95 | p99 | Ollama memory | CPU | Verdict |
| ---: | ---: | --- | ---: | ---: | ---: | --- | --- | --- |
| 1 | 60s | `TimeoutError=1` | 60008ms | 60008ms | 60008ms | 3.057-3.131GiB / 4GiB | ~198% | Exceeds 60s SLO |
| 50 | 20s | `TimeoutError=50` | 20013ms | 20034ms | 20034ms | 2.946GiB / 4GiB | saturated | Saturated |

Single-session evidence (`out/llmchat-prod-readiness/step4-load-total1.json`):

```json
{
  "total": 1,
  "timeoutMs": 60000,
  "counts": { "TimeoutError": 1 },
  "p50_ms": 60008,
  "p95_ms": 60008,
  "p99_ms": 60008
}
```

Docker/Ollama evidence after the single-session timeout:

```text
cordum-ollama-1 CPU=187.24% MEM=3.057GiB / 4GiB MEM%=76.44%
ollama OOMKilled=false RestartCount=0 Status=running ExitCode=0
Ollama log: POST /v1/chat/completions ran 1m0s and then aborted because the client closed the connection.
A later backend log showed the same request family continuing until 4m0s before aborting.
```

**Ceiling for this host**: for a 60s full-response SLO, the measured safe
concurrent-session ceiling is **0**; even one full knowledge-pack turn exceeded
the 60s caller timeout. For availability/containment, the stack is safe: no OOM,
no container restart, and health returned after restarting Ollama. For
production latency, this is a follow-up optimization item (reduce retrieved
prompt tokens/context length, stream by first-token SLO, or require a larger CPU
host/GPU opt-in for higher concurrency).

**Bottleneck**: CPU in the Ollama runner. During the timed-out single request,
`/usr/lib/ollama/runners/cpu_avx2/ollama_llama_server` consumed ~196% CPU while
`cordum-llm-chat` stayed near idle memory/CPU.

## Health checks and observability

### Endpoint semantics

The service now separates product health from process liveness:

| Endpoint | Purpose | Dependencies checked | Healthy body | Degraded behavior |
| --- | --- | --- | --- | --- |
| `/livez` | Kubernetes liveness/process check | none | `{"status":"ok","service":"cordum-llm-chat"}` | remains 200 while dependencies are down |
| `/healthz` | user-facing chat health | inference backend + knowledge-pack paths | `{"status":"ok","vllm":"ok","knowledge":"ok"}` | 503 with failed component |
| `/readyz` | traffic readiness | Redis + inference backend + knowledge-pack paths | `{"status":"ok","redis":"ok","vllm":"ok","knowledge":"ok"}` | 503 with failed component |

The response key remains `vllm` for compatibility with existing dashboard and
probe consumers, but under the Ollama-default profile it means "active inference
provider".

Helm liveness uses `/livez`; readiness uses `/readyz`. Docker Compose healthchecks
continue to use `/healthz` so a local stack visibly reports chat-product
degradation when Ollama or the knowledge pack is unavailable.

### Live inference-restart evidence

After rebuilding `cordum-llm-chat` from this branch, stopping only the inference
container with `docker stop cordum-ollama-1` showed the intended distinction:

```text
healthy_livez  {"status":"ok","service":"cordum-llm-chat"} HTTP=200
healthy_healthz {"status":"ok","vllm":"ok","knowledge":"ok"} HTTP=200
healthy_readyz {"status":"ok","redis":"ok","vllm":"ok","knowledge":"ok"} HTTP=200

cordum-llm-chat-1 Up 45 seconds (healthy)
cordum-ollama-1 Exited (137) 3 seconds ago

degraded_livez {"status":"ok","service":"cordum-llm-chat"} HTTP=200
degraded_healthz {"status":"degraded","vllm":"fail: llmchat/openai: health GET failed: Get \"http://ollama:11434/v1/models\": dial tcp: lookup ollama on 127.0.0.11:53: no such host","knowledge":"ok"} HTTP=503
degraded_readyz {"status":"degraded","redis":"ok","vllm":"fail: llmchat/openai: health GET failed: Get \"http://ollama:11434/v1/models\": dial tcp: lookup ollama on 127.0.0.11:53: no such host","knowledge":"ok"} HTTP=503

restored_livez {"status":"ok","service":"cordum-llm-chat"} HTTP=200
restored_healthz {"status":"ok","vllm":"ok","knowledge":"ok"} HTTP=200
restored_readyz {"status":"ok","redis":"ok","vllm":"ok","knowledge":"ok"} HTTP=200
```

`docker compose stop ollama` also stops/recreates dependent services in this
Compose graph, so use `docker stop cordum-ollama-1` when testing inference-only
failure semantics locally.

### Boot log evidence

Startup logs include the backend identity at info level:

```json
{"msg":"llm-chat backend active","backend":"ollama-cpu","base_url":"http://ollama:11434/v1","model":"qwen2.5-coder:3b-instruct-q4_K_M-ctx32k"}
```

### Metrics, dashboard, and alert

The existing `chat_vllm_latency_seconds` histogram is the provider-call latency
source. The Helm dashboard panel now charts p50, p95, and p99 via
`histogram_quantile()` over that histogram, and the alert bundle includes
`LLMChatHighP95Latency`:

```promql
histogram_quantile(0.95, sum(rate(chat_vllm_latency_seconds_bucket[5m])) by (le)) > 60
```

This pages/tickets when p95 provider latency stays above 60 seconds for 5
minutes, matching the resource-sizing finding above.
