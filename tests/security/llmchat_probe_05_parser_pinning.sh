#!/usr/bin/env bash
set -euo pipefail
PROBE_ID="llmchat_probe_05_parser_pinning"
# shellcheck source=tests/security/llmchat_common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/llmchat_common.sh"
probe_init
write_probe_manifest "SUPERSEDED for production default: Ollama has no tool-call parser; opt-in vLLM profile must not enable auto-tool-choice or tool-call-parser flags in informational-only mode"
record_section "attack payload"
log_evidence 'payload (historical): malicious operator sets --tool-call-parser=qwen3_coder/hermes. Expected current defense: production default is Ollama (no parser), and opt-in vLLM lint rejects any tool-parser/auto-tool-choice flag because chat no longer uses tools.'
log_evidence "scope=superseded reason=Ollama/Qwen2.5-Coder is production default and informational-only vLLM has no parser surface"

assert_file_contains "cordum-helm/values.yaml" 'backend:[[:space:]]+ollama-cpu' "Helm default inference backend must be ollama-cpu"
assert_file_contains "docker-compose.yml" 'LLMCHAT_OPS_BACKEND=\$\{LLMCHAT_OPS_BACKEND:-ollama-cpu\}' "compose chat service must default to Ollama backend"
assert_file_contains "docker-compose.yml" 'qwen2\.5-coder:3b-instruct-q4_K_M-ctx32k' "compose must serve the default Ollama/Qwen2.5 model"

for file in docker-compose.yml docker-compose.release.yml cordum-helm/templates/deployment-qwen-inference.yaml; do
  assert_file_not_contains "${file}" '^[[:space:]]*-[[:space:]]+--tool-call-parser[[:space:]]*$' "informational-only vLLM must not configure a tool-call parser"
  assert_file_not_contains "${file}" '^[[:space:]]*-[[:space:]]+--enable-auto-tool-choice[[:space:]]*$' "informational-only vLLM must not enable auto tool choice"
  assert_file_not_contains "${file}" '^[[:space:]]*-[[:space:]]+(qwen3_coder|hermes)[[:space:]]*$' "disallowed legacy parser aliases must not appear as command args"
done

run_capture "compose vLLM config lint" bash tools/scripts/vllm_config_lint.sh || probe_fail "compose vLLM config lint failed"
run_capture "compose vLLM negative lint tests" bash tools/scripts/vllm_config_lint_test.sh || probe_fail "compose vLLM negative lint tests failed"
if [ -n "${HELM_BIN}" ] && "${HELM_BIN}" version --client >/dev/null 2>&1; then
  run_capture "Helm vLLM lint" bash tools/scripts/vllm_helm_lint.sh || probe_fail "Helm vLLM lint failed"

  render="${PROBE_OUT_DIR}/helm-parser-override.yaml"
  record_section "helm override negative test"
  log_evidence '+ helm template cordum-helm --set inference.backend=vllm-gpu --set qwenInference.toolCallParser=qwen3_coder ...'
  run_helm_template "${REPO_ROOT}/cordum-helm" --set secrets.apiKey=lint-dummy --set redis.auth.password=lint-dummy --set inference.backend=vllm-gpu --set qwenInference.toolCallParser=qwen3_coder >"${render}" 2>>"${EVIDENCE_FILE}" || probe_fail "helm template with parser override failed to render"
  assert_text_not_contains "${render}" '^[[:space:]]*-[[:space:]]+--tool-call-parser[[:space:]]*$' "rendered Helm must ignore malicious parser override"
  assert_text_not_contains "${render}" '^[[:space:]]*-[[:space:]]+"?(qwen3_coder|hermes)"?[[:space:]]*$' "rendered Helm must not include disallowed parser aliases"
else
  log_evidence "helm_lint=optional_not_run reason=helm CLI unavailable or not executable from this shell; static template assertions remain scored"
fi

probe_pass "parser-pinning production blocker is superseded; no-parser informational vLLM lint passes"
