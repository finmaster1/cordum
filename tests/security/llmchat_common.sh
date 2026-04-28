#!/usr/bin/env bash
# Shared helpers for Cordum LLM-chat security probes.
# Source from llmchat_probe_*.sh scripts; do not execute directly.

set -euo pipefail

LLMCHAT_SECURITY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${LLMCHAT_SECURITY_DIR}/../.." && pwd)"
SECURITY_OUT_DIR="${LLMCHAT_SECURITY_OUT_DIR:-${REPO_ROOT}/out/llmchat-security}"
DEFAULT_OLLAMA_SECURITY_COMPOSE_FILE="${LLMCHAT_SECURITY_DIR}/compose.ollama-cpu.yaml"
SECURITY_COMPOSE_PROJECT="${LLMCHAT_SECURITY_COMPOSE_PROJECT:-cordum-llmchat-secprobes}"
GATEWAY_URL="${LLMCHAT_GATEWAY_URL:-http://127.0.0.1:8081}"
LLMCHAT_DIRECT_URL="${LLMCHAT_DIRECT_URL:-http://127.0.0.1:8090}"
VLLM_URL="${LLMCHAT_VLLM_URL:-http://127.0.0.1:8000}"
CURL_TIMEOUT_SECONDS="${LLMCHAT_CURL_TIMEOUT_SECONDS:-10}"
# LLMCHAT_SECURITY_BACKEND legal values:
#   ollama-cpu     — production default: Ollama + Qwen2.5-Coder-3B (CPU)
#   vllm-gpu/gpu-fp8 — opt-in GPU profile: vLLM + Qwen3-Coder-30B-FP8
#   cpu-vllm-awq   — legacy/interim CPU vLLM profile; non-default
#   ""             — backward-compatible alias for ollama-cpu
LLMCHAT_SECURITY_BACKEND="${LLMCHAT_SECURITY_BACKEND:-ollama-cpu}"
if [ "${LLMCHAT_SECURITY_BACKEND}" = "cpu-vllm-awq" ] && [ -z "${LLMCHAT_CURL_TIMEOUT_SECONDS:-}" ]; then
  CURL_TIMEOUT_SECONDS="120"
fi
if [ "${LLMCHAT_SECURITY_BACKEND}" = "cpu-vllm-awq" ] && [ -z "${LLMCHAT_VLLM_URL:-}" ]; then
  VLLM_URL="http://127.0.0.1:8001"
fi
if [ "${LLMCHAT_SECURITY_BACKEND}" = "ollama-cpu" ] && [ -z "${LLMCHAT_CURL_TIMEOUT_SECONDS:-}" ]; then
  CURL_TIMEOUT_SECONDS="600"
fi
if [ "${LLMCHAT_SECURITY_BACKEND}" = "ollama-cpu" ] && [ -z "${LLMCHAT_VLLM_URL:-}" ]; then
  # Kept as VLLM_URL for backward-compatible probe variable names; this is
  # Ollama's OpenAI-compatible /v1 surface.
  VLLM_URL="http://127.0.0.1:11436"
fi
if [ "${LLMCHAT_SECURITY_BACKEND}" = "ollama-cpu" ] && [ -z "${LLMCHAT_DIRECT_URL:-}" ]; then
  LLMCHAT_DIRECT_URL="http://127.0.0.1:8095"
fi
# Convenience for local clean-compose probes: source non-sensitive runtime
# values from .env without echoing them into evidence. curl_status_body
# redacts auth-bearing headers before logging commands.
if [ -z "${CORDUM_API_KEY:-}" ] && [ -f "${REPO_ROOT}/.env" ]; then
  CORDUM_API_KEY="$(grep -E '^CORDUM_API_KEY=' "${REPO_ROOT}/.env" | tail -1 | cut -d= -f2-)"
  export CORDUM_API_KEY
