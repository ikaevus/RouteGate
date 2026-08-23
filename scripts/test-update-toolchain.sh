#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=scripts/routegate-update-core.sh
source "$ROOT_DIR/scripts/routegate-update-core.sh"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

COMMIT=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
VERSION=v0.2.0
MIGRATION=000134_distinct_tcp_listener_ports

fail() {
  printf 'test-update-toolchain: %s\n' "$*" >&2
  exit 1
}

assert_eq() {
  local actual=$1
  local expected=$2
  local label=$3
  [[ "$actual" == "$expected" ]] || fail "$label: got '$actual', want '$expected'"
}

assert_file_contains() {
  local path=$1
  local expected=$2
  grep -Fq -- "$expected" "$path" || fail "$path does not contain: $expected"
}

make_toolchain_candidate() {
  local work=$1
  local label=$2
  local verified_mode=${3:-valid}
  mkdir -p "$work/tools"

  cat >"$work/tools/release_manifest.py" <<EOF_PYTHON
#!/usr/bin/env python3
import argparse
parser = argparse.ArgumentParser(description="RouteGate ${label} verifier fixture")
parser.parse_args()
EOF_PYTHON

  cat >"$work/tools/routegate-update-core.sh" <<EOF_CORE
#!/usr/bin/env bash
ROUTEGATE_TOOLCHAIN_FIXTURE=${label@Q}
EOF_CORE

  cat >"$work/tools/routegate-update-role.sh" <<EOF_ROLE
#!/usr/bin/env bash
ROUTEGATE_ROLE_FIXTURE=${label@Q}
EOF_ROLE

  cat >"$work/tools/routegate-update-transaction.sh" <<EOF_TRANSACTION
#!/usr/bin/env bash
set -euo pipefail
if [[ \${1:-} == --help || \${1:-} == -h ]]; then
  printf 'transaction-${label}\\n'
  exit 0
fi
exit 1
EOF_TRANSACTION

  if [[ "$verified_mode" == "valid" ]]; then
    cat >"$work/tools/routegate-update-verified.sh" <<EOF_VERIFIED
#!/usr/bin/env bash
set -euo pipefail
if [[ \${1:-} == --help || \${1:-} == -h ]]; then
  printf 'verified-${label}\\n'
  exit 0
fi
exit 1
EOF_VERIFIED
  else
    cat >"$work/tools/routegate-update-verified.sh" <<'EOF_INVALID_VERIFIED'
#!/usr/bin/env bash
if [[
EOF_INVALID_VERIFIED
  fi
}

make_transaction_bundle() {
  local output=$1
  local label=$2
  local verified_mode=${3:-valid}
  local stage="$TMP_DIR/stage-${label}-${verified_mode}"

  mkdir -p \
    "$stage/bin" \
    "$stage/frontend" \
    "$stage/manager/migrations" \
    "$stage/systemd" \
    "$stage/metadata"
  printf 'manager-%s\n' "$label" >"$stage/bin/routegate-manager"
  printf 'agent-%s\n' "$label" >"$stage/bin/routegate-agent"
  printf 'frontend-%s\n' "$label" >"$stage/frontend/index.html"
  printf 'SELECT 1;\n' >"$stage/manager/migrations/${MIGRATION}.up.sql"
  printf '[Unit]\nDescription=Manager %s\n' "$label" >"$stage/systemd/routegate-manager.service"
  printf '[Unit]\nDescription=Agent %s\n' "$label" >"$stage/systemd/routegate-agent.service"
  make_toolchain_candidate "$stage" "$label" "$verified_mode"

  cat >"$stage/metadata/manifest.env" <<EOF_MANIFEST
FORMAT_VERSION=1
VERSION=$VERSION
COMMIT=$COMMIT
BUILD_DATE=2026-08-23T12:00:00Z
OS=linux
ARCH=amd64
EOF_MANIFEST

  tar -C "$stage" -czf "$output" .
}

populate_vpn_host() {
  local root=$1
  mkdir -p "$root/usr/local/bin" "$root/etc/systemd/system" "$root/etc/routegate"
  printf 'agent-old\n' >"$root/usr/local/bin/routegate-agent"
  printf 'agent-unit-old\n' >"$root/etc/systemd/system/routegate-agent.service"
  printf 'manager_url: "https://manager.example"\nagent_token: "rg_agent_fixture"\n' >"$root/etc/routegate/agent.yaml"
}

make_systemctl_stub() {
  local stub_dir=$1
  mkdir -p "$stub_dir"
  cat >"$stub_dir/systemctl" <<'EOF_SYSTEMCTL'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  is-active)
    if [[ ${2:-} == --quiet ]]; then
      exit 0
    fi
    printf 'active\n'
    exit 0
    ;;
  *) exit 0 ;;
esac
EOF_SYSTEMCTL
  chmod 0755 "$stub_dir/systemctl"
}

