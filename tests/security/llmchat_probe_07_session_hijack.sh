#!/usr/bin/env bash
set -euo pipefail
PROBE_ID="llmchat_probe_07_session_hijack"
# shellcheck source=tests/security/llmchat_common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/llmchat_common.sh"
probe_init
write_probe_manifest "Chat session resume checks principal+tenant from trusted gateway auth context; direct spoofed headers fail without service key"
record_section "attack payload"
log_evidence 'payload: steal session_id from localStorage, reconnect with different principal/tenant/User-Agent or spoof X-Cordum-Principal directly to llm-chat; expected 401/403/404 before session reuse.'

assert_file_contains "core/llmchat/chat_handlers.go" 'sessionVisibleToUser\(existing, user, tenant\)' "session resume must call visibility predicate"
assert_file_contains "core/llmchat/chat_handlers.go" 'sess\.UserPrincipal == user && sess\.Tenant == tenant' "session visibility must bind to principal and tenant"
assert_file_contains "core/llmchat/chat_handlers.go" 'forged or cross-tenant chat session id rejected' "forged/cross-tenant session IDs must be logged and rejected"
assert_file_contains "cmd/cordum-llm-chat/main.go" 'requireTrustedForwarder\(cfg\.CordumAPIKey\)' "llm-chat routes must require gateway service key"
assert_file_contains "cmd/cordum-llm-chat/main.go" 'constantTimeEqualString\(provided, string\(expected\)\)' "trusted-forwarder API key must use constant-time compare"

run_go_test "go test chat session hijack defenses" ./core/llmchat -run 'TestChatHandlers_ResumeRequiresTrustedNonEmptyPrincipalAndTenant|TestChatHandlers_DefaultIdentityIgnoresSpoofedHeaders|TestChatHandlers_ForgedSessionIDDoesNotCreateReplacement|TestChatAdminRejectsSpoofedAdminHeadersWithoutTrustedAuth' -count=1 || probe_fail "session hijack regression tests failed"
run_go_test "go test trusted forwarder auth" ./cmd/cordum-llm-chat -run 'TestRequireTrustedForwarder|TestRuntimeConfig' -count=1 || probe_fail "trusted-forwarder/runtime config tests failed"

if [ "${LLMCHAT_SECURITY_LIVE:-0}" = "1" ] && [ -n "${LLMCHAT_SECURITY_STOLEN_SESSION_ID:-}" ]; then
  if [ "${LLMCHAT_SECURITY_BACKEND:-ollama-cpu}" = "ollama-cpu" ] && [ -n "${LLMCHAT_SECURITY_COMPOSE_FILE:-}" ]; then
    record_section "seed owned-stack stolen session"
    now_nano="$(( $(date +%s) * 1000000000 ))"
    run_capture "seed redis session ${LLMCHAT_SECURITY_STOLEN_SESSION_ID}" docker compose -p "${SECURITY_COMPOSE_PROJECT}" -f "${LLMCHAT_SECURITY_COMPOSE_FILE}" exec -T redis-secprobes redis-cli -a secprobe-redis HSET "chat:session:${LLMCHAT_SECURITY_STOLEN_SESSION_ID}" id "${LLMCHAT_SECURITY_STOLEN_SESSION_ID}" user_principal alice tenant default agent_id agent-secprobe-chat-assistant created_at_unix_nano "${now_nano}" last_active_at_unix_nano "${now_nano}" || probe_fail "failed to seed owned-stack stolen session"
    run_capture "expire redis session ${LLMCHAT_SECURITY_STOLEN_SESSION_ID}" docker compose -p "${SECURITY_COMPOSE_PROJECT}" -f "${LLMCHAT_SECURITY_COMPOSE_FILE}" exec -T redis-secprobes redis-cli -a secprobe-redis EXPIRE "chat:session:${LLMCHAT_SECURITY_STOLEN_SESSION_ID}" 86400 || probe_fail "failed to expire owned-stack stolen session"
  fi
  body="${PROBE_OUT_DIR}/stolen-session.body"
  if [ -n "${LLMCHAT_SECURITY_TRUSTED_FORWARDER_API_KEY:-}" ]; then
    status=$(curl_status_body "stolen session resume as different principal" "${body}" -X POST "${GATEWAY_URL}/api/v1/chat" -H "X-API-Key: ${LLMCHAT_SECURITY_TRUSTED_FORWARDER_API_KEY}" -H "X-Chat-Session-Id: ${LLMCHAT_SECURITY_STOLEN_SESSION_ID}" -H "X-Cordum-Principal: mallory" -H "X-Cordum-Tenant: evil.test" -H "X-Cordum-Role: user" -H "Content-Type: application/json" -d '{"message":"resume stolen session"}') || true
  else
    status=$(curl_status_body "stolen session resume as different principal" "${body}" -X POST "${GATEWAY_URL}/api/v1/chat" -H "X-Chat-Session-Id: ${LLMCHAT_SECURITY_STOLEN_SESSION_ID}" -H "X-Principal-Id: mallory" -H "X-Tenant-ID: evil.test" -H "Content-Type: application/json" -d '{"message":"resume stolen session"}') || true
  fi
  assert_http_status_in "${status}" "401,403,404" "stolen session must not resume under different identity"
else
  live_evidence_not_run "live_hijack" "set LLMCHAT_SECURITY_LIVE=1 and LLMCHAT_SECURITY_STOLEN_SESSION_ID to run against clean stack"
fi

probe_pass "session hijack defenses bind session IDs to trusted principal+tenant"
