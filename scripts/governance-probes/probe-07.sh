#!/usr/bin/env bash
# Probe 07: Empty AgentIdentity tool scope denies chat mutations by construction
# task-01aaa6bd scope-reduced review; see docs/llmchat/governance-review.md.
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "$SCRIPT_DIR/common-fixture.sh"
require_jq

echo "[probe-07] PLACEHOLDER — fill in per docs/llmchat/governance-review.md Probe 7"
echo "[probe-07] verdict=PENDING"
exit 0
