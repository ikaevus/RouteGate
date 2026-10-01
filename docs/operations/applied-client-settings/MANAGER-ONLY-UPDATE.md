# Applied client settings: Manager-only update

PR #499 changes only the Manager (backend, migrations `000156`–`000158`, Web
UI); it changes nothing under `agent/`, `scripts/`, `deploy/` or `.github/`.
The production-like host is therefore updated with the Production-like Ops
operation `update-manager`, which replaces the Manager and nothing else.

## What the operation changes, and what it never touches

Changed, from a release bundle built from one pinned commit:

- `/usr/local/bin/routegate-manager`, `/opt/routegate-manager/migrations`,
  the Web UI under `/var/www/routegate` (except `bootstrap/`) and the
  `routegate-manager` unit;
- the Manager database, through the migrations the new Manager applies at start.

Restarted: only `routegate-manager` (stopped before the backup, started on the
new build; on failure stopped and started again on the previous build), plus a
`systemctl daemon-reload` for the unit file, which restarts nothing.

Never touched: the Agent binary, unit, `agent.yaml` and process; sing-box,
WireGuard, Hysteria2 and MTProto with their configs and services; nginx;
observability; the maintenance dispatch; published Agent bootstrap artifacts.
No config render or apply job is created and no remote node is contacted: the
operation connects only to the production-like (US) host. FI and RU are not
touched; their Agents simply reconnect to the restarted Manager.

While the Manager is stopped (backup, migrations, start; normally a few
seconds plus the database dump), the Manager UI, API and subscription links are
unavailable. Established VPN connections are not affected.

## Safeguards

- **Trusted code.** The workflow, the runner
  (`scripts/production-like-update-manager.sh`), the update libraries, the
  down-migration runner and both SQL files come from the trusted `main`
  checkout. Only the bundle is built from the target commit.
- **Pinned target.** The only parameter is a full 40-character lowercase
  commit: dispatch input `commit` (dispatch from `main` only), or the exact
  #268 body `operation=update-manager commit=<40 hex>` (nothing else, no
  trailing space or newline). Before the build the workflow requires a
  successful `RouteGate CI` push run on `main` for that commit, checks out
  exactly that commit and verifies it is contained in `main`. The bundle is
  built from it (`COMMIT` is that commit), its SHA-256 travels with it, and the
  host verifies checksum, archive safety and manifest commit. After install the
  Manager binary must equal the verified bundle's binary.
- **Scope.** The bundle must target `000158_explicit_account_protocol_preferences`.
  The database must be at `000155_config_apply_trigger_invariant_repair`
  (upgrade) or already at `000158` (same-schema redeploy); anything else is
  refused before any change.
- **Preflight gate (upgrade only).** The pinned `preflight-schema-155.sql`
  runs read-only. The update is refused if P3 lists any account or P2 predicts
  any node state other than `served`/`served_compatibility_mode`.
- **Lock, drain, backup.** The run holds `/run/lock/routegate-production-like.lock`,
  waits for active Agent jobs to finish, stops the Manager, then backs up the
  Manager binary, unit, environment, migrations, Web UI and a `pg_dump` to
  `/root/routegate-backups/update-management-<commit>-<UTC timestamp>`, with
  `manager-update.meta` (previous schema, target commit, previous binary
  SHA-256). The name matches the maintenance dispatch retention rule.
- **Postflight gate.** After the new Manager is healthy and the schema is
  `000158`, the pinned `postflight-schema-158.sql` runs read-only (full Q0–Q6
  output is kept). Gate: Q0 is `000158` with all three new migrations; Q3 has
  no *active* version without a snapshot (inactive ones, such as a pending
  version, do not block); Q5 is empty; every Q4 state is `ready`; and the
  served active accounts per node equal the active accounts per node counted
  before the update in the same run. Then the public Web UI must answer 200.
- **Rollback.** Any failure after the first file change: stop the Manager;
  undo `000158`…`000156` with their atomic down files (the
  `deploy-production-like-bundle.sh --run-down-migration` runner) and
  `pg_restore --clean` the backup, which is the order the production-like deploy
  uses; restore the previous binary, migrations directory, Web UI, unit and
  environment; start the previous Manager. The previous migrations directory
  has no `000156+` file, so the old build cannot apply them again. The run then
  checks Manager health, that the schema is back at the backup schema with no
  newer `schema_migrations` row, and that no newer migration file is
  installed, and reports `ROLLBACK COMPLETE` or `ROLLBACK INCOMPLETE`.
