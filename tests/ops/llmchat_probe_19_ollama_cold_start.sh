#!/usr/bin/env bash
# Probe 19 — Ollama cold start + tool_calls[] contract gate
#
# Failure mode: ollama container starts but the model is not yet pulled,
# OR an older Ollama tag (<0.5) embeds tool calls as JSON in the content
# field instead of emitting OpenAI-format `delta.tool_calls[]` arrays —
# the format the chat client `core/llmchat/provider_openai.go` parses.
# Either failure silently breaks tool dispatch.
#
# Acceptance criteria: ollama /api/tags lists qwen2.5-coder:7b-instruct-q4_K_M
# within ${LLMCHAT_OPS_OLLAMA_PULL_TIMEOUT:-600}s of compose up; one
# /v1/chat/completions call with a single tool emits at least one SSE
# frame whose JSON contains `delta.tool_calls`; llm-chat /readyz reaches
# 200 within 60s after the model is loaded.
# Expected recovery time: <=600s cold pull on a fresh cache; <=30s on a
# warm `ollama_models` named volume.
# Nightly/manual marker: cpu-nightly.

set -euo pipefail
PROBE_ID="llmchat_probe_19_ollama_cold_start"
# shellcheck source=tests/ops/llmchat_common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/llmchat_common.sh"
probe_init
write_probe_manifest \
  'ollama profile starts but the model is not yet pulled, OR Ollama emits tool calls in the content field instead of delta.tool_calls' \
  'ollama /api/tags lists the expected model within timeout; one /v1/chat/completions tool-call request emits at least one SSE frame containing delta.tool_calls; llm-chat /readyz reaches 200 within 60s' \
  '<=600s cold pull on fresh cache; <=30s on warm ollama_models volume' \
  'cpu-nightly'

record_section 'setup'
require_real_ollama
require_cmd curl

EXPECTED_MODEL="${LLMCHAT_OLLAMA_MODEL:-qwen2.5-coder:7b-instruct-q4_K_M}"
PULL_TIMEOUT="${LLMCHAT_OPS_OLLAMA_PULL_TIMEOUT:-600}"
READYZ_TIMEOUT="${LLMCHAT_OPS_OLLAMA_READYZ_TIMEOUT:-60}"

record_section "wait for ${EXPECTED_MODEL} on /api/tags"
if ! wait_for_ollama_model_loaded "${EXPECTED_MODEL}" "${PULL_TIMEOUT}"; then
  probe_fail "ollama did not register ${EXPECTED_MODEL} within ${PULL_TIMEOUT}s; cold pull may still be running or the compose pull step failed"
fi

record_section 'tool-call contract gate'
# Single OpenAI-format chat-completions call with one trivial tool. The
# critical assertion is that ollama emits at least one SSE frame whose
# JSON contains a `tool_calls` field inside `delta`. Older Ollama tags
# (<0.5) embed the tool call as serialized JSON inside the content
# field, which would silently break the chat-loop dispatcher in
# core/llmchat/provider_openai.go (lines 368-380).
toolcall_body="${PROBE_OUT_DIR}/toolcall.body"
toolcall_req="${PROBE_OUT_DIR}/toolcall.req.json"
cat >"${toolcall_req}" <<JSON
{
  "model": "${EXPECTED_MODEL}",
  "messages": [
    {"role": "system", "content": "When the user asks to list jobs you MUST call the list_jobs tool."},
    {"role": "user", "content": "list my running jobs"}
  ],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "list_jobs",
        "description": "List jobs visible to the current principal.",
        "parameters": {
          "type": "object",
          "additionalProperties": false,
          "properties": {"limit": {"type": "integer"}},
          "required": []
        }
      }
    }
  ],
  "tool_choice": "auto",
  "stream": true,
  "temperature": 0.1
}
JSON

run_capture 'curl /v1/chat/completions stream' \
  curl -sS --max-time 120 -o "${toolcall_body}" \
    -H 'Content-Type: application/json' \
    -X POST "${OLLAMA_URL}/v1/chat/completions" \
    --data-binary "@${toolcall_req}" || probe_fail 'curl to /v1/chat/completions failed'

if [ ! -s "${toolcall_body}" ]; then
  probe_fail 'empty response body from /v1/chat/completions'
fi

# Streaming response is `data: {...}\n\n` SSE. Look for the literal
# `"tool_calls"` token anywhere in any data frame; if it appears only
# inside a `"content"` string that is a parser-rewrite signal, fail
# loud.
if grep -q '"tool_calls"' "${toolcall_body}"; then
  log_evidence 'tool_calls_token_found=true'
else
  sed -e 's/^/toolcall_body: /' "${toolcall_body}" >>"${EVIDENCE_FILE}"
  probe_fail 'no "tool_calls" token in any SSE frame; Ollama may be on a tag <0.5 that emits tool calls in content field — bump the pin in tools/scripts/ollama_pin_digest.sh'
fi

# Belt-and-suspenders: assert the token appears INSIDE a `delta` object,
# not just buried inside a content string. The provider parser walks
# `choices[].delta.tool_calls[]`; anything else is silent breakage.
if ! grep -E '"delta"[[:space:]]*:[[:space:]]*\{[^}]*"tool_calls"' "${toolcall_body}" >/dev/null 2>&1; then
  # Multi-line SSE frames are common; fall back to a python-aided check
  # if available so we don't false-fail on whitespace differences.
  if [ -n "${PYTHON_BIN}" ]; then
    ${PYTHON_BIN} - "${toolcall_body}" >>"${EVIDENCE_FILE}" 2>&1 <<'PY' || probe_fail 'no delta.tool_calls in any SSE frame; Ollama is emitting tool calls in content field — bump the pin'
import json, sys
ok = False
with open(sys.argv[1], encoding='utf-8') as f:
    for line in f:
        line = line.strip()
        if not line.startswith('data:'):
            continue
        payload = line[len('data:'):].strip()
        if payload == '[DONE]':
            continue
        try:
            obj = json.loads(payload)
        except Exception:
            continue
        for choice in obj.get('choices', []):
            delta = choice.get('delta') or {}
            if delta.get('tool_calls'):
                print(f'delta_tool_calls_frame={payload[:200]}')
                ok = True
                break
        if ok:
            break
if not ok:
    raise SystemExit(1)
PY
  else
    probe_fail 'cannot validate delta.tool_calls without python; install python3 to disambiguate'
  fi
fi

record_section 'llm-chat /readyz after model load'
readyz_url="${LLMCHAT_DIRECT_URL%/}/readyz"
poll_readyz "${readyz_url}" 200 "${READYZ_TIMEOUT}" || probe_fail "llm-chat /readyz did not reach 200 within ${READYZ_TIMEOUT}s after Ollama loaded ${EXPECTED_MODEL}"

probe_pass "Ollama profile cold-start gate green: ${EXPECTED_MODEL} pulled and emits OpenAI delta.tool_calls; llm-chat /readyz=200"
