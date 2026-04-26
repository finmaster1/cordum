#!/usr/bin/env bash
# Probe 08: Data classification scope deny (PII)
# task-931eaea2, see docs/llmchat/governance-review.md for full procedure.
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "$SCRIPT_DIR/common-fixture.sh"
require_jq

echo "[probe-08] PLACEHOLDER — fill in per docs/llmchat/governance-review.md Probe 8"
echo "[probe-08] verdict=PENDING"
exit 0
