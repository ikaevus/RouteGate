# Applied client settings (PR #499): deployment checks

Read-only SQL checks for updating Manager from schema `000155` to `000158`
(migrations 156–158 plus the snapshot backfill Manager runs at start).

| File | When | Schema |
|---|---|---|
| `preflight-schema-155.sql` | before stopping the old Manager | `000155` (does not use columns added by 156–158) |
| `postflight-schema-158.sql` | after the new Manager has started | `000158` |

Both files run in one `READ ONLY` transaction that ends with `ROLLBACK`. They
print node names, account ids, counts, reason codes and booleans only — no keys,
passwords, MTProto secrets, subscription tokens or rendered configs. The
postflight file fails on schema 155 (its columns do not exist yet) and is not a
substitute for the preflight file; the preflight file also runs on schema 158,
which is used to verify a rollback.

JSON values are read with `jsonb_typeof` guards and without casts that can
fail: malformed rendered configs (non-numeric or out-of-range ports, flags of
the wrong type, sections of the wrong type) produce reason codes in P4 instead
of aborting the script.

## Where and how to run

On the Manager host, against the Manager database only. No VPN node is
accessed: all node and account data the checks read is in the Manager
database. Nodes excluded from changes (FI, which runs Hiddify) are not touched
by these checks, the update or a rollback; if the Manager database lists such
a node, its rows are only read. Load the URL
from the Manager environment file without printing it, and keep the output
next to the update notes:

```bash
cd docs/operations/applied-client-settings
sudo bash -c 'set -a; . /etc/routegate/manager.env; set +a;
  PGOPTIONS="-c default_transaction_read_only=on" \
  psql "$ROUTEGATE_DATABASE_URL" -X -f preflight-schema-155.sql' \
  > "preflight-$(date +%Y%m%d-%H%M).txt" 2>&1
```

Use the same command with `postflight-schema-158.sql` after the update.

## Reading the preflight (schema 155)

| Query | Shows | Blocks the update when |
|---|---|---|
| P0 | latest migration, count of `000156+` rows | latest is not `000155_*`, or any `000156+` row exists |
| P1 | nodes, saved protocol, whether any version was ever applied, active accounts | — (context for P2) |
| P2 | predicted state of every active account right after the update, per node | any `withheld_first_apply` on a node with users |
| P3 | the accounts P2 predicts will get no link (ids only) | any row with `in_active_render = t` (see below) |
| P4 | versions whose snapshot cannot be derived; versions checked in compatibility mode | review rows with `is_active = t` |
| P5 | MTProto proxy per node (boolean) and accounts using MTProto | — (see MTProto) |
| P6 | protocol rows per node and protocol, pending enable/disable | — (baseline for Q1) |
| P7 | pending node protocol switches and primary preferences | — (applied only by the next successful apply) |

P2 states:

- `served`: the account keeps receiving the same link.
- `served_compatibility_mode`: the active version does not list its accounts
  reliably; links are served without per-account checks, as today.
- `unchecked_no_snapshot`: the active version's snapshot cannot be derived
  (P4 names the reason); the node keeps serving its saved settings without the
  new protection, as today. Not blocking, but the node is unprotected until its
  next successful apply.
- `withheld_until_apply`: the account gets "awaiting apply" instead of a link
  until the node's next successful apply.
  - `in_active_render = f` (for example, created after the last apply): the
    node has no credentials for it, so its current link does not work either.
    Not blocking; tell the owner.
  - `in_active_render = t`: the render deployed the account, but its primary
    protocol or one of its active protocol rows (`not_deployed_by_active_render`)
    was not deployed. A connection is served whole or not at all, so the
    account also loses the protocols that work today. **Blocking**: fixing it
    needs a render and apply on that node. Never render or apply on a node
    excluded from changes (FI with Hiddify); there the result blocks the
    update until the owner decides.
- `withheld_first_apply`: the node has never had a successful apply, so all
  its accounts lose their links. **Blocking** on a node with users.

P2/P3 and Q4/Q5 share one model of `vpnaccounts.BuildClientConnection`, the
path of `GET /client-connection`, `/sub/`, the JSON subscription, devices and
deliveries. The account's primary protocol (`vpn_client_profiles.active_protocol`)
is built first, then every active protocol row is checked, so the checked set
is the union of the primary and the active rows, without duplicates. It also
includes the rows `GetClientProtocolSets` seeds from the snapshot on the first
read after the update (a listed account's deployed protocols it has no row
for), so the prediction does not change once the backend has read an account.
A new profile's primary is the listed account's first protocol in Go order
(vless, wireguard, hysteria2, shadowsocks, mtproto), as in
`configs.renderedAccountProtocols`.

