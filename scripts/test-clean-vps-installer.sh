#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=install.sh
source "$ROOT_DIR/install.sh"

TESTS_RUN=0
TESTS_FAILED=0
TEST_TMP=$(mktemp -d /tmp/routegate-installer-tests.XXXXXX)
ROUTEGATE_LOG_FILE="$TEST_TMP/installer.log"
touch "$ROUTEGATE_LOG_FILE"

cleanup_tests() {
  rm -rf "$TEST_TMP"
}
trap cleanup_tests EXIT

pass() {
  TESTS_RUN=$((TESTS_RUN + 1))
  printf 'ok %d - %s\n' "$TESTS_RUN" "$1"
}

fail() {
  TESTS_RUN=$((TESTS_RUN + 1))
  TESTS_FAILED=$((TESTS_FAILED + 1))
  printf 'not ok %d - %s\n' "$TESTS_RUN" "$1" >&2
}

assert_true() {
  local name=$1
  shift
  if "$@"; then
    pass "$name"
  else
    fail "$name"
  fi
}

assert_false() {
  local name=$1
  shift
  if "$@"; then
    fail "$name"
  else
    pass "$name"
  fi
}

assert_equal() {
  local name=$1
  local expected=$2
  local actual=$3
  if [[ "$expected" == "$actual" ]]; then
    pass "$name"
  else
    fail "$name (expected: $expected, actual: $actual)"
  fi
}

test_validation_helpers() {
  assert_true "accepts a normal FQDN" validate_domain "us.routegate.org"
  assert_true "accepts a multi-label FQDN" validate_domain "manager.us.routegate.org"
  assert_false "rejects a URL as a domain" validate_domain "https://us.routegate.org"
  assert_false "rejects a single-label hostname" validate_domain "localhost"
  assert_false "rejects a label beginning with a dash" validate_domain "-bad.example.org"

  assert_true "accepts a normal email" validate_email "admin@routegate.org"
  assert_true "accepts a plus-address email" validate_email "admin+routegate@example.org"
  assert_false "rejects an incomplete email" validate_email "admin@routegate"
  assert_false "rejects whitespace in an email" validate_email "admin @routegate.org"

  assert_true "accepts latest as a release selector" validate_release_version latest
  assert_true "accepts a semantic release tag" validate_release_version v1.2.3
  assert_false "rejects a release selector with a slash" validate_release_version feature/test

  assert_true "accepts the supported platform tuple" platform_tuple_supported ubuntu 24.04 amd64 1
  assert_false "rejects Ubuntu 22.04" platform_tuple_supported ubuntu 22.04 amd64 1
  assert_false "rejects arm64 in the installer MVP" platform_tuple_supported ubuntu 24.04 arm64 1
  assert_false "rejects a non-systemd tuple" platform_tuple_supported ubuntu 24.04 amd64 0

  assert_equal "normalizes Prometheus yes" "1" "$(normalize_prometheus_choice yes)"
  assert_equal "normalizes Prometheus true" "1" "$(normalize_prometheus_choice true)"
  assert_equal "normalizes Prometheus no" "0" "$(normalize_prometheus_choice no)"
  assert_equal "empty Prometheus choice defaults to no" "0" "$(normalize_prometheus_choice '')"
  assert_false "rejects invalid Prometheus choice" normalize_prometheus_choice maybe
}

