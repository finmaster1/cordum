#!/usr/bin/env bash
set -euo pipefail
PROBE_ID="llmchat_probe_06_loopback_binding"
# shellcheck source=tests/security/llmchat_common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/llmchat_common.sh"
probe_init
write_probe_manifest "default Ollama and opt-in vLLM inference endpoints are host-loopback only in Compose and ClusterIP only in Helm"
record_section "attack payload"
log_evidence 'payload: change compose inference port to 0.0.0.0/bare host binding, or Helm Service.type=LoadBalancer/NodePort. Expected static lint/probe failure before deploy. Default scope is Ollama :11434; opt-in vLLM keeps :8000.'

for file in docker-compose.yml docker-compose.release.yml; do
  assert_file_contains "${file}" '127\.0\.0\.1:11434:11434' "compose Ollama host port must bind loopback"
  assert_file_not_contains "${file}" '^[[:space:]]*-[[:space:]]+"?0\.0\.0\.0:11434:11434"?[[:space:]]*$' "compose Ollama must not bind wildcard host"
  assert_file_not_contains "${file}" '^[[:space:]]*-[[:space:]]+"?11434:11434"?[[:space:]]*$' "compose Ollama must not use bare host-port mapping"
  assert_file_contains "${file}" '127\.0\.0\.1:8000:8000' "opt-in compose vLLM host port must bind loopback"
  assert_file_not_contains "${file}" '^[[:space:]]*-[[:space:]]+"?0\.0\.0\.0:8000:8000"?[[:space:]]*$' "compose vLLM must not bind wildcard host"
  assert_file_not_contains "${file}" '^[[:space:]]*-[[:space:]]+"?8000:8000"?[[:space:]]*$' "compose vLLM must not use bare host-port mapping"
done
assert_file_contains "cordum-helm/templates/deployment-ollama-inference.yaml" 'type:[[:space:]]+ClusterIP' "Helm Ollama Service must be ClusterIP"
assert_file_contains "cordum-helm/templates/service-qwen-inference.yaml" 'type:[[:space:]]+ClusterIP' "Helm qwen-inference Service must be ClusterIP"
assert_file_not_contains "cordum-helm/templates/deployment-ollama-inference.yaml" '^[[:space:]]*type:[[:space:]]*(LoadBalancer|NodePort)' "Helm Ollama Service must never expose externally"
assert_file_not_contains "cordum-helm/templates/service-qwen-inference.yaml" '^[[:space:]]*type:[[:space:]]*(LoadBalancer|NodePort)' "Helm qwen-inference Service must never expose externally"

if command -v docker >/dev/null 2>&1 && docker version >/dev/null 2>&1; then
  run_capture "docker compose default Ollama config" bash -lc 'CORDUM_API_KEY=dummy REDIS_PASSWORD=dummy docker compose -f docker-compose.yml config -q' || probe_fail "docker compose default Ollama config failed"
  run_capture "docker compose release default Ollama config" bash -lc 'CORDUM_API_KEY=dummy REDIS_PASSWORD=dummy CORDUM_TLS_DIR=./certs SAFETY_POLICY_PUBLIC_KEY=dummy SAFETY_POLICY_SIGNATURE=dummy docker compose -f docker-compose.release.yml config -q' || probe_fail "docker compose release default Ollama config failed"
else
  log_evidence "docker_compose_config=optional_not_run reason=docker CLI unavailable in this shell; static Compose loopback assertions remain scored"
fi
run_capture "compose vLLM loopback lint" bash tools/scripts/vllm_config_lint.sh || probe_fail "vLLM compose loopback lint failed"
if [ -n "${HELM_BIN}" ] && "${HELM_BIN}" version --client >/dev/null 2>&1; then
  run_capture "Helm vLLM ClusterIP lint" bash tools/scripts/vllm_helm_lint.sh || probe_fail "vLLM Helm ClusterIP lint failed"

  render_default="${PROBE_OUT_DIR}/helm-render-default.yaml"
  run_helm_template "${REPO_ROOT}/cordum-helm" --set secrets.apiKey=lint-dummy --set redis.auth.password=lint-dummy >"${render_default}" 2>>"${EVIDENCE_FILE}" || probe_fail "default helm template failed"
  assert_text_contains "${render_default}" 'ollama-inference' "default rendered Helm must include Ollama inference Service"
  assert_text_contains "${render_default}" 'type:[[:space:]]+ClusterIP' "rendered Ollama Service must be ClusterIP"
  assert_text_not_contains "${render_default}" 'type:[[:space:]]+(LoadBalancer|NodePort)' "default rendered Helm must not expose inference externally"

  render_vllm="${PROBE_OUT_DIR}/helm-render-vllm.yaml"
  run_helm_template "${REPO_ROOT}/cordum-helm" --set secrets.apiKey=lint-dummy --set redis.auth.password=lint-dummy --set inference.backend=vllm-gpu >"${render_vllm}" 2>>"${EVIDENCE_FILE}" || probe_fail "vLLM helm template failed"
  assert_text_contains "${render_vllm}" 'qwen-inference' "vLLM rendered Helm must include qwen-inference Service"
  assert_text_contains "${render_vllm}" 'type:[[:space:]]+ClusterIP' "rendered qwen-inference Service must be ClusterIP"
  assert_text_not_contains "${render_vllm}" 'type:[[:space:]]+(LoadBalancer|NodePort)' "vLLM rendered Helm must not expose inference externally"
else
  log_evidence "helm_render=optional_not_run reason=helm CLI unavailable or not executable from this shell; static Helm ClusterIP assertions remain scored"
fi

if [ "${LLMCHAT_SECURITY_LIVE:-0}" = "1" ]; then
  host_body="${PROBE_OUT_DIR}/inference-host-loopback.body"
  status=$(curl_status_body "host loopback inference models" "${host_body}" "${VLLM_URL}/v1/models") || true
  assert_http_status_in "${status}" "200,000" "host loopback check must be deterministic; 200 when local inference is up, 000 when down"
  ext_host="${LLMCHAT_SECURITY_EXTERNAL_HOST:-host.docker.internal}"
  ext_body="${PROBE_OUT_DIR}/inference-external.body"
  port="8000"
  [ "${LLMCHAT_SECURITY_BACKEND}" = "ollama-cpu" ] && port="11434"
  ext_status=$(curl_status_body "external interface inference should refuse" "${ext_body}" "http://${ext_host}:${port}/v1/models") || true
  assert_http_status_in "${ext_status}" "000,403,404" "external interface must not expose inference models"
else
  live_evidence_not_run "live_loopback" "optional: set LLMCHAT_SECURITY_LIVE=1 after clean compose-up to curl host/external/intra-network paths"
fi

probe_pass "Ollama default and opt-in vLLM loopback/ClusterIP exposure checks passed"
