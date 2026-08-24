#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'
umask 077

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
TRUST_REPOSITORY="ikaevus/RouteGate"
TRUST_SIGNER_WORKFLOW="ikaevus/RouteGate/.github/workflows/release.yml"
TRUST_PREDICATE_TYPE="https://slsa.dev/provenance/v1"
TARGET_OS="linux"

GH_VERIFIER_VERSION="2.97.0"
GH_VERIFIER_PARENT="/usr/local/lib/routegate"
GH_VERIFIER_DIR="${GH_VERIFIER_PARENT}/verifier"
GH_VERIFIER="${GH_VERIFIER_DIR}/gh"
GH_VERIFIER_METADATA="${GH_VERIFIER_DIR}/runtime.env"
GH_VERIFIER_RELEASE_BASE="https://github.com/cli/cli/releases/download/v${GH_VERIFIER_VERSION}"
GH_VERIFIER_SHA_AMD64="a2c9b8497e1f85b1ad0dfcb78b5a622e098801b8e461e459e88e1ee12f018112"
GH_VERIFIER_SHA_ARM64="73ea440ecad9c9e284429997ee6f93577bc6f7bc6fba357ef62c53ad8fb641a5"

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
VERIFIED_DESCRIPTOR=""
VERIFIED_BUNDLE=""
VERIFIED_BUNDLE_NAME=""
VERIFIED_SHA=""
VERIFIED_COMMIT=""
VERIFIED_VERSION=""
VERIFIED_ARCH=""

log() {
  printf '[routegate-verified-update] %s\n' "$*"
}

verification_log() {
  if [[ "$OPERATION" == "verify" ]]; then
    printf '[routegate-verified-update] %s\n' "$*" >&2
  else
    log "$@"
  fi
}

die() {
  printf '[routegate-verified-update] ERROR: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'USAGE'
RouteGate verified local update gate

Usage:
  sudo routegate-update-verified.sh install-verifier

  routegate-update-verified.sh verify \
    --manifest /path/to/release-manifest.json \
    --manifest-attestation /path/to/release-manifest.attestation.json \
    --checksums /path/to/SHA256SUMS \
    --bundle /path/to/routegate-<version>-linux-<arch>.tar.gz \
    --bundle-attestation /path/to/release-bundles.attestation.json

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

The verify command performs no release discovery, artifact download, or host
mutation. It snapshots the provided release files into a private temporary area,
verifies those exact copies with the fixed RouteGate-owned attestation verifier,
and writes only the verified target JSON descriptor to stdout. Diagnostics go
to stderr.

The apply command performs no release discovery or RouteGate artifact download.
It repeats the same frozen-copy verification immediately before invoking the
privileged role-aware host transaction.

The install-verifier command installs one pinned GitHub CLI release from its
fixed upstream URL after checking the hard-coded SHA-256 for this architecture.
USAGE
}

cleanup() {
  if [[ -n "$WORK_DIR" && -d "$WORK_DIR" ]]; then
    rm -rf -- "$WORK_DIR"
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

parse_release_args() {
  local allow_role=$1
  shift

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
        [[ "$allow_role" == "1" ]] || die "--role is only supported by apply"
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

verifier_expected_sha() {
  case "$1" in
    amd64) printf '%s\n' "$GH_VERIFIER_SHA_AMD64" ;;
    arm64) printf '%s\n' "$GH_VERIFIER_SHA_ARM64" ;;
    *) return 1 ;;
  esac
}

trusted_path_is_secure() {
  local path=$1
  local label=$2
  local owner permissions

  owner=$(stat -c '%u' -- "$path") || return 1
  permissions=$(stat -c '%A' -- "$path") || return 1
  [[ "$owner" == "0" ]] || {
    printf '[routegate-verified-update] ERROR: %s is not root-owned: %s\n' "$label" "$path" >&2
    return 1
  }
  [[ "${permissions:5:1}" != "w" && "${permissions:8:1}" != "w" ]] || {
    printf '[routegate-verified-update] ERROR: %s is group/world writable: %s\n' "$label" "$path" >&2
    return 1
  }
}

validate_verifier_parent() {
  [[ -d "$GH_VERIFIER_PARENT" && ! -L "$GH_VERIFIER_PARENT" ]] || {
    printf '[routegate-verified-update] ERROR: verifier parent is missing or unsafe: %s\n' "$GH_VERIFIER_PARENT" >&2
    return 1
  }
  trusted_path_is_secure "$GH_VERIFIER_PARENT" "verifier parent" || return 1
}

