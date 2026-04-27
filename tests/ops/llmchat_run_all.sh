#!/usr/bin/env bash
# Run all Cordum LLM-chat production-readiness probes and aggregate results.

set -euo pipefail

OPS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tests/ops/llmchat_common.sh
. "${OPS_DIR}/llmchat_common.sh"

PROBES=(
  llmchat_probe_01_cold_start.sh
  llmchat_probe_02_vllm_crash.sh
  llmchat_probe_03_long_tool_conversation.sh
  llmchat_probe_04_redis_partition.sh
  llmchat_probe_05_nats_partition.sh
  llmchat_probe_06_tier1_capacity.sh
  llmchat_probe_07_gpu_oom.sh
  llmchat_probe_08_prefix_caching.sh
  llmchat_probe_09_session_ttl.sh
  llmchat_probe_10_token_budget.sh
  llmchat_probe_11_repeat_call.sh
  llmchat_probe_12_rolling_upgrade.sh
  llmchat_probe_13_hf_cache_loss.sh
  llmchat_probe_14_tier2_awq.sh
  llmchat_probe_15_tier3_a100.sh
  llmchat_probe_16_concurrent_isolation.sh
  llmchat_probe_17_graceful_shutdown.sh
  llmchat_probe_18_backpressure.sh
  llmchat_probe_19_ollama_cold_start.sh
)

mkdir -p "${OPS_OUT_DIR}"
RESULTS_TSV="${OPS_OUT_DIR}/ops-results.tsv"
RESULTS_JSON="${OPS_OUT_DIR}/ops-results.json"
: >"${RESULTS_TSV}"

pass=0
fail=0
skip=0
defer=0

DEFERRED_PROBES="${LLMCHAT_OPS_DEFERRED_PROBES:-}"
if [ -z "${DEFERRED_PROBES}" ] && [ "${LLMCHAT_OPS_BACKEND}" = "cpu-vllm-awq" ]; then
  DEFERRED_PROBES="llmchat_probe_06_tier1_capacity.sh llmchat_probe_07_gpu_oom.sh llmchat_probe_12_rolling_upgrade.sh llmchat_probe_13_hf_cache_loss.sh llmchat_probe_14_tier2_awq.sh llmchat_probe_15_tier3_a100.sh llmchat_probe_19_ollama_cold_start.sh"
fi
if [ -z "${DEFERRED_PROBES}" ] && [ "${LLMCHAT_OPS_BACKEND}" = "ollama-cpu" ]; then
  # Probes 02/07/13/14/15 grep the vLLM cmdline or assume FP8/AWQ weights;
  # probe 06 is the H100/Tier-1 capacity gate that does not apply to a CPU
  # Ollama box. Defer them with the same reason as the cpu-vllm-awq path.
  DEFERRED_PROBES="llmchat_probe_02_vllm_crash.sh llmchat_probe_06_tier1_capacity.sh llmchat_probe_07_gpu_oom.sh llmchat_probe_13_hf_cache_loss.sh llmchat_probe_14_tier2_awq.sh llmchat_probe_15_tier3_a100.sh"
fi
if [ -z "${DEFERRED_PROBES}" ] && [ "${LLMCHAT_OPS_BACKEND}" = "gpu-fp8" ]; then
  # Probe 19 is Ollama-specific; defer cleanly on the GPU profile.
  DEFERRED_PROBES="llmchat_probe_19_ollama_cold_start.sh"
fi
DEFERRED_REASON="${LLMCHAT_OPS_DEFERRED_REASON:-formal CPU-mode scope decision: GPU/k8s tier-matrix probe deferred under task-a5d09fad}"

is_deferred_probe() {
  local probe="$1" item
  for item in ${DEFERRED_PROBES}; do
    if [ "${probe}" = "${item}" ] || [ "${probe%.sh}" = "${item%.sh}" ]; then
      return 0
    fi
  done
  return 1
}

