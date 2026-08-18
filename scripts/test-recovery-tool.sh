#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=scripts/routegate-recovery
source "$ROOT_DIR/scripts/routegate-recovery"

TESTS_RUN=0
TESTS_FAILED=0
TEST_TMP=$(mktemp -d /tmp/routegate-recovery-tests.XXXXXX)
LOG_FILE="$TEST_TMP/recovery.log"
STATE_FILE="$TEST_TMP/install-state.env"

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

in_subshell() {
  ("$@")
}

test_input_allowlist() {
  assert_true "accepts managed FQDN" validate_domain "manager.routegate.example"
  assert_false "rejects command material in domain" validate_domain 'manager.example;id'
  assert_true "accepts config version UUID" validate_version_id "123e4567-e89b-42d3-a456-426614174000"
  assert_false "rejects config backup path" validate_version_id "../../config.json"
  assert_false "rejects shell material as version" validate_version_id '123e4567-e89b-42d3-a456-426614174000;id'
}

test_help_and_no_shell_escape() {
  local help
  help=$(main --help)
  assert_true "help lists certificate renewal" grep -Fq "renew-certificate" <<<"$help"
  assert_true "help lists UUID-scoped rollback" grep -Fq "rollback-vpn-config <version>" <<<"$help"
  assert_false "tool does not use eval" grep -Eq '(^|[^[:alpha:]])eval([[:space:]]|$)' "$ROOT_DIR/scripts/routegate-recovery"
  assert_false "tool does not expose shell command execution" grep -Eq '(bash|sh)[[:space:]]+-c' "$ROOT_DIR/scripts/routegate-recovery"
}

test_state_validation() {
  cat >"$STATE_FILE" <<'EOF_STATE'
STATUS=complete
DOMAIN=manager.routegate.example
EOF_STATE
  assert_true "reads completed managed domain" test "$(managed_domain)" = "manager.routegate.example"

  cat >"$STATE_FILE" <<'EOF_STATE'
STATUS=complete
DOMAIN=manager.example;id
EOF_STATE
  assert_false "rejects unsafe domain from root-owned state" in_subshell managed_domain
}

test_structured_status_without_certificate() {
  cat >"$STATE_FILE" <<'EOF_STATE'
STATUS=complete
DOMAIN=manager.routegate.example
EOF_STATE
  local mock_bin="$TEST_TMP/bin"
  mkdir -p "$mock_bin"
  cat >"$mock_bin/systemctl" <<'EOF_SYSTEMCTL'
#!/bin/sh
case "$1" in
  is-active)
    [ "${2:-}" = "--quiet" ] || printf 'active\n'
    exit 0
    ;;
  is-enabled)
    [ "${2:-}" = "--quiet" ] || printf 'enabled\n'
    exit 0
    ;;
esac
exit 0
EOF_SYSTEMCTL
  chmod 0755 "$mock_bin/systemctl"

  local output status_rc=0
  output=$(PATH="$mock_bin:$PATH" show_status) || status_rc=$?
  assert_true "status reports completed ownership" grep -Fq "routegate_state=complete domain=manager.routegate.example" <<<"$output"
  assert_true "status reports fixed Manager service" grep -Fq "service=routegate-manager.service active=active enabled=enabled" <<<"$output"
  assert_true "status reports fixed Certbot timer" grep -Fq "service=certbot.timer active=active enabled=enabled" <<<"$output"
  assert_true "status reports unavailable certificate without leaking errors" grep -Fq "certificate=manager.routegate.example available=false" <<<"$output"
  assert_true "missing certificate makes status non-zero" test "$status_rc" -eq 1
}

test_uuid_scoped_rollback() {
  local version_id="123e4567-e89b-42d3-a456-426614174000"
  local runtime="$TEST_TMP/runtime"
  local mock_bin="$runtime/bin"
  ACTIVE_CONFIG="$runtime/config.json"
  BACKUP_DIR="$runtime/backups"
  mkdir -p "$mock_bin" "$BACKUP_DIR"
  printf '{"current":true}\n' >"$ACTIVE_CONFIG"
  printf '{"backup":true}\n' >"$BACKUP_DIR/${version_id}.previous.json"

  cat >"$mock_bin/sing-box" <<'EOF_SING_BOX'
#!/bin/sh
[ "$1" = "check" ] && [ "$2" = "-c" ] && [ -f "$3" ]
EOF_SING_BOX
  cat >"$mock_bin/systemctl" <<'EOF_SYSTEMCTL'
#!/bin/sh
exit 0
EOF_SYSTEMCTL
  chmod 0755 "$mock_bin/sing-box" "$mock_bin/systemctl"

  PATH="$mock_bin:$PATH" rollback_vpn_config "$version_id"
  assert_true "rollback promotes only the UUID-scoped backup" grep -Fq '"backup":true' "$ACTIVE_CONFIG"

  printf '{"rescue":true}\n' >"$ACTIVE_CONFIG"
  cat >"$mock_bin/systemctl" <<'EOF_SYSTEMCTL'
#!/bin/sh
exit 1
EOF_SYSTEMCTL
  chmod 0755 "$mock_bin/systemctl"
  # shellcheck disable=SC2016
  assert_false "failed service health restores previous active config" in_subshell env PATH="$mock_bin:$PATH" bash -c \
    'source "$1/scripts/routegate-recovery"; ACTIVE_CONFIG="$2/config.json"; BACKUP_DIR="$2/backups"; LOG_FILE="$3"; rollback_vpn_config "$4"' \
    _ "$ROOT_DIR" "$runtime" "$LOG_FILE" "$version_id"
  assert_true "failed rollback restored the rescue config" grep -Fq '"rescue":true' "$ACTIVE_CONFIG"
}

printf 'TAP version 13\n'
test_input_allowlist
test_help_and_no_shell_escape
test_state_validation
test_structured_status_without_certificate
test_uuid_scoped_rollback
printf '1..%d\n' "$TESTS_RUN"

if ((TESTS_FAILED > 0)); then
  printf '%d recovery tests failed.\n' "$TESTS_FAILED" >&2
  exit 1
fi
