#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'
umask 077

ROUTEGATE_REPOSITORY="${ROUTEGATE_REPOSITORY:-ikaevus/RouteGate}"
ROUTEGATE_VERSION="${ROUTEGATE_VERSION:-latest}"
ROUTEGATE_MANAGER_URL="${ROUTEGATE_MANAGER_URL:-}"
ROUTEGATE_REGISTRATION_TOKEN="${ROUTEGATE_REGISTRATION_TOKEN:-}"
ROUTEGATE_BUNDLE_FILE="${ROUTEGATE_BUNDLE_FILE:-}"
ROUTEGATE_CHECKSUM_FILE="${ROUTEGATE_CHECKSUM_FILE:-}"
ROUTEGATE_BUNDLE_URL="${ROUTEGATE_BUNDLE_URL:-}"
ROUTEGATE_CHECKSUM_URL="${ROUTEGATE_CHECKSUM_URL:-}"
ROUTEGATE_HYSTERIA_VERSION="${ROUTEGATE_HYSTERIA_VERSION:-2.12.1}"
ROUTEGATE_MTG_VERSION="${ROUTEGATE_MTG_VERSION:-2.2.8}"

ROUTEGATE_AGENT_CONFIG="${ROUTEGATE_AGENT_CONFIG:-/etc/routegate/agent.yaml}"
ROUTEGATE_AGENT_BINARY="${ROUTEGATE_AGENT_BINARY:-/usr/local/bin/routegate-agent}"
ROUTEGATE_AGENT_SERVICE="${ROUTEGATE_AGENT_SERVICE:-/etc/systemd/system/routegate-agent.service}"
ROUTEGATE_WORK_DIR=""
ROUTEGATE_ARCH=""
ROUTEGATE_RESOLVED_VERSION=""
ROUTEGATE_BUNDLE_NAME=""

usage() {
  cat <<'USAGE'
RouteGate VPN Node Agent Installer

Usage:
  sudo env ROUTEGATE_MANAGER_URL='https://manager.example' \
    ROUTEGATE_REGISTRATION_TOKEN='rg_reg_...' bash install-agent.sh

Options:
  --manager-url URL          Public HTTPS URL of RouteGate Manager.
  --registration-token TOKEN
                             Short-lived one-time token created by Manager.
  --version VERSION          Release tag to install. Defaults to latest.
  --bundle-file PATH         Use a local release bundle.
  --checksum-file PATH       SHA256SUMS file for --bundle-file.
  --bundle-url URL           Use an explicit release bundle URL.
  --checksum-url URL         SHA256SUMS URL for --bundle-url.
  --help                     Show this help.

Supported target: Ubuntu 24.04 LTS, amd64 or arm64, systemd.
USAGE
}

log() {
  printf '[RouteGate Agent] %s\n' "$*"
}

die() {
  printf '[RouteGate Agent] ERROR: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  if [[ -n "${ROUTEGATE_WORK_DIR:-}" && -d "$ROUTEGATE_WORK_DIR" ]]; then
    rm -rf "$ROUTEGATE_WORK_DIR"
  fi
}

