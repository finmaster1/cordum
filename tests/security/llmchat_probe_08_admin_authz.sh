#!/usr/bin/env bash
set -euo pipefail
PROBE_ID="llmchat_probe_08_admin_authz"
# shellcheck source=tests/security/llmchat_common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/llmchat_common.sh"
probe_init
write_probe_manifest "Admin chat-session endpoints require chat.read_all via PermissionChecker and fail closed without it"
record_section "attack payload"
log_evidence 'payload: non-admin/non-chat.read_all caller GET /api/v1/chat/sessions; expected 403 from RequirePermission(chat.read_all). Admin/cross-tenant caller gets 200 paginated list.'

assert_file_contains "core/llmchat/handlers_admin.go" 'gatewayauth\.PermChatReadAll' "admin session list must require chat.read_all"
assert_file_contains "core/llmchat/handlers_admin.go" 'RequirePermission\(r, gatewayauth\.PermChatReadAll\)' "admin gate must use PermissionChecker"
assert_file_contains "core/llmchat/handlers_admin.go" 'permission checker required' "admin gate must fail closed without checker"
assert_file_contains "core/controlplane/gateway/auth/rbac.go" 'PermChatReadAll[[:space:]]*=[[:space:]]*"chat\.read_all"' "RBAC permission constant must be stable"

run_go_test "go test chat admin RBAC" ./core/llmchat -run 'TestChatAdminListPermissionAndTenantScope|TestChatAdminDetailTranscriptAndCrossTenantNotFound|TestChatAdminFailsClosedWithoutPermissionChecker' -count=1 || probe_fail "chat admin RBAC regression tests failed"

if [ "${LLMCHAT_SECURITY_LIVE:-0}" = "1" ]; then
  body="${PROBE_OUT_DIR}/non-admin-sessions.body"
  if [ -n "${LLMCHAT_SECURITY_TRUSTED_FORWARDER_API_KEY:-}" ]; then
    status=$(curl_status_body "non-admin GET chat sessions" "${body}" -X GET "${GATEWAY_URL}/api/v1/chat/sessions" -H "X-API-Key: ${LLMCHAT_SECURITY_TRUSTED_FORWARDER_API_KEY}" -H "X-Cordum-Tenant: ${LLMCHAT_SECURITY_TENANT:-default}" -H "X-Cordum-Principal: secprobe-user" -H "X-Cordum-Role: user") || true
  else
    status=$(curl_status_body "non-admin GET chat sessions" "${body}" -X GET "${GATEWAY_URL}/api/v1/chat/sessions" -H "X-Tenant-ID: ${LLMCHAT_SECURITY_TENANT:-default}" -H "X-Principal-Role: user") || true
  fi
  assert_http_status_in "${status}" "401,403" "non-admin/non-chat.read_all caller must not list all chat sessions"
else
  live_evidence_not_run "live_admin_authz" "set LLMCHAT_SECURITY_LIVE=1 with a clean stack to exercise gateway auth paths"
fi

probe_pass "admin chat sessions are gated by chat.read_all and fail closed"
