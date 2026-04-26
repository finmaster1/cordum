# Cordum LLM Chat — Model Version Bump Protocol (phase 11)

The chat-assistant ships pinned to a specific model version
(`qwen3_coder_30b_fp8` for v1). Bumping that pin — to a newer Qwen
release, a different size class, a thinking variant, or a non-Qwen
model — must NEVER be a single-line PR. Even fractional differences
on generic tool-calling benchmarks can translate to 20% differences
on Cordum's specific tool schemas, so the only relevant signal is
**our own eval against our own cases**.

This doc describes the 5-step bump procedure and the pinned-baseline
discipline that goes with it.

## 5-step bump

### 1. Stage the new model

Deploy the candidate model to a staging vLLM sidecar. For a Qwen
release this is typically:

```bash
# On the GPU staging host
docker run --gpus all --rm \
  -v ~/.cache/huggingface:/root/.cache/huggingface \
  -p 127.0.0.1:8000:8000 \
  vllm/vllm-openai:latest \
    --model <new-model-id> \
    --served-model-name qwen3-coder \
    --enable-auto-tool-choice \
    --tool-call-parser qwen3_xml \
    --max-model-len 131072 \
    --kv-cache-dtype fp8 \
    --enable-prefix-caching \
    --gpu-memory-utilization 0.9 \
    --host 0.0.0.0 \
    --port 8000
```

Confirm the new model passes `/v1/models` and a hand-test chat round
trip before running the eval.

### 2. Run the eval against the staged model

From the cordum repo, with a cordum stack already up and pointed at
the staging vLLM endpoint:

```bash
EVAL_LLMCHAT_URL=https://staging-cordum:8081 \
EVAL_VLLM_URL=http://staging-vllm:8000/v1 \
EVAL_API_KEY=$STAGING_API_KEY \
EVAL_MODEL_NAME=qwen3_coder_40b_fp8 \
EVAL_BASELINE=tests/eval/baseline/qwen3_coder_30b_fp8.json \
go test -tags=eval -run TestLLMChatToolEval -count=1 -timeout 60m -v \
  ./tests/eval/...
```

The harness writes:

- `tests/eval/results/<run_id>/cases/<case_name>.json` per case
- `tests/eval/results/<run_id>/summary.json` aggregate
- `tests/eval/results/<run_id>/diff.md` baseline diff (when
  `EVAL_BASELINE` is set)

### 3. Review the diff

Open `diff.md`. The harness flags every case where the per-case score
moved by more than the threshold (default 5%):

- ❌ **Regressions** — `>5%` degradation. Each entry blocks merge
  without an explicit waiver in the bump PR description.
- ⚠️ **New failing cases** — case present in the new run but absent
  from baseline (typically because new cases were added).
- 🗑️ **Cases removed** — present in baseline but missing in the new
  run. Do NOT silently remove cases to make a regression go away.
- ✅ **Improvements** — `>5%` gain. Worth calling out so reviewers
  know why the new model is the better choice.

### 4. Update the baseline + the pin in the SAME PR

If the diff is acceptable (no severe regressions, OR each regression
has a documented waiver), regenerate the baseline:

```bash
cp tests/eval/results/<run_id>/summary.json \
   tests/eval/baseline/<new_model_name>.json

# Tag this baseline as a real capture so the comparator gates against
# it on the next bump (see "First real capture", below).
jq '.provenance = "captured"' \
   tests/eval/baseline/<new_model_name>.json \
   > tests/eval/baseline/<new_model_name>.json.tmp \
&& mv tests/eval/baseline/<new_model_name>.json.tmp \
      tests/eval/baseline/<new_model_name>.json
```

Then bump the model pin in the SAME PR:

- `cordum-helm/values.yaml`: `qwenInference.model: <new-model-id>`
- `docker-compose.yml`: the `--model` arg in the `qwen-inference`
  service command.
- `docker-compose.release.yml`: same.
- `tests/eval/cases/`: any new cases discovered during eval triage.
- `CHANGELOG.md`: entry under `[Unreleased]`.

> **Rail #4 — pinned per model version.** Never silently update an
> existing baseline file to make a failing bump pass. A baseline is a
> snapshot of *that model's* behavior; updating it for a different
> model is the same as deleting the gate.

#### First real capture (placeholder → captured)

The v1 baseline (`tests/eval/baseline/qwen3_coder_30b_fp8.json`) ships
with `"provenance": "placeholder"` because no GPU host was available
at v1 cut-time to record real scores. The comparator detects this
marker and downgrades the diff to **informational only** — every
delta is reported but `severeFailures` stays at zero, so the first
real capture against the pinned model is not mistaken for a
regression.

When you produce the first real capture (typically as part of a v2
GPU-budget rollout, but possibly any time staging vLLM access becomes
available before that), follow step 4 above and additionally set
`"provenance": "captured"` (the `jq` snippet handles this). From that
point on, any per-case >5% regression in a future bump fails CI as
designed.

You can regenerate the placeholder shape at any time (e.g. after
adding new cases) via:

```bash
go run tests/eval/cmd/genplaceholderbaseline/main.go \
  > tests/eval/baseline/qwen3_coder_30b_fp8.json
```

The generator only ships a placeholder; replacing it with a real
capture is always a deliberate human action.

### 5. Merge → deploy → monitor

After the PR merges, deploy the new model to production and watch the
existing observability surfaces for 24 hours:

- `/api/v1/audit/verify` chain integrity (no breaks).
- Job/run success rates (no degradation vs the baseline week).
- Approval-gate firing pattern (no spike in unexpected-mutation
  attempts — a sign the new model is hallucinating tool calls).
- LLM-chat session abort rate (no spike).

If any of these regress beyond noise, roll back via Helm value
override and file an incident review.

## Waiver discipline

When a regression is acceptable (e.g. "the new model deliberately
prefers `cordum_run_timeline` over `cordum_get_run` for richer
output, which our case pins as a deviation"), the bump PR description
MUST include:

```markdown
## Eval waivers

| case | baseline → current | reason |
| ---- | ------------------ | ------ |
| filtered_reads/jobs_by_user | 1.00 → 0.92 | new model includes a redundant cordum_status call (cosmetic; doesn't change semantics). Will tighten the case forbidden_tool_calls list in a follow-up. |
```

A reviewer should be able to read each waiver and judge whether the
regression is benign or material. If you can't articulate the reason
in one sentence, the regression is probably real and the bump
shouldn't ship yet.

## Adding new cases for a bump

If the bump exposes behaviors the existing corpus didn't cover (a new
tool-call pattern, a new prompt-injection vector), add the case in
the same PR. New cases:

- Add `tests/eval/cases/<category>/<name>.yaml`.
- Update `tests/eval/baseline/<new_model_name>.json` with the case's
  scores (re-run the eval).
- Note in the PR description: "Added N cases covering <category>".

The default-tag tests (`go test ./tests/eval/`) verify YAML
parseability + 5-cases-per-category minimum, so a malformed case
trips CI before reviewers even see it.

## v2 considerations

This protocol is v1 (manual-trigger eval; no continuous CI gating).
v2 will need:

- A GPU-equipped runner (or staging-vLLM endpoint accessible from
  GitHub Actions).
- A required-check promotion of `llmchat-eval` (or a subset of cases)
  for any PR touching `cordum-helm/values.yaml::qwenInference.model`
  or `docker-compose*.yml::qwen-inference.command`.
- A drift-watcher that re-runs the eval on the current pin nightly
  and opens an issue if scores degrade by >5% (catches upstream-vLLM
  regressions that ship with the same model identifier).

Filed as separate v2 follow-up tasks; out of scope for phase 11.
