#!/usr/bin/env bash
set -euo pipefail
PROBE_ID="llmchat_probe_09_ws_origin"
# shellcheck source=tests/security/llmchat_common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/llmchat_common.sh"
probe_init
write_probe_manifest "Browser-facing gateway CORS/origin middleware rejects malicious Origin before WS proxying"
record_section "attack payload"
log_evidence 'payload: WebSocket upgrade GET /api/v1/chat/ws with Origin=https://attacker.example and stolen cookie/token; expected 403 origin not allowed from gateway corsMiddleware/isAllowedOrigin.'

assert_file_contains "core/controlplane/gateway/middleware.go" 'origin not allowed' "gateway middleware must reject disallowed Origin"
assert_file_contains "core/controlplane/gateway/middleware.go" 'func isAllowedOrigin' "gateway origin allowlist helper must exist"
assert_file_contains "core/controlplane/gateway/gateway.go" 'GET /api/v1/chat/ws' "chat WS route must be registered behind gateway middleware"
assert_file_contains "core/controlplane/gateway/handlers_stream.go" 'CheckOrigin: func\(r \*http\.Request\) bool \{ return isAllowedOrigin\(r\) \}' "existing gateway WS streams must use same origin helper"
# Internal llm-chat has CheckOrigin=true because it is behind the gateway; record as evidence, not the browser-facing defense.
assert_file_contains "cmd/cordum-llm-chat/main.go" 'requireTrustedForwarder\(cfg\.CordumAPIKey\)' "direct llm-chat path must still require trusted-forwarder API key"

run_go_test "go test gateway origin middleware" ./core/controlplane/gateway -run 'TestCorsMiddleware|TestAllowedOriginsFromEnv|TestRequestHostname' -count=1 || probe_fail "gateway origin/CORS tests failed"

if [ "${LLMCHAT_SECURITY_LIVE:-0}" = "1" ]; then
  body="${PROBE_OUT_DIR}/ws-origin.body"
  origin_gateway_url="${LLMCHAT_SECURITY_ORIGIN_GATEWAY_URL:-${GATEWAY_URL}}"
  log_evidence "origin_gateway_url=${origin_gateway_url}"
  status=$(curl_status_body "malicious-origin WS upgrade" "${body}" -i -N "${origin_gateway_url}/api/v1/chat/ws" -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' -H 'Origin: https://attacker.example') || true
  assert_http_status_in "${status}" "403" "malicious WS Origin must be rejected"
  assert_text_contains "${body}" 'origin not allowed|Forbidden|forbidden' "origin rejection body must identify defense layer"
else
  live_evidence_not_run "live_ws_origin" "set LLMCHAT_SECURITY_LIVE=1 after clean compose-up to exercise actual upgrade path"
fi

probe_pass "WS origin defense is enforced at gateway origin allowlist"
