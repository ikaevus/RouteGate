#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=scripts/routegate-update-core.sh
source "$ROOT_DIR/scripts/routegate-update-core.sh"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

fail() {
  printf 'test-update-core: %s\n' "$*" >&2
  exit 1
}

assert_eq() {
  local actual=$1
  local expected=$2
  local label=$3
  [[ "$actual" == "$expected" ]] || fail "$label: got '$actual', want '$expected'"
}

assert_file_content() {
  local path=$1
  local expected=$2
  local actual
  actual=$(cat "$path")
  assert_eq "$actual" "$expected" "$path"
}

make_stage() {
  local stage=$1
  local commit=$2
  mkdir -p \
    "$stage/bin" \
    "$stage/frontend" \
    "$stage/manager/migrations" \
    "$stage/systemd" \
    "$stage/metadata"
  printf 'manager-new\n' >"$stage/bin/routegate-manager"
  printf 'agent-new\n' >"$stage/bin/routegate-agent"
  printf 'frontend-new\n' >"$stage/frontend/index.html"
  printf 'SELECT 1;\n' >"$stage/manager/migrations/000134_distinct_tcp_listener_ports.up.sql"
  printf '[Unit]\nDescription=Manager\n' >"$stage/systemd/routegate-manager.service"
  printf '[Unit]\nDescription=Agent\n' >"$stage/systemd/routegate-agent.service"
  cat >"$stage/metadata/manifest.env" <<EOF_MANIFEST
FORMAT_VERSION=1
VERSION=v0.2.0
COMMIT=$commit
BUILD_DATE=2026-08-23T12:00:00Z
OS=linux
ARCH=amd64
EOF_MANIFEST
}

test_bundle_verification() {
  local commit="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  local stage="$TMP_DIR/stage"
  local bundle="$TMP_DIR/routegate-v0.2.0-linux-amd64.tar.gz"
  local work="$TMP_DIR/work"
  local sha

  make_stage "$stage" "$commit"
  tar -C "$stage" -czf "$bundle" .
  sha=$(sha256sum "$bundle" | awk '{print $1}')

  rg_update_verify_and_extract_bundle "$bundle" "$sha" "$commit" linux amd64 "$work"
  assert_eq "$RG_UPDATE_BUNDLE_VERSION" "v0.2.0" "bundle version"
  assert_eq "$RG_UPDATE_BUNDLE_COMMIT" "$commit" "bundle commit"
  assert_eq "$RG_UPDATE_EXPECTED_SCHEMA" "000134_distinct_tcp_listener_ports" "expected schema"
}

test_metadata_is_never_evaluated() {
  local metadata="$TMP_DIR/malicious.env"
  local marker="$TMP_DIR/metadata-executed"
  local commit="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

  cat >"$metadata" <<EOF_METADATA
FORMAT_VERSION=1
VERSION=\$(touch $marker)
COMMIT=$commit
BUILD_DATE=2026-08-23T12:00:00Z
OS=linux
ARCH=amd64
EOF_METADATA

  if rg_update_read_bundle_metadata "$metadata" "$commit" linux amd64 >/dev/null 2>&1; then
    fail "malicious metadata unexpectedly passed validation"
  fi
  [[ ! -e "$marker" ]] || fail "bundle metadata was evaluated as shell code"
}

test_unsafe_archive_is_rejected() {
  local bundle="$TMP_DIR/unsafe.tar.gz"

  python3 - "$bundle" <<'PY'
import io
import sys
import tarfile

with tarfile.open(sys.argv[1], "w:gz") as archive:
    entry = tarfile.TarInfo("../escape")
    entry.size = 1
    archive.addfile(entry, io.BytesIO(b"x"))
PY

  if rg_update_validate_archive "$bundle" >/dev/null 2>&1; then
    fail "unsafe archive unexpectedly passed validation"
  fi
}

install_command_stubs() {
  local stub_dir=$1
  mkdir -p "$stub_dir"

  cat >"$stub_dir/systemctl" <<'EOF_SYSTEMCTL'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  is-active)
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
EOF_SYSTEMCTL

  cat >"$stub_dir/chown" <<'EOF_CHOWN'
