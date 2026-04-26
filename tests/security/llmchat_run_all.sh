#!/usr/bin/env bash
# Run all Cordum LLM-chat security probes and aggregate results.

set -euo pipefail

SECURITY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tests/security/llmchat_common.sh
. "${SECURITY_DIR}/llmchat_common.sh"

PROBES=(
  llmchat_probe_01_delegation_scope.sh
  llmchat_probe_02_agent_identity_scope.sh
  llmchat_probe_03_preapproved_mutation.sh
  llmchat_probe_04_prompt_injection.sh
  llmchat_probe_05_parser_pinning.sh
  llmchat_probe_06_loopback_binding.sh
  llmchat_probe_07_session_hijack.sh
  llmchat_probe_08_admin_authz.sh
  llmchat_probe_09_ws_origin.sh
  llmchat_probe_10_rate_limit.sh
  llmchat_probe_11_log_redaction.sh
  llmchat_probe_12_entitlement_bypass.sh
)

mkdir -p "${SECURITY_OUT_DIR}"
RESULTS_TSV="${SECURITY_OUT_DIR}/security-review-results.tsv"
RESULTS_JSON="${SECURITY_OUT_DIR}/security-review-results.json"
: >"${RESULTS_TSV}"

if [ "${LLMCHAT_SECURITY_COMPOSE_UP:-0}" = "1" ]; then
  PROBE_ID="compose-baseline"
  PROBE_OUT_DIR="${SECURITY_OUT_DIR}/${PROBE_ID}"
  EVIDENCE_FILE="${PROBE_OUT_DIR}/evidence.txt"
  probe_init
  compose_clean_up
fi

pass=0
fail=0
skip=0
live_missing=0

for probe in "${PROBES[@]}"; do
  path="${SECURITY_DIR}/${probe}"
  name="${probe%.sh}"
  start=$(date +%s)
  stdout_file="${SECURITY_OUT_DIR}/${name}.stdout"
  stderr_file="${SECURITY_OUT_DIR}/${name}.stderr"
  evidence="${SECURITY_OUT_DIR}/${name}/evidence.txt"

  if [ ! -f "${path}" ]; then
    status="FAIL"
    code=127
    echo "missing probe script ${path}" >"${stderr_file}"
  else
    set +e
    bash "${path}" >"${stdout_file}" 2>"${stderr_file}"
    code=$?
    set -e
    if [ "${code}" -eq 0 ]; then
      status="PASS"
    elif [ "${code}" -eq 77 ]; then
      status="SKIP"
    else
      status="FAIL"
    fi
  fi

  end=$(date +%s)
  duration=$((end - start))
  case "${status}" in
    PASS) pass=$((pass + 1)) ;;
    SKIP) skip=$((skip + 1)) ;;
    FAIL) fail=$((fail + 1)) ;;
  esac
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "${name}" "${status}" "${code}" "${duration}" "${evidence}" "${stdout_file}" "${stderr_file}" >>"${RESULTS_TSV}"
  printf '[%s] %s (exit=%s, duration=%ss)\n' "${name}" "${status}" "${code}" "${duration}"
done

: >"${SECURITY_OUT_DIR}/live-missing-evidence.txt"
while IFS=$'\t' read -r probe status code duration evidence stdout stderr; do
  if [ -f "${evidence}" ] && grep -nE '=(not_run|not_asserted)([[:space:]]|$)' "${evidence}" >>"${SECURITY_OUT_DIR}/live-missing-evidence.txt" 2>&1; then
    live_missing=$((live_missing + 1))
  fi
done <"${RESULTS_TSV}"

${PYTHON_BIN} - "${RESULTS_TSV}" "${RESULTS_JSON}" "${pass}" "${fail}" "${skip}" "${live_missing}" <<'PY'
import csv, json, sys
rows = []
with open(sys.argv[1], newline='', encoding='utf-8') as f:
    for row in csv.reader(f, delimiter='\t'):
        if not row:
            continue
        probe, status, code, duration, evidence, stdout, stderr = row
        rows.append({
            "probe": probe,
            "status": status,
            "exit_code": int(code),
            "duration_seconds": int(duration),
            "evidence": evidence,
            "stdout": stdout,
            "stderr": stderr,
        })
summary = {
    "pass": int(sys.argv[3]),
    "fail": int(sys.argv[4]),
    "skip": int(sys.argv[5]),
    "live_missing": int(sys.argv[6]),
    "total": len(rows),
    "live_required": bool(__import__('os').environ.get('LLMCHAT_SECURITY_REQUIRE_LIVE') == '1'),
}
with open(sys.argv[2], 'w', encoding='utf-8') as f:
    json.dump({"summary": summary, "probes": rows}, f, indent=2, sort_keys=True)
    f.write('\n')
print(json.dumps(summary, sort_keys=True))
PY

if [ "${fail}" -gt 0 ]; then
  echo "[llmchat_run_all] FAILED: ${fail} probe(s) failed; see ${RESULTS_JSON}" >&2
  exit 1
fi
if [ "${LLMCHAT_SECURITY_REQUIRE_LIVE:-0}" = "1" ] && [ "${skip}" -gt 0 ]; then
  echo "[llmchat_run_all] FAILED: live required but ${skip} probe(s) skipped; see ${RESULTS_JSON}" >&2
  exit 1
fi
if [ "${LLMCHAT_SECURITY_REQUIRE_LIVE:-0}" = "1" ] && [ "${live_missing}" -gt 0 ]; then
  echo "[llmchat_run_all] FAILED: live required but ${live_missing} probe evidence file(s) still contain not_run/not_asserted markers; see ${SECURITY_OUT_DIR}/live-missing-evidence.txt" >&2
  exit 1
fi

# In default (non-REQUIRE_LIVE) mode, surface partial-coverage explicitly so a
# green PASS is not mistaken for full DoD coverage. The previous behavior printed
# only "OK: pass=N" which let live_missing markers go unnoticed in CI/PR review.
if [ "${live_missing}" -gt 0 ]; then
  echo "[llmchat_run_all] PARTIAL: pass=${pass} skip=${skip} fail=${fail}, live_missing=${live_missing} (set LLMCHAT_SECURITY_REQUIRE_LIVE=1 on a GPU/clean-stack runner to gate-check live probes); results=${RESULTS_JSON}"
  exit 0
fi

echo "[llmchat_run_all] OK: pass=${pass} skip=${skip} fail=${fail}; results=${RESULTS_JSON}"
exit 0
