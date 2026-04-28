#!/usr/bin/env bash
set -euo pipefail
PROBE_NAME="probe-12"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "${SCRIPT_DIR}/common-fixture.sh"

write_probe_header
log_evidence "title=log sampling / volume bounds"
require_docker_compose

service="${LLMCHAT_LOG_SERVICE:-llm-chat-ollama}"
before="${probe_dir}/logs-before.txt"
after="${probe_dir}/logs-after.txt"
docker_compose logs --no-color --since "${LLMCHAT_LOG_SINCE:-10m}" "${service}" >"${before}" 2>/dev/null || probe_skip "cannot read compose logs for ${service}"
before_count=$(wc -l <"${before}" | tr -d ' ')
log_evidence "service=${service}"
log_evidence "before_line_count=${before_count}"

if [[ "${LLMCHAT_OPS_LIVE}" == "1" && "${LLMCHAT_OPS_RUN_LOAD:-0}" == "1" ]]; then
  require_cmd curl
  if [[ -z "${CORDUM_API_KEY:-}" ]]; then
    probe_fail "CORDUM_API_KEY required for live load"
  fi
  for i in $(seq 1 10); do
    payload=$(printf '{"message":"ops log sampling probe %02d: answer with one short sentence"}' "$i")
    curl_api --request POST --header 'Content-Type: application/json' --data "${payload}" "${CORDUM_API_BASE}/chat" >"${probe_dir}/chat-${i}.json" || true
  done
else
  log_evidence "live_load=not_run set LLMCHAT_OPS_LIVE=1 LLMCHAT_OPS_RUN_LOAD=1 to send 10 messages"
fi

docker_compose logs --no-color --since "${LLMCHAT_LOG_SINCE:-10m}" "${service}" >"${after}" 2>/dev/null || probe_skip "cannot read compose logs after load for ${service}"
after_count=$(wc -l <"${after}" | tr -d ' ')
delta=$((after_count-before_count))
log_evidence "after_line_count=${after_count}"
log_evidence "line_delta=${delta}"

# Token-delta logging should not appear at INFO/WARN. Search common frame names.
if grep -EIn 'assistant_delta|token_delta|delta.content|stream chunk|chunk_delta' "${after}" >"${probe_dir}/delta-log-hits.txt"; then
  log_evidence "delta_log_hits:"
  head -20 "${probe_dir}/delta-log-hits.txt" >>"${evidence_file}"
  probe_fail "token/assistant delta log lines found"
fi

# If a live load was requested, enforce the <100-line bound from the task.
if [[ "${LLMCHAT_OPS_LIVE}" == "1" && "${LLMCHAT_OPS_RUN_LOAD:-0}" == "1" && "${delta}" -ge 100 ]]; then
  probe_fail "log volume delta ${delta} exceeds bound 100 for 10-message load"
fi

if [[ "${LLMCHAT_OPS_LIVE}" != "1" || "${LLMCHAT_OPS_RUN_LOAD:-0}" != "1" ]]; then
  # Static/sampling check can pass for the absence of token-delta logs, while
  # the review doc must still mark the load-bound subcheck as not run.
  log_evidence "load_bound_enforced=false"
fi
probe_pass
