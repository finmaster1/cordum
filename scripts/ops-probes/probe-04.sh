#!/usr/bin/env bash
set -euo pipefail
PROBE_NAME="probe-04"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "${SCRIPT_DIR}/common-fixture.sh"

write_probe_header
log_evidence "title=admin session viewer + audit"

handler="core/llmchat/handlers_admin.go"
page="dashboard/src/pages/settings/ChatAssistantSessionsPage.tsx"
hook="dashboard/src/hooks/useChatAssistantSessions.ts"
route_test="dashboard/src/App.copilot-session-route.test.tsx"
for file in "${handler}" "${page}" "${hook}"; do
  [[ -f "${file}" ]] || probe_fail "missing expected admin viewer file ${file}"
  log_evidence "file_present=${file}"
done

for pattern in 'HandleListSessions' 'HandleGetSession' 'chat.read_all' 'NextCursor' 'tenant' 'user_principal'; do
  if grep -RIn "${pattern}" "${handler}" "${page}" "${hook}" >"${probe_dir}/pattern-${pattern//[^A-Za-z0-9_]/_}.txt"; then
    log_evidence "pattern_present=${pattern}"
  else
    log_evidence "pattern_missing=${pattern}"
  fi
done

search_hits="${probe_dir}/search-hits.txt"
grep -RInE 'search|query|session_id|user_principal|tenant' "${page}" "${hook}" "${handler}" >"${search_hits}" || true
log_evidence "search_static_hits=$(wc -l <"${search_hits}" | tr -d ' ')"
head -30 "${search_hits}" >>"${evidence_file}" || true

# Admin reads must themselves be audit events. Runtime currently logs admin
# reads via slog, so require a concrete admin-session-viewed event/action rather
# than generic audit plumbing symbols.
audit_hits="${probe_dir}/admin-view-audit-hits.txt"
grep -RInE 'chat\.admin_session_viewed|AdminSessionViewed|admin_session_viewed|SIEMActionChatAdminSessionViewed' core/llmchat core/audit dashboard/src/pages/settings >"${audit_hits}" || true
log_evidence "admin_view_audit_hits=$(wc -l <"${audit_hits}" | tr -d ' ')"
head -30 "${audit_hits}" >>"${evidence_file}" || true

if [[ -n "${CORDUM_API_KEY:-}" && "${LLMCHAT_OPS_LIVE}" == "1" ]]; then
  status_file="${probe_dir}/admin-list.status"
  body_file="${probe_dir}/admin-list.json"
  http_code=$(curl "${curl_common[@]}" -H "X-API-Key: ${CORDUM_API_KEY}" -o "${body_file}" -w '%{http_code}' "${CORDUM_API_BASE}/chat/sessions?limit=1" || true)
  printf '%s\n' "${http_code}" >"${status_file}"
  log_evidence "live_admin_list_http=${http_code}"
else
  log_evidence "live_admin_api=not_run set CORDUM_API_KEY and LLMCHAT_OPS_LIVE=1"
fi

failures=0
if [[ ! -s "${audit_hits}" ]]; then
  log_evidence "finding=P1 missing chat.admin_session_viewed SIEM/audit event for admin reads"
  failures=$((failures+1))
fi
if ! grep -RInE 'placeholder=.*[Ss]earch|Search sessions|params\.set\("(q|query|search|user|user_principal|tenant|session_id)"' "${page}" "${hook}" >/dev/null 2>&1; then
  log_evidence "finding=P1 admin session viewer/search API not evident"
  failures=$((failures+1))
fi
if grep -RIn '/copilot/sessions' "${page}" "${route_test}" >/dev/null 2>&1; then
  log_evidence "finding=P2 admin viewer appears to route detail clicks through /copilot/sessions instead of chat-specific read-only transcript"
fi

if [[ "${failures}" -gt 0 ]]; then
  probe_fail "admin session viewer audit/search requirements not met"
fi
probe_pass
