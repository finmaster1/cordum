# Shadow Agent Remediation (EDGE-142)

Cordum Edge's shadow-agent surface produces redacted findings via the
EDGE-140 scanner and the EDGE-141 finding store. EDGE-142 adds a
deterministic **remediation generator** that maps a finding to
advisory guidance: classification, recommended action, audience-aware
steps, and machine-readable command/API request placeholders.

**Advisory only.** The generator does not execute. It does not enqueue
Cordum Jobs. It does not mutate finding state. It does not call the
Safety Kernel. Operators copy + parameterise the suggested commands
and run them under their own MDM or local-admin authority.

See also: [shadow-agent-findings.md](shadow-agent-findings.md) for the
EDGE-141 finding lifecycle, [shadow-scanner.md](shadow-scanner.md) for
the local opt-in scanner, and
[managed-settings-deploy.md](managed-settings-deploy.md) for the MDM
deployment workflow that the enterprise audience steps reference.

## Classification

The classifier folds two finding shapes onto a single feature
projection — both EDGE-141 lifecycle records
(`/api/v1/edge/shadow-agents/{finding_id}`) and EDGE-140 scanner
observations (`cordumctl shadow scan` JSONL output) are accepted.

Priority order (most-specific first):

| Signal / hint                                                         | Action kind                       |
|-----------------------------------------------------------------------|-----------------------------------|
| `signal_set` contains `direct_provider_url`                           | `route_through_llm_proxy`         |
| `signal_set` contains `k8s_heartbeat_missing` or evidence=`heartbeat` | `run_edge_doctor`                 |
| `signal_set` contains `unmanaged_mcp_server`                          | `attach_mcp_gateway`              |
| `signal_set` contains `unmanaged_claude_settings`                     | `use_cordumctl_edge_claude`       |
| `signal_set` contains `unmanaged_process` or evidence=`process_name`  | `investigate_process`             |
| evidence=`environment_var`                                            | `route_through_llm_proxy`         |
| evidence=`config_file` + path matches `mcp.json` / `/mcp/`            | `attach_mcp_gateway`              |
| evidence=`config_file` + path matches `~/.claude/`, `~/.cursor/`, `~/.codex/` | `use_cordumctl_edge_claude` |
| no match                                                              | `manual_review`                   |

When `audience=enterprise` is requested AND the natural choice would
have been `use_cordumctl_edge_claude` (a dev-wrapper recommendation),
the resolver swaps in `deploy_managed_settings` so the plan surfaces
the MDM path rather than the per-developer wrapper.

## Audiences

| Audience      | Wording                                                                          |
|---------------|----------------------------------------------------------------------------------|
| `dev`         | Local-wrapper steps (`cordumctl edge claude`, `cordumctl edge doctor`).          |
| `enterprise`  | MDM-driven steps (`cordumctl edge managed-settings export/verify`).              |
| `both`        | Layered: dev steps first, enterprise steps second (default).                     |

Severity mirrors the source finding's risk: `low` → `low`, `medium` →
`medium`, `high|critical` → `high`. Severity is the plan-level scale;
consumers needing `critical` should surface it from the source finding
directly.

## Placeholders

Every command in every step uses literal placeholders. The generator
never substitutes live values — operators must replace placeholders
locally:

| Placeholder                              | Source                                            |
|------------------------------------------|---------------------------------------------------|
| `<gateway-url>`                          | Cordum Gateway base URL for the tenant.           |
| `<tenant-id>`                            | Cordum tenant id (matches `X-Tenant-ID`).         |
| `<principal-id>`                         | Operator principal initiating remediation.        |
| `<output-dir>`                           | Path for `managed-settings.json` + `managed-mcp.json`. |
| `<llm-proxy-url>`                        | Cordum LLM proxy base URL.                        |
| `<api-key-helper-command>`               | Local credential-helper script path.              |
| `<unmanaged-config-path>`                | Absolute path to the unmanaged config file.       |
| `<path-to-managed-settings.json>`        | Verified path used by `managed-settings verify`.  |
| `<path-to-managed-mcp.json>`             | Verified MCP payload path.                        |
| `<finding-id>`                           | Finding identifier for API-shaped steps.          |

The generator routes every finding-derived string (product name,
signal labels) through `shadow.stripSecretMarkers` and a
printable-ASCII filter so a malicious uploader cannot inject `sk-…`
tokens, bearer headers, or terminal escapes into the operator-facing
output.

