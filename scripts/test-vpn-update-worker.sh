#!/usr/bin/env bash
set -Eeuo pipefail
IFS=$'\n\t'

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORKER="$ROOT_DIR/scripts/routegate-vpn-update-worker.sh"
UNIT="$ROOT_DIR/deploy/systemd/routegate-vpn-update@.service"

fail() {
  printf '[test-vpn-update-worker] FAIL: %s\n' "$*" >&2
  exit 1
}

assert_contains() {
  local needle=$1 file=$2
  grep -Fq -- "$needle" "$file" || fail "$file missing expected contract: $needle"
}

assert_not_contains() {
  local needle=$1 file=$2
  if grep -Fq -- "$needle" "$file"; then
    fail "$file contains forbidden contract: $needle"
  fi
}

bash -n "$WORKER"
[[ -f "$UNIT" ]] || fail "detached systemd unit is missing"

assert_contains 'STAGING_ROOT=/var/lib/routegate-agent/update-staging' "$WORKER"
assert_contains 'VERIFIED_UPDATER=/usr/local/lib/routegate/update/routegate-update-verified.sh' "$WORKER"
assert_contains 'task id must be canonical lowercase UUIDv4' "$WORKER"
assert_contains 'staged candidate contains unexpected entries' "$WORKER"
assert_contains 'staged candidate contains multiple release bundles' "$WORKER"
assert_contains 'exec "$VERIFIED_UPDATER" apply' "$WORKER"
assert_contains '--role vpn' "$WORKER"
assert_not_contains 'eval ' "$WORKER"
assert_not_contains 'bash -c' "$WORKER"
assert_not_contains 'sh -c' "$WORKER"

assert_contains 'ExecStart=/usr/local/lib/routegate/update/routegate-vpn-update-worker.sh %i' "$UNIT"
assert_contains 'Type=oneshot' "$UNIT"
assert_contains 'User=root' "$UNIT"
assert_contains 'TimeoutStartSec=30min' "$UNIT"
assert_not_contains 'routegate-agent.service' "$UNIT"

if "$WORKER" 'not-a-uuid' >/dev/null 2>&1; then
  fail "non-canonical task id was accepted"
fi
if "$WORKER" '550E8400-E29B-41D4-A716-446655440000' >/dev/null 2>&1; then
  fail "uppercase UUID was accepted"
fi
if "$WORKER" '550e8400-e29b-41d4-a716-446655440000' extra >/dev/null 2>&1; then
  fail "extra worker argument was accepted"
fi

printf '[test-vpn-update-worker] PASS\n'
