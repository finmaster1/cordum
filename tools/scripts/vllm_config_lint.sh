#!/usr/bin/env bash
# vllm_config_lint.sh — phase-10 hard gate against vLLM config drift in
# docker-compose. Asserts the qwen-inference service ships the exact
# parser, model, sampling flags, and host-port boundary the LLM-chat
# epic prescribes. If ANY rule violates, exits non-zero (count of
# violations) so CI fails the PR.
#
# Rules are deliberately strict: phase-7 shipped the wrong parser
# (`hermes` instead of `qwen3_xml`) before this gate existed and burned
# a QA cycle. The lint exists to make that class of bug impossible to
# re-introduce.
#
# Usage: bash tools/scripts/vllm_config_lint.sh [compose-file ...]
# Default targets: docker-compose.yml + docker-compose.release.yml.
# Tier 1 (default) expects FP8; CORDUM_LLMCHAT_TIER=2 expects AWQ.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=tools/scripts/vllm_lint_common.sh
. "${REPO_ROOT}/tools/scripts/vllm_lint_common.sh"

DEFAULT_TARGETS=(
	"${REPO_ROOT}/docker-compose.yml"
	"${REPO_ROOT}/docker-compose.release.yml"
)

if [ "$#" -gt 0 ]; then
	TARGETS=("$@")
else
	TARGETS=("${DEFAULT_TARGETS[@]}")
fi

# Each target is checked independently; FAILs aggregate.
for file in "${TARGETS[@]}"; do
	if [ ! -f "$file" ]; then
		vllm_lint_print_fail "$file" "-" "target-missing" "compose file does not exist"
		continue
	fi

	# Skip targets that don't define qwen-inference at all (e.g. files
	# scoped to non-llmchat profiles). Assertion: if `qwen-inference:`
	# absent, the target has nothing to lint.
	if ! grep -nE '^[[:space:]]+qwen-inference:' "$file" >/dev/null 2>&1; then
		# Not a vLLM target. Skip silently.
		continue
	fi

	# Rule: model must match the active tier. Models are exact identifiers,
	# not regex families (rail #4: explicit Tier-2 codepath).
	expected_model=$(vllm_lint_tier_model_name)
	# Anchor on `- ` to match a YAML list element line.
	vllm_lint_assert_present "$file" "^[[:space:]]*-[[:space:]]+${expected_model}[[:space:]]*$" \
		"model-must-match-tier" "expected vLLM model '${expected_model}' (CORDUM_LLMCHAT_TIER=${CORDUM_LLMCHAT_TIER:-1})"

	# Rule: parser must be qwen3_xml. The phase-7 incident shipped
	# `hermes` which produces malformed tool-call deltas; `qwen3_coder`
	# is not an upstream-vLLM parser at all.
	vllm_lint_assert_present "$file" "^[[:space:]]*-[[:space:]]+qwen3_xml[[:space:]]*$" \
		"parser-must-be-qwen3-xml" "missing '- qwen3_xml' parser arg"
	vllm_lint_assert_absent "$file" "^[[:space:]]*-[[:space:]]+hermes[[:space:]]*$" \
		"parser-disallowed-hermes" "disallowed parser '- hermes' present (use qwen3_xml)"
	vllm_lint_assert_absent "$file" "^[[:space:]]*-[[:space:]]+qwen3_coder[[:space:]]*$" \
		"parser-disallowed-qwen3-coder" "disallowed parser '- qwen3_coder' present (not a real upstream parser; use qwen3_xml)"

	# Rule: --max-model-len 131072 (the model's full context window).
	vllm_lint_assert_present "$file" "^[[:space:]]*-[[:space:]]+--max-model-len[[:space:]]*$" \
		"max-model-len-flag" "missing '--max-model-len' flag"
	vllm_lint_assert_present "$file" "^[[:space:]]*-[[:space:]]+\"131072\"[[:space:]]*$" \
		"max-model-len-value" "missing '131072' value for --max-model-len"

	# Rule: --kv-cache-dtype fp8 (memory budget halves under FP8 KV cache).
	vllm_lint_assert_present "$file" "^[[:space:]]*-[[:space:]]+--kv-cache-dtype[[:space:]]*$" \
		"kv-cache-dtype-flag" "missing '--kv-cache-dtype' flag"
	vllm_lint_assert_present "$file" "^[[:space:]]*-[[:space:]]+fp8[[:space:]]*$" \
		"kv-cache-dtype-value" "missing 'fp8' value for --kv-cache-dtype"

	# Rule: --enable-prefix-caching (multi-turn chats reuse system-prompt KV).
	vllm_lint_assert_present "$file" "^[[:space:]]*-[[:space:]]+--enable-prefix-caching[[:space:]]*$" \
		"enable-prefix-caching" "missing '--enable-prefix-caching' flag"

	# Rule: ports binding loopback-only. The container bridge bind
	# (`--host 0.0.0.0`) is intentional for Docker DNS reachability;
	# the host-side boundary is the loopback port mapping.
	vllm_lint_assert_present "$file" "127\.0\.0\.1:8000:8000" \
		"ports-must-be-loopback" "qwen-inference must publish on 127.0.0.1:8000:8000 (loopback only)"
	vllm_lint_assert_absent "$file" "^[[:space:]]*-[[:space:]]+\"?0\.0\.0\.0:8000:8000\"?[[:space:]]*$" \
		"ports-disallow-wildcard" "ports binding must NOT be 0.0.0.0:8000:8000 (would expose vLLM to the network)"
	vllm_lint_assert_absent "$file" "^[[:space:]]*-[[:space:]]+\"?8000:8000\"?[[:space:]]*$" \
		"ports-disallow-bare" "ports binding must NOT be bare 8000:8000 (defaults to 0.0.0.0; use 127.0.0.1:8000:8000)"

	# Rule: healthcheck.start_period 300s. FP8 weights are ~30GB;
	# anything shorter risks the container being marked unhealthy
	# before vLLM finishes warmup.
	if vllm_lint_have_yq; then
		# Use yq for the precise nested query.
		got=$(yq -r '.services."qwen-inference".healthcheck.start_period' "$file" 2>/dev/null || echo "<missing>")
		if [ "$got" != "300s" ]; then
			vllm_lint_print_fail "$file" "-" "start-period-must-be-300s" \
				"qwen-inference.healthcheck.start_period = '$got', want '300s' (FP8 weights take 3-5min to load)"
		fi
	else
		vllm_lint_assert_present "$file" "^[[:space:]]+start_period:[[:space:]]+300s[[:space:]]*$" \
			"start-period-must-be-300s" "qwen-inference healthcheck.start_period must be 300s"
	fi
done

if [ "$FAILS" -gt 0 ]; then
	echo "[vllm-config-lint] FAILED with ${FAILS} rule violation(s). Fix the reported file:line entries above." >&2
	exit "$FAILS"
fi

echo "[vllm-config-lint] OK — all rules pass on ${#TARGETS[@]} target(s)."
exit 0
