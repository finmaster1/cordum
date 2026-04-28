#!/usr/bin/env bash
# Probe 01: CAP SDK chat-assistant identity bootstrap is no-tool scoped
# task-01aaa6bd scope-reduced review; see docs/llmchat/governance-review.md.
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "$SCRIPT_DIR/common-fixture.sh"
require_jq

echo "[probe-01] PLACEHOLDER — fill in per docs/llmchat/governance-review.md Probe 1"
echo "[probe-01] verdict=PENDING"
exit 0
