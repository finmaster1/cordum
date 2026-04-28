#!/usr/bin/env bash
set -euo pipefail
PROBE_ID="llmchat_probe_11_log_redaction"
# shellcheck source=tests/security/llmchat_common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/llmchat_common.sh"
probe_init
write_probe_manifest "no API-key/JWT/Bearer leakage in llm-chat/gateway/inference logs; local knowledge-pack content is redacted before prompt insertion"
record_section "attack payload"
log_evidence 'payload: synthetic sk-test-*, JWT-looking eyJ..., Bearer ..., and cloud-key-shaped strings inside curated OpenAPI/site docs or user prompts. Expected: DefaultRedactor removes knowledge-pack secrets before model context, providers do not log auth/prompt bodies, and optional log-dir grep finds zero hits.'

assert_file_contains "core/llmchat/knowledge/api_substituter.go" 'mcp\.DefaultRedactor\(\)' "API knowledge substituter must use core/mcp DefaultRedactor"
assert_file_contains "core/llmchat/knowledge/site_substituter.go" 'mcp\.DefaultRedactor\(\)' "site knowledge substituter must use core/mcp DefaultRedactor"
assert_file_contains "core/llmchat/knowledge/api_substituter.go" 'redactKnowledgeText' "knowledge-pack redaction helper must be applied"
assert_file_contains "cmd/cordum-llm-chat/main.go" 'slog\.Info\("knowledge pack loaded"' "knowledge-pack startup logging must be bounded to counts, not content"
assert_file_not_contains "core/llmchat/agent.go" 'slog\.(Debug|Info|Warn|Error).*UserMessage|slog\.(Debug|Info|Warn|Error).*prompt|slog\.(Debug|Info|Warn|Error).*Messages' "agent must not log user prompts or prompt bodies"
assert_file_not_contains "core/llmchat/provider_openai.go" 'slog\.(Debug|Info|Warn|Error).*Authorization|slog\.(Debug|Info|Warn|Error).*APIKey|slog\.(Debug|Info|Warn|Error).*requestBody' "OpenAI/Ollama provider must not log auth headers or prompt bodies"
assert_file_not_contains "docker-compose.yml" 'OLLAMA_DEBUG=1' "default Ollama compose must not enable verbose prompt/debug logging"
assert_file_contains "docker-compose.yml" 'OLLAMA_NUM_PARALLEL=1' "Ollama default should keep bounded local inference parallelism"
assert_file_contains "docker-compose.yml" 'VLLM_LOGGING_LEVEL=WARNING|--disable-log-requests|VLLM_DISABLE_LOG_REQUESTS' "opt-in vLLM should suppress request/prompt logs"

run_go_test "go test knowledge-pack redaction" ./core/llmchat/knowledge -run 'TestAPISubstituterCuratesOpenAPI|TestSiteSubstituterCuratesMDXDirectory|TestAPISubstituterBudgetRefusesOversizedBlob|TestSiteSubstituterBudgetRefusesOversizedBlob' -count=1 || probe_fail "knowledge redaction/budget tests failed"
run_go_test "go test provider/auth non-leak" ./core/llmchat -run 'TestOpenAIProvider_AuthHeader|TestOpenAIProvider_StreamsTextAndOmitsToolFields' -count=1 || probe_fail "llmchat provider auth/no-tool tests failed"
run_go_test "go test MCP redaction" ./core/mcp -run 'TestDefaultRedactor_' -count=1 || probe_fail "core/mcp default redactor tests failed"
run_go_test "go test auth logging avoids raw keys" ./core/controlplane/gateway/auth -run 'TestNewBasicAuthProviderLogsAPIKeySource|TestParseAPIKeysFormats' -count=1 || probe_fail "gateway auth logging/key parsing tests failed"

if [ -n "${LLMCHAT_SECURITY_LOG_DIR:-}" ]; then
  assert_no_secret_patterns_in_dir "${LLMCHAT_SECURITY_LOG_DIR}" "provided log directory must not contain probe secret patterns"