fi
PYTHON_BIN="${LLMCHAT_PYTHON_BIN:-}"
if [ -z "${PYTHON_BIN}" ]; then
  if command -v python >/dev/null 2>&1; then
    PYTHON_BIN="python"
  elif command -v python3 >/dev/null 2>&1; then
    PYTHON_BIN="python3"
  elif command -v py >/dev/null 2>&1; then
    PYTHON_BIN="py -3"
  else
    PYTHON_BIN=""
  fi
fi
GO_BIN="${LLMCHAT_GO_BIN:-}"
if [ -z "${GO_BIN}" ]; then
  if command -v go >/dev/null 2>&1; then
    GO_BIN="go"
  elif [ -x /snap/bin/go ]; then
    GO_BIN="/snap/bin/go"
  elif command -v go.exe >/dev/null 2>&1; then
    GO_BIN="go.exe"
  else
    GO_BIN=""
  fi
fi
HELM_BIN="${LLMCHAT_HELM_BIN:-}"
if [ -z "${HELM_BIN}" ]; then
  if command -v helm >/dev/null 2>&1; then
    HELM_BIN="helm"
  elif command -v helm.exe >/dev/null 2>&1; then
    HELM_BIN="helm.exe"
  else
    HELM_BIN=""
  fi
fi

# Probe scripts may override PROBE_ID before sourcing; default derives from script name.
PROBE_ID="${PROBE_ID:-$(basename "${0:-probe}" .sh)}"
PROBE_OUT_DIR="${SECURITY_OUT_DIR}/${PROBE_ID}"
EVIDENCE_FILE="${PROBE_OUT_DIR}/evidence.txt"

probe_init() {
  mkdir -p "${PROBE_OUT_DIR}"
  : >"${EVIDENCE_FILE}"
  log_evidence "probe=${PROBE_ID}"
  log_evidence "repo_root=${REPO_ROOT}"
  log_evidence "started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  log_evidence "backend=${LLMCHAT_SECURITY_BACKEND:-any-real}"
  validate_security_backend
  if [ "${LLMCHAT_SECURITY_LIVE:-0}" = "1" ] && [ "${LLMCHAT_SECURITY_SKIP_LIVE_CHECK:-0}" != "1" ]; then
    require_real_vllm
  fi
}

validate_security_backend() {
  case "${LLMCHAT_SECURITY_BACKEND}" in
    ""|ollama-cpu|vllm-gpu|gpu-fp8|cpu-vllm-awq) ;;
    *) probe_fail "unsupported LLMCHAT_SECURITY_BACKEND=${LLMCHAT_SECURITY_BACKEND}; allowed: ollama-cpu, vllm-gpu/gpu-fp8, cpu-vllm-awq, or empty" ;;
  esac
}

log_evidence() {
  printf '%s\n' "$*" | tee -a "${EVIDENCE_FILE}"
}

record_section() {
  log_evidence ""
  log_evidence "## $*"
}

probe_pass() {
  local msg="${1:-PASS}"
  log_evidence "status=PASS message=${msg}"
  exit 0
}

probe_fail() {
  local msg="${1:-FAIL}"
  log_evidence "status=FAIL message=${msg}"
  echo "[${PROBE_ID}] FAIL: ${msg}" >&2
  exit 1
}

probe_skip() {
  local msg="${1:-SKIP}"
  log_evidence "status=SKIP message=${msg}"
  echo "[${PROBE_ID}] SKIP: ${msg}" >&2
  exit 77
}

probe_retired() {
  local msg="${1:-retired/superseded by informational-only rescope}"
  log_evidence "scope=retired"
  log_evidence "status=RETIRED message=${msg}"
  exit 0
}

live_evidence_not_run() {
  local key="$1"
  shift
  local reason="$*"
  if [ "${LLMCHAT_SECURITY_REQUIRE_LIVE:-0}" = "1" ]; then
    log_evidence "${key}=not_run reason=${reason}"
    probe_skip "live evidence required but ${key} was not run: ${reason}"
  fi
  log_evidence "${key}=optional_not_run reason=${reason}"
}

