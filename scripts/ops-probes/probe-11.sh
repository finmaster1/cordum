#!/usr/bin/env bash
set -euo pipefail
PROBE_NAME="probe-11"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "${SCRIPT_DIR}/common-fixture.sh"

write_probe_header
log_evidence "title=admin debug dump"

hits="${probe_dir}/debug-dump-hits.txt"
grep -RInE 'debug dump|debug_dump|support bundle|support_bundle|frame log|frame_log|transcript export|export.*transcript|session.*dump|dump.*session' core/llmchat core/controlplane/gateway dashboard/src docs/llmchat 2>/dev/null >"${hits}" || true
log_evidence "debug_dump_static_hits=$(wc -l <"${hits}" | tr -d ' ')"
head -40 "${hits}" >>"${evidence_file}" || true

if [[ -n "${LLMCHAT_DEBUG_DUMP_URL:-}" && "${LLMCHAT_OPS_LIVE}" == "1" ]]; then
  require_cmd curl
  curl "${curl_common[@]}" "${LLMCHAT_DEBUG_DUMP_URL}" >"${probe_dir}/debug-dump.json" || probe_fail "debug dump URL configured but request failed"
  scan_for_secret_patterns "${probe_dir}/debug-dump.json" || probe_fail "secret-like pattern found in debug dump"
  log_evidence "live_debug_dump=pass"
else
  log_evidence "live_debug_dump=not_run set LLMCHAT_DEBUG_DUMP_URL and LLMCHAT_OPS_LIVE=1"
fi

if ! grep -RInE 'Handle.*Debug|DebugDump|support_bundle|debug_dump' core/llmchat core/controlplane/gateway dashboard/src >/dev/null 2>&1; then
  log_evidence "finding=P1 admin session debug dump endpoint/UI not implemented"
  probe_fail "admin debug dump support not found"
fi
probe_pass
