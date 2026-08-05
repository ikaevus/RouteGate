#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'
umask 022

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
OUTPUT_DIR="${OUTPUT_DIR:-${ROOT_DIR}/dist}"
VERSION="${VERSION:-}"
COMMIT="${COMMIT:-}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
ARCHITECTURES="${ARCHITECTURES:-amd64 arm64}"

log() {
  printf '[RouteGate release] %s\n' "$*"
}

die() {
  printf '[RouteGate release] ERROR: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required."
}

validate_version() {
  [[ -n "$VERSION" ]] || die "VERSION is required."
  [[ "$VERSION" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]] \
    || die "VERSION contains unsupported characters: $VERSION"
}

prepare_metadata() {
  if [[ -z "$COMMIT" ]]; then
    COMMIT=$(git -C "$ROOT_DIR" rev-parse HEAD)
  fi
  [[ "$COMMIT" =~ ^[a-f0-9]{40}$ ]] || die "COMMIT must be a full Git SHA."
}

build_frontend() {
  log "Building production frontend."
  (
    cd "$ROOT_DIR/frontend"
    npm ci
    npm run i18n:check
    npm run build
  )
}

build_architecture() {
  local arch=$1
  local stage_dir="$OUTPUT_DIR/stage-${arch}"
  local package="$OUTPUT_DIR/routegate-${VERSION}-linux-${arch}.tar.gz"

  case "$arch" in
    amd64|arm64) ;;
    *) die "Unsupported release architecture: $arch" ;;
  esac

  rm -rf "$stage_dir"
  mkdir -p \
    "$stage_dir/bin" \
    "$stage_dir/manager" \
    "$stage_dir/frontend" \
    "$stage_dir/systemd" \
    "$stage_dir/nginx" \
    "$stage_dir/metadata"

  log "Building Manager for linux/${arch}."
  (
    cd "$ROOT_DIR/backend"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build \
      -trimpath \
      -ldflags "-s -w -X github.com/ikaevus/routegate/backend/internal/buildinfo.Version=${VERSION} -X github.com/ikaevus/routegate/backend/internal/buildinfo.GitCommit=${COMMIT} -X github.com/ikaevus/routegate/backend/internal/buildinfo.BuildDate=${BUILD_DATE}" \
      -o "$stage_dir/bin/routegate-manager" \
      ./cmd/routegate-manager
  )

  log "Building Agent for linux/${arch}."
  (
    cd "$ROOT_DIR/agent"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build \
      -trimpath \
      -ldflags "-s -w -X github.com/ikaevus/routegate/agent/internal/buildinfo.Version=${VERSION} -X github.com/ikaevus/routegate/agent/internal/buildinfo.GitCommit=${COMMIT} -X github.com/ikaevus/routegate/agent/internal/buildinfo.BuildDate=${BUILD_DATE}" \
      -o "$stage_dir/bin/routegate-agent" \
      ./cmd/routegate-agent
  )

  cp -a "$ROOT_DIR/backend/migrations" "$stage_dir/manager/migrations"
  cp -a "$ROOT_DIR/frontend/dist/." "$stage_dir/frontend/"
  cp "$ROOT_DIR/deploy/systemd/routegate-manager.service" "$stage_dir/systemd/"
  cp "$ROOT_DIR/deploy/systemd/routegate-agent.service" "$stage_dir/systemd/"
  cp "$ROOT_DIR/deploy/nginx/routegate.conf.example" "$stage_dir/nginx/"

  cat >"$stage_dir/metadata/manifest.env" <<EOF_MANIFEST
FORMAT_VERSION=1
VERSION=${VERSION}
COMMIT=${COMMIT}
BUILD_DATE=${BUILD_DATE}
OS=linux
ARCH=${arch}
EOF_MANIFEST

  if [[ -f "$ROOT_DIR/LICENSE" ]]; then
    cp "$ROOT_DIR/LICENSE" "$stage_dir/metadata/LICENSE"
  fi
  if [[ -f "$ROOT_DIR/NOTICE" ]]; then
    cp "$ROOT_DIR/NOTICE" "$stage_dir/metadata/NOTICE"
  fi

  chmod 0755 "$stage_dir/bin/routegate-manager" "$stage_dir/bin/routegate-agent"
  find "$stage_dir" -type d -exec chmod 0755 {} +
  find "$stage_dir" -type f ! -path '*/bin/*' -exec chmod 0644 {} +

  tar -C "$stage_dir" \
    --sort=name \
    --owner=0 \
    --group=0 \
    --numeric-owner \
    --mtime="@$SOURCE_DATE_EPOCH" \
    -czf "$package" .

  tar -tzf "$package" >/dev/null
  log "Created $(basename "$package")."
}

main() {
  require_command git
  require_command go
  require_command npm
  require_command tar
  require_command sha256sum
  validate_version
  prepare_metadata

  if [[ -z "${SOURCE_DATE_EPOCH:-}" ]]; then
    SOURCE_DATE_EPOCH=$(git -C "$ROOT_DIR" show -s --format=%ct "$COMMIT")
    export SOURCE_DATE_EPOCH
  fi
  [[ "$SOURCE_DATE_EPOCH" =~ ^[0-9]+$ ]] || die "SOURCE_DATE_EPOCH must be numeric."

  rm -rf "$OUTPUT_DIR"
  mkdir -p "$OUTPUT_DIR"

  build_frontend

  local arch
  local architectures=()
  IFS=' ' read -r -a architectures <<<"$ARCHITECTURES"
  for arch in "${architectures[@]}"; do
    [[ -n "$arch" ]] || continue
    build_architecture "$arch"
  done

  (
    cd "$OUTPUT_DIR"
    sha256sum routegate-*.tar.gz >SHA256SUMS
  )

  rm -rf "$OUTPUT_DIR"/stage-*
  log "Release bundles and SHA256SUMS are ready in ${OUTPUT_DIR}."
}

main "$@"
