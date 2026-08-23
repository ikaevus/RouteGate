#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=scripts/routegate-update-verified.sh
source "$ROOT_DIR/scripts/routegate-update-verified.sh"

TMP_DIR=$(mktemp -d)
trap 'rm -rf -- "$TMP_DIR"' EXIT

fail() {
  printf 'test-attestation-verifier-runtime: %s\n' "$*" >&2
  exit 1
}

assert_eq() {
  local actual=$1
  local expected=$2
  local label=$3
  [[ "$actual" == "$expected" ]] || fail "$label: got '$actual', want '$expected'"
}

make_fixture_archive() {
  local archive=$1
  local mode=${2:-valid}
  local stage="$TMP_DIR/stage-${mode}"
  local root="$stage/gh_${GH_VERIFIER_VERSION}_linux_amd64"
  mkdir -p "$root/bin"

  cat >"$root/bin/gh" <<'EOF_GH'
#!/usr/bin/env bash
set -euo pipefail
if [[ ${1:-} == --version ]]; then
  printf 'gh version 2.97.0 (fixture)\n'
  exit 0
fi
if [[ ${1:-} == attestation && ${2:-} == verify && ${3:-} == --help ]]; then
  printf '      --predicate-type string\n'
  exit 0
fi
exit 0
EOF_GH
  chmod 0755 "$root/bin/gh"

  if [[ "$mode" == "unsafe-link" ]]; then
    ln -s /etc/passwd "$root/unsafe-link"
  fi

  tar -C "$stage" -czf "$archive" .
}

configure_mock_paths() {
  local root=$1
  GH_VERIFIER_PARENT="$root/usr/local/lib/routegate"
  GH_VERIFIER_DIR="${GH_VERIFIER_PARENT}/verifier"
  GH_VERIFIER="${GH_VERIFIER_DIR}/gh"
  GH_VERIFIER_METADATA="${GH_VERIFIER_DIR}/runtime.env"
  GH_VERIFIER_RELEASE_BASE="https://example.invalid/cli/v${GH_VERIFIER_VERSION}"
  install -d -m 0755 "$GH_VERIFIER_PARENT"
}

test_pinned_release_contract() {
  assert_eq \
    "$GH_VERIFIER_VERSION" \
    "2.97.0" \
    "pinned GitHub CLI version"
  assert_eq \
    "$GH_VERIFIER_SHA_AMD64" \
    "a2c9b8497e1f85b1ad0dfcb78b5a622e098801b8e461e459e88e1ee12f018112" \
    "pinned amd64 archive digest"
  assert_eq \
    "$GH_VERIFIER_SHA_ARM64" \
    "73ea440ecad9c9e284429997ee6f93577bc6f7bc6fba357ef62c53ad8fb641a5" \
    "pinned arm64 archive digest"
}

test_archive_install_and_validation() {
  local root="$TMP_DIR/good-root"
  local archive="$TMP_DIR/good.tar.gz"
  local source_url
  make_fixture_archive "$archive"
  configure_mock_paths "$root"

  GH_VERIFIER_SHA_AMD64=$(sha256sum "$archive" | awk '{print $1}')
  source_url="${GH_VERIFIER_RELEASE_BASE}/gh_${GH_VERIFIER_VERSION}_linux_amd64.tar.gz"
  WORK_DIR="$TMP_DIR/good-work"
  install -d -m 0700 "$WORK_DIR"

  install_verifier_archive "$archive" amd64 "$GH_VERIFIER_SHA_AMD64" "$source_url"
  validate_attestation_verifier

  [[ -x "$GH_VERIFIER" ]] || fail "installed verifier is not executable"
  [[ -f "$GH_VERIFIER_METADATA" && ! -L "$GH_VERIFIER_METADATA" ]] \
    || fail "installed verifier metadata is missing"
  grep -Fxq "VERSION=${GH_VERIFIER_VERSION}" "$GH_VERIFIER_METADATA" \
    || fail "metadata version is missing"
  grep -Fxq "ARCHIVE_SHA256=${GH_VERIFIER_SHA_AMD64}" "$GH_VERIFIER_METADATA" \
    || fail "metadata archive digest is missing"
  grep -Fxq "SOURCE_URL=${source_url}" "$GH_VERIFIER_METADATA" \
    || fail "metadata source URL is missing"
}

