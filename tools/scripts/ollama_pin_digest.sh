#!/usr/bin/env bash
# ollama_pin_digest.sh — supply-chain pin for the Ollama image used by
# the `llmchat-ollama` compose profile. Mirrors the role
# `vllm_pin_digest.sh` is documented to play for the vLLM image.
#
# Resolves a moving tag (e.g. `0.5.7`) to its content-addressed digest
# and prints the canonical `ollama/ollama@sha256:...` reference. Pipe
# the output into the compose `image:` field, or run this in CI to
# detect a tag that has been re-pushed without a coordinated bump.
#
# Usage:
#   bash tools/scripts/ollama_pin_digest.sh [tag]
#
# Defaults to the value of OLLAMA_TAG, then 0.5.7 (the floor that emits
# OpenAI-format `delta.tool_calls[]` arrays — older tags embed tool
# calls as JSON in the content field and silently break the chat).

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TAG="${1:-${OLLAMA_TAG:-0.5.7}}"
IMAGE="ollama/ollama:${TAG}"

if ! command -v docker >/dev/null 2>&1; then
	echo "[ERROR] docker CLI not installed; required to resolve manifest digest" >&2
	exit 2
fi

echo "[ollama-pin] resolving digest for ${IMAGE}" >&2
docker pull --quiet "${IMAGE}" >/dev/null

DIGEST="$(docker inspect --format='{{index .RepoDigests 0}}' "${IMAGE}" 2>/dev/null || true)"
if [ -z "${DIGEST}" ] || [ "${DIGEST}" = "<no value>" ]; then
	echo "[ERROR] no RepoDigests for ${IMAGE}; pull may have hit a local-only build" >&2
	exit 3
fi

# RepoDigests entry is `ollama/ollama@sha256:...`; that's the canonical
# form compose `image:` accepts. Print only that line on stdout so the
# script is pipe-friendly: e.g. `image: $(bash …/ollama_pin_digest.sh)`.
echo "${DIGEST}"

# Suggested update:
echo "[ollama-pin] update docker-compose.yml ollama service:" >&2
echo "  image: \"${DIGEST}\"" >&2
echo "  # tag-resolved: ${IMAGE} on $(date -u +%Y-%m-%dT%H:%M:%SZ)" >&2
