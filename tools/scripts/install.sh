#!/usr/bin/env bash
set -euo pipefail

# ============================================================================
# EDGE-151 — binary integrity pre-activation gate
# ----------------------------------------------------------------------------
# Production release pubkey fingerprint pinned here at script-edit-time.
# Empty means production signing has not yet been provisioned, in which case
# only `--dev-allow-unsigned` is accepted (with a TEST-ONLY-* key under
# tools/test-keys/). When the production GPG key is provisioned the value
# below MUST be filled in AND the matching public key committed at
# tools/keys/cordum-release.pub.asc. See docs/security/binary-signing.md
# for the threat model and rotation procedure.
# ============================================================================
CORDUM_PROD_FINGERPRINT_PIN="${CORDUM_RELEASE_FINGERPRINT:-}"

# Audit-log context — populated as the verify flow progresses; consumed by
# emit_audit so each event line carries the signing scheme + signer fingerprint
# alongside the per-binary fields. Empty values are emitted as "" — schema
# remains stable for downstream SIEM mappings per docs/security/binary-signing.md §8.
CORDUM_AUDIT_SIG_SCHEME=""
CORDUM_AUDIT_FINGERPRINT=""

emit_audit() {
  local event="$1" reason="${2:-}" hash="${3:-}" rel="${4:-}" exit_code="${5:-0}"
  # No secrets, no full paths — `rel` is already a manifest-relative basename
  # because the manifest itself never carries absolute paths (rejected up
  # front by reject_path). Reason strings are controlled BINARY-VERIFY-FAIL
  # text from this script, so no JSON-escaping pass is needed.
  printf '{"event":"%s","hash":"%s","path":"%s","sig_scheme":"%s","fingerprint":"%s","reason":"%s","exit_code":%d}\n' \
    "$event" "$hash" "$rel" "$CORDUM_AUDIT_SIG_SCHEME" "$CORDUM_AUDIT_FINGERPRINT" "$reason" "$exit_code" >&2
}

verify_fail() {
  local reason="$*"
  emit_audit "binary-verify-fail" "$reason" "" "" 1
  echo "BINARY-VERIFY-FAIL: $reason" >&2
  exit 1
}

normalise_fpr() {
  printf '%s' "$1" | tr -d ' \t\r\n' | tr '[:lower:]' '[:upper:]'
}

