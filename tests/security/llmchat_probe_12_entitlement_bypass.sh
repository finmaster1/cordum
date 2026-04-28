#!/usr/bin/env bash
set -euo pipefail
PROBE_ID="llmchat_probe_12_entitlement_bypass"
# shellcheck source=tests/security/llmchat_common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/llmchat_common.sh"
probe_init
write_probe_manifest "LLMChatAssistant entitlement gates gateway proxy, internal chat handlers, and dashboard header-button availability"
record_section "attack payload"
log_evidence 'payload: flip LLMChatAssistant=false and request /api/v1/chat/* plus dashboard header health gate; expected chat endpoints denied by entitlement gate and header button hidden while jobs/approvals endpoints remain unaffected.'

assert_file_contains "core/licensing/license.go" 'LLMChatAssistant[[:space:]]+bool[[:space:]]+`json:"llm_chat_assistant,omitempty"`' "license entitlement field must exist"
assert_file_contains "core/licensing/tiers.go" 'LLMChatAssistant:[[:space:]]+true' "Enterprise tier must default LLMChatAssistant=true"
assert_file_contains "core/licensing/tiers_test.go" 'LLMChatAssistant' "tier tests must cover LLMChatAssistant defaults"
assert_file_contains "core/llmchat/chat_handlers.go" 'http\.StatusPaymentRequired' "internal chat handlers must return 402 when feature disabled"
assert_file_contains "core/llmchat/chat_handlers.go" 'feature_unavailable' "internal chat handlers must emit stable feature_unavailable code"
assert_file_contains "core/controlplane/gateway/handlers_llmchat_proxy.go" 'requireFeatureEntitlement\(w, llmChatFeatureName' "browser-facing gateway proxy must gate before forwarding"
assert_file_contains "dashboard/src/hooks/useChatAssistantAvailability.ts" 'POLL_INTERVAL_MS[[:space:]]*=[[:space:]]*10_000' "dashboard availability hook must poll health every 10s"
assert_file_contains "dashboard/src/hooks/useChatAssistantAvailability.ts" 'setTimeout\(async \(\) =>|POLL_INTERVAL_MS' "dashboard availability hook must schedule repeated health probes"
assert_file_contains "dashboard/src/components/chat-assistant/ChatHeaderButton.test.tsx" 'hides the button when healthz returns 503' "dashboard tests must cover health-hide behavior"

run_go_test "go test licensing LLMChatAssistant defaults" ./core/licensing -run 'TestDefaultEntitlementsByTier' -count=1 || probe_fail "licensing default entitlement tests failed"
run_go_test "go test internal chat entitlement gate" ./core/llmchat -run 'TestChatHandlers_LicenseGateAppliesToPostSSEAndWS' -count=1 || probe_fail "internal chat entitlement gate tests failed"
run_go_test "go test gateway chat entitlement proxy" ./core/controlplane/gateway -run 'TestHandleLLMChatProxyRequiresEntitlementBeforeForwarding|TestHandleLLMChatProxyHealthForwardsReadyzWithTrustedIdentity' -count=1 || probe_fail "gateway llm-chat entitlement proxy tests failed"

if command -v npm >/dev/null 2>&1 && npm --version >/dev/null 2>&1 && [ -d "${REPO_ROOT}/dashboard/node_modules" ]; then
  run_capture "dashboard chat availability/header tests" npm --prefix dashboard test -- src/hooks/useChatAssistantAvailability.test.ts src/components/chat-assistant/ChatHeaderButton.test.tsx || probe_fail "dashboard chat availability/header tests failed"
else
  log_evidence "dashboard_tests=optional_not_run reason=npm unusable in this shell or dashboard/node_modules unavailable; source-level entitlement/health-hide assertions above remain scored"
fi

if [ "${LLMCHAT_SECURITY_LIVE:-0}" = "1" ]; then
  body="${PROBE_OUT_DIR}/entitlement-live.body"
  if [ -n "${LLMCHAT_SECURITY_TRUSTED_FORWARDER_API_KEY:-}" ]; then
    status=$(curl_status_body "chat health with current entitlement" "${body}" -X GET "${GATEWAY_URL}/api/v1/chat/healthz" -H "X-API-Key: ${LLMCHAT_SECURITY_TRUSTED_FORWARDER_API_KEY}" -H "X-Cordum-Tenant: ${LLMCHAT_SECURITY_TENANT:-default}" -H "X-Cordum-Principal: secprobe-user" -H "X-Cordum-Role: user") || true
  else
    status=$(curl_status_body "chat health with current entitlement" "${body}" -X GET "${GATEWAY_URL}/api/v1/chat/healthz" -H "X-Tenant-ID: ${LLMCHAT_SECURITY_TENANT:-default}") || true
  fi
  log_evidence "live_current_entitlement_status=${status} (set LLMCHAT_SECURITY_EXPECT_DISABLED=1 after flipping license false to assert denial)"
  if [ "${LLMCHAT_SECURITY_EXPECT_DISABLED:-0}" = "1" ]; then
    assert_http_status_in "${status}" "402,403" "disabled LLMChatAssistant must deny chat endpoints at entitlement gate"
    assert_text_contains "${body}" 'feature_unavailable|tier_limit_exceeded|llm_chat_assistant' "disabled-entitlement response must name the entitlement defense"
  fi
else
  live_evidence_not_run "live_entitlement" "set LLMCHAT_SECURITY_LIVE=1 after clean compose-up; set LLMCHAT_SECURITY_EXPECT_DISABLED=1 after flipping license false"
fi

probe_pass "entitlement gate and dashboard hide behavior tests passed"