verifier_supports_policy() {
  local path=$1
  local version_line
  version_line=$("$path" --version 2>/dev/null | head -n1) || return 1
  [[ "$version_line" == "gh version ${GH_VERIFIER_VERSION} "* ]] || return 1
  "$path" attestation verify --help 2>/dev/null | grep -Fq -- '--predicate-type'
}

validate_attestation_verifier() {
  local arch expected_archive_sha expected_source actual_binary_sha path unexpected=0
  local meta_format="" meta_version="" meta_arch="" meta_archive_sha=""
  local meta_binary_sha="" meta_source="" key value

  arch=$(platform_architecture "$(uname -m)") || {
    printf '[routegate-verified-update] ERROR: unsupported host architecture for attestation verifier\n' >&2
    return 1
  }
  expected_archive_sha=$(verifier_expected_sha "$arch") || return 1
  expected_source="${GH_VERIFIER_RELEASE_BASE}/gh_${GH_VERIFIER_VERSION}_linux_${arch}.tar.gz"

  validate_verifier_parent || return 1
  [[ -d "$GH_VERIFIER_DIR" && ! -L "$GH_VERIFIER_DIR" ]] || {
    printf '[routegate-verified-update] ERROR: verifier directory is missing or unsafe: %s\n' "$GH_VERIFIER_DIR" >&2
    return 1
  }
  trusted_path_is_secure "$GH_VERIFIER_DIR" "verifier directory" || return 1
  [[ -f "$GH_VERIFIER" && ! -L "$GH_VERIFIER" && -x "$GH_VERIFIER" ]] || {
    printf '[routegate-verified-update] ERROR: pinned attestation verifier is missing or unsafe: %s\n' "$GH_VERIFIER" >&2
    return 1
  }
  [[ -f "$GH_VERIFIER_METADATA" && ! -L "$GH_VERIFIER_METADATA" ]] || {
    printf '[routegate-verified-update] ERROR: verifier metadata is missing or unsafe: %s\n' "$GH_VERIFIER_METADATA" >&2
    return 1
  }
  trusted_path_is_secure "$GH_VERIFIER" "attestation verifier" || return 1
  trusted_path_is_secure "$GH_VERIFIER_METADATA" "verifier metadata" || return 1

  while IFS= read -r path; do
    case "$(basename -- "$path")" in
      gh|runtime.env) ;;
      *) unexpected=1 ;;
    esac
  done < <(find "$GH_VERIFIER_DIR" -mindepth 1 -maxdepth 1 -print)
  ((unexpected == 0)) || {
    printf '[routegate-verified-update] ERROR: verifier directory contains unexpected entries\n' >&2
    return 1
  }

  while IFS='=' read -r key value || [[ -n "$key" ]]; do
    [[ -n "$key" ]] || continue
    case "$key" in
      FORMAT_VERSION)
        [[ -z "$meta_format" ]] || return 1
        meta_format=$value
        ;;
      VERSION)
        [[ -z "$meta_version" ]] || return 1
        meta_version=$value
        ;;
      ARCH)
        [[ -z "$meta_arch" ]] || return 1
        meta_arch=$value
        ;;
      ARCHIVE_SHA256)
        [[ -z "$meta_archive_sha" ]] || return 1
        meta_archive_sha=$value
        ;;
      BINARY_SHA256)
        [[ -z "$meta_binary_sha" ]] || return 1
        meta_binary_sha=$value
        ;;
      SOURCE_URL)
        [[ -z "$meta_source" ]] || return 1
        meta_source=$value
        ;;
      *)
        printf '[routegate-verified-update] ERROR: unsupported verifier metadata key: %s\n' "$key" >&2
        return 1
        ;;
    esac
  done <"$GH_VERIFIER_METADATA"

  [[ "$meta_format" == "1" ]] || return 1
  [[ "$meta_version" == "$GH_VERIFIER_VERSION" ]] || return 1
  [[ "$meta_arch" == "$arch" ]] || return 1
  [[ "$meta_archive_sha" == "$expected_archive_sha" ]] || return 1
  [[ "$meta_binary_sha" =~ ^[a-f0-9]{64}$ ]] || return 1
  [[ "$meta_source" == "$expected_source" ]] || return 1

  actual_binary_sha=$(sha256sum "$GH_VERIFIER" | awk '{print $1}') || return 1
  [[ "$actual_binary_sha" == "$meta_binary_sha" ]] || {
    printf '[routegate-verified-update] ERROR: pinned attestation verifier binary digest mismatch\n' >&2
    return 1
  }
  verifier_supports_policy "$GH_VERIFIER" || {
    printf '[routegate-verified-update] ERROR: pinned attestation verifier does not satisfy the required policy capability\n' >&2
    return 1
  }
}