test_tampered_binary_fails_closed() {
  local root="$TMP_DIR/tamper-root"
  local archive="$TMP_DIR/tamper.tar.gz"
  local source_url
  make_fixture_archive "$archive"
  configure_mock_paths "$root"

  GH_VERIFIER_SHA_AMD64=$(sha256sum "$archive" | awk '{print $1}')
  source_url="${GH_VERIFIER_RELEASE_BASE}/gh_${GH_VERIFIER_VERSION}_linux_amd64.tar.gz"
  WORK_DIR="$TMP_DIR/tamper-work"
  install -d -m 0700 "$WORK_DIR"
  install_verifier_archive "$archive" amd64 "$GH_VERIFIER_SHA_AMD64" "$source_url"

  printf '#tamper\n' >>"$GH_VERIFIER"
  if validate_attestation_verifier >/dev/null 2>&1; then
    fail "tampered verifier binary unexpectedly validated"
  fi
}

test_tampered_metadata_fails_closed() {
  local root="$TMP_DIR/metadata-root"
  local archive="$TMP_DIR/metadata.tar.gz"
  local source_url
  make_fixture_archive "$archive"
  configure_mock_paths "$root"

  GH_VERIFIER_SHA_AMD64=$(sha256sum "$archive" | awk '{print $1}')
  source_url="${GH_VERIFIER_RELEASE_BASE}/gh_${GH_VERIFIER_VERSION}_linux_amd64.tar.gz"
  WORK_DIR="$TMP_DIR/metadata-work"
  install -d -m 0700 "$WORK_DIR"
  install_verifier_archive "$archive" amd64 "$GH_VERIFIER_SHA_AMD64" "$source_url"

  sed -i 's#^SOURCE_URL=.*#SOURCE_URL=https://example.invalid/tampered#' "$GH_VERIFIER_METADATA"
  if validate_attestation_verifier >/dev/null 2>&1; then
    fail "tampered verifier metadata unexpectedly validated"
  fi
}

test_writable_or_unexpected_state_fails_closed() {
  local root="$TMP_DIR/unsafe-state-root"
  local archive="$TMP_DIR/unsafe-state.tar.gz"
  local source_url
  make_fixture_archive "$archive"
  configure_mock_paths "$root"

  GH_VERIFIER_SHA_AMD64=$(sha256sum "$archive" | awk '{print $1}')
  source_url="${GH_VERIFIER_RELEASE_BASE}/gh_${GH_VERIFIER_VERSION}_linux_amd64.tar.gz"
  WORK_DIR="$TMP_DIR/unsafe-state-work"
  install -d -m 0700 "$WORK_DIR"
  install_verifier_archive "$archive" amd64 "$GH_VERIFIER_SHA_AMD64" "$source_url"

  chmod 0775 "$GH_VERIFIER"
  if validate_attestation_verifier >/dev/null 2>&1; then
    fail "group-writable verifier unexpectedly validated"
  fi
  chmod 0755 "$GH_VERIFIER"

  printf 'unexpected\n' >"$GH_VERIFIER_DIR/extra"
  if validate_attestation_verifier >/dev/null 2>&1; then
    fail "verifier directory with unexpected entry unexpectedly validated"
  fi
}

test_unsafe_archive_rejected() {
  local archive="$TMP_DIR/unsafe-link.tar.gz"
  make_fixture_archive "$archive" unsafe-link
  if validate_verifier_archive "$archive" >/dev/null 2>&1; then
    fail "verifier archive containing a symlink unexpectedly validated"
  fi
}

[[ ${EUID:-$(id -u)} -eq 0 ]] || fail "run this test as root"

test_pinned_release_contract
test_archive_install_and_validation
test_tampered_binary_fails_closed
test_tampered_metadata_fails_closed
test_writable_or_unexpected_state_fails_closed
test_unsafe_archive_rejected

printf 'RouteGate pinned attestation verifier runtime tests passed.\n'
