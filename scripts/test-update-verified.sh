#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TMP_DIR=$(mktemp -d)

VERSION=v0.2.0
COMMIT=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
BUILD_DATE=2026-08-23T12:00:00Z
MIGRATION=000134_distinct_tcp_listener_ports
ARTIFACTS="$TMP_DIR/artifacts"
STAGE="$TMP_DIR/stage"
MIGRATIONS="$TMP_DIR/migrations"
TOOLS="$TMP_DIR/tools"
STUBS="$TMP_DIR/stubs"
GH_LOG="$TMP_DIR/gh.log"
PATH_GH_LOG="$TMP_DIR/path-gh.log"
TRANSACTION_LOG="$TMP_DIR/transaction.log"
VERIFIER_PARENT=/usr/local/lib/routegate
VERIFIER_DIR="$VERIFIER_PARENT/verifier"
VERIFIER_BACKUP="$TMP_DIR/verifier-backup"
PARENT_EXISTED=0
PINNED_ARCHIVE_SHA=a2c9b8497e1f85b1ad0dfcb78b5a622e098801b8e461e459e88e1ee12f018112
PINNED_SOURCE_URL=https://github.com/cli/cli/releases/download/v2.97.0/gh_2.97.0_linux_amd64.tar.gz

cleanup() {
  sudo rm -rf -- "$VERIFIER_DIR"
  if [[ -d "$VERIFIER_BACKUP" ]]; then
    sudo cp -a -- "$VERIFIER_BACKUP" "$VERIFIER_DIR"
  elif ((PARENT_EXISTED == 0)); then
    sudo rmdir "$VERIFIER_PARENT" >/dev/null 2>&1 || true
  fi
  rm -rf -- "$TMP_DIR"
}
trap cleanup EXIT

fail() {
  printf 'test-update-verified: %s\n' "$*" >&2
  exit 1
}

assert_contains() {
  local path=$1
  local expected=$2
  grep -Fq -- "$expected" "$path" || fail "$path does not contain: $expected"
}

prepare_bundle() {
  mkdir -p \
    "$ARTIFACTS" \
    "$MIGRATIONS" \
    "$STAGE/bin" \
    "$STAGE/frontend" \
    "$STAGE/manager/migrations" \
    "$STAGE/systemd" \
    "$STAGE/metadata" \
    "$STAGE/tools"

  printf 'manager\n' >"$STAGE/bin/routegate-manager"
  printf 'agent\n' >"$STAGE/bin/routegate-agent"
  printf 'html\n' >"$STAGE/frontend/index.html"
  printf 'SELECT 1;\n' >"$STAGE/manager/migrations/${MIGRATION}.up.sql"
  printf '[Unit]\nDescription=Manager\n' >"$STAGE/systemd/routegate-manager.service"
  printf '[Unit]\nDescription=Agent\n' >"$STAGE/systemd/routegate-agent.service"
  printf 'SELECT 1;\n' >"$MIGRATIONS/${MIGRATION}.up.sql"

  local tool
  for tool in \
    release_manifest.py \
    routegate-update-bootstrap.sh \
    routegate-update-core.sh \
    routegate-update-role.sh \
    routegate-update-transaction.sh \
    routegate-update-verified.sh; do
    printf '#!/usr/bin/env bash\n' >"$STAGE/tools/$tool"
  done

  cat >"$STAGE/metadata/manifest.env" <<EOF_MANIFEST
FORMAT_VERSION=1
VERSION=$VERSION
COMMIT=$COMMIT
BUILD_DATE=$BUILD_DATE
OS=linux
ARCH=amd64
EOF_MANIFEST

  tar -C "$STAGE" -czf "$ARTIFACTS/routegate-${VERSION}-linux-amd64.tar.gz" .
  (
    cd "$ARTIFACTS"
    sha256sum "routegate-${VERSION}-linux-amd64.tar.gz" >SHA256SUMS
  )
  python3 "$ROOT_DIR/scripts/release_manifest.py" build \
    --output-dir "$ARTIFACTS" \
    --version "$VERSION" \
    --commit "$COMMIT" \
    --build-date "$BUILD_DATE" \
    --migrations-dir "$MIGRATIONS" >/dev/null

  printf '{}\n' >"$ARTIFACTS/release-manifest.attestation.json"
  printf '{}\n' >"$ARTIFACTS/release-bundles.attestation.json"
}

