# `cordumctl edge doctor`

`cordumctl edge doctor` is the operator preflight + diagnostics tool for Cordum
Edge. It runs a fixed pipeline of read-only checks against the local agentd, the
Gateway control plane, the Safety Kernel, and the Claude Code generated
settings, and reports the result in human-readable or JSON form.

```text
cordumctl edge doctor [--json] [--timeout=<sec>] [--policy-mode=<mode>] \
    [--shadow-cluster=<kubeconfig-path>] \
    [--shadow-ci=<provider>:<token-or-config-path>] \
    [<standard doctor flags>]
```

Standard flags drive the default doctor checks pipeline (gateway / auth / Safety
Kernel / agentd / settings / dashboard / policy-mode / managed-settings).

## Shadow preview flags (EDGE-143.8)

The `--shadow-cluster` and `--shadow-ci` flags let an operator **preview** what
the EDGE-143 shadow-agent detectors would surface in their environment **before
opting in via a managed-settings deployment**. The previews are strictly
read-only:

- They build the relevant detector with an in-memory `dryRunStore` so
  `store.CreateFinding` records to a local slice instead of Redis.
- The Kubernetes client adapter exposes only `list` / `get` / `watch` verbs
  (`core/edge/shadow/k8s/detector.go`); no `create` / `update` / `patch` /
  `delete` verb is reachable from this command.
- The CI HTTP transport (`edgeDoctorCIHTTPTransport`) is checked by
  `TestEdgeDoctor_ShadowCI_ReadOnly` so any future POST / PUT / PATCH / DELETE
  issued during a preview fails CI.

Findings are printed to stdout, never persisted to the EDGE-141 store, never
mutate the cluster or CI provider.

### `--shadow-cluster=<kubeconfig-path>`

Runs the EDGE-143.1 Kubernetes shadow detector in dry-run mode against the
cluster pointed to by the operator-supplied kubeconfig.

```bash
cordumctl edge doctor --shadow-cluster ~/.kube/config
cordumctl edge doctor --shadow-cluster ~/.kube/config --json
```

**What runs**: one full `Detector.Scan(ctx)` against the live cluster. The
detector lists pods, namespaces, services, and network policies, runs the nine
§7.1 extractors, applies extraction-time redaction, and emits redacted
`ShadowAgentFinding` records into the in-memory dry-run store.

**Kubeconfig handling**: parsed via
`k8s.io/client-go/tools/clientcmd.NewNonInteractiveDeferredLoadingClientConfig`
so the loader never prompts. The supplied path is read once at startup and
never written to. We recommend running with an RBAC role limited to `get` /
`list` / `watch` on `pods`, `namespaces`, `services`, `serviceaccounts`, and
`networkpolicies`; the detector code itself cannot reach a mutating verb, but
constraining RBAC is best practice.

### `--shadow-ci=<provider>:<token-or-config-path>`

Runs the EDGE-143.2 / EDGE-143.3 CI shadow detector for the named provider in
dry-run mode.

Supported providers (closed enum):

| Provider | Status |
|----------|--------|
| `github` | EDGE-143.2 — detector module pending |
| `gitlab` | EDGE-143.3 — detector module pending |
| `jenkins` | EDGE-143.3 — detector module pending |
| `buildkite` | EDGE-143.3 — detector module pending |
| `circleci` | EDGE-143.3 — detector module pending |

```bash
cordumctl edge doctor --shadow-ci github:$GITHUB_TOKEN
cordumctl edge doctor --shadow-ci gitlab:$GITLAB_TOKEN --json
```

**Partial support today**: as of EDGE-143.8 landing, neither EDGE-143.2 nor
EDGE-143.3 has shipped a detector module. Every supported provider therefore
exits with a clear actionable message:

```text
cordumctl edge doctor: provider github not supported in this build;
EDGE-143.2/.3 detector(s) must DONE first
```

Once those tasks land, the providers gated on them will exercise the same
dry-run pipeline as `--shadow-cluster`.

**Unknown providers** (anything outside the closed enum) print a clear
"not recognized" error plus the full supported list, and exit non-zero:

```text
cordumctl edge doctor: provider azuredevops not recognized;
supported: github/gitlab/jenkins/buildkite/circleci
```

**Malformed specs** (missing colon) print format guidance and exit non-zero:

```text
cordumctl edge doctor: --shadow-ci value must be in
<provider>:<token-or-config-path> format; got "github-no-colon"
```

**Token handling**: the supplied token is held in process memory for the
duration of the scan only. Tokens are never logged, never persisted, and never
echoed in error messages. Operators should source tokens from environment
variables (`$GITHUB_TOKEN`) or a secret manager rather than literal values on
the command line.

### `--json` envelope

When `--json` is set together with either preview flag, output is a single
JSON envelope:

```json
{
  "mode": "shadow_cluster_preview",
  "dry_run": true,
  "provider": "",
  "findings": [
    {
      "finding_id": "preview_1",
      "tenant_id": "tenant-test",
      "risk": "medium",
      "status": "detected",
      "source_type": "kubernetes",
      "namespace": "default",
      "workload_name": "evil-claude",
      "signal_set": ["untrusted_image"],
      "...": "..."
    }
  ]
}
```

`findings` is always present (empty slice when zero findings) so consumers do
not need a nil guard. `dry_run` is always `true` for these preview modes.

### Operator workflow

1. Run `--shadow-cluster <path>` (and / or `--shadow-ci <provider>:<token>`) to
   preview what the detectors would surface in your environment.
2. Review the findings; if the volume / shape is acceptable, opt in by deploying
   the EDGE-143 managed-settings rollout.
3. Re-run `cordumctl edge doctor` (without `--shadow-*`) to verify the standard
   doctor checks still pass after rollout.

See also `docs/edge/kubernetes-ci-shadow-detector-design.md` for the full
design, signal catalog, and §10.1 finding-shape reference.