test_argument_parsing() {
  local output
  output=$(
    ROUTEGATE_DOMAIN=""
    ROUTEGATE_EMAIL=""
    ROUTEGATE_ADMIN_EMAIL=""
    ROUTEGATE_SERVER_NAME=""
    ROUTEGATE_VERSION="latest"
    ROUTEGATE_ASSUME_YES=0
    ROUTEGATE_INSTALL_PROMETHEUS=""
    parse_args \
      --domain US.RouteGate.org \
      --email owner@example.org \
      --version v1.2.3 \
      --yes
    prompt_for_inputs
    printf '%s|%s|%s|%s|%s|%s|%s' \
      "$ROUTEGATE_DOMAIN" \
      "$ROUTEGATE_EMAIL" \
      "$ROUTEGATE_ADMIN_EMAIL" \
      "$ROUTEGATE_SERVER_NAME" \
      "$ROUTEGATE_VERSION" \
      "$ROUTEGATE_ASSUME_YES" \
      "$ROUTEGATE_INSTALL_PROMETHEUS"
  )
  assert_equal \
    "parses installer arguments and defaults Prometheus to no" \
    "us.routegate.org|owner@example.org|owner@example.org|us.routegate.org|v1.2.3|1|0" \
    "$output"

  output=$(
    ROUTEGATE_DOMAIN=""
    ROUTEGATE_EMAIL=""
    ROUTEGATE_ADMIN_EMAIL=""
    ROUTEGATE_SERVER_NAME=""
    ROUTEGATE_VERSION="latest"
    ROUTEGATE_ASSUME_YES=0
    ROUTEGATE_INSTALL_PROMETHEUS=""
    parse_args \
      --domain us.routegate.org \
      --email owner@example.org \
      --with-prometheus \
      --yes
    prompt_for_inputs
    printf '%s' "$ROUTEGATE_INSTALL_PROMETHEUS"
  )
  assert_equal "parses explicit managed Prometheus opt-in" "1" "$output"
}

test_artifact_urls() {
  local output
  output=$(artifact_urls v1.2.3 amd64 | paste -sd '|')
  assert_equal \
    "constructs versioned release URLs" \
    "https://github.com/ikaevus/RouteGate/releases/download/v1.2.3/routegate-v1.2.3-linux-amd64.tar.gz|https://github.com/ikaevus/RouteGate/releases/download/v1.2.3/SHA256SUMS" \
    "$output"
}

test_checksum_verification() {
  local bundle_name="routegate-vtest-linux-amd64.tar.gz"
  local bundle="$TEST_TMP/$bundle_name"
  local checksums="$TEST_TMP/SHA256SUMS"
  printf 'verified bundle\n' >"$bundle"
  (
    cd "$TEST_TMP"
    sha256sum "$bundle_name" >SHA256SUMS
  )

  assert_true \
    "accepts the matching release checksum" \
    verify_bundle_checksum "$bundle" "$checksums" "$bundle_name"

  printf 'tampered\n' >>"$bundle"
  # shellcheck disable=SC2016
  assert_false \
    "rejects a tampered release bundle" \
    bash -c 'source "$1"; ROUTEGATE_LOG_FILE="$2"; verify_bundle_checksum "$3" "$4" "$5"' \
      _ "$ROOT_DIR/install.sh" "$ROUTEGATE_LOG_FILE" "$bundle" "$checksums" "$bundle_name"
}

test_piped_entrypoint_guard() {
  local guard
  guard=$(tail -n 3 "$ROOT_DIR/install.sh" | head -n1)
  # shellcheck disable=SC2016
  assert_equal \
    "piped installer entrypoint tolerates an unset BASH_SOURCE" \
    'if [[ "${BASH_SOURCE[0]:-$0}" == "$0" ]]; then' \
    "$guard"
}

test_setup_url_contract() {
  ROUTEGATE_DOMAIN="us.routegate.org"
  ROUTEGATE_SETUP_URL="https://${ROUTEGATE_DOMAIN}/setup#token=test-token"
  assert_equal \
    "uses a URL fragment for the initial setup token" \
    "https://us.routegate.org/setup#token=test-token" \
    "$ROUTEGATE_SETUP_URL"
}

run_confirmation_prompt() {
  local input=$1
  local command
  printf -v command \
    'source %q; ROUTEGATE_LOG_FILE=%q; ROUTEGATE_DOMAIN=us.routegate.org; ROUTEGATE_ADMIN_EMAIL=admin@example.org; ROUTEGATE_ARCH=amd64; ROUTEGATE_ASSUME_YES=0; ROUTEGATE_INSTALL_PROMETHEUS=0; confirm_installation' \
    "$ROOT_DIR/install.sh" "$ROUTEGATE_LOG_FILE"
  printf '%b' "$input" | script -qefc "bash -c $(printf '%q' "$command")" /dev/null
}

