#!/usr/bin/env bash
# Probe 02: Dashboard chat affordance is informational and health/entitlement gated
# task-01aaa6bd scope-reduced review; see docs/llmchat/governance-review.md.
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "$SCRIPT_DIR/common-fixture.sh"
require_jq

echo "[probe-02] PLACEHOLDER — fill in per docs/llmchat/governance-review.md Probe 2"
echo "[probe-02] verdict=PENDING"
exit 0
