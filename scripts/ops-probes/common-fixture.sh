#!/usr/bin/env bash
set -euo pipefail

# Shared helpers for task-8eab552b llm-chat observability probes.
# Defaults target the local docker-compose stack. Override with env vars in CI.

: "${CORDUM_API_BASE:=https://127.0.0.1:8081/api/v1}"
: "${LLMCHAT_METRICS_URL:=http://127.0.0.1:8090/metrics}"
: "${LLMCHAT_HEALTH_URL:=http://127.0.0.1:8090/readyz}"
: "${LLMCHAT_OUT_DIR:=out/llmchat-ops}"
: "${LLMCHAT_OPS_LIVE:=0}"
: "${LLMCHAT_OPS_REQUIRE_LIVE:=0}"

probe_name="${PROBE_NAME:-unknown}"
probe_dir="${LLMCHAT_OUT_DIR}/${probe_name}"
evidence_file="${probe_dir}/evidence.txt"
mkdir -p "${probe_dir}"
: >"${evidence_file}"

log_evidence() {
  printf '%s\n' "$*" | tee -a "${evidence_file}"
}

probe_pass() {
  log_evidence "RESULT=PASS"
  exit 0
}

probe_fail() {
  log_evidence "RESULT=FAIL reason=$*"
  exit 1
}

probe_skip() {
  log_evidence "RESULT=SKIP reason=$*"
  if [[ "${LLMCHAT_OPS_REQUIRE_LIVE}" == "1" ]]; then
    exit 1
  fi
  exit 77
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || probe_fail "missing required command: $1"
}

find_first_cmd() {
  local candidate
  for candidate in "$@"; do
    if command -v "${candidate}" >/dev/null 2>&1; then
      command -v "${candidate}"
      return 0
    fi
  done
  return 1
}

resolve_python_bin() {
  if [[ -n "${PYTHON_BIN:-}" ]]; then
    printf '%s\n' "${PYTHON_BIN}"
    return 0
  fi
  find_first_cmd python python3 python.exe
}

run_python() {
  local python_bin
  python_bin="$(resolve_python_bin)" || probe_fail "missing required command: python/python3/python.exe"
  "${python_bin}" "$@"
}

resolve_docker_bin() {
  if [[ -n "${DOCKER_BIN:-}" ]]; then
    printf '%s\n' "${DOCKER_BIN}"
    return 0
  fi
  find_first_cmd docker docker.exe
}

docker_compose() {
  local docker_bin
  docker_bin="$(resolve_docker_bin)" || probe_fail "missing required command: docker/docker.exe"
  MSYS_NO_PATHCONV=1 "${docker_bin}" compose "$@"
}

require_docker_compose() {
  local stderr_file="${probe_dir}/docker-compose-version.stderr"
  if ! docker_compose version >/dev/null 2>"${stderr_file}"; then
    cat "${stderr_file}" >>"${evidence_file}" || true
    probe_skip "docker compose unavailable from this shell; run with Git Bash/MSYS on Windows or enable Docker WSL integration"
  fi
}

resolve_llmchat_log_service() {
  if [[ -n "${LLMCHAT_LOG_SERVICE:-}" ]]; then
    printf '%s\n' "${LLMCHAT_LOG_SERVICE}"
    return 0
  fi
  local services
  services="$(docker_compose config --services 2>/dev/null || true)"
  local candidate
  for candidate in llm-chat-ollama llm-chat; do
    if grep -Fxq "${candidate}" <<<"${services}"; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done
  printf '%s\n' "llm-chat"
}

curl_common=(--silent --show-error --location --max-time "${LLMCHAT_CURL_TIMEOUT:-15}")
if [[ "${CORDUM_API_BASE}" == https://* ]]; then
  curl_common+=(--insecure)
fi

curl_api() {
  if [[ -z "${CORDUM_API_KEY:-}" ]]; then
    probe_fail "CORDUM_API_KEY is required for API call $*"
  fi
  curl "${curl_common[@]}" -H "X-API-Key: ${CORDUM_API_KEY}" "$@"
}

fetch_metrics() {
  require_cmd curl
  curl "${curl_common[@]}" "${LLMCHAT_METRICS_URL}"
}

assert_metric_family() {
  local metrics_file="$1"
  local family="$2"
  grep -Eq "^(# HELP ${family}|${family})([ {]|$)" "${metrics_file}" || probe_fail "missing metric family ${family}"
}

count_metric_series() {
  local metrics_file="$1"
  local family="$2"
  grep -E "^${family}(\{| |$)" "${metrics_file}" | wc -l | tr -d ' '
}

scan_for_secret_patterns() {
  local target="$1"
  if [[ ! -s "${target}" ]]; then
    log_evidence "secret_scan_target_empty=${target}"
    return 0
  fi
  local hit_file="${target}.secret_hits"
  : >"${hit_file}"

  local labels=(
    "Bearer"
    "X-API-Key"
    "CORDUM_API_KEY"
    "JWT"
    "OpenAI-key"
    "AWS-AKIA"
    "64-hex"
    "private-key"
  )
  local patterns=(
    'Bearer[[:space:]]+[^[:space:]]+'
    '[Xx]-?[Aa][Pp][Ii]-?[Kk][Ee][Yy][=:[:space:]]+[A-Za-z0-9._-]{12,}'
    '[Cc][Oo][Rr][Dd][Uu][Mm]_[Aa][Pp][Ii]_[Kk][Ee][Yy][=:[:space:]]+[A-Za-z0-9._-]{12,}'
    'eyJ[A-Za-z0-9._-]{20,}'
    '(^|[^[:alnum:]_])sk-[A-Za-z0-9_-]{12,}'
    'AKIA[0-9A-Z]{16}'
    '[A-Fa-f0-9]{64,}'
    '-----BEGIN (RSA |EC |OPENSSH |)PRIVATE KEY-----'
  )

  local i line _rest
  for i in "${!labels[@]}"; do
    while IFS=: read -r line _rest; do
      if [[ "${line}" =~ ^[0-9]+$ ]]; then
        printf 'MATCH %s:line=%s pattern=%s\n' "$(basename "${target}")" "${line}" "${labels[$i]}" >>"${hit_file}"
      fi
    done < <(grep -En -- "${patterns[$i]}" "${target}" || true)
  done

  if [[ -s "${hit_file}" ]]; then
    sort -u "${hit_file}" | head -20 >>"${evidence_file}"
    return 1
  fi
  log_evidence "secret_scan=zero_hits target=${target}"
}

live_required_or_skip() {
  if [[ "${LLMCHAT_OPS_LIVE}" != "1" ]]; then
    probe_skip "live stack required; set LLMCHAT_OPS_LIVE=1"
  fi
}

write_probe_header() {
  log_evidence "probe=${probe_name}"
  log_evidence "timestamp_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  log_evidence "api_base=${CORDUM_API_BASE}"
  log_evidence "metrics_url=${LLMCHAT_METRICS_URL}"
  log_evidence "live=${LLMCHAT_OPS_LIVE} require_live=${LLMCHAT_OPS_REQUIRE_LIVE}"
}
