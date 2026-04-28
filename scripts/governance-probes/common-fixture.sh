#!/usr/bin/env bash
# Common fixture for the LLM-chat governance senior-review probes
# (task-01aaa6bd scope-reduced surviving probes). Source this from each probe-NN.sh.
#
# Required env:
#   CORDUM_API_KEY    — admin API key (default reads from cordum/.env if unset)
#   CORDUM_BASE       — gateway base URL (default https://localhost:8081)
#   CURL_TLS_FLAG     — TLS flag for curl (default --insecure for local dev)
#
# Provides:
#   api_get <path>           — GET against $CORDUM_BASE
#   api_post <path> <body>   — POST JSON
#   audit_count <type>       — total count of audit events of a given type
#   require_jq               — exits 1 if jq is missing
#   timestamp_iso            — current UTC timestamp in RFC3339

set -Eeuo pipefail

: "${CORDUM_BASE:=https://localhost:8081}"
: "${CURL_TLS_FLAG:=--insecure}"

if [[ -z "${CORDUM_API_KEY:-}" ]]; then
  if [[ -f "$(dirname "${BASH_SOURCE[0]}")/../../.env" ]]; then
    # shellcheck disable=SC2002
    CORDUM_API_KEY=$(cat "$(dirname "${BASH_SOURCE[0]}")/../../.env" | grep -E '^CORDUM_API_KEY=' | head -1 | cut -d= -f2-)
  fi
fi
if [[ -z "${CORDUM_API_KEY:-}" ]]; then
  echo "FATAL: CORDUM_API_KEY not set and not in cordum/.env" >&2
  exit 2
fi
export CORDUM_API_KEY CORDUM_BASE CURL_TLS_FLAG

require_jq() {
  command -v jq >/dev/null 2>&1 || { echo "jq required" >&2; exit 2; }
}

api_get() {
  curl -sk $CURL_TLS_FLAG -m 10 -H "X-API-Key: $CORDUM_API_KEY" "${CORDUM_BASE}$1"
}

api_post() {
  curl -sk $CURL_TLS_FLAG -m 10 -H "X-API-Key: $CORDUM_API_KEY" \
       -H "Content-Type: application/json" \
       -X POST -d "$2" "${CORDUM_BASE}$1"
}

audit_count() {
  # NOTE: endpoint corrected per historical governance finding F5
  # path is /api/v1/audit/query, NOT /api/v1/audit/events
  local type="$1"
  api_get "/api/v1/audit/query?type=${type}&limit=1" | jq -r '.total // 0'
}

timestamp_iso() {
  date -u +%Y-%m-%dT%H:%M:%SZ
}
