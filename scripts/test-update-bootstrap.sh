#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
TMP_DIR=$(mktemp -d)
trap 'sudo rm -rf -- "$TMP_DIR"' EXIT

fail() {
  printf 'test-update-bootstrap: %s\n' "$*" >&2
  exit 1
}

assert_contains() {
  local path=$1
  local expected=$2
  grep -Fq -- "$expected" "$path" || fail "$path does not contain: $expected"
}

make_bundle_fixture() {
  local bundle=$1
  mkdir -p "$bundle/tools" "$bundle/systemd"
  cp "$ROOT_DIR/scripts/release_manifest.py" "$bundle/tools/"
  cp "$ROOT_DIR/scripts/routegate-update-bootstrap.sh" "$bundle/tools/"
  cp "$ROOT_DIR/scripts/routegate-update-core.sh" "$bundle/tools/"
  cp "$ROOT_DIR/scripts/routegate-update-role.sh" "$bundle/tools/"
  cp "$ROOT_DIR/scripts/routegate-update-transaction.sh" "$bundle/tools/"
  cp "$ROOT_DIR/scripts/routegate-update-dispatch.py" "$bundle/tools/"
  cp "$ROOT_DIR/deploy/systemd/routegate-update-dispatch.socket" "$bundle/systemd/"
  cp "$ROOT_DIR/deploy/systemd/routegate-update-dispatch@.service" "$bundle/systemd/"

  cat >"$bundle/tools/routegate-update-verified.sh" <<'EOF_VERIFIED_FIXTURE'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  --help|-h)
    printf 'verified-fixture\n'
    ;;
  install-verifier)
    root=${RG_UPDATE_ROOT:?test root is required}
    install -d -m 0700 "$root/var/lib/routegate-test"
    printf 'ready\n' >"$root/var/lib/routegate-test/verifier-runtime"
    ;;
  *)
    exit 1
    ;;
esac
EOF_VERIFIED_FIXTURE
  chmod 0755 \
    "$bundle/tools/routegate-update-bootstrap.sh" \
    "$bundle/tools/routegate-update-verified.sh" \
    "$bundle/tools/routegate-update-dispatch.py"
}

run_bootstrap() {
  local bundle=$1
  local root=$2
  sudo env \
    PATH="$PATH" \
    RG_UPDATE_ROOT="$root" \
    bash "$bundle/tools/routegate-update-bootstrap.sh"
}

test_fresh_bootstrap_and_preserve() {
  local bundle="$TMP_DIR/bundle"
  local root="$TMP_DIR/root"
  make_bundle_fixture "$bundle"
  mkdir -p "$root"

  run_bootstrap "$bundle" "$root" >/dev/null

  local tool_dir="$root/usr/local/lib/routegate/update"
  local entrypoint="$root/usr/local/sbin/routegate-update"
  [[ -d "$tool_dir" && ! -L "$tool_dir" ]] || fail "trusted updater directory was not installed safely"
  [[ -x "$entrypoint" && ! -L "$entrypoint" ]] || fail "trusted updater entrypoint was not installed"
  assert_contains "$entrypoint" 'exec /usr/local/lib/routegate/update/routegate-update-verified.sh "$@"'
  [[ $(stat -c '%a' "$tool_dir/routegate-update-core.sh") == 644 ]] || fail "core mode is not 0644"
  [[ $(stat -c '%a' "$tool_dir/routegate-update-verified.sh") == 755 ]] || fail "verified gate mode is not 0755"
  [[ $(stat -c '%u' "$tool_dir/routegate-update-verified.sh") == 0 ]] || fail "trusted verifier is not root-owned"
  [[ -x "$tool_dir/routegate-update-dispatch.py" && ! -L "$tool_dir/routegate-update-dispatch.py" ]] \
    || fail "privileged dispatch executable was not installed"
  [[ -f "$root/etc/systemd/system/routegate-update-dispatch.socket" ]] \
    || fail "dispatch socket unit was not installed"
  [[ -f "$root/etc/systemd/system/routegate-update-dispatch@.service" ]] \
    || fail "dispatch service unit was not installed"
  [[ $(stat -c '%a' "$root/etc/systemd/system/routegate-update-dispatch.socket") == 644 ]] \
    || fail "dispatch socket unit mode is not 0644"
  sudo test -f "$root/var/lib/routegate-test/verifier-runtime" \
    || fail "fresh bootstrap did not invoke verifier runtime installation"

  local before
  before=$(sha256sum "$tool_dir/routegate-update-verified.sh" | awk '{print $1}')
  run_bootstrap "$bundle" "$root" >/dev/null
  [[ $(sha256sum "$tool_dir/routegate-update-verified.sh" | awk '{print $1}') == "$before" ]] \
    || fail "second bootstrap unexpectedly replaced a complete trusted updater"
}

test_rejects_writable_existing_verifier() {
  local bundle="$TMP_DIR/writable-bundle"
  local root="$TMP_DIR/writable-root"
  make_bundle_fixture "$bundle"
  mkdir -p "$root"
  run_bootstrap "$bundle" "$root" >/dev/null

  sudo chmod g+w "$root/usr/local/lib/routegate/update/routegate-update-verified.sh"
  if run_bootstrap "$bundle" "$root" >/dev/null 2>&1; then
    fail "bootstrap accepted a group-writable trusted verifier"
  fi
}

test_rejects_symlinked_parent_before_write() {
  local bundle="$TMP_DIR/symlink-bundle"
  local root="$TMP_DIR/symlink-root"
  local redirected="$TMP_DIR/redirected"
  make_bundle_fixture "$bundle"
  mkdir -p "$root/usr/local/lib" "$root/usr/local/sbin" "$redirected"
  sudo ln -s "$redirected" "$root/usr/local/lib/routegate"

  if run_bootstrap "$bundle" "$root" >/dev/null 2>&1; then
    fail "bootstrap accepted a symlinked trusted updater parent"
  fi
  [[ ! -e "$redirected/update" ]] || fail "bootstrap wrote through a symlinked trusted parent"
}

test_failed_first_bootstrap_cleans_partial_state() {
  local bundle="$TMP_DIR/broken-bundle"
  local root="$TMP_DIR/broken-root"
  make_bundle_fixture "$bundle"
  mkdir -p "$root"
  cat >"$bundle/tools/routegate-update-verified.sh" <<'EOF_BROKEN'
#!/usr/bin/env bash
if [[
EOF_BROKEN

  if run_bootstrap "$bundle" "$root" >/dev/null 2>&1; then
    fail "bootstrap with an invalid candidate verifier unexpectedly succeeded"
  fi
  [[ ! -e "$root/usr/local/lib/routegate/update" ]] || fail "failed bootstrap left a partial updater directory"
  [[ ! -e "$root/usr/local/sbin/routegate-update" ]] || fail "failed bootstrap left a partial updater entrypoint"
}

test_fresh_bootstrap_and_preserve
test_rejects_writable_existing_verifier
test_rejects_symlinked_parent_before_write
test_failed_first_bootstrap_cleans_partial_state

printf 'RouteGate updater installer bootstrap tests passed.\n'
