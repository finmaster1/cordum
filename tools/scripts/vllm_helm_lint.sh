#!/usr/bin/env bash
# vllm_helm_lint.sh — phase-10 hard gate against vLLM config drift in
# the Helm chart. Renders `helm template cordum-helm -f values.yaml`
# and asserts the rendered qwen-inference Deployment + Service match
# the LLM-chat epic prescription.
#
# Lints the rendered output (not the templates directly) so a values
# default change cannot silently bypass the rules.
#
# Usage: bash tools/scripts/vllm_helm_lint.sh [chart-dir] [values-file ...]
# Defaults: cordum-helm + cordum-helm/values.yaml.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=tools/scripts/vllm_lint_common.sh
. "${REPO_ROOT}/tools/scripts/vllm_lint_common.sh"

CHART_DIR="${1:-${REPO_ROOT}/cordum-helm}"
shift || true
VALUES_FILES=()
if [ "$#" -gt 0 ]; then
	VALUES_FILES=("$@")
else
	VALUES_FILES=("${CHART_DIR}/values.yaml")
fi

HELM_BIN="${LLMCHAT_HELM_BIN:-}"
if [ -z "$HELM_BIN" ]; then
	if command -v helm >/dev/null 2>&1; then
		HELM_BIN="helm"
	elif command -v helm.exe >/dev/null 2>&1; then
		HELM_BIN="helm.exe"
	fi
fi

if [ -z "$HELM_BIN" ]; then
	echo "[ERROR] helm CLI not installed; install via setup-helm@v4 in CI or 'choco/brew install helm' locally" >&2
	exit 2
fi