The model covers the deployment rule only; a link can additionally be refused
for an incomplete setting or an unsupported deployment role, exactly as today.

## Reading the postflight (schema 158)

| Query | Expected | Roll back when |
|---|---|---|
| Q0 | latest `000158_explicit_account_protocol_preferences`, `new_migrations_applied = 3` | anything else |
| Q1 | column `desired_explicit`, `NOT NULL`, default `false`; per node and protocol, `explicit_rows` equals `total_rows` of preflight P6 and `seeded_rows_since_update` is 0 right after start | pre-existing rows not explicit |
| Q2 | every active version has a snapshot, except those P4 flagged | an active version without a snapshot that P4 did not flag |
| Q3 | only versions P4 flagged | unexpected active rows |
| Q4 | the same counts per node and state as preflight P2 (`ready` = `served`, `served_unchecked_no_snapshot` = `unchecked_no_snapshot`) | more accounts withheld than predicted |
| Q5 | the same account ids as preflight P3 | any additional account id |
| Q6 | the same MTProto booleans and counts as P5 | a change |

Also check the Manager start log for `backfilled applied client settings` and
for `config version has no derivable client settings` (logged as an error for
active versions); the latter must match P4.

## MTProto with the node-wide secret

- MTProto is reported only as a boolean (`*_mtproto_proxy*`); do not select or
  compare secrets.
- The proxy does not list accounts. An account served only through it is
  absent from `vpnAccounts` and is counted as `served` in P2 when the proxy
  runs; it is withheld only when the active version has no proxy.
- Any MTProto link issued earlier works while the node runs the proxy with the
  same secret, including for accounts that were suspended or had MTProto
  removed (P5/Q6 `inactive_accounts_with_mtproto`). The update does not change
  this; revoking such access requires rotating the node's MTProto secret, a
  separate change.

## Rollback

Migrations are applied by `db.Migrate` at Manager start: it runs every
`*.up.sql` in the working directory's `migrations/` whose name is not in
`schema_migrations`, and never runs `*.down.sql`. The update tooling
(`scripts/routegate-update-role.sh`) backs up the binary, `migrations/`,
frontend and a `pg_dump` of the database before an update, and on rollback
stops Manager, runs `pg_restore --clean` and reinstalls the files. Rollback
never touches VPN nodes; anything rendered and applied while the new build ran
stays applied on the node.