## Backup, preview, destructive flags

Steps that disable or remove unmanaged configuration are always
emitted with all three safety flags set:

```
preview_only=true   requires_backup=true   destructive=true
```

The generator emits a backup step ahead of any disable step so the
ordered list reads `backup → preview → rename`. Dashboards and the
CLI render the flags so operators see the gate at every render
surface.

## Gateway API

```
POST /api/v1/edge/shadow-agents/{finding_id}/remediation
X-Tenant-ID: <tenant-id>
Content-Type: application/json

{
  "audience": "dev|enterprise|both",
  "omit_commands": false
}
```

Response (200 OK):

```json
{
  "finding_id": "edge_shadow_…",
  "tenant_id": "tenant-alpha",
  "remediation": {
    "audience": "dev",
    "severity": "medium",
    "action_kind": "use_cordumctl_edge_claude",
    "summary": "Bring claude-code configuration under Cordum Edge management.",
    "risk_explanation": "…",
    "recommended_action": "Launch Claude Code via `cordumctl edge claude` …",
    "safety_notes": ["All steps are advisory …"],
    "steps": [
      {
        "id": "use_cordumctl_edge_claude.dev.launch",
        "title": "Launch Claude Code via cordumctl edge claude",
        "kind": "use_cordumctl_edge_claude",
        "command": "cordumctl edge claude --gateway <gateway-url> --tenant <tenant-id> --principal <principal-id>",
        "docs_url": "docs/edge/cordumctl-edge-claude.md",
        "conditions": ["requires cordum-agentd in PATH on the host"]
      }
    ],
    "generator_version": "1.0.0",
    "generated_at": "2026-05-17T16:00:00Z",
    "advisory_only": true
  }
}
```

Error mapping (uses the standard Edge `{code, message, request_id, details}` envelope):

| Status | Code              | When                                              |
|--------|-------------------|---------------------------------------------------|
| 400    | invalid_request   | Unknown audience, malformed body, generator validation. |
| 400    | invalid_json      | Body decode failure.                              |
| 400    | missing_path_param| `{finding_id}` absent.                            |
| 401    | unauthorized      | Auth context missing.                             |
| 403    | access_denied     | Caller lacks `audit.read` or `admin` role.        |
| 404    | not_found         | Finding missing or cross-tenant.                  |
| 503    | store_unavailable | Shadow finding store offline.                     |
| 500    | internal_error    | Catch-all; details suppressed.                    |

## CLI

```
cordumctl shadow remediate --finding-file <path|-> [--audience dev|enterprise|both] [--json] [--omit-commands]
```

The CLI is offline by default — it reads a single finding JSON from a
file or stdin (`-`) and emits the plan as either deterministic
human-readable text (default) or JSON (`--json`). No Cordum Gateway
calls, no API keys required.

Exit codes: `0` success, `2` parse/validation/unsupported flag.

Both finding shapes are accepted:

* EDGE-141 lifecycle records emitted by
  `GET /api/v1/edge/shadow-agents/{finding_id}`.
* EDGE-140 scanner observations emitted by
  `cordumctl shadow scan` (one JSON object per line). Pipe a single
  JSONL record through the CLI to preview guidance without posting
  the finding to the Gateway:

  ```sh
  cordumctl shadow scan --enable-shadow-scan \
    | head -n1 \
    | cordumctl shadow remediate --finding-file - --audience dev --json
  ```

## Advisory-only limitations

* The generator never executes destructive operations. Backup, disable,
  and rename steps are emitted with `preview_only=true` regardless of
  audience.
* `advisory_only=true` is hard-coded on every emitted plan. A future
  enforcement mode (out of scope for EDGE-142 per the task rail) may
  flip this without changing the type signature.
* The CLI does not contact the Gateway. A future enhancement may add a
  `--finding-id` flag that uses the existing Cordum API client
  helpers; until then operators must pull the finding JSON via
  `cordumctl edge` tools or the API client and pipe it into
  `cordumctl shadow remediate --finding-file -`.
* The plan's `command` and `api_request.body` fields always carry
  placeholders. Operators are responsible for substituting live
  values — do not copy-paste secrets onto the command line; use the
  `<api-key-helper-command>` placeholder as documented in
  [managed-settings-deploy.md](managed-settings-deploy.md).
