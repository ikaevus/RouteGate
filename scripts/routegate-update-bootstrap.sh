#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'
umask 077

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
BUNDLE_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)

# The bootstrap helper is shipped inside the release bundle next to the update
# core. Fresh installers already trust the bundle according to their installer
# release policy; this helper only establishes the fixed local updater layout.
# shellcheck source=scripts/routegate-update-core.sh
source "$SCRIPT_DIR/routegate-update-core.sh"

RG_UPDATE_LOG_PREFIX='[routegate-update-bootstrap]'

log() {
  rg_update_log "$*"
}

die() {
  rg_update_die "$*"
  exit 1
}

require_commands() {
  rg_update_require_commands \
    bash \
    basename \
    cat \
    chmod \
    cp \
    dirname \
    find \
    grep \
    head \
    install \
    python3 \
    rm \
    sed \
    stat || return 1
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

validate_parent_security() {
  local tool_parent entrypoint entrypoint_parent path
  tool_parent=$(rg_update_path /usr/local/lib/routegate) || return 1
  entrypoint=$(rg_update_path "$RG_UPDATE_ENTRYPOINT") || return 1
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
}

validate_installed_security() {
  local state tool_dir entrypoint tool_parent entrypoint_parent path file
  local expected_entrypoint actual_entrypoint

  state=$(rg_update_toolchain_state) || return 1
  [[ "$state" == "complete" ]] || {
    rg_update_die "trusted updater bootstrap did not produce a complete state"
    return 1
  }

  validate_parent_security || return 1
  tool_dir=$(rg_update_path "$RG_UPDATE_TOOLCHAIN_DIR") || return 1
  entrypoint=$(rg_update_path "$RG_UPDATE_ENTRYPOINT") || return 1
  tool_parent=$(rg_update_path /usr/local/lib/routegate) || return 1
  entrypoint_parent=$(dirname "$entrypoint") || return 1

  for path in "$tool_parent" "$entrypoint_parent"; do
    [[ -d "$path" && ! -L "$path" ]] || {
      rg_update_die "trusted updater parent path is unsafe: $path"
      return 1
    }
  done

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

remove_bootstrap_state() {
  local tool_dir entrypoint
  tool_dir=$(rg_update_path "$RG_UPDATE_TOOLCHAIN_DIR") || return 1
  entrypoint=$(rg_update_path "$RG_UPDATE_ENTRYPOINT") || return 1
  rm -rf -- "$tool_dir"
  rm -f -- "$entrypoint"
}

main() {
  rg_update_require_root || exit 1
  require_commands || exit 1
  rg_update_candidate_toolchain_complete "$BUNDLE_ROOT" || exit 1
  validate_parent_security || exit 1

  local state
  state=$(rg_update_toolchain_state) || exit 1
  case "$state" in
    complete)
      validate_installed_security || exit 1
      rg_update_validate_toolchain || exit 1
      log "existing trusted updater preserved"
      ;;
    absent)
      if ! rg_update_apply_toolchain "$BUNDLE_ROOT"; then
        remove_bootstrap_state >/dev/null 2>&1 || true
        die "trusted updater bootstrap failed during installation"
      fi
      if ! validate_installed_security || ! rg_update_validate_toolchain; then
        remove_bootstrap_state >/dev/null 2>&1 || true
        die "trusted updater bootstrap failed validation"
      fi
      log "trusted updater bootstrap installed"
      ;;
    *)
      die "unexpected trusted updater state: $state"
      ;;
  esac

  if command -v gh >/dev/null 2>&1 \
    && gh attestation verify --help 2>/dev/null | grep -Fq -- '--predicate-type'; then
    log "attestation verifier runtime is available"
  else
    log "attestation verifier runtime is not bundled yet; update execution remains unavailable until a compatible verifier is installed"
  fi
}

main "$@"
