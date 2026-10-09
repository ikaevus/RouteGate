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

### Scoped Agent executor (second implementation slice)

`CredentialRemovalExecutor` is an isolated, **unwired** execution primitive.
The heartbeat dispatcher does not accept its task kind, no capability is
advertised, and Manager has no endpoint/job producer for it. Do not wire it to
ordinary `config_apply`: the reservations and recovery coordinator below are
mandatory prerequisites. This slice cannot perform an administrator-triggered
revocation and does not close #545.

Its version-1 request binds operation/account/server/Agent/job/version IDs, the
baseline and candidate runtime digests, a single UUID, and explicit acceptance
of shared-process session interruption. The runtime digest is SHA-256 of the
Agent's canonical JSON: recursively sorted object keys, preserved array order
and number spelling, Go JSON string escaping. Duplicate keys, invalid UTF-8,
trailing values and excessive size/depth fail closed. This digest is distinct
from the Manager envelope hash, which remains only a correlation identifier.
The Manager must generate these digests from the pinned baseline and candidate
using this exact versioned contract before integration.

The executor independently verifies that the on-disk JSON changes by removing
exactly one stable account name/UUID from the managed Reality inbound. Other
inbounds, users, credentials, routing and options remain identical. It uses
only the locally selected VLESS adapter, validates before promotion, and
requires service control and an enabled service. Last-user removal keeps an
empty listener; ordinary apply authorization remains unchanged.

Evidence includes the canonical active-file digest and a fresh systemd
InvocationID, PID and monotonic start timestamp, with active/running state and
`KillMode=control-group`. The observed process executable must be the same
binary used for validation. `/proc/<pid>/cmdline` must select the expected
absolute config file, directly or through a directory containing exactly one
JSON config. Merged sources, symlinks, implicit/default config paths, stdin and
unknown command options are unsupported and fail closed. Generation is checked
again around process-source inspection and after listener verification.

A private fsynced receipt and nonblocking filesystem lock serialize this
executor. A durable inflight marker precedes atomic active-file replacement.
Failures before mutation are `failed`; uncertain/post-mutation outcomes are
`recovery_required` and retain the marker. There is **no automatic rollback**
that could regrant the removed UUID. An identical successful redelivery only
rechecks file, process generation, enabled state and listener; it does not
restart. A changed request, evidence, unreadable receipt or unresolved marker
cannot produce success. Receipts contain no credentials, paths or raw process
output and live in a dedicated subdirectory outside ordinary artifact cleanup.
The local `succeeded` outcome is evidence for a future Manager verifier, not an
account-level `confirmed` state or a claim of sustained VPN connectivity.

The lock currently covers this executor only. It does not fence legacy Agent
apply, rollback, service tasks, updates or direct local administration. Before
activation, Manager reservations must exclude every conflicting mutation, and
Agent dispatch must share the fence with all runtime mutation paths, including
restart/recovery after Agent process death. Arbitrary privileged out-of-band
changes (including SIGHUP or manual restoration) cannot be prevented by these
measurements; subsequent drift must invalidate previously confirmed evidence.

The opt-in test uses a disposable systemd CI runner, a temporary service and
loopback-only Reality clients/targets. It exercises the actual executor and
adapter, rejects a cached removed UUID, preserves the other account's ability
to reconnect, checks read-only lost-ack replay, and tests deny-all last-user
removal. It also observes both already-open streams closing on the shared
process restart. That interruption requires explicit operator acceptance;
this is not selective session termination. Production services are never used.

Next slice: durable Manager operation/reservation and audit tables; baseline
preflight/proof retention; a read-only preview and explicit confirmation bound
to that preview; capability negotiation; strict result verification; blocked
subscription/reactivation semantics; reconciliation of pending/failed/recovery
states without automatically restoring the credential. Test Manager/PostgreSQL
concurrency and all Agent mutation fences before enabling dispatch or UI.

### Durable Manager preparation (third implementation slice)

Migration 000160 adds an internal preparation ledger and transactional audit.
`revocations.Service.Prepare` locks the account/node, composes with transfer and
Manager-update admission, checks current applied version/hash, account identity,
Agent identity/current authenticated heartbeat, and rejects conflicting jobs.
It creates a secret-bearing candidate from the pinned applied snapshot and
stores both version-1 runtime digests. Retry of the same actor/target/baseline
returns the same preparation; a different binding cannot reuse it.

The desired-state comparison is read-only, including credential resolution:
no config version, apply task or WireGuard credential is created. It permits
only the selected account's active/suspended/revoked status difference and
rejects unrelated rendered changes and recent edits to hidden disabled accounts.
This conservative check may require resolving unapplied changes before a
preparation can be made. It is not a fresh on-node measurement: that still
belongs to the explicitly authorized Agent execution.

A preparation reserves the whole node, because sing-box config and process
are shared. PostgreSQL guards block account/credential/protocol/routing edits,
config/service/update tasks, transfer admission, active-version changes and
Agent credential rotation while reserved. Shared routing and Manager updates
are fenced too; Agent heartbeat remains writable. The persisted baseline and
candidate/audit survive cleanup and restart. Cancellation is explicit,
idempotent, and only releases a **never-dispatched** preparation; it never
changes account/subscription state or restores a UUID. There is no automatic
expiry/unlock. Downgrade refuses to discard any preparation/audit history.

The only persisted states in this slice are `prepared` and `cancelled`.
`prepared` is not `pending` execution. The schema refuses a `confirmed` write.
There is no HTTP route, task producer, worker, Agent capability advertisement,
confirmation control or enabled dispatcher. This is deliberately not a usable
revocation operation yet and does not close #545.

`VerifySuccess` separately validates bounded, strict version-1 Agent evidence
against a trusted dispatch binding. It rejects wrong job/account/node/Agent,
version/hash/port mismatches, missing restart generation, unsupported outcomes,
duplicate/unknown fields and credential-bearing extensions. Manager and Agent
use shared golden digest vectors despite being separate Go modules. This pure
verifier does not authenticate the sender, promote applied membership or write
`confirmed`; all three remain the future coordinator's responsibility.

Next activation gate: add a capability-negotiated durable outbox and explicit
confirmation bound to this preparation; recheck freshness/credential generation
and saved state at confirmation; add common Agent mutation fences; atomically
verify authenticated evidence, promote applied state and persist pending,
failed, recovery-required or confirmed outcomes; retain uncertain reservations
and add explicit reconciliation. Exercise that complete path with PostgreSQL,
Agent restart/lost acknowledgment and real Reality traffic before enabling UI.