test_absent_promotion_and_restore() {
  local root="$TMP_DIR/absent-root"
  local work="$TMP_DIR/absent-work"
  local backup="$TMP_DIR/absent-backup"
  mkdir -p "$root" "$backup"
  make_toolchain_candidate "$work" new
  RG_UPDATE_ROOT=$root

  assert_eq "$(rg_update_toolchain_state)" absent "initial updater state"
  rg_update_create_toolchain_backup "$backup"
  assert_file_contains "$backup/update-toolchain.meta" 'STATE=absent'

  rg_update_apply_toolchain "$work"
  rg_update_validate_toolchain
  assert_eq "$(rg_update_toolchain_state)" complete "promoted updater state"
  assert_file_contains "$root/usr/local/lib/routegate/update/routegate-update-core.sh" 'new'
  [[ -x "$root/usr/local/sbin/routegate-update" ]] || fail "promoted entrypoint is not executable"

  rg_update_restore_toolchain_backup "$backup"
  assert_eq "$(rg_update_toolchain_state)" absent "restored absent updater state"
  [[ ! -e "$root/usr/local/lib/routegate/update" ]] || fail "absent rollback left updater directory"
  [[ ! -e "$root/usr/local/sbin/routegate-update" ]] || fail "absent rollback left updater entrypoint"
  RG_UPDATE_ROOT=""
}

test_complete_round_trip() {
  local root="$TMP_DIR/complete-root"
  local old_work="$TMP_DIR/old-work"
  local new_work="$TMP_DIR/new-work"
  local backup="$TMP_DIR/complete-backup"
  mkdir -p "$root" "$backup"
  make_toolchain_candidate "$old_work" old
  make_toolchain_candidate "$new_work" new
  RG_UPDATE_ROOT=$root

  rg_update_apply_toolchain "$old_work"
  rg_update_validate_toolchain
  rg_update_create_toolchain_backup "$backup"
  assert_file_contains "$backup/update-toolchain.meta" 'STATE=complete'

  rg_update_apply_toolchain "$new_work"
  rg_update_validate_toolchain
  assert_file_contains "$root/usr/local/lib/routegate/update/routegate-update-core.sh" 'new'

  rg_update_restore_toolchain_backup "$backup"
  rg_update_validate_toolchain
  assert_file_contains "$root/usr/local/lib/routegate/update/routegate-update-core.sh" 'old'
  assert_file_contains "$root/usr/local/lib/routegate/update/routegate-update-verified.sh" 'verified-old'
  RG_UPDATE_ROOT=""
}

test_partial_and_unexpected_state_fail_closed() {
  local partial="$TMP_DIR/partial-root"
  local unexpected="$TMP_DIR/unexpected-root"

  mkdir -p "$partial/usr/local/lib/routegate/update"
  printf 'partial\n' >"$partial/usr/local/lib/routegate/update/release_manifest.py"
  RG_UPDATE_ROOT=$partial
  if rg_update_toolchain_state >/dev/null 2>&1; then
    fail "partial updater state unexpectedly passed"
  fi

  RG_UPDATE_ROOT=$unexpected
  mkdir -p "$unexpected"
  local work="$TMP_DIR/unexpected-work"
  make_toolchain_candidate "$work" clean
  rg_update_apply_toolchain "$work"
  printf 'unexpected\n' >"$unexpected/usr/local/lib/routegate/update/extra"
  if rg_update_toolchain_state >/dev/null 2>&1; then
    fail "updater state with unexpected file unexpectedly passed"
  fi
  RG_UPDATE_ROOT=""
}

