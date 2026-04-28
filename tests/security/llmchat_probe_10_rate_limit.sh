#!/usr/bin/env bash
set -euo pipefail
PROBE_ID="llmchat_probe_10_rate_limit"
# shellcheck source=tests/security/llmchat_common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/llmchat_common.sh"
probe_init
write_probe_manifest "SUPERSEDED as chat->MCP flood probe; retained evidence checks generic gateway rate limiting and informational chat output/backpressure budgets"
record_section "attack payload"
log_evidence 'payload (historical): flood chat->MCP tool loop at 100 msgs/sec. Expected current defense: no chat->MCP path exists; generic gateway rate limiting and WS/backpressure/message-size/turn budgets still bound informational chat.'
log_evidence "scope=superseded reason=chat no longer calls MCP; rate-limit-on-MCP-loop is not production-scored"

assert_file_contains "core/controlplane/gateway/middleware.go" 'newRedisRateLimiter|newKeyedRateLimiter' "gateway must have rate limiter implementation"
assert_file_contains "core/controlplane/gateway/gateway.go" 'API_RATE_LIMIT_RPS' "gateway rate limit must be configurable"
assert_file_contains "core/llmchat/chat_handlers.go" 'maxWSMessageBytes[[:space:]]*=[[:space:]]*64 \* 1024' "chat WS must cap message size"
assert_file_contains "core/llmchat/chat_handlers.go" 'ErrorCode: "message_too_large"' "chat WS must emit message_too_large on oversized frame"
assert_file_contains "core/llmchat/chat_handlers.go" 'ErrorCode: "backpressure"' "chat WS must emit backpressure when client is too slow"
assert_file_not_contains "core/llmchat/agent.go" 'maxToolCallsPerTurn|MaxToolCallsPerTurn|RepeatCallDetector' "retired tool-loop budget must not remain in informational agent"
assert_file_contains "core/llmchat/agent.go" 'defaultMaxWallClock' "informational agent must bound wall-clock per turn"
assert_file_contains "core/llmchat/agent.go" 'defaultMaxAssistantBytes' "informational agent must bound assistant bytes per turn"

run_go_test "go test gateway rate limit middleware" ./core/controlplane/gateway -run 'TestRateLimitMiddleware|TestRateLimitAuthOrdering|TestRateLimitKeyTenantBased|TestRateLimitTenantSharedAcrossIPs|TestRedisRateLimiter_RedTeam14_BurstExceeded|TestRedisRateLimiter_120Requests_MostRejected|TestKeyedRateLimiter_BurstEnforced' -count=1 || probe_fail "gateway rate-limit regression tests failed"
run_go_test "go test llmchat WS/informational flood guards" ./core/llmchat -run 'TestChatWSOversizeMessageClosesWithErrorFrame|TestTurn_WallClockBudgetTrips|TestTurn_AssistantBytesBudgetTrips|TestTurn_InformationalSingleProviderCallStreamsFinal' -count=1 || probe_fail "llmchat WS/agent flood guard tests failed"

live_evidence_not_run "live_rate_limit" "optional only: run LLMCHAT_SECURITY_RUN_FLOOD=1 on owned stack if model-behavior soak evidence is requested"
probe_pass "chat->MCP rate-limit probe is superseded; retained generic rate/backpressure guards pass"
