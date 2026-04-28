#!/usr/bin/env bash
# Shared helpers for Cordum LLM-chat production-readiness probes.
# Source from llmchat_probe_*.sh scripts; do not execute directly.

set -euo pipefail

LLMCHAT_OPS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${LLMCHAT_OPS_DIR}/../.." && pwd)"
OPS_OUT_DIR="${LLMCHAT_OPS_OUT_DIR:-${REPO_ROOT}/out/llmchat-ops}"
GATEWAY_URL="${LLMCHAT_GATEWAY_URL:-http://127.0.0.1:8081}"
LLMCHAT_DIRECT_URL="${LLMCHAT_DIRECT_URL:-http://127.0.0.1:8090}"
VLLM_URL="${LLMCHAT_VLLM_URL:-http://127.0.0.1:8000}"
OLLAMA_URL="${LLMCHAT_OLLAMA_URL:-http://127.0.0.1:11434}"
CURL_TIMEOUT_SECONDS="${LLMCHAT_CURL_TIMEOUT_SECONDS:-10}"
# LLMCHAT_OPS_BACKEND legal values:
#   gpu-fp8        — vLLM + Qwen3-Coder-30B-FP8 (production GPU profile)
#   cpu-vllm-awq   — vLLM + Qwen3-Coder-30B-AWQ (CPU/16-24GB RAM)
#   ollama-cpu     — Ollama + Qwen2.5-Coder-3B-Q4_K_M (CPU/~2GB RAM, default)
LLMCHAT_OPS_BACKEND="${LLMCHAT_OPS_BACKEND:-ollama-cpu}"

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
KUBECTL_BIN="${LLMCHAT_KUBECTL_BIN:-}"
if [ -z "${KUBECTL_BIN}" ]; then
  if command -v kubectl >/dev/null 2>&1; then
    KUBECTL_BIN="kubectl"
  elif command -v kubectl.exe >/dev/null 2>&1; then
    KUBECTL_BIN="kubectl.exe"
  else
    KUBECTL_BIN=""
  fi
fi

PROBE_ID="${PROBE_ID:-$(basename "${0:-probe}" .sh)}"
PROBE_OUT_DIR="${OPS_OUT_DIR}/${PROBE_ID}"
EVIDENCE_FILE="${PROBE_OUT_DIR}/evidence.txt"

probe_init() {
  mkdir -p "${PROBE_OUT_DIR}"
  : >"${EVIDENCE_FILE}"
  log_evidence "probe=${PROBE_ID}"
  log_evidence "repo_root=${REPO_ROOT}"
  log_evidence "started_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  log_evidence "live=${LLMCHAT_OPS_LIVE:-0} require_live=${LLMCHAT_OPS_REQUIRE_LIVE:-0}"
  log_evidence "backend=${LLMCHAT_OPS_BACKEND}"
}

log_evidence() { printf '%s\n' "$*" | tee -a "${EVIDENCE_FILE}"; }
record_section() { log_evidence ''; log_evidence "## $*"; }
probe_pass() { log_evidence "status=PASS message=${1:-PASS}"; exit 0; }
probe_fail() { local msg="${1:-FAIL}"; log_evidence "status=FAIL message=${msg}"; echo "[${PROBE_ID}] FAIL: ${msg}" >&2; exit 1; }
probe_skip() { local msg="${1:-SKIP}"; log_evidence "status=SKIP message=${msg}"; echo "[${PROBE_ID}] SKIP: ${msg}" >&2; exit 77; }
probe_deferred() { local msg="${1:-DEFERRED}"; log_evidence "status=DEFERRED message=${msg}"; echo "[${PROBE_ID}] DEFERRED: ${msg}" >&2; exit 78; }

