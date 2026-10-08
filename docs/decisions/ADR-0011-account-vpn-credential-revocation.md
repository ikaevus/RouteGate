# ADR-0011 — Subscription status is not verified VPN credential revocation

- **Status:** Proposed / design gate; no runtime revocation engine implemented.
- **Date:** 2026-10-09
- **Tracking:** [RG-140 credential revocation #545](https://github.com/ikaevus/RouteGate/issues/545);
  [subscription continuity #509](https://github.com/ikaevus/RouteGate/issues/509).

## Current confirmed behavior

1. `POST /devices/{deviceId}/revoke` and token rotation invalidate
   **subscription bearer links** only. Already imported VLESS/Reality, Hysteria2
   and other protocol credentials are not cleared from the VPN runtime.
2. `vpnaccounts.Repository.SetAccountStatus` updates a PostgreSQL account
   row (`status`, `updated_at`, `config_updated_at`). Public
   `/sub/<token>` immediately denies non-active accounts, **without**
   applying anything on the node. Bulk status updates have the same scope.
3. `DeleteAccount` deletes a database record; it does not prove the
   previously applied listener stopped accepting this account's credentials.
4. Re-activating a suspended/revoked account can allow a still-valid token
   to retrieve a subscription again. It is **not** a trustworthy way to
   restore a previously removed VPN runtime account.

Therefore the existing status badge `Suspended` or `Revoked` means **the
account record is in that lifecycle state**, not that VPN tunnels were
terminated. `Inactive` and `Expired` are not synonyms for
`verified blocked in VPN runtime`.

## First stage: truthful UI (separate PR)

- Explicit confirmations on suspend/revoke/delete, **including bulk**.
- Display the difference between blocked subscription retrieval and
  unverified runtime credential removal while account is suspended,
  revoked or expired. Show the existing Protocols/Apply section as the
  next place to inspect, **not** an auto-apply or a completed badge.
- Account status success response says *status changed* rather than
  *VPN access disabled*. Reactivation warns existing unexpired tokens
  can begin working again.
- No automatic cleanup, extra node operations, entitlement reissue,
  revocation of another device, or old-token redirects.

## Required future runtime operation (#545)

Suggested durable states:

```text
REQUESTED → PREFLIGHT → PREPARING → APPLYING → VERIFYING → CONFIRMED
                   ↘ BLOCKED / FAILED / RECOVERY_REQUIRED
```

Use a transactionally persisted operation with explicit actor, exact
account and source-node ID, pinned applied config version/hash, apply
job ID, protocol identity and timing information.

### Safety invariants

- **Scope:** one account, one eligible VPN/Hybrid node, initially applied
  VLESS/Reality. No assumptions about geographic direction, node name,
  443 vs 8443, or local version ordering. US Hybrid must retain Manager,
  HTTPS, Agent, and unrelated protocols.
- **Preflight:** reject stale/unknown applied snapshot, absent Agent,
  conflicting transfer, apply/maintenance reservation, or unrelated
  unapplied saved changes. Never deploy all saved edits merely to revoke
  this account.
- **Renderer:** construct a controlled config from the proven current
  applied baseline, removing **only** the selected account's specific
  deployed VLESS UUID while retaining all unrelated accounts and
  listeners. Apply must be version-pinned, validated and atomic.
- **Proof:** Agent-confirmed apply for expected version+hash, applied
  snapshot **absence** of revoked identity, unchanged membership for
  all unrelated users, runtime/listener checks, and protocol-specific
  analysis of pre-existing sessions. No `confirmed` on timeout, status
  update, heartbeat or HTTP 200 alone.
- **Failure:** if apply fails or proof is missing, the UI must say runtime
  state **unknown/pending**, retain the failed operation and guide
  explicit recovery. Restore of previous config may re-enable
  credentials and must never be silently automatic outside carefully
  designed bounded rollback semantics.
- **Concurrency:** protect target account/node from reactivation,
  deletion, transfers, fresh assignments or unrelated config applies
  while an operation is active; survive Manager restarts without
  replaying jobs.
- **Security/audit:** least privilege, scoped operator confirmation,
  no bearer links/private keys/UUIDs in logs, no raw shell input,
  no account tokens rotated by default.
- **Testing:** two-account protected baseline; one removed, one still
  served. Include failed apply, stale versions, Agent disconnect,
  competing changes, Manager restart, repeated requests, and existing
  client sessions in an isolated sing-box test before any live testing.

### Per-device limitation

Devices currently have **individual subscription URLs** but share
the VPN Account's protocol credential (e.g. VLESS UUID). Consequently,
device-token revocation must **not** be shown as disconnecting only that
device from the VPN. True device-level termination needs a separate
per-device VPN identity design and cannot be emulated by revoking the
shared account credential (which would disrupt other devices).

### Unsupported / shared-protocol caution

A node-wide MTProto secret or other shared credential cannot be
revoked for one user without impacting others. The future feature must
explicitly refuse unsupported modes, rather than overclaim isolation.

## Rollout boundaries

The runtime operation in this ADR remains **design**, not an executable revocation endpoint.
Internal candidate preparation does not authorize an apply.
No production migrations, Agent restarts, token rotations, FI tests or
Manager updates are approved by this document. The first PR improves
only the warnings/status UX; follow-on backend work requires review
and isolated end-to-end validation.

## Code audit and first implementation slice — 2026-10-09

The first slice is an **internal, side-effect-free candidate planner** in
`backend/internal/configs/credential_removal.go`. It is not exposed by an HTTP
route, does not persist a version/job, and cannot change account status, links,
or a running node. The complete operation remains a design gate.

### Confirmed existing contracts

- `configs.Service.render` resolves desired account protocols and reads current
  saved server/accounts/routing before creating a version. It must not be used
  as the revocation baseline: unrelated pending edits could be deployed. Even
  transfer `checkBaseline` calls this mutating render and can seed WireGuard
  credentials via `ResolveServerAccountProtocols`.
- `configs.Service.Apply` checks the stored envelope hash/validation and queues
  a job; it does not establish that an arbitrary old version is the currently
  running node baseline. The dedicated operation must pin and reserve it.
- `agents.Repository.CompleteConfigTask` authenticates the job's assigned Agent
  and accepts completion only while `in_progress`. Successful completion drives
  the applied-version/client-settings triggers (migrations 000155/000157).
  Their `applied` status alone is not a verified revocation result: the existing
  Agent also reports success when service control is disabled.
- `agent/internal/heartbeat/multi_config_apply.go` stages and validates selected
  runtimes, replaces their files, restarts services, and checks active/enabled
  state and listeners. Its success report carries component/listener evidence
  but no measured active-file digest or process-generation evidence. The stager
  copies `task.ConfigHash` into its result; this is not a measured file hash.
- The adapter selects **all configured runtimes**, so a generic apply may
  restart Hysteria/WireGuard/MTProto too. VLESS and Shadowsocks share sing-box.
  An unchanged authenticator does not imply an uninterrupted existing session:
  restarting their common process can disconnect unrelated users as well.
- Current rollback can restore the previous runtime files and restart them.
  This can restore the revoked UUID; a failed revocation must never become
  `confirmed` and must retain the account's blocked subscription lifecycle.
- Active transfer reservations already guard node jobs, account/setting edits,
  Manager updates, and protected proof (migration 000159). New revocation
  reservations must compose with these guards. Terminal config-job cleanup
  currently deletes successful jobs: durable revocation proof needs explicit
  retention/protection beyond transient job history.

### Candidate planner contract

`PrepareVLESSRemoval` takes a baseline and an operation-resolved target:
server ID, account ID, expected version ID/hash and UUID. It requires an applied
timestamp, a matching envelope hash and server identity, an explicit VPN/Hybrid
role, an Agent identity, and a supported Reality listener. It checks a bijection
between account metadata and the actual VLESS `users` list; duplicate/shared
UUIDs (including case variants), unknown users, missing users, or mismatched
stable names fail closed. It rejects a multi-protocol target, shared MTProto,
custom/multiple VLESS listeners, and unrepresentable envelope fields.

It copies the baseline, removing exactly the selected account metadata and
authenticator. All other listener options, identities, ports, routing, protocol
payloads, Agent/server identity and render timestamp are preserved. No saved
settings are read. `ValidateVLESSRemovalDelta` rejects any additional change.
The candidate is excluded from JSON serialization and errors contain no secrets.

Last-user preparation keeps the same Reality listener with an empty `users`
list, without borrowing `transferEmptyUsers` authorization. The ordinary apply
safety gate still rejects that candidate. Supporting an authorized deny-all
operation requires its own durable authority and verification in the next slice.

These checks prove **only the intended configuration delta**. They do not prove
Agent freshness, current on-node state, absence of pending edits, exclusive
operation ownership, applied membership, or existing-session termination.

### Remaining execution gates

1. Persist actor/target, expected active version, exact candidate and job, safe
   error codes and state transitions. Block conflicts (transfer, activation,
   deletion, assignment, config/service/update/maintenance operations) using
   database reservations before status changes or any Agent job. Revalidate
   current baseline and account identity inside the reserving transaction.
2. Check unrelated saved edits with a read-only comparison, including disabled
   accounts and protocol/routing preferences; a status-only change to the target
   must not make revocation of an already suspended account impossible.
3. Add an Agent contract for a **scoped sing-box apply** with expected baseline
   digest, candidate digest, measured active-file digest, job/version binding,
   process/restart and listener results. Do not restart independent runtimes.
   Explicitly state and acknowledge any shared-process session interruption.
4. Verify the exact applied delta and measured evidence; keep pending/failed/
   recovery-required states on missing or inconsistent proof. Define bounded
   rollback, idempotent lost-ack recovery, restart persistence and proof
   retention before offering a confirmation badge.
5. Exercise isolated Manager/PostgreSQL/Agent failures, concurrent operations,
   restart/lost acknowledgment, stale/wrong versions and already-open sessions.
   Add operator confirmation and truthful UI only around that proven lifecycle.

The standalone sing-box test for this slice exercises cached UUIDs against
locally prepared baseline/removal/zero-user JSON using only loopback services.
It models a whole-process restart explicitly. It is not a Manager/Agent
operation test and does not establish selective session termination.
