# RG-140 preliminary VLESS import — pinned US Manager-only release

**Status: prepared, not executed.** The implementation is already on `main` at
`567a1e154254eebb7f8ee95e46a97c5d0b3c8e4a` ([PR #542](https://github.com/ikaevus/RouteGate/pull/542)).
The release workflow in `.github/workflows/rg140-vless-preview-manager-release.yml`
is manual only; merging workflow changes is not deployment.

## Scope and prerequisites

- **Target:** only the existing US Hybrid/Manager host; **FI** and **RU** stay
  in service. No Manager relocation, node reinstallation, Agent/VPN service
  restart, protocol apply, client subscription or token rotation.
- **Exact candidate:** `567a1e154254eebb7f8ee95e46a97c5d0b3c8e4a`.
  Two candidate **push** checks already completed successfully on `main`:
  [RouteGate CI #37823646085](https://github.com/ikaevus/RouteGate/actions/runs/37823646085),
  [Workspace integration #37823646080](https://github.com/ikaevus/RouteGate/actions/runs/37823646080).
  The workflow verifies their SHA, event, branch, name and successful result
  before SSH; it also tests its backup/rollback runner with a fake host.
- **Database:** existing schema
  `000159_staged_account_transfers` is a mandatory *runtime* precondition.
  The candidate does not add migrations beyond schema159. The runner refuses
  any other schema before replacing files. This document does **not** claim the
  current live US schema has been independently checked in this session.
- **Operator approval:** explicit production authorization is needed in addition
  to GitHub Actions environment protections. Launch only when the owner has
  authorized a short Manager availability interruption and a rollback window.
- **Bootstrap caveat:** the Manager-only release does **not** publish Agent
  bootstrap files for the new commit. Existing nodes continue with their
  installed Agent and VPN runtimes. New-node onboarding that requires the new
  commit's bootstrap is a distinct release action; don't assume it works until
  the artifacts are published separately.

## Release procedure (manual; do not schedule)

1. Confirm the exact candidate is still on `main`, both historical
   candidate push checks are green and no new migration is needed. Select
   **Actions → RG140 VLESS preview Manager release → Run workflow**, branch
   `main`, and enter exactly `deploy-US-manager-only-567a1e15`.
   Do not use the old **RG140 Manager main update** workflow: it remains pinned
   to a different, older candidate.
2. The workflow verifies the acknowledgement and exact candidate checks,
   runs same-schema fault-injection tests, builds a pinned SHA-256 verified
   bundle, and holds the existing production-like deployment lock. SSH host
   key validation is mandatory. The remote runner requires root through a
   constrained `sudo -n` invocation.
3. Before stopping Manager, the runner confirms the US Management role,
   schema159, a trustworthy applied snapshot baseline, absence of active
   transfers/reservations, and drained Agent/update jobs. It rechecks those
   conditions after stopping Manager.
4. The runner makes a private consistent PostgreSQL dump and copies the
   previous Manager binary, environment, migrations, unit and frontend.
   It **only** replaces Manager binary/unit, migration bundle and frontend,
   then restarts Manager. Old static assets are retained for existing tabs.
   Current sing-box, Agent, WireGuard, Hysteria2, MTProto, nginx, bootstrap
   files and their service state are not intentionally modified.
5. Postflight checks include Manager health, unchanged schema159, current
   public UI index, preserved account/device/token/transfer identities,
   continued served-account identities and unchanged hashes/unit state of
   unrelated services. The exact logged outcome must be
   `RESULT=updated commit=567a1e154254eebb7f8ee95e46a97c5d0b3c8e4a schema=000159_staged_account_transfers`.
6. If a check fails after mutation, the runner attempts an immediate
   backup restore and restarts the previous Manager. **Do not claim success**
   from a green SSH step alone: look for `RESULT=updated` and ensure there
   is no `ROLLBACK INCOMPLETE`. An exit code 6 means Manager changed but an
   unrelated service fingerprint also changed: investigate instead of
   reporting a clean update.

### Expected availability impact

Manager UI, API and **subscription refreshes** can briefly be unavailable
while Manager is stopped. Existing established VPN data traffic normally
continues because the VPN runtimes are not restarted. That is an architecture
expectation, **not** a zero-downtime guarantee or proof of end-user connectivity.

## Post-release acceptance (no speculative apply)

1. Open the public US UI, sign in and confirm the new Protocols workspace
   renders. Confirm the existing US subscription refresh still works with
   its original bearer token **without posting the token or link anywhere**.
2. For an already-authorized account **known to be pending VLESS**, use
   Protocols → preliminary import and verify:
   - explicit acknowledgement is required before requesting a direct
     `vless://` URI;
   - the UI says *not applied / may not work*, never *Ready*;
   - leaving Protocols removes the visible URI; returning does not reveal it;
   - no config apply or token rotation occurs.
   Do not create a new production account or reveal a live URI solely to
   satisfy this verification.
3. Confirm previously serving VPN accounts remain in the applied snapshot
   and existing FI/RU Agent heartbeats resume. Independently check at least
   one existing US VPN data-plane connection; don't mistake a healthy
   Manager for verified client tunnel traffic.
4. Do not treat a successful import of a preliminary VLESS URI as proof of
   a working tunnel. The old intermittent FI issue [#541](https://github.com/ikaevus/RouteGate/issues/541)
   remains independent and paused. Normal subscriptions stay applied-only.
5. Record exact release SHA, time, runner result, backup directory reference,
   health checks, and any incomplete acceptance item in
   [RG-140 #538](https://github.com/ikaevus/RouteGate/issues/538).

## Rollback boundaries

- The automatic rollback is for **immediate deployment failure**. It restores
  a captured consistent database and Manager file state while the Manager is
  deliberately stopped.
- **Never** restore that old database dump hours later after live admin
  writes: doing so could erase newly created accounts, tokens or transfers.
  Any later rollback requires a separate reconciled plan and user approval.
- Retained private backups are under `/root/routegate-backups` on US; do not
  upload them as public CI artifacts. The runner output must not include
  database URLs, private keys or bearer tokens.

## Remaining RG-140 work

PR #540 fixed guided readiness and PR #542 provides **static, direct VLESS**
pre-import. A long-lived pre-apply subscription that auto-updates is **not**
implemented and requires a distinct access/revocation lifecycle and design
review. Do not close [#538](https://github.com/ikaevus/RouteGate/issues/538)
solely because this Manager-only release is prepared or even deployed.