1. Stop Manager: `sudo systemctl stop routegate-manager`.
2. Restore a schema-155 database, either
   - **A (default, as the update tooling does)**: restore the `pg_dump` taken
     right before the update. Manager database writes made since then are
     lost.
     ```bash
     pg_restore --clean --if-exists --no-owner --no-privileges --exit-on-error \
       --dbname="$ROUTEGATE_DATABASE_URL" /path/to/backup/routegate.pgdump
     ```
   - **B (keeps data written since the update)**: from `/opt/routegate-manager`
     (the new build's `migrations/`, before step 3 replaces it), in one
     transaction run the down migrations in reverse order and remove their
     records:
     ```sql
     \set ON_ERROR_STOP on
     BEGIN;
     \i migrations/000158_explicit_account_protocol_preferences.down.sql
     \i migrations/000157_applied_version_account_protocols.down.sql
     \i migrations/000156_config_version_client_settings.down.sql
     DELETE FROM schema_migrations WHERE version IN (
       '000156_config_version_client_settings',
       '000157_applied_version_account_protocols',
       '000158_explicit_account_protocol_preferences');
     COMMIT;
     ```
     The records must be removed: otherwise a later update would skip the
     migrations while their objects are gone.
3. Restore the previous build: `/usr/local/bin/routegate-manager`, **and**
   `/opt/routegate-manager/migrations` without the 156–158 files (the old
   binary runs every file missing from `schema_migrations`, so leftover new
   files would be applied again at start), and the frontend.
4. Start Manager (`sudo systemctl start routegate-manager`) and verify: no
   `applied migration` lines in its log, `preflight-schema-155.sql` P0 shows
   `000155_*` with no `000156+` rows, P2/P3 match the preflight taken before
   the update, and a known account's client link and subscription work.

## Rehearsal

`rehearsal/rehearse.sh` repeats the whole check on a disposable database.

> **Warning:** the rehearsal drops and recreates the `public` schema of the
> database it connects to and then migrates it. Use it only with a disposable
> test database created for it; never with a Manager database or any database
> holding data you need.

Database name rule: the name of the database actually connected to
(`SELECT current_database()`) must match `^[a-z0-9_]+_rehearsal$`, for example
`routegate_rehearsal`. The URL text is never trusted: `rehearsal` in the user
name, password, host or parameters does not count. The check runs on three
levels, each before it changes anything:

- `rehearse.sh`, before seeding (it prints only the database name, never the
  URL or psql's connection errors);
- the seed test, on the same connection that drops the schema, right before
  `DROP SCHEMA public`;
- `applied_client_settings_rehearsal_test.go`, before every phase, including
  the migrations of the upgrade phase.

A database with any other name is refused with an explicit error and left
unchanged.

```bash
REHEARSAL_DATABASE_URL='postgres://user:pass@127.0.0.1:5432/routegate_rehearsal?sslmode=disable' \
  docs/operations/applied-client-settings/rehearsal/rehearse.sh
```

It needs `git`, `go` and `psql`, and:

1. seeds schema `000155` with the base build's own code: a temporary git
   worktree of `BASE_REF` (default: `36a2b72`, the last `main` commit on
   schema 155) runs `rehearsal/seed_schema155_test.go.txt`;
2. runs `preflight-schema-155.sql` with psql (it must succeed; the postflight
   file must fail on this schema) and records the P2/P3/P4 prediction through
   `backend/internal/db/applied_client_settings_rehearsal_test.go`, which reads
   the statements from the committed SQL files;
3. upgrades with `db.Migrate` and `configs.Repository.BackfillClientSettings`,
   as Manager does at start;
4. runs `postflight-schema-158.sql` (and the preflight file again) with psql,
   then compares the prediction, Q4/Q5 before and after the backend has read
   every account, and `vpnaccounts.BuildClientConnection` for every active
   account, and P4 with the versions the backfill left without a snapshot;
5. fails if any psql or test output contains a key, secret, VLESS UUID,
   account credential or token hash present in the database.

The seed covers: render, apply and a failed apply; an account created after
the apply; a suspended account; a pending preference; an active WireGuard row
the render did not deploy; a primary drifted to WireGuard while the active rows
and the render hold VLESS only; an MTProto proxy; a node never applied; a
version without `vpnAccounts`; an unparsable Reality key; and malformed ports:
`"abc"`, `1e30`, `-1`, `8443.9` (Go truncates it: valid), a port beyond the
`bigint` range as a string and as a number, a Shadowsocks port `"8388x"`, and
Reality `enabled` as the string `"true"` (Go ignores it: valid).

Result on PostgreSQL 16: the prediction, Q4/Q5 (before and after backend reads)
and `BuildClientConnection` agreed for all 13 active accounts on 8 nodes
(4 withheld: the post-apply account, the WireGuard row, the WireGuard primary
and the never-applied node); P4 named exactly the 7 versions the backfill left
without a snapshot; both SQL files completed on the malformed data; 82 secret
values were checked and none appeared in the output. The previous revision of
the files missed the WireGuard-primary account and aborted with
`value "99999999999999999999999999" is out of range for type bigint`.

Rollback was rehearsed separately on the same kind of database: rollback A
produced a schema identical to a fresh `000155` database; rollback B produced
the same schema except comments inside the restored trigger function; the old
build applied no migration and served the same client connections as before
the update; after rollback B the new build upgraded the database again.

## Limitations

- P4's WireGuard, Hysteria2 and MTProto text checks and its JSON type checks
  are structural approximations of the Go parsers; Q3 and the Manager start
  log are definitive.
- The model covers the deployment rule, not other reasons a link can be
  refused (incomplete settings, deployment role).
- Unknown protocol names in an old render sort alphabetically after the known
  ones; Go appends them in map order. Manager never renders such names.
- The rehearsal data is synthetic; renders made by older releases may differ,
  which is why the preflight must run against the Manager database (on the
  Manager host, never on a VPN node) before the update.
