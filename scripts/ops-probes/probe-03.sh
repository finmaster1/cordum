#!/usr/bin/env bash
set -euo pipefail
PROBE_NAME="probe-03"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "${SCRIPT_DIR}/common-fixture.sh"

write_probe_header
log_evidence "title=trace propagation / Jaeger"

trace_scan="${probe_dir}/trace-static.txt"
{
  grep -RInE 'OTEL|OpenTelemetry|Jaeger|traceparent|tracer|TraceProvider' cmd/cordum-llm-chat core/llmchat docker-compose.yml cordum-helm 2>/dev/null || true
} >"${trace_scan}"
log_evidence "static_trace_scan=${trace_scan}"
log_evidence "static_trace_matches=$(wc -l <"${trace_scan}" | tr -d ' ')"
head -40 "${trace_scan}" >>"${evidence_file}" || true

if [[ -n "${LLMCHAT_JAEGER_QUERY_URL:-}" ]]; then
  require_cmd curl
  log_evidence "jaeger_query_url=${LLMCHAT_JAEGER_QUERY_URL}"
  if curl "${curl_common[@]}" "${LLMCHAT_JAEGER_QUERY_URL}" >"${probe_dir}/jaeger.json"; then
    log_evidence "jaeger_query=ok bytes=$(wc -c <"${probe_dir}/jaeger.json" | tr -d ' ')"
  else
    probe_fail "Jaeger query configured but request failed"
  fi
  required_ops="${LLMCHAT_JAEGER_REQUIRED_OPS:-chat.ws.connect chat.turn llm.inference chat.audit.emit}"
  missing_ops=()
  for op in ${required_ops}; do
    if grep -Fq "\"operationName\":\"${op}\"" "${probe_dir}/jaeger.json"; then
      log_evidence "jaeger_operation_${op}=present"
    else
      log_evidence "jaeger_operation_${op}=missing"
      missing_ops+=("${op}")
    fi
  done
  if (( ${#missing_ops[@]} > 0 )); then
    probe_fail "Jaeger query missing required operation(s): ${missing_ops[*]}"
  fi
else
  log_evidence "jaeger_query=not_configured"
fi

# The scan may find passive trace_id fields, but this probe requires an OTEL/
# Jaeger exporter/provider. Fail unless explicit live Jaeger evidence exists.
if [[ -z "${LLMCHAT_JAEGER_QUERY_URL:-}" ]]; then
  probe_fail "no llm-chat OTEL/Jaeger exporter evidence configured"
fi
probe_pass