prepare_fixed_verifier() {
  if sudo test -d "$VERIFIER_PARENT"; then
    PARENT_EXISTED=1
  fi
  if sudo test -e "$VERIFIER_DIR" || sudo test -L "$VERIFIER_DIR"; then
    sudo cp -a -- "$VERIFIER_DIR" "$VERIFIER_BACKUP"
    sudo rm -rf -- "$VERIFIER_DIR"
  fi

  local fixture="$TMP_DIR/fixed-gh"
  cat >"$fixture" <<'EOF_FIXED_GH'
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
if [[ ${1:-} == attestation && ${2:-} == verify ]]; then
  {
    printf 'gh'
    for arg in "$@"; do
      printf '\t%s' "$arg"
    done
    printf '\n'
  } >>"${RG_TEST_GH_LOG:?}"
  subject=${3:-}
  if [[ -n ${RG_TEST_GH_FAIL_BASENAME:-} && "$(basename "$subject")" == "$RG_TEST_GH_FAIL_BASENAME" ]]; then
    exit 79
  fi
  exit 0
fi
exit 1
EOF_FIXED_GH
  chmod 0755 "$fixture"
  local binary_sha
  binary_sha=$(sha256sum "$fixture" | awk '{print $1}')

  sudo install -d -m 0755 "$VERIFIER_PARENT" "$VERIFIER_DIR"
  sudo install -m 0755 "$fixture" "$VERIFIER_DIR/gh"
  local metadata="$TMP_DIR/runtime.env"
  cat >"$metadata" <<EOF_METADATA
FORMAT_VERSION=1
VERSION=2.97.0
ARCH=amd64
ARCHIVE_SHA256=$PINNED_ARCHIVE_SHA
BINARY_SHA256=$binary_sha
SOURCE_URL=$PINNED_SOURCE_URL
EOF_METADATA
  sudo install -m 0644 "$metadata" "$VERIFIER_DIR/runtime.env"
}

prepare_tools_and_stubs() {
  mkdir -p "$TOOLS" "$STUBS"
  cp "$ROOT_DIR/scripts/routegate-update-verified.sh" "$TOOLS/"
  cp "$ROOT_DIR/scripts/release_manifest.py" "$TOOLS/"
  chmod 0755 "$TOOLS/routegate-update-verified.sh"

  cat >"$TOOLS/routegate-update-transaction.sh" <<'EOF_TRANSACTION'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${RG_TEST_TRANSACTION_LOG:?}"
EOF_TRANSACTION
  chmod 0755 "$TOOLS/routegate-update-transaction.sh"

  cat >"$STUBS/gh" <<'EOF_PATH_GH'
#!/usr/bin/env bash
set -euo pipefail
printf 'PATH gh was invoked\n' >>"${RG_TEST_PATH_GH_LOG:?}"
exit 88
EOF_PATH_GH
  chmod 0755 "$STUBS/gh"

  : >"$GH_LOG"
  : >"$PATH_GH_LOG"
  : >"$TRANSACTION_LOG"
}

run_gate() {
  sudo env \
    PATH="$STUBS:$PATH" \
    RG_TEST_GH_LOG="$GH_LOG" \
    RG_TEST_PATH_GH_LOG="$PATH_GH_LOG" \
    RG_TEST_TRANSACTION_LOG="$TRANSACTION_LOG" \
    RG_TEST_GH_FAIL_BASENAME="${RG_TEST_GH_FAIL_BASENAME:-}" \
    bash "$TOOLS/routegate-update-verified.sh" apply \
      --manifest "$ARTIFACTS/release-manifest.json" \
      --manifest-attestation "$ARTIFACTS/release-manifest.attestation.json" \
      --checksums "$ARTIFACTS/SHA256SUMS" \
      --bundle "$ARTIFACTS/routegate-${VERSION}-linux-amd64.tar.gz" \
      --bundle-attestation "$ARTIFACTS/release-bundles.attestation.json" \
      --role vpn
}

test_verified_handoff() {
  : >"$GH_LOG"
  : >"$PATH_GH_LOG"
  : >"$TRANSACTION_LOG"
  unset RG_TEST_GH_FAIL_BASENAME || true

  run_gate >/dev/null

  [[ $(wc -l <"$GH_LOG") -eq 2 ]] || fail "expected exactly two provenance checks"
  [[ ! -s "$PATH_GH_LOG" ]] || fail "verified gate executed gh from PATH"
  [[ $(wc -l <"$TRANSACTION_LOG") -eq 1 ]] || fail "expected exactly one host transaction"

  assert_contains "$GH_LOG" $'--repo\tikaevus/RouteGate'
  assert_contains "$GH_LOG" $'--signer-workflow\tikaevus/RouteGate/.github/workflows/release.yml'
  assert_contains "$GH_LOG" $'--predicate-type\thttps://slsa.dev/provenance/v1'
  assert_contains "$GH_LOG" "release-manifest.attestation.json"
  assert_contains "$GH_LOG" "release-bundles.attestation.json"

  local sha
  sha=$(sha256sum "$ARTIFACTS/routegate-${VERSION}-linux-amd64.tar.gz" | awk '{print $1}')
  assert_contains "$TRANSACTION_LOG" "--sha256 $sha"
  assert_contains "$TRANSACTION_LOG" "--commit $COMMIT"
  assert_contains "$TRANSACTION_LOG" "--role vpn"
}