parse_args() {
  while (($# > 0)); do
    case "$1" in
      --manager-url)
        (($# >= 2)) || die "--manager-url requires a value."
        ROUTEGATE_MANAGER_URL="$2"
        shift 2
        ;;
      --registration-token)
        (($# >= 2)) || die "--registration-token requires a value."
        ROUTEGATE_REGISTRATION_TOKEN="$2"
        shift 2
        ;;
      --version)
        (($# >= 2)) || die "--version requires a value."
        ROUTEGATE_VERSION="$2"
        shift 2
        ;;
      --bundle-file)
        (($# >= 2)) || die "--bundle-file requires a value."
        ROUTEGATE_BUNDLE_FILE="$2"
        shift 2
        ;;
      --checksum-file)
        (($# >= 2)) || die "--checksum-file requires a value."
        ROUTEGATE_CHECKSUM_FILE="$2"
        shift 2
        ;;
      --bundle-url)
        (($# >= 2)) || die "--bundle-url requires a value."
        ROUTEGATE_BUNDLE_URL="$2"
        shift 2
        ;;
      --checksum-url)
        (($# >= 2)) || die "--checksum-url requires a value."
        ROUTEGATE_CHECKSUM_URL="$2"
        shift 2
        ;;
      --help|-h)
        usage
        exit 0
        ;;
      *) die "Unknown option: $1" ;;
    esac
  done
}

validate_release_version() {
  [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]]
}

validate_manager_url() {
  local value=${1%/}
  [[ "$value" =~ ^https://[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?$ ]]
}

validate_registration_token() {
  [[ "$1" =~ ^rg_reg_[A-Za-z0-9_-]{43}$ ]]
}

platform_architecture() {
  case "$1" in
    x86_64|amd64) printf 'amd64\n' ;;
    aarch64|arm64) printf 'arm64\n' ;;
    *) return 1 ;;
  esac
}

platform_tuple_supported() {
  local os_id=$1
  local version_id=$2
  local arch=$3
  local systemd_running=$4
  [[ "$os_id" == "ubuntu" && "$version_id" == "24.04" && ("$arch" == "amd64" || "$arch" == "arm64") && "$systemd_running" == "1" ]]
}

validate_inputs() {
  ROUTEGATE_MANAGER_URL=${ROUTEGATE_MANAGER_URL%/}
  validate_manager_url "$ROUTEGATE_MANAGER_URL" || die "ROUTEGATE_MANAGER_URL must be a public HTTPS origin without a path, query, or fragment."
  validate_registration_token "$ROUTEGATE_REGISTRATION_TOKEN" || die "ROUTEGATE_REGISTRATION_TOKEN is invalid. Create a fresh token in RouteGate Manager."
  validate_release_version "$ROUTEGATE_VERSION" || die "ROUTEGATE_VERSION contains unsupported characters."

  if [[ -n "$ROUTEGATE_BUNDLE_FILE" || -n "$ROUTEGATE_CHECKSUM_FILE" ]]; then
    [[ -n "$ROUTEGATE_BUNDLE_FILE" && -n "$ROUTEGATE_CHECKSUM_FILE" ]] || die "--bundle-file and --checksum-file must be provided together."
  fi
  if [[ -n "$ROUTEGATE_BUNDLE_URL" || -n "$ROUTEGATE_CHECKSUM_URL" ]]; then
    [[ -n "$ROUTEGATE_BUNDLE_URL" && -n "$ROUTEGATE_CHECKSUM_URL" ]] || die "--bundle-url and --checksum-url must be provided together."
  fi
  [[ -z "$ROUTEGATE_BUNDLE_FILE" || -z "$ROUTEGATE_BUNDLE_URL" ]] || die "Choose either a local bundle or an explicit bundle URL."
}

require_supported_host() {
  [[ ${EUID:-$(id -u)} -eq 0 ]] || die "Run this installer through sudo or as root."
  [[ -r /etc/os-release ]] || die "/etc/os-release is required."
  # shellcheck disable=SC1091
  source /etc/os-release
  ROUTEGATE_ARCH=$(platform_architecture "$(uname -m)") || die "Unsupported CPU architecture: $(uname -m)."
  local systemd_running=0
  [[ -d /run/systemd/system ]] && systemd_running=1
  platform_tuple_supported "${ID:-}" "${VERSION_ID:-}" "$ROUTEGATE_ARCH" "$systemd_running" \
    || die "Supported target: Ubuntu 24.04 LTS on amd64 or arm64 with systemd."
}

install_dependencies() {
  log "Installing Agent bootstrap dependencies."
  export DEBIAN_FRONTEND=noninteractive
  apt-get update >/dev/null
	apt-get install -y ca-certificates curl iproute2 iptables jq tar wireguard-tools >/dev/null
}

install_hysteria2_runtime() {
  local source_dir=$1
  local asset="hysteria-linux-${ROUTEGATE_ARCH}"
  local binary_path="$ROUTEGATE_WORK_DIR/$asset"
  local hashes_path="$ROUTEGATE_WORK_DIR/hysteria-hashes.txt"
  local release_base="https://github.com/apernet/hysteria/releases/download/app%2Fv${ROUTEGATE_HYSTERIA_VERSION}"
  local expected actual

  [[ "$ROUTEGATE_HYSTERIA_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
    || die "ROUTEGATE_HYSTERIA_VERSION must be a numeric semantic version."
  [[ -f "$source_dir/systemd/hysteria-server.service" ]] \
    || die "Release bundle is missing the Hysteria2 systemd unit."
  if [[ -e /usr/local/bin/hysteria || -e /etc/systemd/system/hysteria-server.service ]]; then
    grep -Fq "Description=RouteGate managed Hysteria2 server" /etc/systemd/system/hysteria-server.service 2>/dev/null \
      || die "An unmanaged Hysteria installation already exists; RouteGate will not overwrite it."
  fi

  log "Installing checksum-verified Hysteria ${ROUTEGATE_HYSTERIA_VERSION}."
  curl -fL --retry 3 --connect-timeout 15 --max-time 300 -o "$binary_path" "$release_base/$asset"
  curl -fL --retry 3 --connect-timeout 15 --max-time 60 -o "$hashes_path" "$release_base/hashes.txt"
  expected=$(awk -v name="$asset" '$2 == name || $2 == "*" name {print $1; exit}' "$hashes_path")
  [[ "$expected" =~ ^[a-fA-F0-9]{64}$ ]] || die "No valid Hysteria2 SHA-256 entry was found."
  actual=$(sha256sum "$binary_path" | awk '{print $1}')
  [[ "$actual" == "$expected" ]] || die "Hysteria2 binary checksum verification failed."

  install -m 0755 "$binary_path" /usr/local/bin/hysteria
  install -m 0644 "$source_dir/systemd/hysteria-server.service" /etc/systemd/system/hysteria-server.service
  install -d -m 0700 /etc/hysteria /var/lib/hysteria /var/lib/hysteria/acme
  systemctl enable hysteria-server.service >/dev/null
}

install_mtproto_runtime() {
  local source_dir=$1
  local asset="mtg-${ROUTEGATE_MTG_VERSION}-linux-${ROUTEGATE_ARCH}.tar.gz"
  local archive_path="$ROUTEGATE_WORK_DIR/$asset"
  local checksums_path="$ROUTEGATE_WORK_DIR/mtg-checksums.txt"
  local release_base="https://github.com/9seconds/mtg/releases/download/v${ROUTEGATE_MTG_VERSION}"
  local extract_dir="$ROUTEGATE_WORK_DIR/mtg-extracted"
  local expected actual binary_path

  [[ "$ROUTEGATE_MTG_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] \
    || die "ROUTEGATE_MTG_VERSION must be a numeric semantic version."
  [[ -f "$source_dir/systemd/routegate-mtproto.service" ]] \
    || die "Release bundle is missing the MTProto systemd unit."
  if [[ -e /usr/local/bin/mtg || -e /etc/systemd/system/routegate-mtproto.service ]]; then
    grep -Fq "Description=RouteGate managed MTProto proxy" /etc/systemd/system/routegate-mtproto.service 2>/dev/null \
      || die "An unmanaged mtg installation already exists; RouteGate will not overwrite it."
  fi

  log "Installing checksum-verified mtg ${ROUTEGATE_MTG_VERSION}."
  curl -fL --retry 3 --connect-timeout 15 --max-time 300 -o "$archive_path" "$release_base/$asset"
  curl -fL --retry 3 --connect-timeout 15 --max-time 60 -o "$checksums_path" "$release_base/mtg-${ROUTEGATE_MTG_VERSION}-checksums.txt"
  expected=$(awk -v name="$asset" '$2 == name || $2 == "*" name {print $1; exit}' "$checksums_path")
  [[ "$expected" =~ ^[a-fA-F0-9]{64}$ ]] || die "No valid mtg SHA-256 entry was found."
  actual=$(sha256sum "$archive_path" | awk '{print $1}')
  [[ "$actual" == "$expected" ]] || die "mtg archive checksum verification failed."
  if tar -tzf "$archive_path" | awk '$0 ~ /^\// || $0 ~ /(^|\/)\.\.(\/|$)/ {found=1} END {exit !found}'; then
    die "mtg archive contains an unsafe path."
  fi
  if tar -tvzf "$archive_path" | awk '$1 ~ /^[lh]/ {found=1} END {exit !found}'; then
    die "mtg archive contains a symbolic or hard link."
  fi
  mkdir -p "$extract_dir"
  tar -xzf "$archive_path" -C "$extract_dir"
	binary_path="$extract_dir/mtg-${ROUTEGATE_MTG_VERSION}-linux-${ROUTEGATE_ARCH}/mtg"
	[[ -f "$binary_path" ]] || die "mtg archive does not contain the expected versioned binary path."
  [[ $(find "$extract_dir" -type f -name mtg | wc -l) -eq 1 ]] || die "mtg archive contains an ambiguous binary layout."

  install -m 0755 "$binary_path" /usr/local/bin/mtg
  install -m 0644 "$source_dir/systemd/routegate-mtproto.service" /etc/systemd/system/routegate-mtproto.service
  install -d -m 0700 /etc/routegate-mtproto
  systemctl enable routegate-mtproto.service >/dev/null
}

resolve_release_version() {
  if [[ "$ROUTEGATE_VERSION" != "latest" ]]; then
    ROUTEGATE_RESOLVED_VERSION="$ROUTEGATE_VERSION"
    return
  fi
  ROUTEGATE_RESOLVED_VERSION=$(curl -fsSL --max-time 30 \
    "https://api.github.com/repos/${ROUTEGATE_REPOSITORY}/releases/latest" | jq -er '.tag_name') \
    || die "No published RouteGate release could be resolved."
}

artifact_urls() {
  local version=$1
  local arch=$2
  local bundle_name="routegate-${version}-linux-${arch}.tar.gz"
  printf '%s\n' \
    "https://github.com/${ROUTEGATE_REPOSITORY}/releases/download/${version}/${bundle_name}" \
    "https://github.com/${ROUTEGATE_REPOSITORY}/releases/download/${version}/SHA256SUMS"
}

verify_bundle_checksum() {
  local bundle_path=$1
  local checksum_path=$2
  local bundle_name=$3
  local expected actual
  expected=$(awk -v name="$bundle_name" '$2 == name || $2 == "*" name {print $1; exit}' "$checksum_path")
  [[ "$expected" =~ ^[a-fA-F0-9]{64}$ ]] || die "No valid SHA-256 entry for ${bundle_name} was found."
  actual=$(sha256sum "$bundle_path" | awk '{print $1}')
  [[ "$actual" == "$expected" ]] || die "Release bundle checksum verification failed."
}

extract_bundle() {
  local bundle_path=$1
  local extract_dir="$ROUTEGATE_WORK_DIR/extracted"
  mkdir -p "$extract_dir"
  if tar -tzf "$bundle_path" | awk '$0 ~ /^\// || $0 ~ /(^|\/)\.\.(\/|$)/ {found=1} END {exit !found}'; then
    die "Release bundle contains an unsafe path."
  fi
  if tar -tvzf "$bundle_path" | awk '$1 ~ /^[lh]/ {found=1} END {exit !found}'; then
    die "Release bundle contains a symbolic or hard link."
  fi
  tar -xzf "$bundle_path" -C "$extract_dir"
  [[ -s "$extract_dir/bin/routegate-agent" ]] || die "Release bundle is missing RouteGate Agent."
  [[ -f "$extract_dir/systemd/routegate-agent.service" ]] || die "Release bundle is missing the Agent systemd unit."
  [[ -f "$extract_dir/metadata/manifest.env" ]] || die "Release bundle is missing its manifest."

  local manifest_version manifest_os manifest_arch
  manifest_version=$(sed -n 's/^VERSION=//p' "$extract_dir/metadata/manifest.env" | head -n1)
  manifest_os=$(sed -n 's/^OS=//p' "$extract_dir/metadata/manifest.env" | head -n1)
  manifest_arch=$(sed -n 's/^ARCH=//p' "$extract_dir/metadata/manifest.env" | head -n1)
  [[ -n "$manifest_version" && "$manifest_os" == "linux" && "$manifest_arch" == "$ROUTEGATE_ARCH" ]] \
    || die "Release bundle manifest does not match this host."
}

prepare_bundle() {
  ROUTEGATE_WORK_DIR=$(mktemp -d /tmp/routegate-agent-installer.XXXXXX)
  local bundle_path="$ROUTEGATE_WORK_DIR/routegate-bundle.tar.gz"
  local checksum_path="$ROUTEGATE_WORK_DIR/SHA256SUMS"

  if [[ -n "$ROUTEGATE_BUNDLE_FILE" ]]; then
    cp "$ROUTEGATE_BUNDLE_FILE" "$bundle_path"
    cp "$ROUTEGATE_CHECKSUM_FILE" "$checksum_path"
    ROUTEGATE_BUNDLE_NAME=$(basename "$ROUTEGATE_BUNDLE_FILE")
  elif [[ -n "$ROUTEGATE_BUNDLE_URL" ]]; then
    ROUTEGATE_BUNDLE_NAME=$(basename "${ROUTEGATE_BUNDLE_URL%%\?*}")
    curl -fL --retry 3 --connect-timeout 15 --max-time 300 -o "$bundle_path" "$ROUTEGATE_BUNDLE_URL"
    curl -fL --retry 3 --connect-timeout 15 --max-time 60 -o "$checksum_path" "$ROUTEGATE_CHECKSUM_URL"
  else
    resolve_release_version
    ROUTEGATE_BUNDLE_NAME="routegate-${ROUTEGATE_RESOLVED_VERSION}-linux-${ROUTEGATE_ARCH}.tar.gz"
    local urls=()
    mapfile -t urls < <(artifact_urls "$ROUTEGATE_RESOLVED_VERSION" "$ROUTEGATE_ARCH")
    curl -fL --retry 3 --connect-timeout 15 --max-time 300 -o "$bundle_path" "${urls[0]}"
    curl -fL --retry 3 --connect-timeout 15 --max-time 60 -o "$checksum_path" "${urls[1]}"
  fi

  verify_bundle_checksum "$bundle_path" "$checksum_path" "$ROUTEGATE_BUNDLE_NAME"
  extract_bundle "$bundle_path"
}

config_value() {
  local path=$1
  local key=$2
  sed -n "s/^${key}:[[:space:]]*\"\(.*\)\"[[:space:]]*$/\1/p" "$path" | head -n1
}

write_agent_config() {
  local path=$1
  install -d -m 0755 "$(dirname "$path")"
  cat >"$path" <<EOF_CONFIG
manager_url: "${ROUTEGATE_MANAGER_URL}"
registration_token: "${ROUTEGATE_REGISTRATION_TOKEN}"
heartbeat_interval_seconds: 30
config_staging_dir: "/var/lib/routegate-agent/configs"
active_config_path: "/etc/sing-box/config.json"
config_backup_dir: "/var/lib/routegate-agent/backups"
sing_box_path: "sing-box"
sing_box_service_name: "sing-box"
wireguard_staging_dir: "/var/lib/routegate-agent/wireguard-configs"
wireguard_active_config_path: "/etc/wireguard/routegate-wg0.conf"
wireguard_backup_dir: "/var/lib/routegate-agent/wireguard-backups"
wg_quick_path: "/usr/bin/wg-quick"
wg_path: "/usr/bin/wg"
wireguard_service_name: "wg-quick@routegate-wg0"
wireguard_interface: "routegate-wg0"
hysteria2_staging_dir: "/var/lib/routegate-agent/hysteria2-configs"
hysteria2_active_config_path: "/etc/hysteria/config.json"
hysteria2_backup_dir: "/var/lib/routegate-agent/hysteria2-backups"
hysteria2_path: "/usr/local/bin/hysteria"
hysteria2_service_name: "hysteria-server"
ss_path: "/usr/bin/ss"
mtproto_staging_dir: "/var/lib/routegate-agent/mtproto-configs"
mtproto_active_config_path: "/etc/routegate-mtproto/config.toml"
mtproto_backup_dir: "/var/lib/routegate-agent/mtproto-backups"
mtg_path: "/usr/local/bin/mtg"
mtproto_service_name: "routegate-mtproto"
service_control_enabled: true
traffic_collection_enabled: false
traffic_collection_interval_seconds: 60
traffic_usage_file_path: "/var/lib/routegate-agent/traffic-usage.json"
EOF_CONFIG
  chmod 0600 "$path"
}

install_agent() {
  local source_dir="$ROUTEGATE_WORK_DIR/extracted"
	install -d -m 0700 /var/lib/routegate-agent /var/lib/routegate-agent/configs /var/lib/routegate-agent/backups /var/lib/routegate-agent/wireguard-configs /var/lib/routegate-agent/wireguard-backups /var/lib/routegate-agent/hysteria2-configs /var/lib/routegate-agent/hysteria2-backups /var/lib/routegate-agent/mtproto-configs /var/lib/routegate-agent/mtproto-backups
	install -d -m 0700 /etc/wireguard
	printf 'net.ipv4.ip_forward=1\n' > /etc/sysctl.d/99-routegate-wireguard.conf
	sysctl -p /etc/sysctl.d/99-routegate-wireguard.conf >/dev/null
  install -m 0755 "$source_dir/bin/routegate-agent" "$ROUTEGATE_AGENT_BINARY"
  install -m 0644 "$source_dir/systemd/routegate-agent.service" "$ROUTEGATE_AGENT_SERVICE"
  install_hysteria2_runtime "$source_dir"
  install_mtproto_runtime "$source_dir"

  if [[ -r "$ROUTEGATE_AGENT_CONFIG" ]] && [[ $(config_value "$ROUTEGATE_AGENT_CONFIG" agent_token) == rg_agent_* ]]; then
    local configured_manager
    configured_manager=$(config_value "$ROUTEGATE_AGENT_CONFIG" manager_url)
    [[ "${configured_manager%/}" == "$ROUTEGATE_MANAGER_URL" ]] \
      || die "This host is already registered with a different RouteGate Manager."
    log "Existing Agent identity preserved."
  else
    write_agent_config "$ROUTEGATE_AGENT_CONFIG"
  fi

  systemctl daemon-reload
  systemctl enable --now routegate-agent.service >/dev/null
}

wait_for_registration() {
  local agent_token
  for _ in {1..30}; do
    agent_token=$(config_value "$ROUTEGATE_AGENT_CONFIG" agent_token || true)
    if [[ "$agent_token" == rg_agent_* ]]; then
      log "VPN Node connected to ${ROUTEGATE_MANAGER_URL}."
      return
    fi
    systemctl is-active --quiet routegate-agent.service || break
    sleep 1
  done
  journalctl -u routegate-agent.service -n 30 --no-pager >&2 || true
  die "Agent did not complete registration. Create a fresh token in Manager before retrying if the current token was consumed."
}

main() {
  trap cleanup EXIT
  parse_args "$@"
  validate_inputs
  require_supported_host
  install_dependencies
  prepare_bundle
  install_agent
  wait_for_registration
}

if [[ "${BASH_SOURCE[0]:-$0}" == "$0" ]]; then
  main "$@"
fi
