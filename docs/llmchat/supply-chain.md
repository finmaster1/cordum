# LLM Chat — Supply-Chain Gate

Operator-facing reference for the qwen-inference vLLM image supply chain.
Pairs with the CI workflow `.github/workflows/supply-chain-vllm.yml` and
the helper script `tools/scripts/vllm_pin_digest.sh`.

Scope: the **vLLM container image**. Model weights (HuggingFace
`Qwen/Qwen3-Coder-…` artifacts) are out of scope here — see
[Out of scope](#out-of-scope) below.

## 1. Why digest pinning

CordClaw's compose + Helm references previously read
`vllm/vllm-openai:v0.16.0`. A moving tag means:

- An upstream re-push of the same tag silently swaps the image content
  on every `docker pull`. We would have no way to know whether the
  release we shipped is the image we tested.
- A typo in the tag (`v0.16.0` vs `v0.16` vs `latest`) produces a
  different image with no test signal.
- A compromised upstream account can push a malicious image under the
  same tag.

Pinning by `sha256` digest closes all three. The digest is content-
addressable: the image bytes are exactly the bytes we tested, or
`docker pull` fails. The supply-chain CI gate verifies the literal
pattern `vllm/vllm-openai@sha256:<64-hex>` is present in
`docker-compose.yml`; if a future change drifts back to a tag, CI fails
closed before merge.

## 2. Current pin

| Field | Value |
| --- | --- |
| Image repo | `vllm/vllm-openai` |
| Version tag | `v0.16.0` |
| Manifest-list digest | `sha256:4801151759655c57606c844662e5213403c032a62d149c7ce61d615759a821ef` |
| Pinned by | task-991597a4 |
| Files | `docker-compose.yml`, `docker-compose.release.yml`, `cordum-helm/values.yaml`, `cordum-helm/templates/deployment-qwen-inference.yaml` |

The manifest-list digest covers every architecture upstream publishes
(currently `linux/amd64` and `linux/arm64`). Cordum deploys
`linux/amd64`; the arm64 manifest is included so a future arm64 deploy
inherits the same supply-chain pin without a second bump.

## 3. Upgrade procedure

When a new vLLM version becomes available:

1. **Compatibility check.** Verify the new version still supports
   the runtime config we depend on:
   - Informational-only vLLM no longer requires tool-call parser flags; do not reintroduce legacy parser overrides.
   - The deployment flags that landed in
     [`task-6a8680fc`](#) (e.g. the `--disable-log-requests` flag
     replacement / vLLM CLI changes).
   - The `--max-model-len 131072`, `--kv-cache-dtype fp8`, and
     `--enable-prefix-caching` flags from
     `docs/llmchat/vllm-config-verification.md`.
2. **Resolve and pin.** Run the helper script:
   ```bash
   bash tools/scripts/vllm_pin_digest.sh v0.16.1
   ```
   The script resolves the manifest-list digest, prints a `diff -u`
   for review, and prompts before writing. Pass `--yes` for
   non-interactive use:
   ```bash
   bash tools/scripts/vllm_pin_digest.sh v0.16.1 --yes
   ```
3. **Commit and push** on the current branch. The four files
   (`docker-compose.yml`, `docker-compose.release.yml`,
   `cordum-helm/values.yaml`,
   `cordum-helm/templates/deployment-qwen-inference.yaml`) are
   updated in lock-step by the script.
4. **CI gate runs automatically.** The supply-chain workflow
   (`.github/workflows/supply-chain-vllm.yml`) runs on the PR and
   performs:
   - Drift detection: fails if the image isn't pinned by digest.
   - Cross-file consistency: fails if release-compose, values.yaml,
     or the deployment template disagree with `docker-compose.yml`.
   - Trivy scan: `severity: CRITICAL,HIGH`, `ignore-unfixed: true`,
     SARIF upload to the Security tab.
   - Syft SBOM: SPDX JSON, 90-day artifact retention.
   - Waiver expiry check.
5. **Merge** once CI is green and a reviewer has signed off.

The script is **idempotent** — re-running with the same tag is a no-op
(the digest hasn't moved, no diff). Re-running with a NEW tag prints
the diff before writing so you can sanity-check the resolved digest
against an external source (Docker Hub, the vLLM release notes).

## 4. Vulnerability waivers

The Trivy scan blocks merge on any unwaived CRITICAL or HIGH severity
vulnerability with a fix available. To override an individual finding,
add an entry to `tools/scripts/vllm-vuln-waivers.yaml`:

```yaml
waivers:
  - cve: CVE-2025-12345
    reason: >
      vector requires unsanitized network ingress on TCP 80;
      qwen-inference exposes only loopback 127.0.0.1:8000 per
      epic rail #5. Not exploitable in our deployment.
    expires_at: "2026-07-26"   # YYYY-MM-DD UTC, default 90 days
    approved_by: yaront1111
    fixed_in: vllm v0.16.5  # optional; lets us mechanically drop the waiver
vulnerabilities:
  - id: CVE-2025-12345        # Trivy v0.70.0+ reads from here
cves:
  - CVE-2025-12345            # legacy compat for Trivy < v0.70.0
```

**Schema note (task-2cf6b514 step-1 verification).** The Trivy CLI
version bundled with `aquasecurity/trivy-action@v0.36.0` is
`v0.70.0`, which honors the top-level `vulnerabilities:` list as the
enforcement key. The legacy top-level `cves:` list is retained for
backward compat with older Trivy versions but is metadata-only at
v0.70.0+. Every active waiver MUST appear in BOTH `waivers:`
(structured metadata source for expiry, audit trail, deployment
reasoning) and `vulnerabilities:` (Trivy enforcement). The three
lists are kept alphabetically sorted and in sync.

**Approval mechanism: PR review.** The waiver file is in version
control. Adding a waiver requires opening a PR; security-team review
on that PR IS the approval. There is no out-of-band approval channel.

**Expiry.** Every waiver carries an `expires_at` date. The CI workflow
fails on any waiver with `expires_at < today (UTC)` so stale
exemptions can't rot into silent accepts. Default 90 days; renew via PR.

## 4a. Initial Waiver Review

**Date:** 2026-04-27 (task-2cf6b514). **Image:**
`vllm/vllm-openai@sha256:4801151759655c57606c844662e5213403c032a62d149c7ce61d615759a821ef`
(vLLM v0.16.0). **Total findings:** 15 unique CRITICAL+HIGH+fixable
CVEs (29 SARIF instances; 2 critical, 27 high). All 15 waived because
the exploit vector is not reachable in Cordum's deployment posture.
The gate is now ENFORCING — `continue-on-error: true` removed from
the Trivy step in the same commit as the waiver population.

**Deployment posture invariants** that ground the waivers:

- **Loopback-only vLLM bind** (`127.0.0.1:8000`, ClusterIP in Helm
  per epic rail) — no remote network attackers reach vLLM.
- **read_only filesystem** in container — no privilege-escalation
  persistence, no kernel module loading, no sudo persistence.
- **No shell entrypoint** — container has no externally-invokable
  shell; `exec` paths require `docker exec` from the host.
- **Single-node deployment** — Cordum runs single-node vLLM with no
  Ray cluster; aiohttp/cbor2/pyjwt-via-Ray surfaces are loaded but
  not bound or exercised.
- **Text-only chat** — Qwen3-Coder is a text/code model; no
  user-supplied image input → no Pillow image-decode path.
- **Pinned model digest** — model identity is a static config flag
  (`Qwen/Qwen3-Coder-30B-A3B-Instruct-FP8` via image digest). No
  user-controlled model-load path → no malicious-model-repo vector.
- **Cordum gateway upstream of vLLM** — JWT validation, delegation
  tokens and prompt-safety enforcement happen in Cordum
  before traffic reaches vLLM. PyJWT/xgrammar/policy paths inside
  vLLM are not exercised on the request hot path.

**CVE table.** All 15 waivers expire 2026-07-26 (today + 90 days,
UTC) and are approved by `yaront1111` via PR review.

| CVE | Sev | Package | Fixed in | Reachability |
| --- | --- | --- | --- | --- |
| CVE-2025-37849 | HIGH | linux-libc-dev | 5.15.0-174.184 | Kernel headers package; container does not run a kernel |
| CVE-2025-68973 | HIGH | gpgv | 2.2.27-3ubuntu2.5 | Not invoked by vLLM serving path; loopback-only ingress |
| CVE-2026-23111 | HIGH | linux-libc-dev | 5.15.0-174.184 | Kernel headers package; same as CVE-2025-37849 |
| CVE-2026-23268 | HIGH | linux-libc-dev | 5.15.0-173.183 | Kernel headers package; same as CVE-2025-37849 |
| CVE-2026-23410 | HIGH | linux-libc-dev | 5.15.0-173.183 | Kernel headers package; same as CVE-2025-37849 |
| CVE-2026-23411 | HIGH | linux-libc-dev | 5.15.0-173.183 | Kernel headers package; same as CVE-2025-37849 |
| CVE-2026-25048 | HIGH | xgrammar | 0.1.32 | User-supplied grammar definitions vector; we don't expose that path |
| CVE-2026-26209 | HIGH | cbor2 | 5.9.0 | CBOR decoding via Ray; single-node deployment, JSON-only API |
| CVE-2026-27893 | HIGH | vllm | 0.18.0 | `trust_remote_code=True` malicious-model RCE; pinned model only |
| CVE-2026-30922 | HIGH | pyasn1 | 0.6.3 | ASN.1 parsing for SSL/Kerberos; loopback-only, no Kerberos |
| CVE-2026-32597 | HIGH | PyJWT | 2.12.0 | JWT validated by Cordum gateway upstream; vLLM PyJWT for Ray only |
| CVE-2026-34516 | HIGH | aiohttp | 3.13.4 | Ray runtime_env agent dep; single-node, no Ray agent |
| CVE-2026-34520 | CRITICAL | aiohttp | 3.13.4 | Ray runtime_env agent dep; single-node, no Ray agent |
| CVE-2026-35535 | HIGH | sudo | 1.9.9-1ubuntu2.6 | Container has no shell entrypoint; read_only FS |
| CVE-2026-40192 | HIGH | pillow | 12.2.0 | Image-decode path; text-only chat, no user image input |

**Bump-pin opportunities.** Several CVEs have upstream fixes shipped
in vLLM v0.18.0 (CVE-2026-27893, transitive xgrammar/aiohttp/etc.).
Bumping the vLLM pin past v0.16.0 was deferred for two reasons:
v0.17.0 removed the deprecated-but-supported `--disable-log-requests`
flag (see [`task-6a8680fc`](#) — the no-prompt-leak rail), and a
v0.18.0+ bump will need a re-run of the static config verification
and security-review harness on the new image. Tracked as a follow-up
in `unreleased.md` and via the next time the supply-chain workflow
auto-rerun fails (i.e. when these waivers expire on 2026-07-26 or
sooner if a new CRITICAL CVE lands).

## 5. Cosign signature status

The vLLM upstream image (`vllm/vllm-openai:v0.16.0` →
`sha256:480115…`) is **unsigned by Sigstore at this digest**. Step-1
verification used:

```text
docker run --rm gcr.io/projectsigstore/cosign:v2.5.3 verify \
  vllm/vllm-openai@sha256:480115… \
  --certificate-identity-regexp 'vllm-project|vllm.ai' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
# → Error: no signatures found
```

This is a known gap. We mitigate via:

- Digest pinning (immutable bytes regardless of signing).
- Trivy + Syft scans against the pinned digest.
- The supply-chain CI gate's drift + cross-file consistency checks.

Follow-up: monitor upstream vLLM for cosign signing adoption. When
they publish signed releases, add a `cosign verify` step to the
supply-chain workflow.

## 6. Out of scope

- **Model weights** (HuggingFace `Qwen/Qwen3-Coder-30B-A3B-Instruct-FP8`
  and the Tier-2 AWQ variant). Weights are downloaded by the vLLM
  container at start-up and cached in a named volume / PVC. Their
  provenance is a separate concern; a follow-up Moe task should
  add a model-weights digest pin and revision policy.
- **Application-code findings.** This file is for image-scan waivers
  only. Cordum's existing security-review flow handles application
  CVEs.

## Cross-references

- CI workflow: [`.github/workflows/supply-chain-vllm.yml`](../../.github/workflows/supply-chain-vllm.yml).
- Pin helper: [`tools/scripts/vllm_pin_digest.sh`](../../tools/scripts/vllm_pin_digest.sh).
- Waiver file: [`tools/scripts/vllm-vuln-waivers.yaml`](../../tools/scripts/vllm-vuln-waivers.yaml).
- Senior security review: [`security-review.md`](security-review.md).
- vLLM static config verification: [`vllm-config-verification.md`](vllm-config-verification.md).
- Production-readiness ops runbook: [`ops-runbook.md`](ops-runbook.md).