test_missing_fixed_verifier_does_not_fall_back_to_path() {
  : >"$GH_LOG"
  : >"$PATH_GH_LOG"
  : >"$TRANSACTION_LOG"
  sudo mv "$VERIFIER_DIR" "$VERIFIER_DIR.saved"

  if run_gate >/dev/null 2>&1; then
    sudo mv "$VERIFIER_DIR.saved" "$VERIFIER_DIR"
    fail "verified gate unexpectedly succeeded without its fixed verifier"
  fi
  [[ ! -s "$PATH_GH_LOG" ]] || {
    sudo mv "$VERIFIER_DIR.saved" "$VERIFIER_DIR"
    fail "missing fixed verifier caused fallback to PATH gh"
  }
  [[ ! -s "$TRANSACTION_LOG" ]] || {
    sudo mv "$VERIFIER_DIR.saved" "$VERIFIER_DIR"
    fail "missing fixed verifier reached the host transaction"
  }
  sudo mv "$VERIFIER_DIR.saved" "$VERIFIER_DIR"
}

test_tampered_bundle_stops_before_bundle_attestation_and_transaction() {
  local bundle="$ARTIFACTS/routegate-${VERSION}-linux-amd64.tar.gz"
  local original="$TMP_DIR/original-bundle.tar.gz"
  cp "$bundle" "$original"
  printf 'tamper' >>"$bundle"
  : >"$GH_LOG"
  : >"$PATH_GH_LOG"
  : >"$TRANSACTION_LOG"
  unset RG_TEST_GH_FAIL_BASENAME || true

  if run_gate >/dev/null 2>&1; then
    fail "tampered target bundle unexpectedly passed the verified gate"
  fi
  [[ $(wc -l <"$GH_LOG") -eq 1 ]] || fail "tampered bundle should stop after manifest attestation"
  [[ ! -s "$PATH_GH_LOG" ]] || fail "tampered bundle path invoked PATH gh"
  [[ ! -s "$TRANSACTION_LOG" ]] || fail "tampered bundle reached the host transaction"
  mv "$original" "$bundle"
}

test_manifest_attestation_failure_stops_immediately() {
  : >"$GH_LOG"
  : >"$PATH_GH_LOG"
  : >"$TRANSACTION_LOG"
  export RG_TEST_GH_FAIL_BASENAME=release-manifest.json

  if run_gate >/dev/null 2>&1; then
    fail "failed manifest provenance unexpectedly passed"
  fi
  [[ $(wc -l <"$GH_LOG") -eq 1 ]] || fail "manifest attestation failure should stop immediately"
  [[ ! -s "$PATH_GH_LOG" ]] || fail "manifest failure invoked PATH gh"
  [[ ! -s "$TRANSACTION_LOG" ]] || fail "failed manifest provenance reached the host transaction"
  unset RG_TEST_GH_FAIL_BASENAME
}

test_bundle_attestation_failure_stops_before_transaction() {
  : >"$GH_LOG"
  : >"$PATH_GH_LOG"
  : >"$TRANSACTION_LOG"
  export RG_TEST_GH_FAIL_BASENAME="routegate-${VERSION}-linux-amd64.tar.gz"

  if run_gate >/dev/null 2>&1; then
    fail "failed bundle provenance unexpectedly passed"
  fi
  [[ $(wc -l <"$GH_LOG") -eq 2 ]] || fail "bundle attestation failure should occur on the second provenance check"
  [[ ! -s "$PATH_GH_LOG" ]] || fail "bundle failure invoked PATH gh"
  [[ ! -s "$TRANSACTION_LOG" ]] || fail "failed bundle provenance reached the host transaction"
  unset RG_TEST_GH_FAIL_BASENAME
}

prepare_bundle
prepare_fixed_verifier
prepare_tools_and_stubs
test_verified_handoff
test_missing_fixed_verifier_does_not_fall_back_to_path
test_tampered_bundle_stops_before_bundle_attestation_and_transaction
test_manifest_attestation_failure_stops_immediately
test_bundle_attestation_failure_stops_before_transaction

printf 'RouteGate verified update gate tests passed.\n'
