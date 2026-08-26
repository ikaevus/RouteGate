#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'
umask 077

STAGING_ROOT=/var/lib/routegate-agent/update-staging
VERIFIED_UPDATER=/usr/local/lib/routegate/update/routegate-update-verified.sh
TASK_ID=${1:-}

log() {
  printf '[routegate-vpn-update-worker] %s\n' "$*"
}

die() {
  printf '[routegate-vpn-update-worker] ERROR: %s\n' "$*" >&2
  exit 1
}

trusted_path_is_secure() {
  local path=$1 label=$2 owner permissions
  owner=$(stat -c '%u' -- "$path") || return 1
  permissions=$(stat -c '%A' -- "$path") || return 1
  [[ "$owner" == "0" ]] || die "$label is not root-owned"
  [[ "${permissions:5:1}" != "w" && "${permissions:8:1}" != "w" ]] || die "$label is group/world writable"
}

[[ $# -eq 1 ]] || die "exactly one canonical task UUID is required"
[[ "$TASK_ID" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$ ]] \
  || die "task id must be canonical lowercase UUIDv4"
[[ ${EUID:-$(id -u)} -eq 0 ]] || die "VPN update worker must run as root"

[[ -d "$STAGING_ROOT" && ! -L "$STAGING_ROOT" ]] || die "staging root is missing or unsafe"
trusted_path_is_secure "$STAGING_ROOT" "staging root"

stage_dir="$STAGING_ROOT/$TASK_ID"
[[ -d "$stage_dir" && ! -L "$stage_dir" ]] || die "staged candidate directory is missing or unsafe"
trusted_path_is_secure "$stage_dir" "staged candidate directory"

required=(
  release-manifest.json
  release-manifest.attestation.json
  SHA256SUMS
  release-bundles.attestation.json
)

for name in "${required[@]}"; do
  path="$stage_dir/$name"
  [[ -f "$path" && ! -L "$path" ]] || die "staged candidate is missing required regular file: $name"
  trusted_path_is_secure "$path" "staged candidate file $name"
done

bundle=""
unexpected=0
while IFS= read -r entry; do
  name=$(basename -- "$entry")
  case "$name" in
    release-manifest.json|release-manifest.attestation.json|SHA256SUMS|release-bundles.attestation.json) ;;
    routegate-*-linux-amd64.tar.gz|routegate-*-linux-arm64.tar.gz)
      [[ -z "$bundle" ]] || die "staged candidate contains multiple release bundles"
      [[ -f "$entry" && ! -L "$entry" ]] || die "staged release bundle is not a regular file"
      trusted_path_is_secure "$entry" "staged release bundle"
      bundle=$entry
      ;;
    *) unexpected=1 ;;
  esac
done < <(find "$stage_dir" -mindepth 1 -maxdepth 1 -print)

((unexpected == 0)) || die "staged candidate contains unexpected entries"
[[ -n "$bundle" ]] || die "staged candidate release bundle is missing"
[[ -f "$VERIFIED_UPDATER" && ! -L "$VERIFIED_UPDATER" && -x "$VERIFIED_UPDATER" ]] \
  || die "trusted verified updater is missing or unsafe"
trusted_path_is_secure "$VERIFIED_UPDATER" "trusted verified updater"

log "starting fixed-policy verified VPN transaction for task $TASK_ID"
exec "$VERIFIED_UPDATER" apply \
  --manifest "$stage_dir/release-manifest.json" \
  --manifest-attestation "$stage_dir/release-manifest.attestation.json" \
  --checksums "$stage_dir/SHA256SUMS" \
  --bundle "$bundle" \
  --bundle-attestation "$stage_dir/release-bundles.attestation.json" \
  --role vpn