test_missing_candidate_component_fails() {
  local root="$TMP_DIR/missing-root"
  local work="$TMP_DIR/missing-work"
  mkdir -p "$root"
  make_toolchain_candidate "$work" missing
  rm "$work/tools/routegate-update-role.sh"
  RG_UPDATE_ROOT=$root
  if rg_update_apply_toolchain "$work" >/dev/null 2>&1; then
    fail "incomplete candidate updater unexpectedly promoted"
  fi
  RG_UPDATE_ROOT=""
}

run_vpn_transaction() {
  local root=$1
  local bundle=$2
  local backup_root=$3
  local stub_dir=$4
  local sha
  sha=$(sha256sum "$bundle" | awk '{print $1}')

  sudo env \
    PATH="$stub_dir:$PATH" \
    RG_UPDATE_ROOT="$root" \
    RG_UPDATE_LOCK_FILE="$TMP_DIR/transaction.lock" \
    RG_UPDATE_BACKUP_ROOT="$backup_root" \
    bash "$ROOT_DIR/scripts/routegate-update-transaction.sh" apply \
      --bundle "$bundle" \
      --sha256 "$sha" \
      --commit "$COMMIT" \
      --role vpn
}

test_transaction_promotes_toolchain_after_vpn_health() {
  local root="$TMP_DIR/transaction-success-root"
  local bundle="$TMP_DIR/transaction-success.tar.gz"
  local backups="$TMP_DIR/transaction-success-backups"
  local stubs="$TMP_DIR/transaction-success-stubs"
  populate_vpn_host "$root"
  make_systemctl_stub "$stubs"
  make_transaction_bundle "$bundle" success valid

  run_vpn_transaction "$root" "$bundle" "$backups" "$stubs" >/dev/null

  assert_file_contains "$root/usr/local/bin/routegate-agent" 'agent-success'
  assert_file_contains "$root/usr/local/lib/routegate/update/routegate-update-verified.sh" 'verified-success'
  [[ -x "$root/usr/local/sbin/routegate-update" ]] || fail "successful transaction did not install updater entrypoint"
}

test_transaction_rolls_back_platform_and_absent_toolchain_on_validation_failure() {
  local root="$TMP_DIR/transaction-failure-root"
  local bundle="$TMP_DIR/transaction-failure.tar.gz"
  local backups="$TMP_DIR/transaction-failure-backups"
  local stubs="$TMP_DIR/transaction-failure-stubs"
  populate_vpn_host "$root"
  make_systemctl_stub "$stubs"
  make_transaction_bundle "$bundle" broken invalid

  if run_vpn_transaction "$root" "$bundle" "$backups" "$stubs" >/dev/null 2>&1; then
    fail "transaction with invalid candidate updater unexpectedly succeeded"
  fi

  assert_file_contains "$root/usr/local/bin/routegate-agent" 'agent-old'
  assert_file_contains "$root/etc/systemd/system/routegate-agent.service" 'agent-unit-old'
  [[ ! -e "$root/usr/local/lib/routegate/update" ]] || fail "failed transaction left promoted updater directory"
  [[ ! -e "$root/usr/local/sbin/routegate-update" ]] || fail "failed transaction left promoted updater entrypoint"
}

test_absent_promotion_and_restore
test_complete_round_trip
test_partial_and_unexpected_state_fail_closed
test_missing_candidate_component_fails
test_transaction_promotes_toolchain_after_vpn_health
test_transaction_rolls_back_platform_and_absent_toolchain_on_validation_failure

printf 'RouteGate trusted updater promotion tests passed.\n'