write_probe_manifest() {
  record_section 'manifest'
  log_evidence "failure_mode=$1"
  log_evidence "acceptance=$2"
  log_evidence "expected_recovery_time=$3"
  log_evidence "nightly_ci_marker=${4:-manual-gpu-staging}"
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

curl_status_body() {
  local label="$1"
  local body_file="$2"
  shift 2
  record_section "curl ${label}"
  log_evidence "+ curl $*"
  set +e
  local status
  status=$(curl -k -sS --max-time "${CURL_TIMEOUT_SECONDS}" -o "${body_file}" -w '%{http_code}' "$@" 2>>"${EVIDENCE_FILE}")
  local code=$?
  set -e
  log_evidence "curl_exit=${code} http_status=${status} body_file=${body_file}"
  [ -f "${body_file}" ] && sed -e 's/^/body: /' "${body_file}" >>"${EVIDENCE_FILE}" || true
  printf '%s' "${status}"
  return "${code}"
}

require_live_stack() {
  if [ "${LLMCHAT_OPS_LIVE:-0}" != '1' ]; then
    probe_skip 'live production-readiness probe disabled; rerun with LLMCHAT_OPS_LIVE=1 on a dedicated stack'
  fi
}

require_real_vllm() {
  require_live_stack
  case "${LLMCHAT_OPS_BACKEND}" in
    gpu-fp8|cpu-vllm-awq) ;;
    ollama-cpu) probe_skip "probe is vLLM-specific; backend=ollama-cpu uses require_real_ollama instead" ;;
    *) probe_fail "unsupported LLMCHAT_OPS_BACKEND=${LLMCHAT_OPS_BACKEND}; allowed: gpu-fp8, cpu-vllm-awq, ollama-cpu" ;;
  esac
  local cmdline_file="${PROBE_OUT_DIR}/qwen-cmdline.txt"
  local default_container="cordum-qwen-inference-1"
  local expected_model="Qwen/Qwen3-Coder-30B-A3B-Instruct-FP8"
  if [ "${LLMCHAT_OPS_BACKEND}" = "cpu-vllm-awq" ]; then
    default_container="cordum-qwen-inference-cpu-1"
    expected_model="Qwen/Qwen3-Coder-30B-A3B-Instruct-AWQ"
  fi
  if command -v docker >/dev/null 2>&1; then
    docker exec "${LLMCHAT_QWEN_CONTAINER:-${default_container}}" sh -c "tr '\\000' ' ' </proc/1/cmdline" >"${cmdline_file}" 2>>"${EVIDENCE_FILE}" || true
  fi
  if [ ! -s "${cmdline_file}" ]; then
    log_evidence 'qwen_cmdline=unavailable'
    probe_skip "real vLLM cmdline unavailable; run backend=${LLMCHAT_OPS_BACKEND} with a matching qwen-inference container"
  fi
  sed -e 's/^/qwen_cmdline: /' "${cmdline_file}" >>"${EVIDENCE_FILE}"
  if grep -E 'mock_openai|python.*mock|python:3' "${cmdline_file}" >/dev/null 2>&1; then
    probe_skip 'qwen-inference is the dev Python mock, not real vLLM; runtime evidence would be invalid'
  fi
  grep -q -- '--tool-call-parser qwen3_xml' "${cmdline_file}" || probe_fail 'real vLLM parser is not qwen3_xml'
  grep -q -- "${expected_model}" "${cmdline_file}" || probe_fail "real vLLM model does not match ${expected_model} for backend=${LLMCHAT_OPS_BACKEND}"
}

