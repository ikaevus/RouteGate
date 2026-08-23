#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'
umask 077

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
TRUST_REPOSITORY="ikaevus/RouteGate"
TRUST_SIGNER_WORKFLOW="ikaevus/RouteGate/.github/workflows/release.yml"
TRUST_PREDICATE_TYPE="https://slsa.dev/provenance/v1"
TARGET_OS="linux"

OPERATION=${1:-}
if [[ -n "$OPERATION" ]]; then
  shift
fi

MANIFEST=""
MANIFEST_ATTESTATION=""
CHECKSUMS=""
BUNDLE=""
BUNDLE_ATTESTATION=""
REQUESTED_ROLE="auto"
WORK_DIR=""

log() {
  printf '[routegate-verified-update] %s\n' "$*"
}

die() {
  printf '[routegate-verified-update] ERROR: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'USAGE'
RouteGate verified local update gate

Usage:
  sudo routegate-update-verified.sh apply \
    --manifest /path/to/release-manifest.json \
    --manifest-attestation /path/to/release-manifest.attestation.json \
    --checksums /path/to/SHA256SUMS \
    --bundle /path/to/routegate-<version>-linux-<arch>.tar.gz \
    --bundle-attestation /path/to/release-bundles.attestation.json \
    [--role auto|management|vpn|hybrid]

Trust policy is fixed in this executable:
  repository: ikaevus/RouteGate
  signer workflow: ikaevus/RouteGate/.github/workflows/release.yml
  predicate type: https://slsa.dev/provenance/v1

The command performs no release discovery or artifact download. It snapshots the
provided release files into a root-only temporary area, verifies those exact
copies, derives the target commit and bundle digest from the verified contract,
and only then invokes the privileged role-aware host transaction.
USAGE
}

cleanup() {
  if [[ -n "$WORK_DIR" && -d "$WORK_DIR" ]]; then
    rm -rf "$WORK_DIR"
  fi
}