validate_verifier_archive() {
  local archive=$1
  if tar -tzf "$archive" | awk '$0 ~ /^\// || $0 ~ /(^|\/)\.\.(\/|$)/ {found=1} END {exit !found}'; then
    printf '[routegate-verified-update] ERROR: verifier archive contains an unsafe path\n' >&2
    return 1
  fi
  if tar -tvzf "$archive" | awk '$1 ~ /^[lhbcp]/ {found=1} END {exit !found}'; then
    printf '[routegate-verified-update] ERROR: verifier archive contains a link or special filesystem entry\n' >&2
    return 1
  fi
}

install_verifier_archive() {
  local archive=$1
  local arch=$2
  local expected_sha=$3
  local source_url=$4
  local actual_sha extract_dir expected_binary binary_sha

  actual_sha=$(sha256sum "$archive" | awk '{print $1}') || return 1
  [[ "$actual_sha" == "$expected_sha" ]] || {
    printf '[routegate-verified-update] ERROR: downloaded verifier archive SHA-256 mismatch\n' >&2
    return 1
  }
  validate_verifier_archive "$archive" || return 1

  extract_dir="$WORK_DIR/extracted"
  install -d -m 0700 "$extract_dir" || return 1
  tar -xzf "$archive" -C "$extract_dir" --no-same-owner --no-same-permissions || return 1
  expected_binary="$extract_dir/gh_${GH_VERIFIER_VERSION}_linux_${arch}/bin/gh"
  [[ -f "$expected_binary" && ! -L "$expected_binary" ]] || {
    printf '[routegate-verified-update] ERROR: verifier archive does not contain the expected binary path\n' >&2
    return 1
  }
  [[ $(find "$extract_dir" -type f -path '*/bin/gh' | wc -l) -eq 1 ]] || {
    printf '[routegate-verified-update] ERROR: verifier archive contains an ambiguous gh binary layout\n' >&2
    return 1
  }
  chmod 0700 "$expected_binary" || return 1
  verifier_supports_policy "$expected_binary" || {
    printf '[routegate-verified-update] ERROR: downloaded verifier does not satisfy the required version/capability policy\n' >&2
    return 1
  }
  binary_sha=$(sha256sum "$expected_binary" | awk '{print $1}') || return 1

  install -d -m 0755 "$GH_VERIFIER_DIR" || return 1
  install -m 0755 "$expected_binary" "$GH_VERIFIER" || return 1
  install -m 0644 /dev/null "$GH_VERIFIER_METADATA" || return 1
  cat >"$GH_VERIFIER_METADATA" <<EOF_METADATA
FORMAT_VERSION=1
VERSION=${GH_VERIFIER_VERSION}
ARCH=${arch}
ARCHIVE_SHA256=${expected_sha}
BINARY_SHA256=${binary_sha}
SOURCE_URL=${source_url}
EOF_METADATA
  chmod 0644 "$GH_VERIFIER_METADATA" || return 1
  validate_attestation_verifier || return 1
}

install_attestation_verifier() {
  require_root
  require_command awk
  require_command basename
  require_command curl
  require_command find
  require_command grep
  require_command head
  require_command install
  require_command sha256sum
  require_command stat
  require_command tar
  require_command uname
  require_command wc

  validate_verifier_parent || die "trusted RouteGate verifier parent is unavailable"

  if [[ -e "$GH_VERIFIER_DIR" || -L "$GH_VERIFIER_DIR" || -e "$GH_VERIFIER" || -L "$GH_VERIFIER" || -e "$GH_VERIFIER_METADATA" || -L "$GH_VERIFIER_METADATA" ]]; then
    validate_attestation_verifier || die "existing attestation verifier state is not the pinned trusted runtime"
    log "pinned attestation verifier already installed: gh ${GH_VERIFIER_VERSION}"
    return 0
  fi

  local arch expected_sha asset url
  arch=$(platform_architecture "$(uname -m)") || die "unsupported host architecture: $(uname -m)"
  expected_sha=$(verifier_expected_sha "$arch") || die "no pinned verifier digest for architecture: $arch"
  asset="gh_${GH_VERIFIER_VERSION}_linux_${arch}.tar.gz"
  url="${GH_VERIFIER_RELEASE_BASE}/${asset}"

  WORK_DIR=$(mktemp -d /tmp/routegate-attestation-verifier.XXXXXX)
  trap cleanup EXIT
  local archive="$WORK_DIR/$asset"

  log "downloading pinned GitHub CLI ${GH_VERIFIER_VERSION} verifier for linux/${arch}"
  curl \
    --fail \
    --location \
    --retry 3 \
    --connect-timeout 15 \
    --max-time 300 \
    --proto '=https' \
    --proto-redir '=https' \
    --output "$archive" \
    "$url" || die "failed to download pinned attestation verifier"

  if ! install_verifier_archive "$archive" "$arch" "$expected_sha" "$url"; then
    rm -rf -- "$GH_VERIFIER_DIR"
    die "failed to install pinned attestation verifier"
  fi

  log "pinned attestation verifier installed: gh ${GH_VERIFIER_VERSION}"
}

