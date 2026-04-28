#!/usr/bin/env bash
set -euo pipefail
PROBE_ID="llmchat_probe_03_preapproved_mutation"
# shellcheck source=tests/security/llmchat_common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/llmchat_common.sh"
probe_init
write_probe_manifest "SUPERSEDED: informational-only chat has no mutation/tool path, so PreapprovedMutatingTools is empty and not production-scored"
record_section "attack payload"
log_evidence 'payload (historical): ask chat to call cordum_update_policy_bundle / approve_job / cancel_job / submit_job. Expected current defense: no chat MCP client, AllowedTools=[], PreapprovedMutatingTools=[], provider request has no tools/tool_choice, and frames cannot be approval_required/tool_call.'

log_evidence "scope=superseded reason=2026-04-28 informational-only rescope removed chat->MCP mutations"
for retired in core/llmchat/approvals.go core/llmchat/approvals_nats.go core/llmchat/mcpclient.go core/llmchat/apiclient.go; do
  if [ -e "${REPO_ROOT}/${retired}" ]; then
    probe_fail "retired mutation/tool-calling file still exists: ${retired}"
  fi
  log_evidence "assert_file_absent ok: ${retired}"
done

assert_file_contains "core/llmchat/bootstrap.go" 'expectedPreapprovedMutatingTools is intentionally empty' "preapproved mutating tools must be intentionally empty"
assert_file_contains "core/llmchat/bootstrap.go" 'preapproved_mutating_tools_count": "0"' "bootstrap audit must report zero preapproved mutating tools"
assert_file_not_contains "core/llmchat/bootstrap.go" 'ToolSubmitJob|ToolApproveJob|ToolRejectJob|ToolCancelJob|ToolTriggerWorkflow|ToolUpdatePolicyBundle' "chat bootstrap must not list mutating MCP tools"
assert_file_contains "core/llmchat/agent.go" 'FrameAssistantDelta' "informational frame schema should stream text"
assert_file_not_contains "core/llmchat/agent.go" 'FrameApprovalRequired|FrameToolCall|approval_required|tool_call' "agent must not emit retired mutation/tool frames"
assert_file_contains "core/llmchat/provider_openai.go" 'chat never sends OpenAI `tools` or `tool_choice`' "provider must not send tool-calling request fields"

run_go_test "go test empty preapproval scope and no-tool turn" ./core/llmchat -run 'TestBootstrap_LookupMiss_RegistersAndSetsScope|TestBootstrap_DivergentScopeRejected|TestTurn_InformationalSingleProviderCallStreamsFinal|TestOpenAIProvider_StreamsTextAndOmitsToolFields' -count=1 || probe_fail "informational-only mutation-retirement tests failed"

probe_pass "preapproved mutation exploit probe is superseded; current evidence proves no chat mutation path exists"
