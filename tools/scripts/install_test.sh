#!/usr/bin/env bash
# install_test.sh — synthetic-tampered + unsigned scenarios for install.sh.
#
# EDGE-151 step-2 RED: the cases below MUST fail with a typed
# BINARY-VERIFY-FAIL message once step-5 lands. Today install.sh has no
# pre-activation verification gate, so this driver exits non-zero — that is
# the intentional RED state and is wired up by CI in step-4
# (binaries-pr-validation.yml).
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." >/dev/null 2>&1 && pwd)"
INSTALL_SH="$SCRIPT_DIR/install.sh"
TEST_KEYS_DIR="$REPO_ROOT/tools/test-keys"

if ! command -v gpg >/dev/null 2>&1; then
  echo "install_test.sh: gpg required for synthetic-signing fixtures" >&2
  exit 2
fi
if [ ! -f "$TEST_KEYS_DIR/TEST-ONLY-release.priv.asc" ]; then
  echo "install_test.sh: missing $TEST_KEYS_DIR/TEST-ONLY-release.priv.asc — run tools/test-keys/gen.sh" >&2
  exit 2
fi

WORK="$(mktemp -d -t cordum-install-test-XXXXXX)"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

GPG_HOME="$WORK/gpg"
mkdir -p "$GPG_HOME"
chmod 700 "$GPG_HOME" 2>/dev/null || true
gpg --homedir "$GPG_HOME" --batch --quiet \
  --import "$TEST_KEYS_DIR/TEST-ONLY-release.priv.asc"

# Synthetic release dir with one fake binary + signed SHA256SUMS manifest.
build_release_dir() {
  local dest="$1"
  mkdir -p "$dest"
  printf '#!/bin/sh\nexit 0\n' >"$dest/cordum-hook"
  chmod +x "$dest/cordum-hook"
  # Use --text so MSYS/Windows GnuPG produces the same "<hash>  <path>"
  # two-space form that GNU coreutils on Linux emits by default.
  ( cd "$dest" && sha256sum --text cordum-hook ) >"$dest/SHA256SUMS"
  gpg --homedir "$GPG_HOME" --batch --yes --quiet --detach-sign --armor \
    --output "$dest/SHA256SUMS.asc" "$dest/SHA256SUMS"
}

expect_verify_fail() {
  local label="$1" needle="$2" release_dir="$3"
  local out rc
  set +e
  out=$("$INSTALL_SH" --dev-allow-unsigned --release-dir "$release_dir" 2>&1)
  rc=$?
  set -e
  if [ "$rc" -eq 0 ]; then
    echo "FAIL [$label]: install.sh exit=0; expected nonzero" >&2
    printf '%s\n' "$out" >&2
    return 1
  fi
  if ! printf '%s\n' "$out" | grep -q "$needle"; then
    echo "FAIL [$label]: stderr missing '$needle'" >&2
    printf '%s\n' "$out" >&2
    return 1
  fi
  echo "PASS [$label]: install.sh refused with '$needle'"
}

failures=0

# Scenario 1 — tampered binary: hash recomputed inside install.sh must NOT
# match the manifest entry, so install must abort.
build_release_dir "$WORK/release-tampered"
echo 'tamper' >>"$WORK/release-tampered/cordum-hook"
if ! expect_verify_fail "tampered-binary" \
    "BINARY-VERIFY-FAIL: hash mismatch cordum-hook" \
    "$WORK/release-tampered"; then
  failures=$((failures+1))
fi

# Scenario 2 — unsigned manifest: SHA256SUMS.asc removed; install must abort
# even though SHA256SUMS itself is intact.
build_release_dir "$WORK/release-unsigned"
rm -f "$WORK/release-unsigned/SHA256SUMS.asc"
if ! expect_verify_fail "unsigned-manifest" \
    "BINARY-VERIFY-FAIL: unsigned manifest" \
    "$WORK/release-unsigned"; then
  failures=$((failures+1))
fi

if [ "$failures" -ne 0 ]; then
  echo "install_test.sh: $failures scenario(s) failed" >&2
  exit 1
fi
echo "install_test.sh: all synthetic verification scenarios passed"
