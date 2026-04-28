#!/usr/bin/env bash
set -euo pipefail
PROBE_NAME="probe-09"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "${SCRIPT_DIR}/common-fixture.sh"

write_probe_header
log_evidence "title=alert rules"
alerts="cordum-helm/alerts/llm-chat.yaml"
[[ -f "${alerts}" ]] || probe_fail "missing ${alerts}"
log_evidence "alerts=${alerts}"

required_alerts=(
  LLMChatBackendDown
  LLMChatHighErrorRate
  LLMChatApprovalBacklogHigh
  LLMChatNoSessionsFor30m
)
for alert in "${required_alerts[@]}"; do
  grep -Fq "alert: ${alert}" "${alerts}" || probe_fail "missing alert ${alert}"
  log_evidence "alert_present=${alert}"
done

for metric in chat_sessions_active chat_errors_total chat_approval_required_total; do
  grep -Fq "${metric}" "${alerts}" || probe_fail "alert rules missing metric ${metric}"
  log_evidence "metric_referenced=${metric}"
done

grep -Eq 'for:[[:space:]]+5m' "${alerts}" || probe_fail "missing 5m alert duration"
grep -Eq 'for:[[:space:]]+30m' "${alerts}" || probe_fail "missing 30m zero-session duration"
log_evidence "durations_present=5m,30m"

if command -v promtool >/dev/null 2>&1; then
  promtool check rules "${alerts}" >>"${evidence_file}"
  log_evidence "promtool=pass"
else
  log_evidence "promtool=not_available static_yaml_checks_only"
fi
probe_pass