#!/usr/bin/env bash
exit 0
EOF_CHOWN

  cat >"$stub_dir/pg_dump" <<'EOF_PG_DUMP'
#!/usr/bin/env bash
set -euo pipefail
for arg in "$@"; do
  case "$arg" in
    --file=*)
      printf 'db-backup\n' >"${arg#--file=}"
      exit 0
      ;;
  esac
done
exit 2
EOF_PG_DUMP

  cat >"$stub_dir/pg_restore" <<'EOF_PG_RESTORE'
#!/usr/bin/env bash
exit 0
EOF_PG_RESTORE

  chmod 0755 "$stub_dir"/*
}

populate_fake_host() {
  local root=$1
  mkdir -p \
    "$root/usr/local/bin" \
    "$root/opt/routegate-manager/migrations" \
    "$root/var/www/routegate" \
    "$root/etc/systemd/system" \
    "$root/etc/routegate"

  printf 'manager-old\n' >"$root/usr/local/bin/routegate-manager"
  printf 'agent-old\n' >"$root/usr/local/bin/routegate-agent"
  printf 'migration-old\n' >"$root/opt/routegate-manager/migrations/000133_old.up.sql"
  printf 'frontend-old\n' >"$root/var/www/routegate/index.html"
  printf 'manager-unit-old\n' >"$root/etc/systemd/system/routegate-manager.service"
  printf 'agent-unit-old\n' >"$root/etc/systemd/system/routegate-agent.service"
  printf 'ROUTEGATE_DATABASE_URL=test\n' >"$root/etc/routegate/manager.env"
}

test_backup_restore_round_trip() {
  local fake_root="$TMP_DIR/root"
  local backup="$TMP_DIR/backups/roundtrip"
  local stub_dir="$TMP_DIR/stubs"
  local old_path=$PATH

  populate_fake_host "$fake_root"
  install_command_stubs "$stub_dir"

  PATH="$stub_dir:$PATH"
  RG_UPDATE_ROOT="$fake_root"
  RG_UPDATE_MANAGER_OWNER="routegate:routegate"

  rg_update_create_backup "$backup" "postgres://example/test"
  [[ -s "$backup/routegate.pgdump" ]] || fail "database backup was not created"

  printf 'manager-mutated\n' >"$fake_root/usr/local/bin/routegate-manager"
  printf 'agent-mutated\n' >"$fake_root/usr/local/bin/routegate-agent"
  rm -rf "$fake_root/opt/routegate-manager/migrations"
  mkdir -p "$fake_root/opt/routegate-manager/migrations"
  printf 'migration-mutated\n' >"$fake_root/opt/routegate-manager/migrations/changed.up.sql"
  printf 'frontend-mutated\n' >"$fake_root/var/www/routegate/index.html"
  printf 'manager-unit-mutated\n' >"$fake_root/etc/systemd/system/routegate-manager.service"
  printf 'agent-unit-mutated\n' >"$fake_root/etc/systemd/system/routegate-agent.service"
  printf 'mutated=1\n' >"$fake_root/etc/routegate/manager.env"

  rg_update_restore_backup "$backup" "postgres://example/test" 1

  assert_file_content "$fake_root/usr/local/bin/routegate-manager" "manager-old"
  assert_file_content "$fake_root/usr/local/bin/routegate-agent" "agent-old"
  assert_file_content "$fake_root/opt/routegate-manager/migrations/000133_old.up.sql" "migration-old"
  assert_file_content "$fake_root/var/www/routegate/index.html" "frontend-old"
  assert_file_content "$fake_root/etc/systemd/system/routegate-manager.service" "manager-unit-old"
  assert_file_content "$fake_root/etc/systemd/system/routegate-agent.service" "agent-unit-old"
  assert_file_content "$fake_root/etc/routegate/manager.env" "ROUTEGATE_DATABASE_URL=test"
  assert_eq "$RG_UPDATE_DB_RESTORE_RC" "0" "database restore result"

  PATH=$old_path
  RG_UPDATE_ROOT=""
}

test_bundle_verification
test_metadata_is_never_evaluated
test_unsafe_archive_is_rejected
test_backup_restore_round_trip

printf 'RouteGate host update core tests passed.\n'