# reject_path returns 0 when the supplied manifest-relative path is unsafe
# (absolute, parent-traversing, or Windows-drive-rooted), 1 otherwise.
reject_path() {
  local p="$1"
  case "$p" in
    /*|\\*) return 0 ;;
    [A-Za-z]:*) return 0 ;;
  esac
  case "/$p/" in
    */../*) return 0 ;;
  esac
  return 1
}

run_binary_verify() {
  local rdir="$1" dev="$2" install_to="$3"
  [ -d "$rdir" ] || verify_fail "release-dir not found: $rdir"

  local manifest="$rdir/SHA256SUMS"
  local sig="$rdir/SHA256SUMS.asc"
  [ -f "$manifest" ] || verify_fail "manifest not found"
  [ -f "$sig" ]      || verify_fail "unsigned manifest"

  command -v gpg       >/dev/null 2>&1 || verify_fail "gpg required for signature verification"
  command -v sha256sum >/dev/null 2>&1 || verify_fail "sha256sum required for binary verification"

  local script_dir repo_root
  script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" >/dev/null 2>&1 && pwd)"
  repo_root="$(cd -- "$script_dir/../.." >/dev/null 2>&1 && pwd)"

  local pubkey
  if [ "$dev" = "1" ]; then
    pubkey="$repo_root/tools/test-keys/TEST-ONLY-release.pub.asc"
    [ -f "$pubkey" ] || verify_fail "dev mode but TEST-ONLY pubkey missing at $pubkey"
    CORDUM_AUDIT_SIG_SCHEME="dev"
  else
    pubkey="$repo_root/tools/keys/cordum-release.pub.asc"
    [ -f "$pubkey" ] || verify_fail "production release pubkey not provisioned at tools/keys/cordum-release.pub.asc; pass --dev-allow-unsigned for TEST-ONLY mode"
    [ -n "$CORDUM_PROD_FINGERPRINT_PIN" ] || verify_fail "production release fingerprint not provisioned (set CORDUM_RELEASE_FINGERPRINT or pin in install.sh header)"
    CORDUM_AUDIT_SIG_SCHEME="gpg"
  fi

  local tmp_home
  tmp_home="$(mktemp -d -t cordum-install-gpg-XXXXXX)"
  chmod 700 "$tmp_home" 2>/dev/null || true
  # shellcheck disable=SC2064
  trap "rm -rf '$tmp_home'" EXIT

  gpg --homedir "$tmp_home" --batch --quiet --import "$pubkey" 2>/dev/null \
    || verify_fail "gpg --import failed for $pubkey"

  local imported_fpr
  imported_fpr=$(gpg --homedir "$tmp_home" --with-colons --list-keys 2>/dev/null \
    | awk -F: '$1=="fpr" {print $10; exit}')
  [ -n "$imported_fpr" ] || verify_fail "failed to extract fingerprint from pubkey"
  CORDUM_AUDIT_FINGERPRINT="$imported_fpr"

  if [ "$dev" = "1" ]; then
    # Refuse to honour --dev-allow-unsigned unless the imported UID carries
    # the TEST-ONLY marker; otherwise an attacker could ship a production
    # key file under tools/test-keys/ and ride the dev path.
    gpg --homedir "$tmp_home" --with-colons --list-keys 2>/dev/null \
      | awk -F: '$1=="uid" {print $10}' \
      | grep -q 'TEST-ONLY' \
      || verify_fail "dev mode but imported pubkey UID lacks TEST-ONLY marker"
    # Belt-and-braces: reject if the dev mode would ever accept the
    # production-pinned fingerprint (no mistakenly cross-signed manifests).
    if [ -n "$CORDUM_PROD_FINGERPRINT_PIN" ]; then
      local got_n want_n
      got_n=$(normalise_fpr "$imported_fpr")
      want_n=$(normalise_fpr "$CORDUM_PROD_FINGERPRINT_PIN")
      if [ "$got_n" = "$want_n" ]; then
        verify_fail "--dev-allow-unsigned refuses production fingerprint"
      fi
    fi
  else
    local got_n want_n
    got_n=$(normalise_fpr "$imported_fpr")
    want_n=$(normalise_fpr "$CORDUM_PROD_FINGERPRINT_PIN")
    if [ "$got_n" != "$want_n" ]; then
      verify_fail "release pubkey fingerprint $got_n does not match pinned $want_n"
    fi
  fi

  gpg --homedir "$tmp_home" --batch --quiet --verify "$sig" "$manifest" 2>/dev/null \
    || verify_fail "gpg signature invalid"

  while IFS= read -r line || [ -n "$line" ]; do
    [ -z "$line" ] && continue
    local hash="${line%% *}"
    local rel="${line#* }"
    rel="${rel# }"
    # Strip the leading "*" that GNU coreutils sha256sum emits in binary
    # mode (`<hash> *<path>`), so we accept both binary- and text-mode
    # manifests symmetrically.
    rel="${rel#\*}"
    if [ -z "$hash" ] || [ -z "$rel" ]; then
      verify_fail "malformed manifest line"
    fi
    if reject_path "$rel"; then
      verify_fail "manifest path traversal: $rel"
    fi
    local target="$rdir/$rel"
    [ -f "$target" ] || verify_fail "binary missing $rel"
    local got
    got=$(sha256sum --text "$target" | awk '{print $1}')
    if [ "$got" != "$hash" ]; then
      verify_fail "hash mismatch $rel"
    fi
    emit_audit "binary-verify-ok" "" "$hash" "$rel" 0
  done < "$manifest"

  if [ "$(uname -s)" = "Darwin" ] && command -v codesign >/dev/null 2>&1; then
    local previous_scheme="$CORDUM_AUDIT_SIG_SCHEME"
    CORDUM_AUDIT_SIG_SCHEME="codesign"
    while IFS= read -r line || [ -n "$line" ]; do
      [ -z "$line" ] && continue
      local rel="${line#* }"; rel="${rel# }"; rel="${rel#\*}"
      local target="$rdir/$rel"
      [ -f "$target" ] || continue
      codesign --verify --deep --strict "$target" 2>/dev/null \
        || verify_fail "codesign verify failed $rel"
      emit_audit "binary-verify-ok" "" "" "$rel" 0
    done < "$manifest"
    CORDUM_AUDIT_SIG_SCHEME="$previous_scheme"
  fi

  if [ -n "$install_to" ]; then
    mkdir -p "$install_to"
    while IFS= read -r line || [ -n "$line" ]; do
      [ -z "$line" ] && continue
      local hash="${line%% *}"
      local rel="${line#* }"; rel="${rel# }"; rel="${rel#\*}"
      local src="$rdir/$rel"
      local dst="$install_to/$rel"
      mkdir -p "$(dirname "$dst")"
      if ! mv -f "$src" "$dst" 2>/dev/null; then
        # Cross-fs fallback: copy to staging then rename in-place.
        cp -f "$src" "$dst.cordum-staging"
        mv -f "$dst.cordum-staging" "$dst"
      fi
      # Recompute SHA-256 AFTER the move — defence-in-depth against a
      # sig-then-swap race where the attacker substitutes the file between
      # verification and activation.
      local post
      post=$(sha256sum --text "$dst" | awk '{print $1}')
      if [ "$post" != "$hash" ]; then
        verify_fail "post-activation hash mismatch $rel"
      fi
      chmod +x "$dst" 2>/dev/null || true
      emit_audit "binary-verify-ok" "" "$post" "$rel" 0
    done < "$manifest"
    echo "[install] activated $(grep -c '' "$manifest") binaries under $install_to"
  else
    local n
    n=$(grep -c '' "$manifest")
    echo "[install] release-dir verified: $rdir ($n binaries match manifest)"
  fi
}

# ============================================================================
# Argv parsing — when --release-dir is supplied, short-circuit to the
# binary-verify gate and skip the orchestrator block below.
# ============================================================================
RELEASE_DIR=""
DEV_ALLOW_UNSIGNED=0
INSTALL_TO="${CORDUM_INSTALL_DIR:-}"
while [ $# -gt 0 ]; do
  case "$1" in
    --release-dir)        RELEASE_DIR="$2"; shift 2 ;;
    --release-dir=*)      RELEASE_DIR="${1#--release-dir=}"; shift ;;
    --dev-allow-unsigned) DEV_ALLOW_UNSIGNED=1; shift ;;
    --install-dir)        INSTALL_TO="$2"; shift 2 ;;
    --install-dir=*)      INSTALL_TO="${1#--install-dir=}"; shift ;;
    --) shift; break ;;
    -*) break ;;
    *)  break ;;
  esac
done

if [ -n "$RELEASE_DIR" ]; then
  run_binary_verify "$RELEASE_DIR" "$DEV_ALLOW_UNSIGNED" "$INSTALL_TO"
  exit 0
fi

# ============================================================================
# === Cordum platform install (existing orchestrator) ========================
# ============================================================================
REPO_URL=${REPO_URL:-https://github.com/cordum-io/cordum.git}
DEST_DIR=${DEST_DIR:-cordum}
VERSION=${VERSION:-main}
USE_RELEASE_IMAGES=${USE_RELEASE_IMAGES:-0}
CORDUM_VERSION=${CORDUM_VERSION:-latest}

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing dependency: $1" >&2
    exit 1
  fi
}

require git
require docker
compose_cmd=()
if docker compose version >/dev/null 2>&1; then
  compose_cmd=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  compose_cmd=(docker-compose)
  echo "[install] warning: docker-compose v1 detected; prefer Docker Compose v2." >&2
else
  echo "docker compose plugin required" >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "cannot connect to the Docker daemon." >&2
  echo "ensure Docker is running and your user can access /var/run/docker.sock." >&2
  echo "on Linux, add your user to the docker group or re-run with sudo." >&2
  exit 1
fi

port_in_use() {
  local port="$1"
  if command -v ss >/dev/null 2>&1; then
    if ss -ltn 2>/dev/null | awk '{print $4}' | grep -E "(^|:)${port}$" >/dev/null 2>&1; then
      return 0
    fi
    return 1
  fi
  if command -v lsof >/dev/null 2>&1; then
    if lsof -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1; then
      return 0
    fi
    return 1
  fi
  if command -v netstat >/dev/null 2>&1; then
    if netstat -ltn 2>/dev/null | awk '{print $4}' | grep -E "(^|:)${port}$" >/dev/null 2>&1; then
      return 0
    fi
    return 1
  fi
  return 2
}

warn_port() {
  local port="$1"
  local name="$2"
  if port_in_use "${port}"; then
    echo "[install] warning: port ${port} (${name}) is already in use; compose may fail to bind." >&2
  fi
}

warn_port 8081 "api-gateway http"
warn_port 8082 "dashboard"
warn_port 8080 "api-gateway grpc"
warn_port 9092 "gateway metrics"
warn_port 9093 "workflow-engine http"
warn_port 50051 "safety-kernel grpc"
warn_port 50070 "context-engine grpc"
warn_port 4222 "nats client"
warn_port 6379 "redis"

API_KEY=${CORDUM_API_KEY:-${API_KEY:-}}
if [[ -z "${API_KEY}" ]]; then
  echo "CORDUM_API_KEY is required; export it before running install." >&2
  exit 1
fi
export CORDUM_API_KEY="${API_KEY}"
TENANT_ID=${CORDUM_TENANT_ID:-default}
export CORDUM_TENANT_ID="${TENANT_ID}"

if [ -d "${DEST_DIR}/.git" ]; then
  echo "using existing repo at ${DEST_DIR}"
else
  echo "cloning ${REPO_URL} to ${DEST_DIR}"
  git clone --depth 1 --branch "${VERSION}" "${REPO_URL}" "${DEST_DIR}"
fi

cd "${DEST_DIR}"

if [ "${USE_RELEASE_IMAGES}" = "1" ]; then
  echo "starting from GHCR images (CORDUM_VERSION=${CORDUM_VERSION})"
  export CORDUM_VERSION
  "${compose_cmd[@]}" -f docker-compose.release.yml pull
  "${compose_cmd[@]}" -f docker-compose.release.yml up -d
else
  echo "building from source"
  "${compose_cmd[@]}" build
  "${compose_cmd[@]}" up -d
fi

cat <<'EOF'
Cordum is up.
- Dashboard: http://localhost:8082
- API: http://localhost:8081
Next steps (OSS, from repo root):
- Quickstart: ./tools/scripts/quickstart.sh
- Smoke test: CORDUM_API_KEY=<your-api-key> CORDUM_TENANT_ID=default ./tools/scripts/platform_smoke.sh
- Guardrails demo: ./tools/scripts/demo_guardrails_run.sh
- Mock bank demo: ./tools/scripts/demo_mock_bank.sh
EOF