- **Agent/VPN invariance.** Before and after, the run records unit state
  (`LoadState`, `ActiveState`, `SubState`, `MainPID`, start timestamp,
  `NRestarts`) of the Agent, VPN runtimes, nginx and the maintenance socket, and
  SHA-256 checksums of their binaries/configs, the nginx site and the bootstrap
  tree. Only the names of changed items are printed. A difference gives exit 6
  without a rollback (the update does not touch them; investigate).
- **Output.** The database URL, environment, dump and keys are never printed;
  connection errors are redacted. The Actions log and the #268 comment carry
  only the runner's status lines and the artifact name; the full output
  (P0–P7, Q0–Q6: node names and account ids, no secrets) is the artifact
  `routegate-update-manager-<run id>`, kept 7 days.

Runner exit codes: 0 updated and verified; 1 failed and rolled back (check for
`ROLLBACK COMPLETE`); 2 usage, pinned file or copy failure; 3 refused before
any change; 6 updated but Agent/VPN state changed.

## Limits of the automatic evaluation

- Gated automatically: Q0, Q3 (active versions), Q4, Q5 and served accounts per
  node versus the count at the start of the run. Q1 (explicit preference rows,
  compare with P6), Q2 (snapshot mode per node) and Q6 (MTProto) are kept in
  the artifact for review and do not fail the run.
- The baseline is taken by the run itself, not from an earlier preflight run.
  Compare the artifact with the earlier live preflight (US 3 and RU 1 active
  accounts, all served) when reviewing.
- The invariance check compares unit state and file checksums; it does not
  measure traffic.
- Agent bootstrap artifacts of the new commit are not published (that remains
  the full deploy's job). Connect commands for *new* nodes generated by this
  Manager point to `/bootstrap/<commit>/` and fail until a full deploy
  publishes them; existing nodes are unaffected. The run logs a warning.
- The Agent keeps its current version; #499 does not change the Agent.

## Automatic deployment is disabled

`production-like-deploy.yml` no longer runs after a green `RouteGate CI` on
`main`; it runs only on a manual `workflow_dispatch` from `main`. Merging any PR,
including the one that disables the trigger and #499 itself, builds and tests
but deploys nothing. (`workflow_run` uses the workflow file on the default
branch when CI completes; after the merge that file has no such trigger.)

## Run sequence

1. Merge the PR with this operation to `main` (CI green, review). Nothing is
   deployed.
2. Optionally rerun `preflight-applied-client-settings-155` on the current
   `main`.
3. Merge #499 into `main`. Wait for the `RouteGate CI` push run of the merge
   commit `<M>` to succeed. Nothing is deployed.
4. Run Production-like Ops from `main`: operation `update-manager`, commit
   `<M>` (or set #268 body to `operation=update-manager commit=<M>` and reopen
   it).
5. Review the #268 comment and the artifact: `RESULT=updated commit=<M>`,
   Q1/Q2/Q6, and the comparison with the earlier preflight. On exit 1, confirm
   `ROLLBACK COMPLETE`; the backup path is in the log.
6. A full deploy (manual dispatch of Production-like Deploy) to publish the
   bootstrap artifacts and update the Agent is a separate decision.

## Local rehearsal

`rehearse-manager-only-update.sh` (this directory) runs the real runner with
the real Manager builds (base `main`, target #499) against a disposable
PostgreSQL and a fake host root: refusal on the #499 edge-case seed, a clean
155 → 158 update, a failing migration inside the Manager start and a failure
after the full upgrade (both rolled back to 155 with the previous Manager
healthy, no `000156+` rows, database equal to before), Agent/VPN files and
units unchanged in every case. `scripts/test-production-like-update-manager.sh`
covers the same control flow and the workflow wiring with stubs in CI.

## Pinned SQL provenance

`postflight-schema-158.sql` is a byte-for-byte copy of the file in commit
`b72637cf122d4d3db0c988ebb436e87bbe00d6a9`; do not edit it here.

| | |
|---|---|
| Source | `docs/operations/applied-client-settings/postflight-schema-158.sql` |
| Commit | `b72637cf122d4d3db0c988ebb436e87bbe00d6a9` (branch `claude/applied-client-settings`) |
| Git blob | `e7cbc1d3ae5939b202cf2f1c43e7e926f4704fc5` |
| SHA-256 | `1aa258232e47c9970b7a4a4de905cb937ff5e604407552e47206ce1a07fe575b` |

The runner pins both SQL checksums (see `OPS-PREFLIGHT.md` for the preflight).
