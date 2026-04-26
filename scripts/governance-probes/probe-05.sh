#!/usr/bin/env bash
# Probe 05: Safety kernel actually gates a malformed mutating call
# task-931eaea2, see docs/llmchat/governance-review.md for full procedure.
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "$SCRIPT_DIR/common-fixture.sh"
require_jq

echo "[probe-05] PLACEHOLDER — fill in per docs/llmchat/governance-review.md Probe 5"
echo "[probe-05] verdict=PENDING"
exit 0
