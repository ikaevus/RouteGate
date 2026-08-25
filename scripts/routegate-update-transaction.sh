#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'
umask 077

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/routegate-update-core.sh
source "$SCRIPT_DIR/routegate-update-core.sh"
# shellcheck source=scripts/routegate-update-role.sh
source "$SCRIPT_DIR/routegate-update-role.sh"

OPERATION=${1:-}
if [[ -n "$OPERATION" ]]; then
  shift
fi

BUNDLE=""
EXPECTED_SHA=""
EXPECTED_COMMIT=""
REQUESTED_ROLE="auto"
LOCK_FILE=${RG_UPDATE_LOCK_FILE:-/run/lock/routegate-update.lock}
BACKUP_ROOT=${RG_UPDATE_BACKUP_ROOT:-/root/routegate-backups}
WORK_DIR=""
BACKUP_DIR=""
DB_URL=""
ROLE=""
TARGET_ARCH=""
MUTATED=0
DB_MAY_BE_MUTATED=0
STAGE=initializing

usage() {
  cat <<'USAGE'
RouteGate local host update transaction

Usage:
  sudo routegate-update-transaction.sh apply \
    --bundle /path/to/verified-routegate-bundle.tar.gz \
    --sha256 <64-character-sha256> \
    --commit <40-character-git-sha> \
    [--role auto|management|vpn|hybrid]

This command is a root-only local transaction primitive. It performs no release
discovery, network download, or provenance lookup. Do not expose it directly to
Manager/Web input until the privileged update boundary independently verifies
RouteGate release provenance.
USAGE
}

log() {
  RG_UPDATE_LOG_PREFIX='[routegate-update-transaction]'
  rg_update_log "$*"
}

die() {
  RG_UPDATE_LOG_PREFIX='[routegate-update-transaction]'
  rg_update_die "$*"
  exit 1
}

