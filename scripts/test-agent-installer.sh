#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=install-agent.sh
source "$ROOT_DIR/install-agent.sh"

TESTS_RUN=0
TESTS_FAILED=0
TEST_TMP=$(mktemp -d /tmp/routegate-agent-installer-tests.XXXXXX)
trap 'rm -rf "$TEST_TMP"' EXIT

pass() { TESTS_RUN=$((TESTS_RUN + 1)); printf 'ok %d - %s\n' "$TESTS_RUN" "$1"; }
fail() { TESTS_RUN=$((TESTS_RUN + 1)); TESTS_FAILED=$((TESTS_FAILED + 1)); printf 'not ok %d - %s\n' "$TESTS_RUN" "$1" >&2; }
assert_true() { local name=$1; shift; if "$@"; then pass "$name"; else fail "$name"; fi; }
assert_false() { local name=$1; shift; if "$@"; then fail "$name"; else pass "$name"; fi; }
assert_equal() { local name=$1 expected=$2 actual=$3; if [[ "$expected" == "$actual" ]]; then pass "$name"; else fail "$name (expected: $expected, actual: $actual)"; fi; }

valid_token="rg_reg_$(printf 'a%.0s' {1..43})"

assert_true "accepts a public HTTPS Manager origin" validate_manager_url "https://manager.routegate.org"
assert_true "accepts a non-default HTTPS port" validate_manager_url "https://manager.routegate.org:8443"
assert_false "rejects HTTP Manager URL" validate_manager_url "http://manager.routegate.org"
assert_false "rejects Manager URL path" validate_manager_url "https://manager.routegate.org/api"
assert_false "rejects Manager URL query" validate_manager_url "https://manager.routegate.org/?token=value"
assert_true "accepts a generated registration token shape" validate_registration_token "$valid_token"
assert_false "rejects an Agent bearer token" validate_registration_token "rg_agent_$(printf 'a%.0s' {1..43})"
assert_equal "maps x86_64 to amd64" amd64 "$(platform_architecture x86_64)"
assert_equal "maps aarch64 to arm64" arm64 "$(platform_architecture aarch64)"
assert_true "supports Ubuntu 24.04 amd64" platform_tuple_supported ubuntu 24.04 amd64 1
assert_true "supports Ubuntu 24.04 arm64" platform_tuple_supported ubuntu 24.04 arm64 1
assert_false "rejects Ubuntu 22.04" platform_tuple_supported ubuntu 22.04 amd64 1

ROUTEGATE_REPOSITORY=ikaevus/RouteGate
urls=$(artifact_urls v1.2.3 arm64 | paste -sd '|')
assert_equal "constructs versioned Agent bundle URLs" \
  "https://github.com/ikaevus/RouteGate/releases/download/v1.2.3/routegate-v1.2.3-linux-arm64.tar.gz|https://github.com/ikaevus/RouteGate/releases/download/v1.2.3/SHA256SUMS" \
  "$urls"

ROUTEGATE_MANAGER_URL=https://manager.routegate.org
ROUTEGATE_REGISTRATION_TOKEN=$valid_token
config_path="$TEST_TMP/agent.yaml"
write_agent_config "$config_path"
assert_true "writes Agent config with mode 0600" test "$(stat -c '%a' "$config_path")" = 600
assert_equal "writes the Manager URL" "https://manager.routegate.org" "$(config_value "$config_path" manager_url)"
assert_equal "writes the one-time registration token" "$valid_token" "$(config_value "$config_path" registration_token)"

printf '1..%d\n' "$TESTS_RUN"
((TESTS_FAILED == 0))