live_evidence_inconclusive() {
  local key="$1"
  shift
  local reason="$*"
  if [ "${LLMCHAT_SECURITY_REQUIRE_LIVE:-0}" = "1" ]; then
    log_evidence "${key}=not_asserted reason=${reason}"
    probe_fail "live evidence required but ${key} was inconclusive: ${reason}"
  fi
  log_evidence "${key}=optional_not_asserted reason=${reason}"
}

require_cmd() {
  local cmd="$1"
  command -v "${cmd}" >/dev/null 2>&1 || probe_skip "required command '${cmd}' not found"
}

assert_file_exists() {
  local file="$1"
  local msg="${2:-expected file to exist}"
  [ -f "${REPO_ROOT}/${file}" ] || probe_fail "${msg}: ${file}"
  log_evidence "assert_file_exists ok: ${file}"
}

assert_file_contains() {
  local file="$1"
  local pattern="$2"
  local msg="${3:-expected pattern present}"
  assert_file_exists "${file}" "file missing for contains assertion"
  if ! grep -nE -- "${pattern}" "${REPO_ROOT}/${file}" >>"${EVIDENCE_FILE}" 2>&1; then
    probe_fail "${msg}: ${file} pattern=${pattern}"
  fi
  log_evidence "assert_file_contains ok: ${file} pattern=${pattern}"
}

assert_file_not_contains() {
  local file="$1"
  local pattern="$2"
  local msg="${3:-expected pattern absent}"
  assert_file_exists "${file}" "file missing for absent assertion"
  if grep -nE -- "${pattern}" "${REPO_ROOT}/${file}" >>"${EVIDENCE_FILE}" 2>&1; then
    probe_fail "${msg}: ${file} pattern=${pattern}"
  fi
  log_evidence "assert_file_not_contains ok: ${file} pattern=${pattern}"
}

run_capture() {
  local label="$1"
  shift
  record_section "${label}"
  log_evidence "+ $*"
  set +e
  "$@" >>"${EVIDENCE_FILE}" 2>&1
  local code=$?
  set -e
  log_evidence "exit_code=${code}"
  return "${code}"
}

run_go_test() {
  local label="$1"
  shift
  if [ -z "${GO_BIN}" ]; then
    probe_skip "go command not found; set LLMCHAT_GO_BIN"
  fi
  run_capture "${label}" "${GO_BIN}" test "$@"
}

run_bash_capture() {
  local label="$1"
  local script="$2"
  record_section "${label}"
  log_evidence "+ bash -lc ${script}"
  set +e
  bash -lc "${script}" >>"${EVIDENCE_FILE}" 2>&1
  local code=$?
  set -e
  log_evidence "exit_code=${code}"
  return "${code}"
}