for probe in "${PROBES[@]}"; do
  path="${OPS_DIR}/${probe}"
  name="${probe%.sh}"
  start=$(date +%s)
  stdout_file="${OPS_OUT_DIR}/${name}.stdout"
  stderr_file="${OPS_OUT_DIR}/${name}.stderr"
  evidence="${OPS_OUT_DIR}/${name}/evidence.txt"
  if is_deferred_probe "${probe}"; then
    status='DEFERRED'
    code=78
    mkdir -p "$(dirname "${evidence}")"
    : >"${stdout_file}"
    : >"${stderr_file}"
    {
      printf 'probe=%s\n' "${name}"
      printf 'repo_root=%s\n' "${REPO_ROOT}"
      printf 'started_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
      printf 'live=%s require_live=%s\n' "${LLMCHAT_OPS_LIVE:-0}" "${LLMCHAT_OPS_REQUIRE_LIVE:-0}"
      printf 'backend=%s\n' "${LLMCHAT_OPS_BACKEND}"
      printf 'status=DEFERRED message=%s\n' "${DEFERRED_REASON}"
    } >"${evidence}"
  elif [ ! -f "${path}" ]; then
    status='FAIL'
    code=127
    echo "missing probe script ${path}" >"${stderr_file}"
  else
    set +e
    bash "${path}" >"${stdout_file}" 2>"${stderr_file}"
    code=$?
    set -e
    if [ "${code}" -eq 0 ]; then
      status='PASS'
    elif [ "${code}" -eq 77 ]; then
      status='SKIP'
    elif [ "${code}" -eq 78 ]; then
      status='DEFERRED'
    else
      status='FAIL'
    fi
  fi
  end=$(date +%s)
  duration=$((end - start))
  case "${status}" in
    PASS) pass=$((pass + 1)) ;;
    SKIP) skip=$((skip + 1)) ;;
    DEFERRED) defer=$((defer + 1)) ;;
    FAIL) fail=$((fail + 1)) ;;
  esac
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "${name}" "${status}" "${code}" "${duration}" "${evidence}" "${stdout_file}" "${stderr_file}" >>"${RESULTS_TSV}"
  printf '[%s] %s (exit=%s, duration=%ss)\n' "${name}" "${status}" "${code}" "${duration}"
done

if [ -z "${PYTHON_BIN}" ]; then
  echo '[llmchat_ops_run_all] FAILED: python not found; set LLMCHAT_PYTHON_BIN' >&2
  exit 2
fi
${PYTHON_BIN} - "${RESULTS_TSV}" "${RESULTS_JSON}" "${pass}" "${fail}" "${skip}" "${defer}" <<'PY'
import csv, json, os, sys
rows=[]
with open(sys.argv[1], newline='', encoding='utf-8') as f:
    for row in csv.reader(f, delimiter='\t'):
        if not row:
            continue
        probe,status,code,duration,evidence,stdout,stderr=row
        rows.append({
            'probe': probe,
            'status': status,
            'exit_code': int(code),
            'duration_seconds': int(duration),
            'evidence': evidence,
            'stdout': stdout,
            'stderr': stderr,
        })
summary={
    'pass': int(sys.argv[3]),
    'fail': int(sys.argv[4]),
    'skip': int(sys.argv[5]),
    'defer': int(sys.argv[6]),
    'total': len(rows),
    'live_required': os.environ.get('LLMCHAT_OPS_REQUIRE_LIVE') == '1',
    'backend': os.environ.get('LLMCHAT_OPS_BACKEND', 'gpu-fp8'),
}
with open(sys.argv[2], 'w', encoding='utf-8') as f:
    json.dump({'summary': summary, 'probes': rows}, f, indent=2, sort_keys=True)
    f.write('\n')
print(json.dumps(summary, sort_keys=True))
PY

if [ "${fail}" -gt 0 ]; then
  echo "[llmchat_ops_run_all] FAILED: ${fail} probe(s) failed; see ${RESULTS_JSON}" >&2
  exit 1
fi
if [ "${LLMCHAT_OPS_REQUIRE_LIVE:-0}" = '1' ] && [ "${skip}" -gt 0 ]; then
  echo "[llmchat_ops_run_all] FAILED: live required but ${skip} probe(s) skipped; see ${RESULTS_JSON}" >&2
  exit 1
fi

echo "[llmchat_ops_run_all] OK: pass=${pass} skip=${skip} defer=${defer} fail=${fail}; results=${RESULTS_JSON}"
exit 0