test_confirmation_prompt() {
  command -v script >/dev/null 2>&1 || {
    fail "util-linux script command is available for confirmation tests"
    return
  }

  assert_true "empty confirmation input continues installation" run_confirmation_prompt '\n'
  assert_false "n confirmation input cancels installation" run_confirmation_prompt 'n\n'

  local output
  output=$(run_confirmation_prompt 'unexpected\n\n' 2>&1)
  assert_true \
    "unexpected confirmation input reprompts before continuing" \
    grep -Fq "Enter Y to continue or N to cancel." <<<"$output"
}

test_success_output() {
  ROUTEGATE_DOMAIN="us.routegate.org"
  ROUTEGATE_ADMIN_EMAIL="admin@example.org"
  ROUTEGATE_SETUP_URL="https://us.routegate.org/setup#token=setup-secret-token"
  ROUTEGATE_SETUP_EXPIRES_AT="2026-08-05T09:30:00Z"
  ROUTEGATE_CREDENTIALS_FILE="/root/routegate-first-login.txt"
  ROUTEGATE_INSTALL_PROMETHEUS=0
  : >"$ROUTEGATE_LOG_FILE"

  local output
  output=$(print_success)

  assert_true \
    "completion output prioritizes administrator activation" \
    grep -Fq "NEXT ACTION — Complete administrator setup" <<<"$output"
  assert_true \
    "completion output includes a short clickable setup label" \
    grep -Fq "Open RouteGate first-time setup" <<<"$output"
  assert_true \
    "completion output includes the full setup URL fallback" \
    grep -Fq "$ROUTEGATE_SETUP_URL" <<<"$output"
  assert_true \
    "completion output includes the recovery command" \
    grep -Fq "sudo cat /root/routegate-first-login.txt" <<<"$output"
  assert_true \
    "completion output explains optional Prometheus remains available later" \
    grep -Fq "Optional component not installed." <<<"$output"
  assert_false \
    "completion output no longer presents the generic Open block" \
    grep -Fq 'Open:' <<<"$output"
  assert_false \
    "setup token is not written to the installer log" \
    grep -Fq "setup-secret-token" "$ROUTEGATE_LOG_FILE"
}

test_manager_environment_public_url() {
  local original_manager_env="$ROUTEGATE_MANAGER_ENV"
  ROUTEGATE_MANAGER_ENV="$TEST_TMP/manager.env"
  ROUTEGATE_DOMAIN="us.routegate.org"
  ROUTEGATE_ADMIN_EMAIL="admin@example.org"
  ROUTEGATE_INSTALL_PROMETHEUS=0
  ROUTEGATE_MONITORING_TOKEN=""

  write_manager_environment "fixture-db-password" "fixture-admin-password"

  assert_true \
    "manager environment includes the canonical public URL" \
    grep -Fxq 'ROUTEGATE_PUBLIC_URL="https://us.routegate.org"' "$ROUTEGATE_MANAGER_ENV"
  assert_false \
    "manager monitoring surface remains disabled by default" \
    grep -Fq 'ROUTEGATE_MONITORING_' "$ROUTEGATE_MANAGER_ENV"
  assert_equal \
    "manager environment remains private" \
    "600" \
    "$(stat -c '%a' "$ROUTEGATE_MANAGER_ENV")"

  ROUTEGATE_INSTALL_PROMETHEUS=1
  ROUTEGATE_MONITORING_TOKEN="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  write_manager_environment "fixture-db-password" "fixture-admin-password"
  assert_true \
    "managed Prometheus enables the monitoring surface" \
    grep -Fxq 'ROUTEGATE_MONITORING_ENABLED="true"' "$ROUTEGATE_MANAGER_ENV"
  assert_true \
    "managed Prometheus persists the dedicated monitoring token" \
    grep -Fxq "ROUTEGATE_MONITORING_TOKEN=\"${ROUTEGATE_MONITORING_TOKEN}\"" "$ROUTEGATE_MANAGER_ENV"

  ROUTEGATE_MANAGER_ENV="$original_manager_env"
  ROUTEGATE_INSTALL_PROMETHEUS=0
  ROUTEGATE_MONITORING_TOKEN=""
}

