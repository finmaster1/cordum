# Decision Thresholds — 3-axis routing model

> `core/policy/actiongates/decision_thresholds.go` —
> `ClassifyByThresholds(in DecisionThresholdInput) DecisionThresholdResult`

Cordum's safety pipeline emits exactly four `pb.DecisionType` values per
finding: `DENY`, `REQUIRE_HUMAN`, `ALLOW`, `ALLOW_WITH_CONSTRAINTS`. The
*threshold helper* converts a `{severity, confidence, action_binding,
educational_context}` tuple from any producer (action gate / scanner /
governance evaluator) into one of those four. No new `DecisionType`
values are introduced — the helper only routes between the existing
ones — so audit consumers, dashboard surfaces, and downstream
governance checks need no wire-format changes to adopt it.

## The three axes

| Axis | Type | Source | Notes |
| --- | --- | --- | --- |
| `Severity` | `SeverityLevel` (`Low` / `Medium` / `High` / `Critical`) | Producer-reported | Ordered enum; `>= SeverityHigh` is the high-severity floor. `SeverityUnspecified` (zero) is treated as `SeverityMedium` to fail-closed. |
| `Confidence` | `float32` 0.0–1.0 | Producer-reported | Scanner regex confidences from `core/controlplane/safetykernel/scanners.go` plug in directly (range 0.8–0.99). High-confidence floor is `confidenceHighFloor = 0.8`. |
| `ActionBinding` | `ActionBinding` (`PromptOnly` / `ActionBound`) | Producer-classified | `ActionBound` means a real `ActionDescriptor.Kind` with a populated `TargetPath` / `TargetURL` / etc. `PromptOnly` means input/output content scanning that fired on a text mention. |
| `EducationalContext` | `bool` | **Session metadata only** | Authentic auth context / pack manifest / tenant policy. **Never** from `input_text`, `claim_text`, or any client-supplied prose. |

## Routing table

```
action-bound + severity >= High + confidence >= 0.8           => DENY
action-bound + severity == Low                                 => ALLOW (+ AWC if constraints present)
action-bound + otherwise                                       => REQUIRE_HUMAN

prompt-only  + educational_context                              => ALLOW (+ AWC if constraints present)
                                                                  reason prefixed "educational context: ..."
prompt-only  + severity == Low                                  => ALLOW (+ AWC if constraints present)
prompt-only  + otherwise                                        => REQUIRE_HUMAN

unspecified binding                                             => REQUIRE_HUMAN (fail-closed)
```

`SubReason` encodes the routing path verbatim so audit consumers can attribute
without re-deriving from inputs:

| SubReason | Path |
| --- | --- |
| `action_bound:high_severity:high_confidence` | DENY (canonical malicious action) |
| `action_bound:low_severity` | ALLOW (legitimate workspace operation) |
| `action_bound:low_severity:with_constraints` | AWC (sandbox / tier ceiling / read-only mode) |
| `action_bound:ambiguous` | REQUIRE_HUMAN (medium severity OR low confidence) |
| `prompt_only:educational` | ALLOW (security training / compliance doc) |
| `prompt_only:educational:with_constraints` | AWC (redaction / audit-tag on educational) |
| `prompt_only:low_severity` | ALLOW (benign keyword surface) |
| `prompt_only:low_severity:with_constraints` | AWC (audit-tag required) |
| `prompt_only:ambiguous` | REQUIRE_HUMAN (medium/high severity, no educational tag) |
| `unspecified_binding` | REQUIRE_HUMAN (fail-closed) |

## Per-producer mapping

These are the producers from the Phase 1 inventory (`core/policy/actiongates/`,
`core/controlplane/safetykernel/`, `core/governance/evaluator/`) and how their
existing decisions map onto the threshold-helper input tuple.

| Producer | Rule ID shape | Severity source | Confidence source | Action binding | Common educational context |
| --- | --- | --- | --- | --- | --- |
| `core/policy/actiongates/file_gate.go` | `actiongate.file.<sub_reason>` (e.g. `sensitive_path:etc_shadow`) | Built-in: traversal / sensitive path → `Critical`; credential basename → `High` | Static `0.95+` (deterministic match) | `ActionBound` (real `TargetPath`) | Rare — file actions are real backend operations |
| `core/policy/actiongates/url_gate.go` | `actiongate.url.<sub_reason>` (e.g. `metadata_aws`, `exfil_host:ngrok_io`) | Metadata services → `Critical`; exfil hosts → `High`; private-IP rebinding → `High` | Static `0.95+` | `ActionBound` (real `TargetURL`) | Rare — outbound URL is always action-bound |
| `core/policy/actiongates/mcp_gate.go` | `actiongate.mcp.<sub_reason>` | Tool risk from registry | Per-tool | `ActionBound` | Set when pack manifest declares `educational: true` |
| `core/policy/actiongates/mutation_gate.go` | `actiongate.mutation.<sub_reason>` | Destructive verbs → `High`; standard mutations → `Medium` | Static (verb category) | `ActionBound` | N/A |
| `core/policy/actiongates/tenant_gate.go` | `actiongate.tenant.<sub_reason>` | Cross-tenant → `High`; wildcard → `Medium` | Static | `ActionBound` | N/A |
| `core/policy/actiongates/provenance_gate.go` | `actiongate.provenance.<sub_reason>` | Approval mismatch → `High` | Static `0.95+` | `ActionBound` | N/A |
| `core/controlplane/safetykernel/scanners.go` | `scanner.<scanner_name>.<pattern_label>` (e.g. `scanner.secret_leak.aws_access_key_id`) | Regex pattern severity (`high` / `critical`) | Pattern-confidence (0.8–0.99) | **`PromptOnly`** for input/output content matches; gateway populates `ActionBound` only when the scanner fired on a real action payload | Set by session metadata; this is the primary FP-reduction lever |
| `core/governance/evaluator/rules.go` | `governance.<rule_id>` (e.g. `ma_issuer_root_not_allowed`) | Per-rule | Per-rule | Mixed — multi-agent rules are usually `ActionBound`; prompt-text rules are `PromptOnly` | Set by governance policy |

