#!/usr/bin/env bash
set -euo pipefail

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing dependency: $1" >&2
    exit 1
  fi
}

require git
require docker
require curl
require jq

# Exit code used by port_in_use when no suitable port-checking tool is available.
PORT_IN_USE_NO_TOOL_AVAILABLE=2

resolve_path() {
  local target="$1"
  if command -v realpath >/dev/null 2>&1; then
    realpath "$target"
    return $?
  fi
  if command -v readlink >/dev/null 2>&1; then
    readlink -f "$target"
    return $?
  fi
  (cd "$target" >/dev/null 2>&1 && pwd -P)
}

assert_safe_delete() {
  local target="$1"
  local allow="${CORDUM_E2E_ALLOW_DELETE:-0}"
  local safe_prefix="/tmp/cordum-e2e"
  local resolved

  resolved="$(resolve_path "$target")" || {
    echo "unable to resolve ${target} for safe delete check." >&2
    exit 1
  }

  if [[ "${resolved}" == "/" ]]; then
    echo "refusing to delete ${resolved}." >&2
    exit 1
  fi

  if [[ -n "${repo_root:-}" && "${resolved}" == "${repo_root}" ]]; then
    echo "refusing to delete repo root ${resolved}." >&2
    exit 1
  fi

  if [[ "${allow}" != "1" ]]; then
    case "${resolved}" in
      /tmp|/var|/usr|/etc|/opt|/bin|/sbin|/lib|/lib64|/home|/root|/mnt|/media|/srv)
        echo "refusing to delete ${resolved}; set CORDUM_E2E_ALLOW_DELETE=1 to override." >&2
        exit 1
        ;;
    esac
    if [[ "${resolved}" != "${safe_prefix}"* ]]; then
      echo "refusing to delete ${resolved}; set CORDUM_E2E_ALLOW_DELETE=1 or use DEST_DIR under ${safe_prefix}." >&2
      exit 1
    fi
  fi
}

port_in_use() {
  local port="$1"
  if command -v ss >/dev/null 2>&1; then
    if ss -ltn 2>/dev/null | awk '{print $4}' | grep -E "(^|:)${port}$" >/dev/null 2>&1; then
      return 0
    fi
    return 1
  fi
  if command -v lsof >/dev/null 2>&1; then
    if lsof -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1; then
      return 0
    fi
    return 1
  fi
  if command -v netstat >/dev/null 2>&1; then
    if netstat -ltn 2>/dev/null | awk '{print $4}' | grep -E "(^|:)${port}$" >/dev/null 2>&1; then
      return 0
    fi
    return 1
  fi
  return "${PORT_IN_USE_NO_TOOL_AVAILABLE}"
}

assert_ports_free() {
  local allow="${CORDUM_E2E_ALLOW_PORTS:-0}"
  local default_ports="8081 8082 8080 9092 9093 50051 50070 4222 6379"
  local ports_str="${CORDUM_E2E_PORTS:-$default_ports}"
  # Allow ports to be provided as a comma- or space-separated list.
  ports_str=${ports_str//,/ }
  local -a ports=()
  read -ra ports <<<"$ports_str"
  if [[ "${allow}" == "1" ]]; then
    return 0
  fi
  for port in "${ports[@]}"; do
    local status=0
    port_in_use "${port}" || status=$?
    if [[ "${status}" -eq 0 ]]; then
      echo "port ${port} is already in use; set CORDUM_E2E_ALLOW_PORTS=1 to override." >&2
      exit 1
    fi
    if [[ "${status}" -eq "${PORT_IN_USE_NO_TOOL_AVAILABLE}" ]]; then
      echo "no port-checking tool available; install ss, lsof, or netstat, or set CORDUM_E2E_ALLOW_PORTS=1." >&2
      exit 1
    fi
  done
}

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "${script_dir}/../.." && pwd)"

API_KEY=${CORDUM_API_KEY:-${API_KEY:-}}
if [[ -z "${API_KEY}" ]]; then
  echo "CORDUM_API_KEY is required; export it before running this script." >&2
  exit 1
fi
export CORDUM_API_KEY="${API_KEY}"

TENANT_ID=${CORDUM_TENANT_ID:-default}
ORG_ID=${CORDUM_ORG_ID:-${TENANT_ID}}
export CORDUM_TENANT_ID="${TENANT_ID}"
export CORDUM_ORG_ID="${ORG_ID}"

