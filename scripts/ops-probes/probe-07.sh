#!/usr/bin/env bash
set -euo pipefail
PROBE_NAME="probe-07"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "${SCRIPT_DIR}/common-fixture.sh"

write_probe_header
log_evidence "title=Grafana dashboard"
dashboard="cordum-helm/dashboards/llm-chat.json"
[[ -f "${dashboard}" ]] || probe_fail "missing ${dashboard}"
log_evidence "dashboard=${dashboard}"

run_python - "${dashboard}" >>"${evidence_file}" <<'PY'
import json, sys
path=sys.argv[1]
with open(path, encoding='utf-8') as f:
    obj=json.load(f)
panels=obj.get('panels', [])
print(f'dashboard_title={obj.get("title","")}')
print(f'panel_count={len(panels)}')
required = {
    'chat_sessions_active': False,
    'chat_vllm_latency_seconds_bucket': False,
    'chat_errors_total': False,
    'chat_token_budget_used_total': False,
}
for panel in panels:
    for target in panel.get('targets', []) or []:
        expr = target.get('expr', '')
        for metric in list(required):
            if metric in expr:
                required[metric]=True
for metric, present in required.items():
    print(f'metric_panel_{metric}={present}')
    if not present:
        raise SystemExit(f'missing required dashboard metric {metric}')
if len(panels) < 5:
    raise SystemExit('expected at least 5 panels')
missing_nodata=[]
for panel in panels:
    defaults = ((panel.get('fieldConfig') or {}).get('defaults') or {})
    if defaults.get('noValue') != 'No data':
        missing_nodata.append(panel.get('title','<untitled>'))
if missing_nodata:
    raise SystemExit('panels missing noValue=No data: '+', '.join(missing_nodata))
PY

if [[ -n "${GRAFANA_URL:-}" && "${LLMCHAT_OPS_LIVE}" == "1" ]]; then
  log_evidence "grafana_import=not_implemented_in_probe url=${GRAFANA_URL}"
else
  log_evidence "grafana_import=not_run set GRAFANA_URL and LLMCHAT_OPS_LIVE=1 for screenshot/import evidence"
fi
probe_pass