parse_args() {
  while (($# > 0)); do
    case "$1" in
      --bundle)
        (($# >= 2)) || die "--bundle requires a value"
        BUNDLE=$2
        shift 2
        ;;
      --sha256)
        (($# >= 2)) || die "--sha256 requires a value"
        EXPECTED_SHA=$2
        shift 2
        ;;
      --commit)
        (($# >= 2)) || die "--commit requires a value"
        EXPECTED_COMMIT=$2
        shift 2
        ;;
      --role)
        (($# >= 2)) || die "--role requires a value"
        REQUESTED_ROLE=$2
        shift 2
        ;;
      --help|-h)
        usage
        exit 0
        ;;
      *) die "unknown argument: $1" ;;
    esac
  done

  [[ -n "$BUNDLE" ]] || die "--bundle is required"
  [[ -n "$EXPECTED_SHA" ]] || die "--sha256 is required"
  [[ -n "$EXPECTED_COMMIT" ]] || die "--commit is required"
  [[ "$REQUESTED_ROLE" == "auto" ]] || rg_update_validate_role "$REQUESTED_ROLE" || exit 1
  [[ -f "$BUNDLE" && ! -L "$BUNDLE" ]] || die "bundle must be a regular local file"
}

platform_architecture() {
  case "$1" in
    x86_64|amd64) printf 'amd64\n' ;;
    aarch64|arm64) printf 'arm64\n' ;;
    *) return 1 ;;
  esac
}

cleanup() {
  if [[ -n "$WORK_DIR" && -d "$WORK_DIR" ]]; then
    rm -rf "$WORK_DIR"
  fi
}

trusted_path_is_secure() {
  local path=$1
  local label=$2
  local owner permissions

  owner=$(stat -c '%u' -- "$path") || return 1
  permissions=$(stat -c '%A' -- "$path") || return 1

  [[ "$owner" == "$EUID" ]] || {
    rg_update_die "$label is not owned by the privileged updater user: $path"
    return 1
  }
  [[ "${permissions:5:1}" != "w" && "${permissions:8:1}" != "w" ]] || {
    rg_update_die "$label is group/world writable: $path"
    return 1
  }
}

validate_trusted_toolchain_security() {
  local state tool_dir entrypoint tool_parent entrypoint_parent path file
  local expected_entrypoint actual_entrypoint

  state=$(rg_update_toolchain_state) || return 1
  tool_dir=$(rg_update_path "$RG_UPDATE_TOOLCHAIN_DIR") || return 1
  entrypoint=$(rg_update_path "$RG_UPDATE_ENTRYPOINT") || return 1
  tool_parent=$(rg_update_path /usr/local/lib/routegate) || return 1
  entrypoint_parent=$(dirname "$entrypoint") || return 1

  for path in "$tool_parent" "$entrypoint_parent"; do
    if [[ -e "$path" || -L "$path" ]]; then
      [[ -d "$path" && ! -L "$path" ]] || {
        rg_update_die "trusted updater parent path is unsafe: $path"
        return 1
      }
      trusted_path_is_secure "$path" "trusted updater parent" || return 1
    fi
  done

  if [[ "$state" == "absent" ]]; then
    return 0
  fi

  trusted_path_is_secure "$tool_dir" "trusted updater directory" || return 1
  trusted_path_is_secure "$entrypoint" "trusted updater entrypoint" || return 1
  while IFS= read -r file; do
    trusted_path_is_secure "$tool_dir/$file" "trusted updater component" || return 1
  done < <(rg_update_toolchain_files)

  [[ -x "$tool_dir/routegate-update-transaction.sh" && -x "$tool_dir/routegate-update-verified.sh" ]] || {
    rg_update_die "trusted updater executable components are not executable"
    return 1
  }

  expected_entrypoint=$'#!/usr/bin/env bash\nset -Eeuo pipefail\nexec /usr/local/lib/routegate/update/routegate-update-verified.sh "$@"'
  actual_entrypoint=$(cat -- "$entrypoint") || return 1
  [[ "$actual_entrypoint" == "$expected_entrypoint" ]] || {
    rg_update_die "trusted updater entrypoint content is not canonical"
    return 1
  }
}

rollback() {
  local rc=${1:-$?}
  local platform_rollback_rc=0
  local toolchain_rollback_rc=0
  trap - ERR INT TERM
  set +e

  if [[ "$MUTATED" == "1" && -n "$BACKUP_DIR" && -n "$ROLE" ]]; then
    log "failure stage=$STAGE; starting role-aware rollback"

    rg_update_restore_role_backup "$ROLE" "$BACKUP_DIR" "$DB_URL" "$DB_MAY_BE_MUTATED" \
      || platform_rollback_rc=$?
    rg_update_restore_toolchain_backup "$BACKUP_DIR" \
      || toolchain_rollback_rc=$?
    if ((toolchain_rollback_rc == 0)); then
      validate_trusted_toolchain_security || toolchain_rollback_rc=$?
    fi

    if ((platform_rollback_rc != 0)); then
      printf '[routegate-update-transaction] WARNING: platform rollback reported exit %d; backup=%s\n' \
        "$platform_rollback_rc" "$BACKUP_DIR" >&2
    fi
    if ((toolchain_rollback_rc != 0)); then
      printf '[routegate-update-transaction] WARNING: trusted updater rollback reported exit %d; backup=%s\n' \
        "$toolchain_rollback_rc" "$BACKUP_DIR" >&2
    fi
    if ((RG_UPDATE_DB_RESTORE_RC != 0)); then
      printf '[routegate-update-transaction] WARNING: database restore reported exit %d; backup=%s\n' \
        "$RG_UPDATE_DB_RESTORE_RC" "$BACKUP_DIR" >&2
    fi
  fi

  cleanup
  exit "$rc"
}

acquire_lock() {
  install -d -m 0755 "$(dirname "$LOCK_FILE")"
  exec 9>"$LOCK_FILE"
  flock -n 9 || die "another RouteGate host update transaction is already running"
}

require_role_commands() {
  local role=$1
  rg_update_require_commands \
    awk \
    bash \
    basename \
    cat \
    chmod \
    cp \
    date \
    dirname \
    find \
    flock \
    grep \
    head \
    install \
    mktemp \
    python3 \
    rm \
    sed \
    sha256sum \
    sleep \
    stat \
    tar \
    systemctl \
    uname \
    wc || return 1
  if rg_update_role_has_management "$role"; then
    rg_update_require_commands chown curl pg_dump pg_restore psql || return 1
  fi
}

main() {
  if [[ "$OPERATION" == "--help" || "$OPERATION" == "-h" || -z "$OPERATION" ]]; then
    usage
    if [[ -n "$OPERATION" ]]; then
      exit 0
    fi
    exit 2
  fi
  [[ "$OPERATION" == "apply" ]] || die "unsupported operation: $OPERATION"

  parse_args "$@"
  rg_update_require_root || exit 1
  rg_update_require_commands flock install uname || exit 1
  acquire_lock
  trap cleanup EXIT

  TARGET_ARCH=$(platform_architecture "$(uname -m)") || die "unsupported host architecture: $(uname -m)"
  ROLE=$(rg_update_resolve_role "$REQUESTED_ROLE") || exit 1
  require_role_commands "$ROLE" || exit 1
  log "resolved role=$ROLE arch=$TARGET_ARCH"

  STAGE=toolchain_preflight
  validate_trusted_toolchain_security

  STAGE=preflight
  rg_update_role_preflight "$ROLE"

  WORK_DIR=$(mktemp -d /tmp/routegate-update.XXXXXX)
  STAGE=bundle_verification
  rg_update_verify_and_extract_bundle \
    "$BUNDLE" \
    "$EXPECTED_SHA" \
    "$EXPECTED_COMMIT" \
    linux \
    "$TARGET_ARCH" \
    "$WORK_DIR"

  if rg_update_role_has_management "$ROLE"; then
    DB_URL=$(rg_update_read_manager_database_url)
  fi

  STAGE=backup
  BACKUP_DIR="${BACKUP_ROOT%/}/update-${ROLE}-${EXPECTED_COMMIT}-$(date -u +%Y%m%dT%H%M%SZ)"
  rg_update_create_role_backup "$ROLE" "$BACKUP_DIR" "$DB_URL"
  rg_update_create_toolchain_backup "$BACKUP_DIR"
  trap rollback ERR
  trap 'rollback 130' INT
  trap 'rollback 143' TERM

  STAGE=apply
  MUTATED=1
  rg_update_apply_role_files "$ROLE" "$WORK_DIR"

  STAGE=health
  if rg_update_role_has_management "$ROLE"; then
    DB_MAY_BE_MUTATED=1
  fi
  rg_update_start_and_validate_role "$ROLE" "$DB_URL" "$RG_UPDATE_EXPECTED_SCHEMA"

  STAGE=toolchain_promotion
  rg_update_apply_toolchain "$WORK_DIR"

  STAGE=toolchain_validation
  validate_trusted_toolchain_security
  rg_update_validate_toolchain

  STAGE=complete
  trap - ERR INT TERM
  log "status=completed role=$ROLE version=$RG_UPDATE_BUNDLE_VERSION commit=$RG_UPDATE_BUNDLE_COMMIT schema=${RG_UPDATE_EXPECTED_SCHEMA:-none} backup=$BACKUP_DIR"
}

main "$@"
