#!/usr/bin/env bash
set -euo pipefail
PROBE_NAME="probe-02"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "${SCRIPT_DIR}/common-fixture.sh"

write_probe_header
log_evidence "title=metrics + cardinality"
require_cmd curl

metrics_file="${probe_dir}/metrics.prom"
if ! fetch_metrics >"${metrics_file}"; then
  probe_fail "failed to fetch metrics from ${LLMCHAT_METRICS_URL}"
fi
log_evidence "metrics_file=${metrics_file}"
log_evidence "metrics_bytes=$(wc -c <"${metrics_file}" | tr -d ' ')"

required=(
  chat_sessions_active
  chat_tool_calls_total
  chat_approval_required_total
  chat_vllm_latency_seconds
  chat_token_budget_used_total
  chat_errors_total
)
for family in "${required[@]}"; do
  assert_metric_family "${metrics_file}" "${family}"
  log_evidence "family_present=${family}"
done

run_python - "${metrics_file}" >>"${evidence_file}" <<'PY'
import re, sys
from collections import defaultdict
path = sys.argv[1]
series = defaultdict(set)
for raw in open(path, encoding='utf-8', errors='replace'):
    line = raw.strip()
    if not line or line.startswith('#'):
        continue
    m = re.match(r'^([a-zA-Z_:][a-zA-Z0-9_:]*)(?:\{([^}]*)\})?', line)
    if not m:
        continue
    name, labels = m.group(1), m.group(2) or ''
    if name.startswith('chat_'):
        series[name].add(labels)
        if re.search(r'(session_id|principal|tenant|token|prompt|trace_id)\s*=', labels):
            print(f'FORBIDDEN_LABEL_NAME {name} {{{labels}}}')
            sys.exit(2)
        if re.search(r'[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|Bearer\s+|eyJ[A-Za-z0-9._-]+|sk-[A-Za-z0-9_-]{12,}', labels):
            print(f'FORBIDDEN_LABEL_VALUE {name} {{{labels}}}')
            sys.exit(3)
for name in sorted(series):
    count = len(series[name])
    print(f'cardinality {name}={count}')
    if count > 30:
        print(f'CARDINALITY_TOO_HIGH {name}={count}')
        sys.exit(4)
PY
probe_pass
