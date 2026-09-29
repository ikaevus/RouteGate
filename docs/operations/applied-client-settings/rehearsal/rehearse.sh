#!/usr/bin/env bash
# Rehearses the applied-client-settings deployment checks on a disposable
# PostgreSQL database. Never point it at a real Manager database: the seed step
# drops and recreates the public schema.
#
#   REHEARSAL_DATABASE_URL='postgres://user:pass@127.0.0.1:5432/routegate_rehearsal?sslmode=disable' \
#     docs/operations/applied-client-settings/rehearsal/rehearse.sh
#
# Steps:
#   1. seed schema 000155 with the base build's own code (git worktree of
#      BASE_REF, default: the last main commit on schema 155);
#   2. run preflight-schema-155.sql with psql (must succeed) and record the
#      P2/P3/P4 prediction;
#   3. upgrade with db.Migrate and BackfillClientSettings, as Manager does;
#   4. run postflight-schema-158.sql with psql (must succeed) and compare
#      Q4/Q5 and the prediction with vpnaccounts.BuildClientConnection;
#   5. check that no psql output contains a key, secret, UUID or token hash
#      present in the database.
set -euo pipefail

: "${REHEARSAL_DATABASE_URL:?set REHEARSAL_DATABASE_URL to a disposable database}"
case "$REHEARSAL_DATABASE_URL" in
  *rehearsal*) ;;
  *) echo "refusing: the database name must contain 'rehearsal'" >&2; exit 2 ;;
esac
BASE_REF=${BASE_REF:-36a2b726574f2981b38e828119faa514182f9069}

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
checks=$(dirname "$here")
repo=$(git -C "$here" rev-parse --show-toplevel)
work=$(mktemp -d)
out=${REHEARSAL_OUT:-$work/out}
mkdir -p "$out"
chmod 700 "$work" "$out"
cleanup() {
  git -C "$repo" worktree remove --force "$work/base" >/dev/null 2>&1 || true
  rm -f "$work/secrets"
}
trap cleanup EXIT

run_psql() { # file output
  PGOPTIONS="-c default_transaction_read_only=on" psql "$REHEARSAL_DATABASE_URL" -X -q -f "$1" >"$2" 2>&1
}

echo "== 1. seed schema 155 with base build $BASE_REF"
git -C "$repo" worktree add --quiet --detach "$work/base" "$BASE_REF"
expected=$(sed -n 's/.*ExpectedDatabaseSchemaVersion *= *\([0-9]*\).*/\1/p' "$work/base/backend/internal/buildinfo/buildinfo.go" | head -1)
[[ "$expected" == "155" ]] || { echo "base build expects schema $expected, not 155" >&2; exit 1; }
cp "$here/seed_schema155_test.go.txt" "$work/base/backend/internal/db/zz_rehearsal_seed155_test.go"
(cd "$work/base/backend" && REHEARSAL_DATABASE_URL="$REHEARSAL_DATABASE_URL" \
  go test ./internal/db -run '^TestRehearsalSeed155$' -count=1 >/dev/null)

echo "== 2. preflight on schema 155"
run_psql "$checks/preflight-schema-155.sql" "$out/preflight-155.txt" || { echo "preflight failed:" >&2; tail -5 "$out/preflight-155.txt" >&2; exit 1; }
if run_psql "$checks/postflight-schema-158.sql" "$out/postflight-on-155.txt"; then
  echo "postflight unexpectedly succeeded on schema 155" >&2; exit 1
fi
phase() {
  (cd "$repo/backend" && REHEARSAL_DATABASE_URL="$REHEARSAL_DATABASE_URL" REHEARSAL_PHASE="$1" REHEARSAL_OUT="$out" \
    go test ./internal/db -run '^TestAppliedClientSettingsRehearsal$' -count=1 -v) >"$out/phase-$1.txt" 2>&1 \
    || { echo "phase $1 failed:" >&2; grep -E '_test.go:[0-9]+' "$out/phase-$1.txt" >&2 || tail -20 "$out/phase-$1.txt" >&2; exit 1; }
}
phase predict

echo "== 3. upgrade to schema 158 (db.Migrate + BackfillClientSettings)"
phase upgrade

echo "== 4. postflight on schema 158 and comparison with BuildClientConnection"
run_psql "$checks/postflight-schema-158.sql" "$out/postflight-158.txt" || { echo "postflight failed:" >&2; tail -5 "$out/postflight-158.txt" >&2; exit 1; }
run_psql "$checks/preflight-schema-155.sql" "$out/preflight-on-158.txt" || { echo "preflight failed on schema 158:" >&2; tail -5 "$out/preflight-on-158.txt" >&2; exit 1; }
phase verify

echo "== 5. secret check of psql output"
psql "$REHEARSAL_DATABASE_URL" -X -q -At -c "
  SELECT v FROM (
    SELECT reality_private_key AS v FROM servers UNION ALL SELECT reality_public_key FROM servers
    UNION ALL SELECT mtproto_secret FROM servers UNION ALL SELECT shadowsocks_server_key FROM servers
    UNION ALL SELECT wireguard_private_key FROM servers UNION ALL SELECT vless_uuid::text FROM vpn_accounts
    UNION ALL SELECT wireguard_private_key FROM vpn_accounts UNION ALL SELECT hysteria2_password FROM vpn_accounts
    UNION ALL SELECT shadowsocks_user_key FROM vpn_accounts UNION ALL SELECT token_hash FROM vpn_subscription_tokens
  ) s WHERE v IS NOT NULL AND length(v) >= 16" >"$work/secrets"
chmod 600 "$work/secrets"
leaks=0
for file in "$out"/preflight-155.txt "$out"/postflight-158.txt "$out"/preflight-on-158.txt "$out"/phase-*.txt; do
  if grep -qF -f "$work/secrets" "$file"; then
    echo "secret value found in $(basename "$file")" >&2; leaks=1
  fi
done
[[ $leaks == 0 ]] || exit 1
echo "   $(wc -l <"$work/secrets") secret values checked: none in the output"

echo "== done: outputs in $out"
grep -E 'predicted:|backend:|backfill filled' "$out"/phase-*.txt | sed 's/^.*_test.go:[0-9]*: /   /'