# Asserts the running container is real Ollama (not a dev stub) and that
# the chat model is registered. Companion to require_real_vllm() for the
# `ollama-cpu` backend.
require_real_ollama() {
  require_live_stack
  case "${LLMCHAT_OPS_BACKEND}" in
    ollama-cpu) ;;
    gpu-fp8|cpu-vllm-awq) probe_skip "probe is Ollama-specific; backend=${LLMCHAT_OPS_BACKEND} uses require_real_vllm" ;;
    *) probe_fail "unsupported LLMCHAT_OPS_BACKEND=${LLMCHAT_OPS_BACKEND}; allowed: gpu-fp8, cpu-vllm-awq, ollama-cpu" ;;
  esac
  local cmdline_file="${PROBE_OUT_DIR}/ollama-cmdline.txt"
  local default_container="cordum-ollama-1"
  local expected_model="${LLMCHAT_OLLAMA_MODEL:-qwen2.5-coder:3b-instruct-q4_K_M}"
  if command -v docker >/dev/null 2>&1; then
    docker exec "${LLMCHAT_OLLAMA_CONTAINER:-${default_container}}" sh -c "tr '\\000' ' ' </proc/1/cmdline" >"${cmdline_file}" 2>>"${EVIDENCE_FILE}" || true
  fi
  if [ ! -s "${cmdline_file}" ]; then
    log_evidence 'ollama_cmdline=unavailable'
    probe_skip "real Ollama cmdline unavailable; run backend=${LLMCHAT_OPS_BACKEND} with a matching ollama container"
  fi
  sed -e 's/^/ollama_cmdline: /' "${cmdline_file}" >>"${EVIDENCE_FILE}"
  # The container's PID 1 is `/bin/sh -c 'ollama serve & ...'`; assert the
  # serve loop is still in flight rather than greping for a specific arg.
  grep -q -E 'ollama (serve|pull)' "${cmdline_file}" || probe_fail 'real Ollama process not detected in container PID 1'
  local tags_file="${PROBE_OUT_DIR}/ollama-tags.json"
  curl -sS --max-time "${CURL_TIMEOUT_SECONDS}" "${OLLAMA_URL}/api/tags" >"${tags_file}" 2>>"${EVIDENCE_FILE}" || true
  if [ ! -s "${tags_file}" ]; then
    probe_skip "Ollama /api/tags unreachable at ${OLLAMA_URL}; the serve loop may still be pulling the model"
  fi
  sed -e 's/^/ollama_tags: /' "${tags_file}" >>"${EVIDENCE_FILE}"
  grep -q -- "${expected_model}" "${tags_file}" || probe_fail "Ollama model registry does not list ${expected_model}; check OLLAMA_KEEP_ALIVE + the pull step in compose"
}

# Polls Ollama /api/tags until ${expected_model} appears or the timeout
# elapses. First-pull on a cold cache is ~3-5 minutes for the 4.5 GB
# Q4_K_M weights, so the default timeout is generous.
wait_for_ollama_model_loaded() {
  local expected_model="${1:-${LLMCHAT_OLLAMA_MODEL:-qwen2.5-coder:3b-instruct-q4_K_M}}"
  local timeout="${2:-600}"
  local body="${PROBE_OUT_DIR}/ollama-tags-poll.json"
  local start now status
  start=$(date +%s)
  while true; do
    status=$(curl_status_body 'ollama tags poll' "${body}" "${OLLAMA_URL}/api/tags") || true
    if [ "${status}" = "200" ] && grep -q -- "${expected_model}" "${body}" 2>/dev/null; then
      log_evidence "ollama_model_loaded=${expected_model}"
      return 0
    fi
    now=$(date +%s)
    if [ $((now - start)) -ge "${timeout}" ]; then
      log_evidence "ollama_model_load_timeout=${timeout} expected_model=${expected_model} last_status=${status}"
      return 1
    fi
    sleep 10
  done
}

poll_readyz() {
  local url="${1:-${LLMCHAT_DIRECT_URL}/readyz}"
  local want="${2:-200}"
  local timeout="${3:-60}"
  local start now status body="${PROBE_OUT_DIR}/readyz.body"
  start=$(date +%s)
  while true; do
    status=$(curl_status_body 'readyz poll' "${body}" "${url}") || true
    [ "${status}" = "${want}" ] && return 0
    now=$(date +%s)
    if [ $((now - start)) -ge "${timeout}" ]; then
      log_evidence "readyz_timeout=${timeout} last_status=${status} wanted=${want}"
      return 1
    fi
    sleep 5
  done
}

scrape_vllm_metric() {
  local metric="$1"
  local out="${PROBE_OUT_DIR}/vllm-metrics.txt"
  curl -fsS --max-time "${CURL_TIMEOUT_SECONDS}" "${VLLM_URL%/}/metrics" >"${out}" 2>>"${EVIDENCE_FILE}" || return 1
  grep -E "${metric}" "${out}" | tee -a "${EVIDENCE_FILE}"
}


require_cmd() {
  local cmd="$1"
  command -v "${cmd}" >/dev/null 2>&1 || probe_skip "required command '${cmd}' not found"
}

require_go() {
  [ -n "${GO_BIN}" ] || probe_skip "go command not found; set LLMCHAT_GO_BIN"
}

dotenv_get() {
  local key="$1"
  [ -f "${REPO_ROOT}/.env" ] || return 1
  sed -n -E "s/^${key}=//p" "${REPO_ROOT}/.env" | tail -n 1 | sed -e 's/^\"//' -e 's/\"$//' -e "s/^'//" -e "s/'$//"
}