REPO_URL=${REPO_URL:-https://github.com/cordum-io/cordum.git}
DEST_DIR=${DEST_DIR:-/tmp/cordum-e2e}
VERSION=${VERSION:-main}
USE_RELEASE_IMAGES=${USE_RELEASE_IMAGES:-0}
CORDUM_VERSION=${CORDUM_VERSION:-latest}

if [[ -d "${DEST_DIR}" ]]; then
  if [[ "${CORDUM_E2E_CLEAN:-0}" == "1" ]]; then
    assert_safe_delete "${DEST_DIR}"
    rm -rf "${DEST_DIR}"
  elif [[ "${CORDUM_E2E_REUSE:-0}" == "1" ]]; then
    echo "[e2e] reusing existing ${DEST_DIR}"
  else
    echo "${DEST_DIR} already exists; set CORDUM_E2E_CLEAN=1 to delete or CORDUM_E2E_REUSE=1 to reuse." >&2
    exit 1
  fi
fi

assert_ports_free

if [[ ! -f "${repo_root}/tools/scripts/install.sh" ]]; then
  echo "install.sh not found at ${repo_root}/tools/scripts/install.sh" >&2
  exit 1
fi

if [[ "${CORDUM_E2E_REUSE:-0}" != "1" ]]; then
  echo "[e2e] installing Cordum (${VERSION}) into ${DEST_DIR}"
  REPO_URL="${REPO_URL}" \
  DEST_DIR="${DEST_DIR}" \
  VERSION="${VERSION}" \
  USE_RELEASE_IMAGES="${USE_RELEASE_IMAGES}" \
  CORDUM_VERSION="${CORDUM_VERSION}" \
  CORDUM_API_KEY="${API_KEY}" \
  CORDUM_TENANT_ID="${TENANT_ID}" \
  bash "${repo_root}/tools/scripts/install.sh"
fi

pushd "${DEST_DIR}" >/dev/null

e2e_log() {
  echo "[e2e] $*"
}

resolve_tls_opts() {
  CURL_TLS_OPTS=()
  TLS_CA="${CORDUM_TLS_CA:-}"
  if [[ -z "${TLS_CA}" && -f "./certs/ca/ca.crt" ]]; then
    TLS_CA="./certs/ca/ca.crt"
  fi
  if [[ -n "${TLS_CA}" ]]; then
    CURL_TLS_OPTS=(--cacert "${TLS_CA}")
    if curl --version 2>/dev/null | grep -qi schannel; then
      CURL_TLS_OPTS+=(--ssl-no-revoke)
    fi
    API_BASE="${CORDUM_API_BASE:-https://localhost:8081}"
  else
    API_BASE="${CORDUM_API_BASE:-http://localhost:8081}"
  fi
}

run_decision_ready_approval_smoke() {
  local wf_id=""
  local run_id=""
  local approval_job_id=""
  local approvals_json=""
  local resolved_json=""
  local context_status=""
  local source=""
  local vendor=""
  local amount=""
  local why=""
  local resolved_by=""
  local resolved_comment=""

  cleanup_decision_ready_smoke() {
    if [[ -n "${run_id}" ]]; then
      curl -sS "${CURL_TLS_OPTS[@]}" \
        -H "X-API-Key: ${API_KEY}" \
        -H "X-Tenant-ID: ${TENANT_ID}" \
        -X DELETE "${API_BASE}/api/v1/workflow-runs/${run_id}" >/dev/null 2>&1 || true
    fi
    if [[ -n "${wf_id}" ]]; then
      curl -sS "${CURL_TLS_OPTS[@]}" \
        -H "X-API-Key: ${API_KEY}" \
        -H "X-Tenant-ID: ${TENANT_ID}" \
        -X DELETE "${API_BASE}/api/v1/workflows/${wf_id}" >/dev/null 2>&1 || true
    fi
  }

  trap cleanup_decision_ready_smoke RETURN

  e2e_log "creating decision-ready approval workflow"
  wf_id=$(curl -sS "${CURL_TLS_OPTS[@]}" \
    -H "X-API-Key: ${API_KEY}" \
    -H "X-Tenant-ID: ${TENANT_ID}" \
    -H "Content-Type: application/json" \
    -X POST "${API_BASE}/api/v1/workflows" \
    -d @- <<JSON | jq -r '.id'
{
  "name": "decision-ready-approval-smoke",
  "org_id": "${ORG_ID}",
  "steps": {
    "approve": {
      "type": "approval",
      "name": "Manager Approval",
      "input": {
        "amount": "${input.request.amount}",
        "currency": "${input.request.currency}",
        "vendor": "${input.request.vendor}",
        "items": "${input.request.items}",
        "approval_reason": "${input.request.reason}",
        "next_effect": "Approve to continue Manager Approval."
      },
      "input_schema": {
        "type": "object",
        "properties": {
          "amount": { "type": "number" },
          "currency": { "type": "string" },
          "vendor": { "type": "string" },
          "items": { "type": "array" },
          "approval_reason": { "type": "string" },
          "next_effect": { "type": "string" }
        },
        "required": ["amount", "currency", "vendor", "items", "approval_reason"]
      }
    }
  }
}
JSON
)
  if [[ -z "${wf_id}" || "${wf_id}" == "null" ]]; then
    echo "failed to create decision-ready approval workflow" >&2
    exit 1
  fi

  e2e_log "starting decision-ready approval run"
  run_id=$(curl -sS "${CURL_TLS_OPTS[@]}" \
    -H "X-API-Key: ${API_KEY}" \
    -H "X-Tenant-ID: ${TENANT_ID}" \
    -H "Content-Type: application/json" \
    -X POST "${API_BASE}/api/v1/workflows/${wf_id}/runs" \
    -d @- <<JSON | jq -r '.run_id'
{
  "request": {
    "amount": 1250,
    "currency": "USD",
    "vendor": "Acme Travel",
    "items": ["flight", "hotel"],
    "reason": "manager threshold exceeded"
  }
}
JSON
)
  if [[ -z "${run_id}" || "${run_id}" == "null" ]]; then
    echo "failed to start decision-ready approval run" >&2
    exit 1
  fi

  for _ in {1..20}; do
    approvals_json=$(curl -sS "${CURL_TLS_OPTS[@]}" \
      -H "X-API-Key: ${API_KEY}" \
      -H "X-Tenant-ID: ${TENANT_ID}" \
      "${API_BASE}/api/v1/approvals?include_resolved=false")
    approval_job_id=$(printf '%s' "${approvals_json}" | jq -r --arg wf "${wf_id}" --arg run "${run_id}" '
      .items[]
      | select(.workflow_id == $wf and .workflow_run_id == $run)
      | .job.id // empty
    ' | head -n 1)
    if [[ -n "${approval_job_id}" ]]; then
      break
    fi
    sleep 0.5
  done
  if [[ -z "${approval_job_id}" ]]; then
    echo "approval did not appear in /api/v1/approvals for workflow ${wf_id} run ${run_id}" >&2
    echo "${approvals_json}" | jq '.' >&2 || true
    exit 1
  fi

  source=$(printf '%s' "${approvals_json}" | jq -r --arg wf "${wf_id}" --arg run "${run_id}" '
    .items[]
    | select(.workflow_id == $wf and .workflow_run_id == $run)
    | .decision_summary.source // ""
  ' | head -n 1)
  context_status=$(printf '%s' "${approvals_json}" | jq -r --arg wf "${wf_id}" --arg run "${run_id}" '
    .items[]
    | select(.workflow_id == $wf and .workflow_run_id == $run)
    | .decision_summary.context_status // ""
  ' | head -n 1)
  vendor=$(printf '%s' "${approvals_json}" | jq -r --arg wf "${wf_id}" --arg run "${run_id}" '
    .items[]
    | select(.workflow_id == $wf and .workflow_run_id == $run)
    | .decision_summary.vendor // ""
  ' | head -n 1)
  amount=$(printf '%s' "${approvals_json}" | jq -r --arg wf "${wf_id}" --arg run "${run_id}" '
    .items[]
    | select(.workflow_id == $wf and .workflow_run_id == $run)
    | .decision_summary.amount // ""
  ' | head -n 1)
  why=$(printf '%s' "${approvals_json}" | jq -r --arg wf "${wf_id}" --arg run "${run_id}" '
    .items[]
    | select(.workflow_id == $wf and .workflow_run_id == $run)
    | .decision_summary.why // ""
  ' | head -n 1)

  if [[ "${source}" != "workflow_payload" ]]; then
    echo "expected decision_summary.source=workflow_payload, got ${source}" >&2
    exit 1
  fi
  if [[ "${context_status}" != "available" ]]; then
    echo "expected decision_summary.context_status=available, got ${context_status}" >&2
    exit 1
  fi
  if [[ "${vendor}" != "Acme Travel" ]]; then
    echo "expected decision_summary.vendor=Acme Travel, got ${vendor}" >&2
    exit 1
  fi
  if [[ "${amount}" != "1250" ]]; then
    echo "expected decision_summary.amount=1250, got ${amount}" >&2
    exit 1
  fi
  if [[ "${why}" != "manager threshold exceeded" ]]; then
    echo "expected decision_summary.why=manager threshold exceeded, got ${why}" >&2
    exit 1
  fi
  printf '%s' "${approvals_json}" | jq -e --arg wf "${wf_id}" --arg run "${run_id}" '
    .items[]
    | select(.workflow_id == $wf and .workflow_run_id == $run)
    | (.context_ptr | type == "string" and length > 0)
      and (.job_input.decision.vendor == "Acme Travel")
      and (.job_input.decision.amount == 1250)
  ' >/dev/null || {
    echo "approval payload did not expose context_ptr + job_input decision data as expected" >&2
    exit 1
  }

  e2e_log "approving decision-ready approval job ${approval_job_id}"
  curl -sS "${CURL_TLS_OPTS[@]}" \
    -H "X-API-Key: ${API_KEY}" \
    -H "X-Tenant-ID: ${TENANT_ID}" \
    -H "Content-Type: application/json" \
    -X POST "${API_BASE}/api/v1/approvals/${approval_job_id}/approve" \
    -d '{"reason":"approved for smoke","note":"decision-ready approval smoke"}' >/dev/null

  local run_status=""
  for _ in {1..20}; do
    run_status=$(curl -sS "${CURL_TLS_OPTS[@]}" \
      -H "X-API-Key: ${API_KEY}" \
      -H "X-Tenant-ID: ${TENANT_ID}" \
      "${API_BASE}/api/v1/workflow-runs/${run_id}" | jq -r '.status')
    if [[ "${run_status}" == "succeeded" ]]; then
      break
    fi
    sleep 0.5
  done
  if [[ "${run_status}" != "succeeded" ]]; then
    echo "expected run ${run_id} to succeed after approval, got ${run_status}" >&2
    exit 1
  fi

  resolved_json=$(curl -sS "${CURL_TLS_OPTS[@]}" \
    -H "X-API-Key: ${API_KEY}" \
    -H "X-Tenant-ID: ${TENANT_ID}" \
    "${API_BASE}/api/v1/approvals")
  resolved_by=$(printf '%s' "${resolved_json}" | jq -r --arg job "${approval_job_id}" '
    .items[]
    | select(.job.id == $job)
    | .resolved_by // ""
  ' | head -n 1)
  resolved_comment=$(printf '%s' "${resolved_json}" | jq -r --arg job "${approval_job_id}" '
    .items[]
    | select(.job.id == $job)
    | .resolved_comment // ""
  ' | head -n 1)
  if [[ -z "${resolved_by}" ]]; then
    echo "expected resolved approval record for ${approval_job_id}" >&2
    exit 1
  fi
  if [[ "${resolved_comment}" != "decision-ready approval smoke" ]]; then
    echo "expected resolved_comment=decision-ready approval smoke, got ${resolved_comment}" >&2
    exit 1
  fi
  printf '%s' "${resolved_json}" | jq -e --arg job "${approval_job_id}" '
    .items[]
    | select(.job.id == $job)
    | .decision_summary.source == "workflow_payload"
      and .decision_summary.context_status == "available"
      and .decision_summary.vendor == "Acme Travel"
  ' >/dev/null || {
    echo "resolved approval history lost decision-ready approval fields" >&2
    exit 1
  }

  e2e_log "decision-ready approval smoke passed"
}

resolve_tls_opts

e2e_log "running approval workflow smoke test"
CORDUM_API_KEY="${API_KEY}" CORDUM_TENANT_ID="${TENANT_ID}" CORDUM_ORG_ID="${ORG_ID}" \
  bash ./tools/scripts/platform_smoke.sh

e2e_log "running decision-ready approval validation"
run_decision_ready_approval_smoke

if [[ "${CORDUM_E2E_TEARDOWN:-0}" == "1" ]]; then
  e2e_log "tearing down compose stack"
  if docker compose version >/dev/null 2>&1; then
    docker compose down -v
  else
    docker-compose down -v
  fi
fi

popd >/dev/null

e2e_log "done"
