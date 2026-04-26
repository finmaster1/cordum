#!/usr/bin/env bash
# vllm_config_lint_test.sh — negative-case driver for the phase-10
# vLLM lint scripts. Builds tiny fixture compose snippets containing
# specific violations, runs vllm_config_lint.sh against them, and
# asserts (a) non-zero exit and (b) the rule name appears in stderr.
#
# Run each negative case 3× to mirror the -count=3 flake-detection
# convention from go test (no equivalent shell flag, manual loop).
#
# Usage: bash tools/scripts/vllm_config_lint_test.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LINT="${REPO_ROOT}/tools/scripts/vllm_config_lint.sh"

if [ ! -x "$LINT" ]; then
	echo "[ERROR] $LINT not executable; run 'chmod +x' first" >&2
	exit 2
fi

PASS=0
FAIL=0

# write_fixture writes a minimal compose YAML with the qwen-inference
# service. The caller passes the body of the `command:` section, the
# ports binding, and the start_period. Returns the path to the temp
# file (caller responsible for cleanup via the trap).
TMPDIR_FIX="$(mktemp -d -t vllm-lint-fixtures-XXXX)"
trap 'rm -rf "${TMPDIR_FIX}"' EXIT

write_fixture() {
	local label="$1"
	local cmd_body="$2"
	local ports_body="$3"
	local healthcheck_period="$4"
	local out="${TMPDIR_FIX}/${label}.yaml"
	cat >"$out" <<EOF
services:
  qwen-inference:
    profiles: [llmchat]
    image: vllm/vllm-openai:latest
    command:
${cmd_body}
    ports:
${ports_body}
    healthcheck:
      test: ["CMD-SHELL", "wget --spider -q http://127.0.0.1:8000/v1/models || exit 1"]
      interval: 30s
      timeout: 5s
      retries: 5
      start_period: ${healthcheck_period}
EOF
	echo "$out"
}

# Standard correct command body (Tier 1) — used as the basis for
# negative cases (each negative replaces ONE flag with the violation).
correct_cmd() {
	cat <<'EOF'
      - --model
      - Qwen/Qwen3-Coder-30B-A3B-Instruct-FP8
      - --served-model-name
      - qwen3-coder
      - --enable-auto-tool-choice
      - --tool-call-parser
      - qwen3_xml
      - --max-model-len
      - "131072"
      - --kv-cache-dtype
      - fp8
      - --enable-prefix-caching
      - --gpu-memory-utilization
      - "0.9"
      - --host
      - 0.0.0.0
      - --port
      - "8000"
EOF
}

correct_ports() {
	cat <<'EOF'
      - "127.0.0.1:8000:8000"
EOF
}

# run_case invokes the lint against a fixture and asserts the rule
# pattern appears in stderr. $1=label $2=fixture-path $3=expected-rule-substring.
run_case() {
	local label="$1"
	local fixture="$2"
	local expected_rule="$3"
	local got_exit
	local got_err
	got_err=$(bash "$LINT" "$fixture" 2>&1 1>/dev/null || true)
	got_exit=$(bash "$LINT" "$fixture" >/dev/null 2>&1 && echo 0 || echo $?)

	if [ "$got_exit" -eq 0 ]; then
		echo "[FAIL] case ${label}: lint returned 0 against fixture with violation; expected non-zero" >&2
		FAIL=$((FAIL + 1))
		return
	fi
	if ! echo "$got_err" | grep -qE "rule=${expected_rule}"; then
		echo "[FAIL] case ${label}: stderr did not name expected rule '${expected_rule}'" >&2
		echo "  stderr was:" >&2
		echo "$got_err" | head -5 | sed 's/^/    /' >&2
		FAIL=$((FAIL + 1))
		return
	fi
	PASS=$((PASS + 1))
}

# Each case runs 3× to flush flake. Counter still accumulates pass/fail
# at this granularity so a single intermittent case shows up.
for run in 1 2 3; do
	# (i) hermes parser injection — replaces qwen3_xml with hermes.
	hermes_cmd=$(correct_cmd | sed 's/qwen3_xml/hermes/')
	fix=$(write_fixture "hermes_parser_${run}" "$hermes_cmd" "$(correct_ports)" "300s")
	run_case "hermes-parser run=${run}" "$fix" "parser-disallowed-hermes"

	# (ii) host 0.0.0.0 in *port mapping* (the actual rule is on the
	# port mapping, not the bind address — the bind 0.0.0.0 is correct
	# for container DNS). Negative: replace ports with 0.0.0.0:8000:8000.
	bad_ports=$(printf '      - "0.0.0.0:8000:8000"\n')
	fix=$(write_fixture "host_wildcard_${run}" "$(correct_cmd)" "$bad_ports" "300s")
	run_case "ports-wildcard run=${run}" "$fix" "ports-disallow-wildcard"

	# (iii) missing --kv-cache-dtype fp8 — drop the two lines.
	missing_kv_cmd=$(correct_cmd | sed '/--kv-cache-dtype/d; /^      - fp8/d')
	fix=$(write_fixture "missing_kv_cache_${run}" "$missing_kv_cmd" "$(correct_ports)" "300s")
	run_case "missing-kv-cache run=${run}" "$fix" "kv-cache-dtype-flag"

	# (iv) start_period: 600s — wrong duration.
	fix=$(write_fixture "wrong_start_period_${run}" "$(correct_cmd)" "$(correct_ports)" "600s")
	run_case "wrong-start-period run=${run}" "$fix" "start-period-must-be-300s"

	# (v) ports binding 0.0.0.0:8000:8000 (already covered by (ii));
	# add a parallel bare 8000:8000 negative for completeness.
	bare_ports=$(printf '      - "8000:8000"\n')
	fix=$(write_fixture "bare_ports_${run}" "$(correct_cmd)" "$bare_ports" "300s")
	run_case "bare-ports run=${run}" "$fix" "ports-disallow-bare"
done

# Positive case: the actual current docker-compose.yml + .release.yml
# MUST pass (rail #5 self-test). One run is enough for the positive.
if bash "$LINT" >/dev/null 2>&1; then
	PASS=$((PASS + 1))
else
	echo "[FAIL] positive case: actual current compose files failed lint; phase-7 deliverables broken or lint mis-encoded" >&2
	FAIL=$((FAIL + 1))
fi

echo
echo "[vllm-config-lint-test] PASS=${PASS} FAIL=${FAIL}"
if [ "$FAIL" -gt 0 ]; then
	exit 1
fi
exit 0