effective_env() {
  local key="$1"
  local fallback="${2:-}"
  local val="${!key:-}"
  if [ -z "${val}" ]; then
    val="$(dotenv_get "${key}" 2>/dev/null || true)"
  fi
  if [ -z "${val}" ]; then
    val="${fallback}"
  fi
  printf '%s' "${val}"
}

require_chat_api_key() {
  CHAT_API_KEY="${LLMCHAT_TRUSTED_API_KEY:-${CORDUM_API_KEY:-}}"
  if [ -z "${CHAT_API_KEY}" ]; then
    CHAT_API_KEY="$(effective_env CORDUM_API_KEY)"
  fi
  [ -n "${CHAT_API_KEY}" ] || probe_skip 'CORDUM_API_KEY or LLMCHAT_TRUSTED_API_KEY required for live chat probes'
  export CHAT_API_KEY
}

require_destructive() {
  if [ "${LLMCHAT_OPS_ALLOW_DESTRUCTIVE:-0}" != '1' ]; then
    probe_skip 'destructive live probe disabled; rerun on dedicated staging with LLMCHAT_OPS_ALLOW_DESTRUCTIVE=1'
  fi
}

docker_compose() {
  require_cmd docker
  local args=()
  if [ -n "${LLMCHAT_DOCKER_COMPOSE_FILES:-}" ]; then
    # Space-separated file list, e.g. "docker-compose.yml docker-compose.dev.yml".
    # shellcheck disable=SC2206
    local files=( ${LLMCHAT_DOCKER_COMPOSE_FILES} )
    local f
    for f in "${files[@]}"; do
      args+=( -f "${f}" )
    done
  fi
  docker compose "${args[@]}" "$@"
}

service_container_id() {
  local service="$1"
  docker_compose ps -q "${service}" 2>>"${EVIDENCE_FILE}" | tail -n 1
}

require_service_container() {
  local service="$1"
  local cid
  cid="$(service_container_id "${service}")"
  [ -n "${cid}" ] || probe_skip "compose service '${service}' is not running"
  printf '%s' "${cid}"
}

redis_cli() {
  local password
  password="$(effective_env REDIS_PASSWORD cordum-dev)"
  [ -n "${password}" ] || probe_skip 'REDIS_PASSWORD required for Redis mutation probe'
  require_service_container redis >/dev/null
  docker_compose exec -T redis redis-cli --tls --cacert /etc/cordum/tls/ca/ca.crt -a "${password}" "$@"
}

http_to_ws() {
  local raw="$1"
  case "${raw}" in
    http://*) printf 'ws://%s' "${raw#http://}" ;;
    https://*) printf 'wss://%s' "${raw#https://}" ;;
    ws://*|wss://*) printf '%s' "${raw}" ;;
    *) printf 'ws://%s' "${raw}" ;;
  esac
}

chat_base_url() {
  printf '%s' "${LLMCHAT_CHAT_URL:-${LLMCHAT_DIRECT_URL}}"
}

chat_ws_url() {
  local base
  base="$(chat_base_url)"
  base="${base%/}"
  printf '%s/api/v1/chat/ws' "$(http_to_ws "${base}")"
}

chat_post_url() {
  local base
  base="$(chat_base_url)"
  base="${base%/}"
  printf '%s/api/v1/chat' "${base}"
}

chat_auth_curl() {
  require_chat_api_key
  curl -k -sS --max-time "${CURL_TIMEOUT_SECONDS}" \
    -H "X-API-Key: ${CHAT_API_KEY}" \
    -H "X-Cordum-Principal: ${LLMCHAT_OPS_PRINCIPAL:-ops-probe}" \
    -H "X-Cordum-Tenant: ${LLMCHAT_OPS_TENANT:-default}" \
    -H "X-Cordum-Role: ${LLMCHAT_OPS_ROLE:-admin}" \
    "$@"
}