test_prometheus_dependency_and_security_contract() {
  ROUTEGATE_INSTALL_PROMETHEUS=0
  assert_false \
    "Prometheus is not a default platform dependency" \
    grep -Fxq prometheus < <(platform_packages)

  ROUTEGATE_INSTALL_PROMETHEUS=1
  assert_true \
    "Prometheus becomes a dependency only after opt-in" \
    grep -Fxq prometheus < <(platform_packages)
  assert_true \
    "managed Prometheus web listener is loopback-only" \
    grep -Fq -- '--web.listen-address=127.0.0.1:9090' "$ROOT_DIR/install.sh"
  assert_true \
    "managed Prometheus scrapes the Manager metrics endpoint" \
    grep -Fq 'metrics_path: /metrics' "$ROOT_DIR/install.sh"
  assert_true \
    "managed Prometheus scrapes the fleet metrics endpoint" \
    grep -Fq 'metrics_path: /metrics/fleet' "$ROOT_DIR/install.sh"
  assert_true \
    "managed Prometheus uses a credentials file instead of embedding the token in YAML" \
    grep -Fq 'credentials_file: ${ROUTEGATE_PROMETHEUS_TOKEN_FILE}' "$ROOT_DIR/install.sh"

  ROUTEGATE_INSTALL_PROMETHEUS=0
}

test_agent_credentials_detection() {
  local original_config="$ROUTEGATE_AGENT_CONFIG"
  local config_file="$TEST_TMP/agent.yaml"
  ROUTEGATE_AGENT_CONFIG="$config_file"

  printf 'agent_token: "token-only"\n' >"$config_file"
  assert_false "rejects incomplete Agent credentials" agent_has_credentials

  cat >"$config_file" <<'EOF_AGENT_TEST'
agent_id: "agent-id"
server_id: "server-id"
agent_token: "agent-token"
EOF_AGENT_TEST
  assert_true "accepts complete Agent credentials" agent_has_credentials

  ROUTEGATE_AGENT_CONFIG="$original_config"
}

test_conflict_recommendations() {
  local postgres nginx ports prometheus
  postgres=$(conflict_recommendations postgresql)
  nginx=$(conflict_recommendations nginx)
  ports=$(conflict_recommendations ports)
  prometheus=$(conflict_recommendations prometheus)

  assert_true "PostgreSQL conflict recommends preserving existing data" grep -Fq "Keep the existing PostgreSQL deployment unchanged" <<<"$postgres"
  assert_true "nginx conflict recommends preserving existing sites" grep -Fq "Keep the existing nginx sites unchanged" <<<"$nginx"
  assert_true "port conflict recommends a clean VPS" grep -Fq "clean VPS" <<<"$ports"
  assert_true "Prometheus conflict recommends external integration instead of takeover" grep -Fq "external integration" <<<"$prometheus"
}

test_conflict_collection() {
  local root="$TEST_TMP/root"
  mkdir -p "$root/usr/local/bin" "$root/etc/routegate"
  touch "$root/usr/local/bin/routegate-manager"
  touch "$root/etc/routegate/agent.yaml"

  local output
  output=$(collect_routegate_conflicts "$root" | sort | paste -sd '|')
  assert_equal \
    "finds only RouteGate-owned path conflicts under a mock root" \
    "/etc/routegate/agent.yaml|/usr/local/bin/routegate-manager" \
    "$output"
}

printf 'TAP version 13\n'
test_validation_helpers
test_argument_parsing
test_artifact_urls
test_checksum_verification
test_piped_entrypoint_guard
test_setup_url_contract
test_manager_environment_public_url
test_prometheus_dependency_and_security_contract
test_confirmation_prompt
test_success_output
test_agent_credentials_detection
test_conflict_recommendations
test_conflict_collection
printf '1..%d\n' "$TESTS_RUN"

if ((TESTS_FAILED > 0)); then
  printf '%d installer tests failed.\n' "$TESTS_FAILED" >&2
  exit 1
fi