elif [ "${LLMCHAT_SECURITY_LIVE:-0}" = "1" ] && [ "${LLMCHAT_SECURITY_BACKEND}" = "ollama-cpu" ] && [ -n "${LLMCHAT_SECURITY_COMPOSE_FILE:-}" ]; then
  logs_dir="${PROBE_OUT_DIR}/docker-logs"
  mkdir -p "${logs_dir}"
  for svc in cordum-llm-chat-secprobes qwen-inference-secprobes gateway-mock-secprobes redis-secprobes; do
    docker compose -p "${SECURITY_COMPOSE_PROJECT}" -f "${LLMCHAT_SECURITY_COMPOSE_FILE}" logs --no-color "${svc}" >"${logs_dir}/${svc}.log" 2>/dev/null || true
  done
  assert_no_secret_patterns_in_dir "${logs_dir}" "owned Ollama security stack logs must not contain probe secret patterns"
else
  live_evidence_not_run "log_dir_scan" "optional: set LLMCHAT_SECURITY_LOG_DIR after a clean-stack/load run to grep collected logs"
fi

if [ "${LLMCHAT_SECURITY_LIVE:-0}" = "1" ] && [ "${LLMCHAT_SECURITY_RUN_LOAD:-0}" = "1" ]; then
  duration_seconds="${LLMCHAT_SECURITY_LOAD_SECONDS:-3600}"
  turns_per_min="${LLMCHAT_SECURITY_TURNS_PER_MIN:-50}"
  record_section "live load test"
  log_evidence "duration_seconds=${duration_seconds} turns_per_min=${turns_per_min}"
  end=$((SECONDS + duration_seconds))
  interval=$(awk "BEGIN { printf \"%.3f\", 60/${turns_per_min} }")
  i=0
  while [ "${SECONDS}" -lt "${end}" ]; do
    i=$((i + 1))
    token="sk-test-probe-${i}-not-real"
    jwt="eyJhbGciOiJub25lIn0.eyJwcm9iZSI6${i}fQ."
    body="${PROBE_OUT_DIR}/load-${i}.body"
    curl_status_body "load turn ${i}" "${body}" -X POST "${GATEWAY_URL}/api/v1/chat" -H "X-Tenant-ID: ${LLMCHAT_SECURITY_TENANT:-default}" -H "Content-Type: application/json" -d "{\"message\":\"diagnostic ${token} Bearer ${jwt}\"}" >/dev/null || true
    sleep "${interval}"
  done
  logs_dir="${PROBE_OUT_DIR}/docker-logs"
  mkdir -p "${logs_dir}"
  if [ "${LLMCHAT_SECURITY_BACKEND}" = "ollama-cpu" ] && [ -n "${LLMCHAT_SECURITY_COMPOSE_FILE:-}" ]; then
    for svc in cordum-llm-chat-secprobes qwen-inference-secprobes gateway-mock-secprobes redis-secprobes; do
      docker compose -p "${SECURITY_COMPOSE_PROJECT}" -f "${LLMCHAT_SECURITY_COMPOSE_FILE}" logs --no-color "${svc}" >"${logs_dir}/${svc}.log" 2>/dev/null || true
    done
  else
    for svc in cordum-llm-chat llm-chat api-gateway scheduler ollama qwen-inference; do
      docker compose logs --no-color "${svc}" >"${logs_dir}/${svc}.log" 2>/dev/null || true
    done
  fi
  assert_no_secret_patterns_in_dir "${logs_dir}" "live docker logs must not contain synthetic secrets"
elif [ "${LLMCHAT_SECURITY_LIVE:-0}" = "1" ]; then
  log_evidence "live_log_load=not_requested reason=baseline live log scan completed; set LLMCHAT_SECURITY_RUN_LOAD=1 for long-running synthetic log grep"
else
  live_evidence_not_run "live_log_load" "optional: set LLMCHAT_SECURITY_LIVE=1 LLMCHAT_SECURITY_RUN_LOAD=1 for 1h/50tpm clean-stack log grep"
fi

probe_pass "log redaction/secret non-leak static gates and tests passed"