chat_post_json() {
  local payload="$1"
  local body_file="$2"
  local status_file="$3"
  record_section "chat POST $(chat_post_url)"
  log_evidence "payload=${payload}"
  set +e
  local status
  status=$(chat_auth_curl -o "${body_file}" -w '%{http_code}' -H 'Content-Type: application/json' -X POST --data "${payload}" "$(chat_post_url)" 2>>"${EVIDENCE_FILE}")
  local code=$?
  set -e
  printf '%s' "${status}" >"${status_file}"
  log_evidence "curl_exit=${code} http_status=${status} body_file=${body_file}"
  [ -f "${body_file}" ] && sed -e 's/^/body: /' "${body_file}" >>"${EVIDENCE_FILE}" || true
  return "${code}"
}

wait_http_status() {
  local label="$1"
  local want="$2"
  local timeout="$3"
  local interval="$4"
  shift 4
  local start now status body
  body="${PROBE_OUT_DIR}/${label//[^A-Za-z0-9_.-]/_}.body"
  start=$(date +%s)
  while true; do
    status=$(curl_status_body "${label}" "${body}" "$@") || true
    [ "${status}" = "${want}" ] && return 0
    now=$(date +%s)
    if [ $((now - start)) -ge "${timeout}" ]; then
      log_evidence "wait_http_status_timeout label=${label} wanted=${want} last_status=${status} timeout=${timeout}"
      return 1
    fi
    sleep "${interval}"
  done
}

wait_chat_health_status() {
  local label="$1"
  local want="$2"
  local timeout="$3"
  local interval="$4"
  local url="${5:-${GATEWAY_URL%/}/api/v1/chat/healthz}"
  require_chat_api_key
  wait_http_status "${label}" "${want}" "${timeout}" "${interval}" \
    -H "X-API-Key: ${CHAT_API_KEY}" "${url}"
}

json_field() {
  local file="$1"
  local expr="$2"
  if [ -z "${PYTHON_BIN}" ]; then
    probe_skip 'python required for JSON parsing; set LLMCHAT_PYTHON_BIN'
  fi
  ${PYTHON_BIN} - "${file}" "${expr}" <<'PY'
import json, sys
path, expr = sys.argv[1], sys.argv[2]
with open(path, encoding='utf-8') as f:
    data = json.load(f)
cur = data
for part in expr.split('.'):
    if not part:
        continue
    if isinstance(cur, dict):
        cur = cur.get(part)
    else:
        cur = None
        break
if cur is None:
    sys.exit(1)
print(cur)
PY
}

