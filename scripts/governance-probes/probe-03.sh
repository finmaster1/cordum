#!/usr/bin/env bash
# Probe 03: Every MCP call produces an mcp.tool_invocation SIEM event
# task-931eaea2, see docs/llmchat/governance-review.md for full procedure.
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common-fixture.sh
source "$SCRIPT_DIR/common-fixture.sh"
require_jq

echo "[probe-03] PLACEHOLDER — fill in per docs/llmchat/governance-review.md Probe 3"
echo "[probe-03] verdict=PENDING"
exit 0
