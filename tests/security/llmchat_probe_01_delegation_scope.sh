#!/usr/bin/env bash
set -euo pipefail
PROBE_ID="llmchat_probe_01_delegation_scope"
# shellcheck source=tests/security/llmchat_common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/llmchat_common.sh"
probe_init
write_probe_manifest "informational-only chat must not mint or expose MCP delegation tokens; core delegation scope monotonicity remains enforced for non-chat clients"
record_section "attack payload"
log_evidence 'payload: steal a chat session and search for a bearer/delegation JWT or hidden browser frame, then try to widen child scope to cordum_update_policy_bundle/cfg.*. Expected defense: no chat delegation client or browser-visible token exists; generic TokenService still rejects widened child scopes.'

for retired in core/llmchat/delegation.go core/llmchat/mcpclient.go core/llmchat/apiclient.go core/llmchat/approvals.go core/llmchat/approvals_nats.go; do
  if [ -e "${REPO_ROOT}/${retired}" ]; then
    probe_fail "informational-only chat must not retain retired delegation/MCP file: ${retired}"
  fi
  log_evidence "assert_file_absent ok: ${retired}"
done

assert_file_contains "core/llmchat/doc.go" 'does not call MCP tools, does not submit jobs, does not' "package docs must state informational-only/no-mutation scope"
assert_file_contains "core/llmchat/bootstrap.go" 'expectedAllowedTools\(\).*\{[[:space:]]*$|func expectedAllowedTools\(\) \[\]string' "chat-assistant AllowedTools must remain code-pinned"
assert_file_contains "core/llmchat/bootstrap.go" 'return nil' "informational-only bootstrap must use empty/nil tool/preapproval scopes"
assert_file_contains "core/llmchat/provider_openai.go" 'chat never sends OpenAI `tools` or `tool_choice`' "provider must not emit tool-calling request fields"
assert_file_not_contains "core/llmchat/chat_handlers.go" 'Delegation|delegation_token|Authorization: Bearer' "chat handlers must not surface delegation tokens"
assert_file_contains "core/auth/delegation/token.go" 'ErrScopeExceeded' "generic TokenService must still reject widened child scopes"
assert_file_contains "core/auth/delegation/token.go" 'ErrChainTooDeep' "generic TokenService must still enforce delegation chain depth"
assert_file_contains "core/auth/delegation/token.go" '!isSubset\(allowedActions, parent\.AllowedActions\)' "TokenService must compare child actions against parent"
assert_file_contains "core/auth/delegation/token.go" '!isSubset\(allowedTopics, parent\.AllowedTopics\)' "TokenService must compare child topics against parent"

run_go_test "go test delegation scope monotonicity" ./core/auth/delegation -run 'TestIssueAndVerifyDelegationToken|TestIssueDelegationTokenScopeMonotonicityAcrossChain|TestVerifyDelegationTokenRevocationAndScopeDowngrade' -count=1 || probe_fail "delegation TokenService regression tests failed"
run_go_test "go test no-tool chat request/frame contract" ./core/llmchat -run 'TestOpenAIProvider_StreamsTextAndOmitsToolFields|TestOpenAIProvider_IgnoresUnexpectedActionDeltasFromMisconfiguredBackend|TestChatFrameSchemaPinned' -count=1 || probe_fail "informational-only no-tool request/frame tests failed"

log_evidence "live_delegation_token=not_applicable reason=production informational-only chat does not expose delegation bearer frames"
probe_pass "chat delegation path is retired and generic delegation monotonicity remains covered"