write_ws_client() {
  local go_file="${PROBE_OUT_DIR}/llmchat_ws_client.go"
  cat >"${go_file}" <<'GO'
//go:build ignore

package main

import (
  "encoding/base64"
  "encoding/json"
  "flag"
  "fmt"
  "net"
  "net/http"
  "os"
  "time"

  "github.com/gorilla/websocket"
)

type event struct {
  Event string         `json:"event"`
  Time  string         `json:"time"`
  Data  map[string]any `json:"data,omitempty"`
}

func emit(enc *json.Encoder, ev string, data map[string]any) {
  _ = enc.Encode(event{Event: ev, Time: time.Now().UTC().Format(time.RFC3339Nano), Data: data})
}

func main() {
  url := flag.String("url", "", "ws://.../api/v1/chat/ws")
  apiKey := flag.String("api-key", "", "Cordum API key")
  principal := flag.String("principal", "ops-probe", "principal header")
  tenant := flag.String("tenant", "default", "tenant header")
  role := flag.String("role", "admin", "role header")
  sessionID := flag.String("session-id", "", "optional session id")
  origin := flag.String("origin", "http://127.0.0.1", "Origin header")
  messagesJSON := flag.String("messages", "[]", "JSON array of messages to send")
  readSeconds := flag.Int("read-seconds", 60, "seconds to read frames")
  expectTerminal := flag.Bool("expect-terminal", false, "exit non-zero unless final/error/approval_required frame arrives")
  outPath := flag.String("out", "", "JSONL output path")
  flag.Parse()

  if *url == "" || *apiKey == "" || *outPath == "" {
    fmt.Fprintln(os.Stderr, "url, api-key, and out are required")
    os.Exit(2)
  }
  out, err := os.Create(*outPath)
  if err != nil {
    fmt.Fprintln(os.Stderr, err)
    os.Exit(2)
  }
  defer out.Close()
  enc := json.NewEncoder(out)

  var messages []string
  if err := json.Unmarshal([]byte(*messagesJSON), &messages); err != nil {
    emit(enc, "config_error", map[string]any{"error": err.Error()})
    os.Exit(2)
  }

  header := http.Header{}
  header.Set("X-API-Key", *apiKey)
  header.Set("X-Cordum-Principal", *principal)
  header.Set("X-Cordum-Tenant", *tenant)
  header.Set("X-Cordum-Role", *role)
  header.Set("Origin", *origin)
  if *sessionID != "" {
    header.Set("X-Chat-Session-Id", *sessionID)
  }
  token := base64.RawURLEncoding.EncodeToString([]byte(*apiKey))
  dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second, Subprotocols: []string{"cordum-api-key", token}}
  conn, resp, err := dialer.Dial(*url, header)
  if err != nil {
    status := 0
    if resp != nil { status = resp.StatusCode }
    emit(enc, "dial_error", map[string]any{"error": err.Error(), "http_status": status})
    os.Exit(2)
  }
  defer conn.Close()
  emit(enc, "connected", map[string]any{"session_id": resp.Header.Get("X-Chat-Session-Id"), "subprotocol": conn.Subprotocol()})

  readUntilTerminal := func(turn int, seconds int) bool {
    terminal := false
    deadline := time.Now().Add(time.Duration(seconds) * time.Second)
    for time.Now().Before(deadline) {
      _ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
      var frame map[string]any
      err := conn.ReadJSON(&frame)
      if err != nil {
        if ne, ok := err.(net.Error); ok && ne.Timeout() {
          continue
        }
        emit(enc, "read_error", map[string]any{"turn": turn, "error": err.Error()})
        break
      }
      frame["turn_index"] = turn
      emit(enc, "frame", frame)
      typ, _ := frame["type"].(string)
      if typ == "final" || typ == "error" || typ == "approval_required" {
        terminal = true
        break
      }
    }
    emit(enc, "turn_done", map[string]any{"turn": turn, "terminal": terminal})
    return terminal
  }

  allTerminal := true
  if len(messages) == 0 {
    allTerminal = readUntilTerminal(0, *readSeconds)
  } else {
    perTurnSeconds := *readSeconds
    if perTurnSeconds < 5 { perTurnSeconds = 5 }
    for i, msg := range messages {
      if err := conn.WriteJSON(map[string]string{"message": msg}); err != nil {
        emit(enc, "write_error", map[string]any{"turn": i+1, "error": err.Error()})
        os.Exit(2)
      }
      emit(enc, "sent", map[string]any{"turn": i+1, "message": msg})
      if !readUntilTerminal(i+1, perTurnSeconds) {
        allTerminal = false
        break
      }
    }
  }
  emit(enc, "done", map[string]any{"terminal": allTerminal})
  if *expectTerminal && !allTerminal {
    os.Exit(3)
  }
}
GO
  printf '%s' "${go_file}"
}

run_ws_client() {
  local out_file="$1"
  local messages_json="$2"
  local read_seconds="${3:-60}"
  local expect_terminal="${4:-false}"
  require_go
  require_chat_api_key
  local go_file
  go_file="$(write_ws_client)"
  record_section "ws client $(chat_ws_url)"
  log_evidence "messages=${messages_json} read_seconds=${read_seconds} expect_terminal=${expect_terminal} out=${out_file}"
  set +e
  "${GO_BIN}" run "${go_file}" \
    -url "$(chat_ws_url)" \
    -api-key "${CHAT_API_KEY}" \
    -principal "${LLMCHAT_OPS_PRINCIPAL:-ops-probe}" \
    -tenant "${LLMCHAT_OPS_TENANT:-default}" \
    -role "${LLMCHAT_OPS_ROLE:-admin}" \
    -origin "${LLMCHAT_OPS_ORIGIN:-http://127.0.0.1}" \
    -messages "${messages_json}" \
    -read-seconds "${read_seconds}" \
    -expect-terminal="${expect_terminal}" \
    -out "${out_file}" >>"${EVIDENCE_FILE}" 2>&1
  local code=$?
  set -e
  log_evidence "ws_client_exit=${code} out=${out_file}"
  [ -f "${out_file}" ] && sed -e 's/^/ws: /' "${out_file}" >>"${EVIDENCE_FILE}" || true
  return "${code}"
}