helm_path_arg() {
	local path="$1"
	if [[ "$HELM_BIN" == *".exe" ]] && command -v wslpath >/dev/null 2>&1 && [[ "$path" == /* ]] && [ -e "$path" ]; then
		wslpath -w "$path"
	else
		printf '%s\n' "$path"
	fi
}

# Render the chart with mandatory secrets passed inline so `helm
# template` does not error on `required` notices.
RENDER_TMP="$(mktemp -t vllm-helm-lint-XXXX.yaml)"
trap 'rm -f "${RENDER_TMP}"' EXIT

values_args=()
for vf in "${VALUES_FILES[@]}"; do
	values_args+=(-f "$(helm_path_arg "$vf")")
done
CHART_ARG="$(helm_path_arg "$CHART_DIR")"

if ! "$HELM_BIN" template "$CHART_ARG" \
	"${values_args[@]}" \
	--set secrets.apiKey=lint-dummy \
	--set redis.auth.password=lint-dummy \
	--set inference.backend=vllm-gpu \
	>"$RENDER_TMP" 2>/dev/null; then
	echo "[ERROR] helm template render failed for ${CHART_DIR}; cannot lint" >&2
	"$HELM_BIN" template "$CHART_ARG" "${values_args[@]}" --set secrets.apiKey=lint-dummy --set redis.auth.password=lint-dummy --set inference.backend=vllm-gpu >&2 || true
	exit 2
fi

# Rule: model must match the active tier.
expected_model=$(vllm_lint_tier_model_name)
vllm_lint_assert_present "$RENDER_TMP" "^[[:space:]]+-[[:space:]]+\"?${expected_model}\"?[[:space:]]*$" \
	"helm-model-must-match-tier" "rendered Deployment must have vLLM model '${expected_model}'"

# Rule: informational-only vLLM no longer advertises LLM tool-parser flags in Helm.
vllm_lint_assert_absent "$RENDER_TMP" "^[[:space:]]*-[[:space:]]+--enable-auto-tool-choice[[:space:]]*$" \
	"helm-auto-tool-choice-disallowed" "rendered Deployment must not enable auto tool choice"
vllm_lint_assert_absent "$RENDER_TMP" "^[[:space:]]*-[[:space:]]+--tool-call-parser[[:space:]]*$" \
	"helm-tool-call-parser-disallowed" "rendered Deployment must not configure a tool-call parser"

# Rule: --max-model-len 131072.
vllm_lint_assert_present "$RENDER_TMP" "^[[:space:]]+-[[:space:]]+--max-model-len[[:space:]]*$" \
	"helm-max-model-len-flag" "rendered Deployment missing --max-model-len flag"
vllm_lint_assert_present "$RENDER_TMP" "^[[:space:]]+-[[:space:]]+\"?131072\"?[[:space:]]*$" \
	"helm-max-model-len-value" "rendered Deployment missing 131072 value for --max-model-len"

# Rule: --kv-cache-dtype fp8.
vllm_lint_assert_present "$RENDER_TMP" "^[[:space:]]+-[[:space:]]+--kv-cache-dtype[[:space:]]*$" \
	"helm-kv-cache-dtype-flag" "rendered Deployment missing --kv-cache-dtype flag"
vllm_lint_assert_present "$RENDER_TMP" "^[[:space:]]+-[[:space:]]+\"?fp8\"?[[:space:]]*$" \
	"helm-kv-cache-dtype-value" "rendered Deployment missing 'fp8' value for --kv-cache-dtype"

# Rule: --enable-prefix-caching present.
vllm_lint_assert_present "$RENDER_TMP" "^[[:space:]]+-[[:space:]]+--enable-prefix-caching[[:space:]]*$" \
	"helm-enable-prefix-caching" "rendered Deployment missing --enable-prefix-caching flag"

# Rule: --disable-log-requests present.
vllm_lint_assert_present "$RENDER_TMP" "^[[:space:]]+-[[:space:]]+--disable-log-requests[[:space:]]*$" \
	"helm-disable-log-requests" "rendered Deployment missing --disable-log-requests (vLLM must not log prompts/request bodies)"

# Rule: qwen-inference Service must be ClusterIP. Wildcard exposure
# (LoadBalancer / NodePort) would break the zero-egress invariant.
# Extract the qwen-inference Service block and assert type.
qwen_service_lines=$(awk '
	/^---$/ { kind=""; name=""; type=""; block_start=NR; next }
	/^kind:[[:space:]]+Service$/ { kind="Service" }
	/^[[:space:]]+name:[[:space:]]+/ && name=="" { sub(/^[[:space:]]+name:[[:space:]]+/, ""); name=$0 }
	/^[[:space:]]+type:[[:space:]]+/ {
		sub(/^[[:space:]]+type:[[:space:]]+/, ""); type=$0
		if (kind == "Service" && index(name, "qwen-inference") > 0) {
			print NR ":" type
		}
	}
' "$RENDER_TMP")

if [ -z "$qwen_service_lines" ]; then
	vllm_lint_print_fail "$RENDER_TMP" "-" "helm-qwen-service-missing" \
		"no Service named 'qwen-inference' in rendered chart; helm template may have skipped the template"
else
	while IFS= read -r entry; do
		[ -z "$entry" ] && continue
		line=$(echo "$entry" | cut -d: -f1)
		val=$(echo "$entry" | cut -d: -f2-)
		if [ "$val" != "ClusterIP" ]; then
			vllm_lint_print_fail "$RENDER_TMP" "$line" "helm-service-type-clusterip" \
				"qwen-inference Service.type must be ClusterIP (got '$val'); LoadBalancer/NodePort would break zero-egress"
		fi
	done <<<"$qwen_service_lines"
fi

# Rule: values.yaml has the qwenInference section with all 5
# mandatory keys (per task description deliverable 2). Read directly
# from values.yaml; this is the operator-facing override surface.
for vf in "${VALUES_FILES[@]}"; do
	if [ ! -f "$vf" ]; then
		vllm_lint_print_fail "$vf" "-" "values-file-missing" "values file does not exist"
		continue
	fi

	for key in maxModelLen kvCacheDtype enablePrefixCaching gpuMemoryUtilization; do
		if vllm_lint_have_yq; then
			got=$(yq -r ".qwenInference.${key}" "$vf" 2>/dev/null || echo "<missing>")
			if [ "$got" = "null" ] || [ "$got" = "<missing>" ]; then
				vllm_lint_print_fail "$vf" "-" "values-qwenInference-missing-${key}" \
					"qwenInference.${key} missing from values.yaml"
			fi
		else
			# grep fallback: nested key lives under qwenInference: block.
			# Approximate check — does the key appear at all in the file?
			if ! grep -nE "^[[:space:]]+${key}:" "$vf" >/dev/null 2>&1; then
				vllm_lint_print_fail "$vf" "-" "values-qwenInference-missing-${key}" \
					"qwenInference.${key} missing from values.yaml (grep fallback)"
			fi
		fi
	done
done

if [ "$FAILS" -gt 0 ]; then
	echo "[vllm-helm-lint] FAILED with ${FAILS} rule violation(s)." >&2
	exit "$FAILS"
fi

echo "[vllm-helm-lint] OK — chart at ${CHART_DIR} renders with all rules satisfied."
exit 0