verify_attestation() {
  local subject=$1
  local attestation=$2
  "$GH_VERIFIER" attestation verify "$subject" \
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

verify_release_candidate() {
  require_command awk
  require_command basename
  require_command chmod
  require_command cp
  require_command find
  require_command grep
  require_command head
  require_command mkdir
  require_command mktemp
  require_command python3
  require_command sha256sum
  require_command stat
  require_command uname
  validate_attestation_verifier || die "pinned attestation verifier is unavailable; run 'sudo routegate-update install-verifier'"

  local manifest_verifier="$SCRIPT_DIR/release_manifest.py"
  require_regular_file "$manifest_verifier" "release manifest verifier"

  require_regular_file "$MANIFEST" "release manifest"
  require_regular_file "$MANIFEST_ATTESTATION" "manifest attestation bundle"
  require_regular_file "$CHECKSUMS" "SHA256SUMS"
  require_regular_file "$BUNDLE" "release bundle"
  require_regular_file "$BUNDLE_ATTESTATION" "bundle attestation bundle"

  local arch bundle_name
  arch=$(platform_architecture "$(uname -m)") || die "unsupported host architecture: $(uname -m)"
  bundle_name=$(basename -- "$BUNDLE")
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
  cp -- "$MANIFEST" "$frozen_manifest"
  cp -- "$MANIFEST_ATTESTATION" "$frozen_manifest_attestation"
  cp -- "$CHECKSUMS" "$artifacts_dir/SHA256SUMS"
  cp -- "$BUNDLE" "$frozen_bundle"
  cp -- "$BUNDLE_ATTESTATION" "$frozen_bundle_attestation"
  chmod 0600 \
    "$frozen_manifest" \
    "$frozen_manifest_attestation" \
    "$artifacts_dir/SHA256SUMS" \
    "$frozen_bundle" \
    "$frozen_bundle_attestation"

  verification_log "verifying release manifest provenance"
  verify_attestation "$frozen_manifest" "$frozen_manifest_attestation" \
    || die "release manifest provenance verification failed"

  verification_log "verifying release manifest contract for ${TARGET_OS}/${arch}"
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

  verification_log "verifying target bundle provenance"
  verify_attestation "$frozen_bundle" "$frozen_bundle_attestation" \
    || die "release bundle provenance verification failed"

  VERIFIED_DESCRIPTOR=$descriptor
  VERIFIED_BUNDLE=$frozen_bundle
  VERIFIED_BUNDLE_NAME=$verified_name
  VERIFIED_SHA=$verified_sha
  VERIFIED_COMMIT=$verified_commit
  VERIFIED_VERSION=$verified_version
  VERIFIED_ARCH=$arch
}

verify_only() {
  parse_release_args 0 "$@"
  verify_release_candidate
  cat "$VERIFIED_DESCRIPTOR"
}

apply_verified_update() {
  parse_release_args 1 "$@"
  require_root

  local transaction="$SCRIPT_DIR/routegate-update-transaction.sh"
  require_regular_file "$transaction" "role-aware update transaction"
  [[ -x "$transaction" ]] || die "role-aware update transaction is not executable"

  verify_release_candidate

  log "trusted release version=${VERIFIED_VERSION} commit=${VERIFIED_COMMIT} arch=${VERIFIED_ARCH}; starting host transaction"
  "$transaction" apply \
    --bundle "$VERIFIED_BUNDLE" \
    --sha256 "$VERIFIED_SHA" \
    --commit "$VERIFIED_COMMIT" \
    --role "$REQUESTED_ROLE"
}

main() {
  if [[ "$OPERATION" == "--help" || "$OPERATION" == "-h" || -z "$OPERATION" ]]; then
    usage
    if [[ -n "$OPERATION" ]]; then
      exit 0
    fi
    exit 2
  fi

  case "$OPERATION" in
    install-verifier)
      (($# == 0)) || die "install-verifier does not accept arguments"
      install_attestation_verifier
      ;;
    verify)
      verify_only "$@"
      ;;
    apply)
      apply_verified_update "$@"
      ;;
    *)
      die "unsupported operation: $OPERATION"
      ;;
  esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
