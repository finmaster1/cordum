#!/usr/bin/env bash
set -euo pipefail
PROBE_NAME="probe-06"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "${SCRIPT_DIR}/common-fixture.sh"

write_probe_header
log_evidence "title=ops runbook"
runbook="docs/llmchat/ops-runbook.md"
[[ -f "${runbook}" ]] || probe_fail "missing ${runbook}"
log_evidence "runbook=${runbook}"
log_evidence "runbook_bytes=$(wc -c <"${runbook}" | tr -d ' ')"

required_headings=(
  '## Deploy'
  '## Upgrade'
  '## Rollback'
  '## Scale'
  '## Check health'
  '## Common alerts'
  '## Known issues and workarounds'
  '## Escalation matrix'
)
for heading in "${required_headings[@]}"; do
  grep -Fq "${heading}" "${runbook}" || probe_fail "missing runbook heading: ${heading}"
  log_evidence "heading_present=${heading}"
done

for term in 'Ollama' 'Enterprise license' 'llm_chat_assistant' '/healthz' '/readyz' '/metrics' 'rollback' 'scale' 'P0' 'redact'; do
  grep -Fqi "${term}" "${runbook}" || probe_fail "runbook missing required term: ${term}"
  log_evidence "term_present=${term}"
done

if grep -EIn 'Bearer [A-Za-z0-9._-]+|eyJ[A-Za-z0-9._-]+|sk-[A-Za-z0-9_-]{12,}|AKIA[0-9A-Z]{16}' "${runbook}" >"${probe_dir}/runbook-secret-hits.txt"; then
  cat "${probe_dir}/runbook-secret-hits.txt" >>"${evidence_file}"
  probe_fail "secret-like string found in runbook"
fi
probe_pass