container_rss_kb() {
  local service="$1" cid
  cid="$(require_service_container "${service}")"
  docker exec "${cid}" sh -c "awk '/VmRSS:/ {print \\$2}' /proc/1/status" 2>>"${EVIDENCE_FILE}"
}

container_fd_count() {
  local service="$1" cid
  cid="$(require_service_container "${service}")"
  docker exec "${cid}" sh -c 'ls /proc/1/fd 2>/dev/null | wc -l' 2>>"${EVIDENCE_FILE}"
}

assert_no_bang_stream() {
  local file="$1"
  if grep -E '!{8,}' "${file}" >/dev/null 2>&1; then
    probe_fail "detected qwen parser bang-stream corruption in ${file}"
  fi
}

count_ws_frames() {
  local file="$1" type="$2"
  grep -E '"event":"frame"' "${file}" | grep -E '"type":"'"${type}"'"' | wc -l | tr -d ' '
}

pprof_fetch() {
  local profile="$1" out="$2"
  local base="${LLMCHAT_PPROF_URL:-${LLMCHAT_DIRECT_URL%/}/debug/pprof}"
  curl -k -fsS --max-time "${CURL_TIMEOUT_SECONDS}" "${base%/}/${profile}" -o "${out}" 2>>"${EVIDENCE_FILE}"
}


compose_network_name() {
  if [ -n "${LLMCHAT_DOCKER_NETWORK:-}" ]; then
    printf '%s' "${LLMCHAT_DOCKER_NETWORK}"
    return 0
  fi
  local project="${COMPOSE_PROJECT_NAME:-cordum}"
  local candidate
  for candidate in "${project}_default" "${project}-default" cordum_default cordum-net; do
    if docker network inspect "${candidate}" >/dev/null 2>&1; then
      printf '%s' "${candidate}"
      return 0
    fi
  done
  docker network ls --format '{{.Name}}' | grep -E 'cordum.*(default|net)' | head -n 1
}

network_disconnect_service() {
  local service="$1" network cid
  network="$(compose_network_name)"
  [ -n "${network}" ] || probe_skip 'Cordum docker network not found; set LLMCHAT_DOCKER_NETWORK'
  cid="$(require_service_container "${service}")"
  log_evidence "network_disconnect service=${service} container=${cid} network=${network}"
  docker network disconnect -f "${network}" "${cid}"
}

network_connect_service() {
  local service="$1" network cid
  network="$(compose_network_name)"
  [ -n "${network}" ] || probe_skip 'Cordum docker network not found; set LLMCHAT_DOCKER_NETWORK'
  cid="$(require_service_container "${service}")"
  log_evidence "network_connect service=${service} container=${cid} network=${network}"
  docker network connect "${network}" "${cid}" 2>>"${EVIDENCE_FILE}" || true
}


require_helm() {
  [ -n "${HELM_BIN}" ] || probe_skip 'helm command not found; set LLMCHAT_HELM_BIN'
}

require_kubectl() {
  [ -n "${KUBECTL_BIN}" ] || probe_skip 'kubectl command not found; set LLMCHAT_KUBECTL_BIN'
}

require_k8s_live() {
  if [ "${LLMCHAT_OPS_K8S_LIVE:-0}" != '1' ]; then
    probe_skip 'kubernetes live probe disabled; rerun with LLMCHAT_OPS_K8S_LIVE=1 against a staging cluster'
  fi
  require_kubectl
}

metric_max_value() {
  local file="$1"
  ${PYTHON_BIN:-python} - "${file}" <<'PY'
import re, sys
vals=[]
for line in open(sys.argv[1], encoding='utf-8'):
    if line.startswith('#'):
        continue
    m=re.search(r'([-+]?\d+(?:\.\d+)?(?:[eE][-+]?\d+)?)\s*$', line.strip())
    if m:
        try: vals.append(float(m.group(1)))
        except Exception: pass
print(max(vals) if vals else '')
PY
}

ops_probe_scaffold_skip() {
  record_section 'scaffold status'
  log_evidence 'reason=step-2 scaffolding only; destructive/runtime implementation is filled by later plan steps or GPU-staging operator'
  log_evidence 'set LLMCHAT_OPS_LIVE=1 on a dedicated stack when this probe is implemented for live execution'
  probe_skip 'probe scaffolded; live implementation pending later phase / GPU-staging run'
}
