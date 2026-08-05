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
    parse_args \
      --domain US.RouteGate.org \
      --email owner@example.org \
      --version v1.2.3 \
      --yes
    prompt_for_inputs
    printf '%s|%s|%s|%s|%s|%s' \
      "$ROUTEGATE_DOMAIN" \
      "$ROUTEGATE_EMAIL" \
      "$ROUTEGATE_ADMIN_EMAIL" \
      "$ROUTEGATE_SERVER_NAME" \
      "$ROUTEGATE_VERSION" \
      "$ROUTEGATE_ASSUME_YES"
  )
  assert_equal \
    "parses and normalizes installer arguments" \
    "us.routegate.org|owner@example.org|owner@example.org|us.routegate.org|v1.2.3|1" \
    "$output"
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
  local postgres nginx ports
  postgres=$(conflict_recommendations postgresql)
  nginx=$(conflict_recommendations nginx)
  ports=$(conflict_recommendations ports)

  assert_true "PostgreSQL conflict recommends preserving existing data" grep -Fq "Keep the existing PostgreSQL deployment unchanged" <<<"$postgres"
  assert_true "nginx conflict recommends preserving existing sites" grep -Fq "Keep the existing nginx sites unchanged" <<<"$nginx"
  assert_true "port conflict recommends a clean VPS" grep -Fq "clean VPS" <<<"$ports"
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
test_agent_credentials_detection
test_conflict_recommendations
test_conflict_collection
printf '1..%d\n' "$TESTS_RUN"

if ((TESTS_FAILED > 0)); then
  printf '%d installer tests failed.\n' "$TESTS_FAILED" >&2
  exit 1
fi