## REQUIRE_HUMAN routing rationale

The `REQUIRE_HUMAN` path is not a courtesy escalation — it is the
designated route for *ambiguous-but-recoverable* findings, defined as:

- **Medium severity**, regardless of confidence — by definition, the
  signal is not strong enough to deny but not weak enough to allow.
- **Low confidence** (sub-0.8) on any severity — the producer's match
  uncertainty itself is the ambiguity.
- **High/Critical severity + prompt-only + no educational context** — the
  content is alarming but no real action is bound; a human can read
  the surrounding prose and decide whether to block, allow, or quote
  it back for paraphrase.

Routing here instead of `DENY` reduces false-positive over-refusal
(DoD #6) without masking real action-layer misses (an action-bound
high-severity high-confidence finding still routes to `DENY`
regardless of `educational_context` — see "Anti-pattern: educational
tag spoof" below).

## Anti-pattern: educational tag spoof

> **Do not derive `EducationalContext` from `input_text`, `claim_text`,
> or any client-supplied prose.**

`EducationalContext` is typed as a `bool` (not a string) precisely so a
caller cannot accidentally pass user-controlled text. The trust source
MUST be one of:

1. The authenticated session's pack manifest (`packs/<name>/manifest.yaml`
   declares `educational: true` for security-training packs).
2. The tenant policy (`SafetyPolicy.Tenants[<tid>].EducationalContext`
   when a tenant is provisioned as a defensive-research workspace).
3. The governance evaluator's static rule classification for known
   compliance-doc topics.

A widening to a string-typed field would re-introduce the input-text
spoof attack surface immediately — `TestClassifyByThresholds_EducationalContextIsBooleanNotString`
exists to fail-loud against that refactor.

Additionally, **`ActionBinding == ActionBound` ALWAYS wins over
`EducationalContext`** at the routing table's first hop. A request that
declares `educational_context: true` *and* targets the AWS metadata
service still emits `DENY`. The "confidence-as-weakening" attack — a
caller lowering confidence to slip past the action-layer DENY floor —
is also defended: `confidenceHighFloor = 0.8` is a one-way knob;
sub-0.8 confidence on an action-bound critical finding routes to
`REQUIRE_HUMAN`, never to `ALLOW`.

## ALLOW_WITH_CONSTRAINTS carrier

When a producer wants to attach structured constraints to an ALLOW
(sandbox mode, tier ceiling, read-only restriction, redaction span),
it populates `DecisionThresholdInput.ProducerConstraints map[string]any`.
The helper:

- Returns `DECISION_TYPE_ALLOW` with `Constraints: nil` when the map
  is nil or empty.
- Returns `DECISION_TYPE_ALLOW_WITH_CONSTRAINTS` with `Constraints`
  propagated by reference (no copy / serialize) when the map has at
  least one entry. `SubReason` gets a `:with_constraints` suffix.

The carrier shape mirrors `ActionGateDecision.Constraints` and
`core/edge/agentd EvaluateResponse.Constraints` — a single canonical
constraint map across the hook + MCP + governance surfaces.

## Cross-cutting surfaces

### Structured logs / audit events
**No wire-format change.** Decisions still carry `rule_id` and `reason`
exactly as before; the threshold helper preserves the producer's
`ProducerRuleID` verbatim in `DecisionThresholdResult.RuleID`. The new
`SubReason` field is *additive* — audit consumers that don't read it
see no regression.

### Dashboard surfaces
**No change required.** `dashboard/src/api/transform.ts` already maps
both `REQUIRE_HUMAN` and `DECISION_TYPE_REQUIRE_HUMAN` to the existing
`require_approval` surface (transform.ts:570–596). The governance
timeline (`dashboard/src/components/governance/GovernanceTimeline.tsx`)
accepts filters as a caller prop and has no hard-coded predicate that
drops `REQUIRE_HUMAN` rows. New decisions land on the existing
approvals queue + governance timeline without code changes.

### Holdout regression
The threshold helper itself is unit-tested (31 GREEN tests across
Phase 2 + Phase 4 coverage suites). End-to-end holdout regression
against the AgentShield benchmark depends on per-producer wiring —
see follow-up tasks for measurement once producers route through
`ClassifyByThresholds`.

## See also

- `core/policy/actiongates/decision_thresholds.go` — implementation.
- `core/policy/actiongates/decision_thresholds_test.go` — RED-then-GREEN 5-FP table + invariant guards.
- `core/policy/actiongates/decision_thresholds_coverage_test.go` — 16-row 4-path coverage suite.
- `docs/api-reference.md` § Governance — REQUIRE_HUMAN surface contract on the API tier.