require_root() {
  [[ ${EUID:-$(id -u)} -eq 0 ]] || die "verified update operations must run as root"
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

require_regular_file() {
  local path=$1
  local label=$2
  [[ -f "$path" && ! -L "$path" && -r "$path" ]] || die "$label must be a readable regular file"
}

parse_args() {
  while (($# > 0)); do
    case "$1" in
      --manifest)
        (($# >= 2)) || die "--manifest requires a value"
        MANIFEST=$2
        shift 2
        ;;
      --manifest-attestation)
        (($# >= 2)) || die "--manifest-attestation requires a value"
        MANIFEST_ATTESTATION=$2
        shift 2
        ;;
      --checksums)
        (($# >= 2)) || die "--checksums requires a value"
        CHECKSUMS=$2
        shift 2
        ;;
      --bundle)
        (($# >= 2)) || die "--bundle requires a value"
        BUNDLE=$2
        shift 2
        ;;
      --bundle-attestation)
        (($# >= 2)) || die "--bundle-attestation requires a value"
        BUNDLE_ATTESTATION=$2
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

  [[ -n "$MANIFEST" ]] || die "--manifest is required"
  [[ -n "$MANIFEST_ATTESTATION" ]] || die "--manifest-attestation is required"
  [[ -n "$CHECKSUMS" ]] || die "--checksums is required"
  [[ -n "$BUNDLE" ]] || die "--bundle is required"
  [[ -n "$BUNDLE_ATTESTATION" ]] || die "--bundle-attestation is required"
  case "$REQUESTED_ROLE" in
    auto|management|vpn|hybrid) ;;
    *) die "unsupported node role: $REQUESTED_ROLE" ;;
  esac
}

platform_architecture() {
  case "$1" in
    x86_64|amd64) printf 'amd64\n' ;;
    aarch64|arm64) printf 'arm64\n' ;;
    *) return 1 ;;
  esac
}

verify_attestation() {
  local subject=$1
  local attestation=$2
  gh attestation verify "$subject" \
    --repo "$TRUST_REPOSITORY" \
    --signer-workflow "$TRUST_SIGNER_WORKFLOW" \
    --predicate-type "$TRUST_PREDICATE_TYPE" \
    --bundle "$attestation" >/dev/null
}

json_field() {
  local descriptor=$1
  local field=$2
  python3 - "$descriptor" "$field" <<'PY'
import json
import sys

path, field = sys.argv[1:]
with open(path, encoding="utf-8") as handle:
    value = json.load(handle)
for part in field.split("."):
    if not isinstance(value, dict) or part not in value:
        raise SystemExit(2)
    value = value[part]
if not isinstance(value, (str, int)) or isinstance(value, bool):
    raise SystemExit(2)
print(value)
PY
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
  require_root
  require_command gh
  require_command python3
  require_command uname

  local manifest_verifier="$SCRIPT_DIR/release_manifest.py"
  local transaction="$SCRIPT_DIR/routegate-update-transaction.sh"
  require_regular_file "$manifest_verifier" "release manifest verifier"
  require_regular_file "$transaction" "role-aware update transaction"
  [[ -x "$transaction" ]] || die "role-aware update transaction is not executable"

  require_regular_file "$MANIFEST" "release manifest"
  require_regular_file "$MANIFEST_ATTESTATION" "manifest attestation bundle"
  require_regular_file "$CHECKSUMS" "SHA256SUMS"
  require_regular_file "$BUNDLE" "release bundle"
  require_regular_file "$BUNDLE_ATTESTATION" "bundle attestation bundle"

  local arch bundle_name
  arch=$(platform_architecture "$(uname -m)") || die "unsupported host architecture: $(uname -m)"
  bundle_name=$(basename "$BUNDLE")
  [[ "$bundle_name" =~ ^routegate-[A-Za-z0-9][A-Za-z0-9._+-]*-linux-(amd64|arm64)\.tar\.gz$ ]] \
    || die "release bundle name is not canonical"

  WORK_DIR=$(mktemp -d /tmp/routegate-verified-update.XXXXXX)
  trap cleanup EXIT
  local artifacts_dir="$WORK_DIR/artifacts"
  local frozen_manifest="$WORK_DIR/release-manifest.json"
  local frozen_manifest_attestation="$WORK_DIR/release-manifest.attestation.json"
  local frozen_bundle_attestation="$WORK_DIR/release-bundles.attestation.json"
  local frozen_bundle="$artifacts_dir/$bundle_name"
  local descriptor="$WORK_DIR/target.json"

  mkdir -m 0700 "$artifacts_dir"
  cp "$MANIFEST" "$frozen_manifest"
  cp "$MANIFEST_ATTESTATION" "$frozen_manifest_attestation"
  cp "$CHECKSUMS" "$artifacts_dir/SHA256SUMS"
  cp "$BUNDLE" "$frozen_bundle"
  cp "$BUNDLE_ATTESTATION" "$frozen_bundle_attestation"
  chmod 0600 \
    "$frozen_manifest" \
    "$frozen_manifest_attestation" \
    "$artifacts_dir/SHA256SUMS" \
    "$frozen_bundle" \
    "$frozen_bundle_attestation"

  log "verifying release manifest provenance"
  verify_attestation "$frozen_manifest" "$frozen_manifest_attestation" \
    || die "release manifest provenance verification failed"

  log "verifying release manifest contract for ${TARGET_OS}/${arch}"
  python3 "$manifest_verifier" verify-target \
    --manifest "$frozen_manifest" \
    --artifacts-dir "$artifacts_dir" \
    --os "$TARGET_OS" \
    --arch "$arch" >"$descriptor" \
    || die "release manifest or target artifact verification failed"

  local verified_name verified_sha verified_commit verified_version
  verified_name=$(json_field "$descriptor" artifact.name) || die "verified descriptor is missing artifact.name"
  verified_sha=$(json_field "$descriptor" artifact.sha256) || die "verified descriptor is missing artifact.sha256"
  verified_commit=$(json_field "$descriptor" commit) || die "verified descriptor is missing commit"
  verified_version=$(json_field "$descriptor" version) || die "verified descriptor is missing version"
  [[ "$bundle_name" == "$verified_name" ]] || die "provided bundle name does not match the verified target artifact"

  log "verifying target bundle provenance"
  verify_attestation "$frozen_bundle" "$frozen_bundle_attestation" \
    || die "release bundle provenance verification failed"

  log "trusted release version=${verified_version} commit=${verified_commit} arch=${arch}; starting host transaction"
  "$transaction" apply \
    --bundle "$frozen_bundle" \
    --sha256 "$verified_sha" \
    --commit "$verified_commit" \
    --role "$REQUESTED_ROLE"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
