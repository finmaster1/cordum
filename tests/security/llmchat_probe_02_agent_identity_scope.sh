#!/usr/bin/env bash
set -euo pipefail
PROBE_ID="llmchat_probe_02_agent_identity_scope"
# shellcheck source=tests/security/llmchat_common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/llmchat_common.sh"
probe_init
write_probe_manifest "chat-assistant identity has empty AllowedTools/PreapprovedMutatingTools; any direct MCP caller using that identity sees FilterForIdentity remove every tool before policy evaluation"
record_section "attack payload"
log_evidence 'payload: crafted JSON-RPC tools/call name=cordum_unlist_tool_xyz or cordum_approve_job under chat-assistant identity. Expected defense: chat cannot originate MCP calls, and direct MCP identity filtering returns -32601/method-not-found before policy/approval.'

for retired in core/llmchat/mcpclient.go core/llmchat/fallback_toolcalls.go; do
  if [ -e "${REPO_ROOT}/${retired}" ]; then
    probe_fail "informational-only chat must not retain retired MCP client/tool parser file: ${retired}"
  fi
  log_evidence "assert_file_absent ok: ${retired}"
done

assert_file_contains "core/llmchat/bootstrap.go" 'func expectedAllowedTools\(\) \[\]string' "chat-assistant AllowedTools must be code-pinned"
assert_file_contains "core/llmchat/bootstrap.go" 'expectedAllowedTools is intentionally empty' "AllowedTools must be intentionally empty under informational-only scope"
assert_file_contains "core/llmchat/bootstrap.go" 'expectedPreapprovedMutatingTools is intentionally empty' "PreapprovedMutatingTools must be intentionally empty"
assert_file_not_contains "core/llmchat/bootstrap.go" 'mcp\.Tool[A-Za-z]+' "chat bootstrap must not grant MCP tools"
assert_file_contains "core/mcp/filter.go" 'func FilterForIdentity' "MCP identity filter must exist for direct MCP clients"
assert_file_contains "core/mcp/filter.go" 'identityAdmitsTool\(id\.AllowedTools, tool\.Name\)' "AllowedTools gate must run before tools are exposed"
assert_file_contains "core/mcp/server.go" 'jsonRPCMethodNotFoundCode[[:space:]]*=[[:space:]]*-32601' "MCP method-not-found code must be -32601"
assert_file_contains "core/mcp/server.go" 'ErrMethodNotFound' "MCP server must translate filtered/missing tools to ErrMethodNotFound"

run_go_test "go test MCP identity filter" ./core/mcp -run 'TestFilterForIdentity_AllowedToolsGlob|TestFilterForIdentity_NilOrEmpty|TestEvaluateForIdentity_Reasons|TestUnknownMethod' -count=1 || probe_fail "MCP identity-filter/method-not-found tests failed"
run_go_test "go test chat-assistant empty scope" ./core/llmchat -run 'TestBootstrap_LookupMiss_RegistersAndSetsScope|TestBootstrap_DivergentScopeRejected|TestBootstrap_Idempotent' -count=1 || probe_fail "chat-assistant empty-scope tests failed"

log_evidence "live_mcp_call=not_applicable reason=chat no longer owns a delegation token or MCP client; direct MCP identity filtering is covered by retained core/mcp tests"
probe_pass "agent identity scope is empty for chat and FilterForIdentity remains fail-closed for direct MCP callers"