run_helm_template() {
  if [ -z "${HELM_BIN}" ]; then
    probe_skip "helm command not found; set LLMCHAT_HELM_BIN"
  fi
  local args=()
  local arg
  for arg in "$@"; do
    if [[ "${HELM_BIN}" == *".exe" ]] && command -v wslpath >/dev/null 2>&1 && [[ "${arg}" == /* ]] && [ -e "${arg}" ]; then
      args+=("$(wslpath -w "${arg}")")
    else
      args+=("${arg}")
    fi
  done
  "${HELM_BIN}" template "${args[@]}"
}

curl_status_body() {
  local label="$1"
  local body_file="$2"
  shift 2
  record_section "curl ${label}" >&2
  local redacted_args
  redacted_args="$*"
  redacted_args=$(printf '%s' "${redacted_args}" | sed -E 's/(Authorization: (Bearer )?)[^"[:space:]]+/\1<redacted>/g; s/(X-API-Key: )[A-Za-z0-9._:-]+/\1<redacted>/g')
  log_evidence "+ curl ${redacted_args}" >&2
  set +e
  local status
  status=$(curl -k -sS --max-time "${CURL_TIMEOUT_SECONDS}" -o "${body_file}" -w '%{http_code}' "$@" 2>>"${EVIDENCE_FILE}")
  local code=$?
  set -e
  log_evidence "curl_exit=${code} http_status=${status} body_file=${body_file}" >&2
  if [ -f "${body_file}" ]; then
    sed -e 's/^/body: /' "${body_file}" >>"${EVIDENCE_FILE}" || true
  fi
  printf '%s' "${status}"
  return "${code}"
}

assert_http_status_in() {
  local got="$1"
  local allowed_csv="$2"
  local msg="$3"
  IFS=',' read -r -a allowed <<<"${allowed_csv}"
  for want in "${allowed[@]}"; do
    if [ "${got}" = "${want}" ]; then
      log_evidence "assert_http_status_in ok: got=${got} allowed=${allowed_csv}"
      return 0
    fi
  done
  probe_fail "${msg}: got HTTP ${got}, allowed ${allowed_csv}"
}

assert_text_contains() {
  local text_file="$1"
  local pattern="$2"
  local msg="$3"
  if ! grep -E -- "${pattern}" "${text_file}" >>"${EVIDENCE_FILE}" 2>&1; then
    probe_fail "${msg}: ${text_file} pattern=${pattern}"
  fi
  log_evidence "assert_text_contains ok: ${text_file} pattern=${pattern}"
}

assert_text_not_contains() {
  local text_file="$1"
  local pattern="$2"
  local msg="$3"
  if grep -E -- "${pattern}" "${text_file}" >>"${EVIDENCE_FILE}" 2>&1; then
    probe_fail "${msg}: ${text_file} pattern=${pattern}"
  fi
  log_evidence "assert_text_not_contains ok: ${text_file} pattern=${pattern}"
}

require_live_stack() {
  if [ "${LLMCHAT_SECURITY_LIVE:-0}" != "1" ]; then
    probe_skip "live stack probe disabled; rerun with LLMCHAT_SECURITY_LIVE=1 after clean compose-up"
  fi
  require_real_vllm
}

require_real_vllm() {
  validate_security_backend

  local default_container="cordum-ollama-1"
  local expected_model="qwen2.5-coder:3b-instruct-q4_K_M-ctx32k"
  local backend_label="real Ollama"
  if [ "${LLMCHAT_SECURITY_BACKEND}" = "cpu-vllm-awq" ]; then
    default_container="cordum-qwen-inference-cpu-1"
    expected_model="Qwen/Qwen3-Coder-30B-A3B-Instruct-AWQ"
  elif [ "${LLMCHAT_SECURITY_BACKEND}" = "gpu-fp8" ] || [ "${LLMCHAT_SECURITY_BACKEND}" = "vllm-gpu" ]; then
    default_container="cordum-qwen-inference-1"
    expected_model="Qwen/Qwen3-Coder-30B-A3B-Instruct-FP8"
  elif [ "${LLMCHAT_SECURITY_BACKEND}" = "ollama-cpu" ] || [ -z "${LLMCHAT_SECURITY_BACKEND}" ]; then
    default_container="cordum-llmchat-secprobes-qwen-inference-1"
    expected_model="qwen2.5-coder:3b-instruct-q4_K_M-ctx32k"
    backend_label="real Ollama"
  fi

  local cmdline_file="${PROBE_OUT_DIR}/qwen-cmdline.txt"
  if command -v docker >/dev/null 2>&1; then
    local live_compose_file="${LLMCHAT_SECURITY_COMPOSE_FILE:-}"
    if [ "${LLMCHAT_SECURITY_BACKEND}" = "ollama-cpu" ] && [ -z "${live_compose_file}" ] && [ -f "${DEFAULT_OLLAMA_SECURITY_COMPOSE_FILE}" ]; then
      live_compose_file="${DEFAULT_OLLAMA_SECURITY_COMPOSE_FILE}"
    fi
    if [ "${LLMCHAT_SECURITY_BACKEND}" = "ollama-cpu" ] && [ -n "${live_compose_file}" ] && [ -z "${LLMCHAT_QWEN_CONTAINER:-}" ]; then
      docker compose -p "${SECURITY_COMPOSE_PROJECT}" -f "${live_compose_file}" exec -T qwen-inference-secprobes sh -c "tr '\000' ' ' </proc/1/cmdline" >"${cmdline_file}" 2>>"${EVIDENCE_FILE}" || true
    else
      docker exec "${LLMCHAT_QWEN_CONTAINER:-${default_container}}" sh -c "tr '\000' ' ' </proc/1/cmdline" >"${cmdline_file}" 2>>"${EVIDENCE_FILE}" || true
    fi
  fi
  if [ ! -s "${cmdline_file}" ]; then
    log_evidence "qwen_cmdline=unavailable"
    probe_skip "${backend_label} cmdline unavailable; run with a matching inference container"
  fi
  sed -e 's/^/qwen_cmdline: /' "${cmdline_file}" >>"${EVIDENCE_FILE}"

  if grep -E 'mock_openai|python.*mock|python:3' "${cmdline_file}" >/dev/null 2>&1; then
    probe_skip "inference service is the dev Python mock, not ${backend_label}; security evidence would be invalid"
  fi
  if [ "${backend_label}" = "real vLLM" ]; then
    if [ -n "${expected_model}" ]; then
      grep -q -- "${expected_model}" "${cmdline_file}" || probe_fail "real vLLM model does not match ${expected_model} for backend=${LLMCHAT_SECURITY_BACKEND}"
    fi
    if grep -q -- '--tool-call-parser' "${cmdline_file}"; then
      probe_fail "informational-only vLLM must not configure a tool-call parser"
    fi
  else
    grep -E 'ollama|/bin/sh|ollama serve' "${cmdline_file}" >/dev/null 2>&1 || probe_fail "Ollama backend cmdline is not recognizable"
  fi

  local models_file="${PROBE_OUT_DIR}/vllm-models.json"
  curl -sS --max-time "${CURL_TIMEOUT_SECONDS}" "${VLLM_URL}/v1/models" >"${models_file}" 2>>"${EVIDENCE_FILE}" || true
  if [ ! -s "${models_file}" ]; then
    log_evidence "inference_models=unavailable url=${VLLM_URL}/v1/models"
    probe_skip "${backend_label} /v1/models unavailable at ${VLLM_URL}; live security evidence cannot be asserted"
  fi
  sed -e 's/^/models: /' "${models_file}" >>"${EVIDENCE_FILE}"
  if grep -E '"owned_by"[[:space:]]*:[[:space:]]*"cordum-dev-mock"|cordum-dev-mock' "${models_file}" >/dev/null 2>&1; then
    probe_skip "inference /v1/models is the dev mock (owned_by=cordum-dev-mock), not ${backend_label}"
  fi
  if [ "${LLMCHAT_SECURITY_BACKEND}" = "ollama-cpu" ] && [ -n "${expected_model}" ]; then
    grep -q -- "${expected_model}" "${models_file}" || probe_fail "Ollama model list does not include ${expected_model}"
  fi
}

compose_clean_up() {
  local profile="${LLMCHAT_SECURITY_COMPOSE_PROFILE:-}"
  local compose_file="${LLMCHAT_SECURITY_COMPOSE_FILE:-}"
  if [ -z "${profile}" ]; then
    if [ "${LLMCHAT_SECURITY_BACKEND}" = "cpu-vllm-awq" ]; then
      profile="llmchat-cpu"
    elif [ "${LLMCHAT_SECURITY_BACKEND}" = "ollama-cpu" ]; then
      profile="llmchat-ollama"
    else
      profile="llmchat"
    fi
  fi
  record_section "clean compose baseline"
  log_evidence "profile=${profile}"
  if [ "${LLMCHAT_SECURITY_BACKEND}" = "ollama-cpu" ]; then
    if [ -z "${compose_file}" ]; then
      compose_file="${DEFAULT_OLLAMA_SECURITY_COMPOSE_FILE}"
    fi
    log_evidence "compose_file=${compose_file}"
    log_evidence "compose_project=${SECURITY_COMPOSE_PROJECT}"
    if [ ! -f "${compose_file}" ]; then
      probe_fail "owned Ollama security compose file not found: ${compose_file}"
    fi
    run_capture "docker compose down -v owned ollama security stack" docker compose -p "${SECURITY_COMPOSE_PROJECT}" -f "${compose_file}" down -v || return $?
    run_capture "docker compose up -d --build owned ollama security stack" docker compose -p "${SECURITY_COMPOSE_PROJECT}" -f "${compose_file}" up -d --build || return $?
  else
    run_capture "docker compose down -v" docker compose --profile "${profile}" down -v || return $?
    run_capture "docker compose up -d --build" docker compose --profile "${profile}" up -d --build || return $?
  fi
  wait_inference_ready
}

wait_inference_ready() {
  local timeout="${LLMCHAT_SECURITY_READY_TIMEOUT_SECONDS:-900}"
  local start now status
  start=$(date +%s)
  record_section "wait inference ready"
  log_evidence "url=${VLLM_URL}/v1/models timeout_seconds=${timeout}"
  while true; do
    status=$(curl -k -sS --max-time 5 -o "${PROBE_OUT_DIR}/models-wait.json" -w '%{http_code}' "${VLLM_URL}/v1/models" 2>>"${EVIDENCE_FILE}" || true)
    if [ "${status}" = "200" ]; then
      sed -e 's/^/models_ready: /' "${PROBE_OUT_DIR}/models-wait.json" >>"${EVIDENCE_FILE}" || true
      log_evidence "inference_ready_status=200"
      return 0
    fi
    now=$(date +%s)
    if [ $((now - start)) -ge "${timeout}" ]; then
      log_evidence "inference_ready_status=${status}"
      return 1
    fi
    sleep 5
  done
}

jwt_payload_decode() {
  local token="$1"
  ${PYTHON_BIN} - "$token" <<'PY'
import base64, json, sys
parts = sys.argv[1].split('.')
if len(parts) < 2:
    raise SystemExit('not a JWT')
payload = parts[1]
payload += '=' * ((4 - len(payload) % 4) % 4)
print(json.dumps(json.loads(base64.urlsafe_b64decode(payload.encode())), sort_keys=True, indent=2))
PY
}

mint_fixture_jwt() {
  # Fixture-only unsigned JWT for parser tests. Never accepted by Cordum auth.
  local payload_json="$1"
  ${PYTHON_BIN} - "$payload_json" <<'PY'
import base64, json, sys
header = {"alg": "none", "typ": "JWT"}
payload = json.loads(sys.argv[1])
def enc(obj):
    raw = json.dumps(obj, separators=(',', ':'), sort_keys=True).encode()
    return base64.urlsafe_b64encode(raw).decode().rstrip('=')
print(f"{enc(header)}.{enc(payload)}.")
PY
}

secret_grep_pattern() {
  printf '%s' 'sk-test-|Bearer [A-Za-z0-9._-]+|eyJ[A-Za-z0-9._-]+|AKIA[0-9A-Z]{16}'
}

assert_no_secret_patterns_in_file() {
  local file="$1"
  local msg="$2"
  if [ ! -f "${file}" ]; then
    probe_fail "${msg}: file missing ${file}"
  fi
  if grep -E "$(secret_grep_pattern)" "${file}" >>"${EVIDENCE_FILE}" 2>&1; then
    probe_fail "${msg}: secret-like pattern found in ${file}"
  fi
  log_evidence "assert_no_secret_patterns_in_file ok: ${file}"
}

assert_no_secret_patterns_in_dir() {
  local dir="$1"
  local msg="$2"
  if [ ! -d "${dir}" ]; then
    probe_fail "${msg}: dir missing ${dir}"
  fi
  if grep -RIE "$(secret_grep_pattern)" "${dir}" >>"${EVIDENCE_FILE}" 2>&1; then
    probe_fail "${msg}: secret-like pattern found under ${dir}"
  fi
  log_evidence "assert_no_secret_patterns_in_dir ok: ${dir}"
}

write_probe_manifest() {
  record_section "manifest"
  log_evidence "payload_hosts=attacker.example evil.test"
  log_evidence "expected_defense_layer=$1"
}
