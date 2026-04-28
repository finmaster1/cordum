#!/usr/bin/env bash
set -euo pipefail
PROBE_NAME="probe-01"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "${SCRIPT_DIR}/common-fixture.sh"

write_probe_header
log_evidence "title=structured logs + redaction"
require_docker_compose

service="$(resolve_llmchat_log_service)"
log_file="${probe_dir}/llm-chat.log"
rm -f "${probe_dir}/json-invalid-samples.txt" "${log_file}.secret_hits"
if ! docker_compose logs --no-color --since "${LLMCHAT_LOG_SINCE:-1h}" "${service}" >"${log_file}" 2>"${probe_dir}/docker-logs.stderr"; then
  cat "${probe_dir}/docker-logs.stderr" >>"${evidence_file}" || true
  probe_skip "docker compose logs failed for service ${service}"
fi

line_count=$(wc -l <"${log_file}" | tr -d ' ')
log_evidence "service=${service}"
log_evidence "log_file=${log_file}"
log_evidence "line_count=${line_count}"

secret_result=0
scan_for_secret_patterns "${log_file}" || secret_result=$?

json_bad=0
if [[ "${line_count}" != "0" ]]; then
  # Docker compose prefixes service names. Strip '<service>  | ' when present,
  # then require the payload to be JSON. This makes text slog output fail with
  # a concrete evidence sample instead of silently passing.
  run_python - "${log_file}" "${probe_dir}/json-invalid-samples.txt" <<'PY' || json_bad=$?
import json, re, sys
src, sample = sys.argv[1], sys.argv[2]
bad=[]
with open(src, 'r', encoding='utf-8', errors='replace') as f:
    for lineno, line in enumerate(f, 1):
        payload = re.sub(r'^[A-Za-z0-9_.-]+\s+\|\s?', '', line.rstrip('\n'))
        if not payload.strip():
            continue
        try:
            json.loads(payload)
        except Exception:
            bad.append((lineno, payload[:240]))
            if len(bad) >= 10:
                break
if bad:
    with open(sample, 'w', encoding='utf-8') as out:
        for lineno, payload in bad:
            out.write(f'{lineno}: {payload}\n')
    print(f'non_json_count_sampled={len(bad)}')
    sys.exit(1)
print('json_lines_ok=true')
PY
fi

if [[ -s "${probe_dir}/json-invalid-samples.txt" ]]; then
  log_evidence "json_validation=FAIL first_invalid_samples:"
  cat "${probe_dir}/json-invalid-samples.txt" >>"${evidence_file}"
else
  log_evidence "json_validation=PASS"
fi

# Safe correlation-field presence check. Only meaningful if JSON passed.
if [[ "${json_bad}" == "0" && "${line_count}" != "0" ]]; then
  run_python - "${log_file}" >>"${evidence_file}" <<'PY'
import json, re, sys
fields = {"session_id":0, "user_principal":0, "tenant":0, "trace_id":0}
for line in open(sys.argv[1], encoding='utf-8', errors='replace'):
    payload = re.sub(r'^[A-Za-z0-9_.-]+\s+\|\s?', '', line.rstrip('\n'))
    if not payload.strip():
        continue
    obj = json.loads(payload)
    for k in fields:
        if k in obj:
            fields[k] += 1
for k, v in fields.items():
    print(f'field_{k}_count={v}')
PY
fi

if [[ "${json_bad}" != "0" ]]; then
  probe_fail "llm-chat logs are not JSON structured"
fi
if [[ "${secret_result}" != "0" ]]; then
  probe_fail "secret-like patterns found in logs"
fi
probe_pass
