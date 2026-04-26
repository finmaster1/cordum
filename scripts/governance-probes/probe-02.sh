#!/usr/bin/env bash
# Probe 02: Chat-driven calls in dashboard Jobs + Audit pages (BLOCKED on dashboard agent_id column - F6)
# task-931eaea2, see docs/llmchat/governance-review.md for full procedure.
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "$SCRIPT_DIR/common-fixture.sh"
require_jq

echo "[probe-02] PLACEHOLDER — fill in per docs/llmchat/governance-review.md Probe 2"
echo "[probe-02] verdict=PENDING"
exit 0
