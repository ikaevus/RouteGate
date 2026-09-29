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
substitute for the preflight file.

## Where and how to run

On the Manager host, against the Manager database. VPN nodes (FI, RU) need no
access: all node and account data is in the Manager database. Load the URL
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
  - `in_active_render = t`: the render deployed the account, but one of its
    active protocol rows (`not_deployed_by_active_render`) was not deployed.
    A protocol set is served whole or not at all, so the account also loses
    its working protocols. **Blocking**: fixing it needs a render and apply on
    that node, which requires a separate decision for FI.
- `withheld_first_apply`: the node has never had a successful apply, so all
  its accounts lose their links. **Blocking** on a node with users.

P2/P3 model the deployment rules only; a link can additionally be refused for
an incomplete setting, exactly as today.

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

Both files and both rollback methods were rehearsed on a local PostgreSQL 16
database migrated to `000155` by the `main` build and seeded by its own code
(render, apply, failed apply, account created after apply, suspended account,
pending preference, multi-protocol set, MTProto proxy, a node never applied, a
version without `vpnAccounts` and one with an unparsable Reality key). The
database was then upgraded by `db.Migrate` and
`configs.Repository.BackfillClientSettings`, as Manager does at start:

- preflight P2/P3 predicted exactly the post-update Q4/Q5 result, and both
  matched `vpnaccounts.BuildClientConnection` for every active account;
- rollback A produced a schema identical to a fresh `000155` database;
  rollback B produced the same schema except comments inside the restored
  trigger function; the old build applied no migration and served the same
  client connections as before the update; after rollback B the new build
  upgraded the database again.
