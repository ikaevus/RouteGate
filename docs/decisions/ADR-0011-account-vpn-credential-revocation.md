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

### Shared Agent mutation admission primitive (fourth implementation slice)

`tasks.BeginRuntimeMutation` shares the scoped executor's private `runtime.lock`
and introduces a fsynced `mutation-inflight.json` intent for future legacy
config apply/rollback, service, install, maintenance and platform-update callers.
It checks both legacy and removal markers while holding the same nonblocking
filesystem lock. The directory's stable parent must already exist; admission
persists the newly created directory entry before recording an intent. Receipts
and lock files must be local, private and owned by the Agent identity.

`Close` releases only the process lock, never the intent. Process exit therefore
cannot authorize a retry, even with an identical task ID. `Complete` is only for
a live owner after a verified safe terminal runtime result; it cannot be used
for timeout, unknown outcome or detached-worker dispatch acknowledgement. It
checks the unchanged intent and absence of a removal marker before durable
clearance. There is no force-unlock or restart reconciliation API in this slice.

The scoped executor now checks this legacy intent before any execution **or
successful receipt replay**. A prior success cannot bypass an intervening
unknown mutation. Tests cover mutual exclusion in both directions, all admitted
task classes, malformed state, file types, same-task retry, and a subprocess
exiting without releasing its lease. No VPN runtime is needed for these tests;
the existing isolated actual-executor/Reality CI remains the runtime regression.

**Not yet wired:** ordinary heartbeat task handlers, direct commands and detached
update workers do not acquire this lease yet. No production configuration path
is selected, no new task kind is dispatched, and no capability is advertised.
This is a reusable admission prerequisite, not a claim that every mutation path
is fenced. A detached updater needs worker-owned admission and a persisted
handoff/reconciliation contract; holding a lease only in its dispatcher is
insufficient. Wire the handlers only together with truthful bounded failure
reporting and explicit recovery, then test crash/restart and detached handoff
end-to-end before enabling the Manager confirmation/outbox or revocation UI.

### Experimental heartbeat admission (fifth implementation slice)

The heartbeat task loop now has an **off-by-default development gate**:
`experimental_runtime_mutation_fencing`. No installer, capability advertisement
or Manager endpoint enables it. It is for disposable isolation tests only,
not for enabling revocation on an existing node. Configuration save/registration
preserves an explicit opt-in; default saved configs remain unchanged.

When opted in, synchronous config apply (including internal rollback), service,
installation and maintenance handlers acquire the durable lease before entering
the handler and hold it through Manager result acknowledgement. They must match
the local Agent/server and an in-progress task. Handler error, failed result
delivery, process death or unexpected marker change retains the fence. A
cancelled context observed **before** invoking the handler is the one provable
no-mutation cancellation that may clear its own live lease automatically.
Successful legacy task completion does not establish revocation evidence.
Fenced handlers require a positive Manager result acknowledgement: the legacy
client shortcut treating HTTP 404 after an uncertain response as success is
disabled in their context. HTTP 500 followed by 404 therefore retains the fence;
HTTP 500 followed by success retries only the report, not the runtime mutation.
They also suppress ordinary `failed` result envelopes: the legacy result schema
cannot distinguish proven failure from an uncertain mutation. In both cases the
Manager job remains active until a future explicit reconciliation path exists.

The Agent requests `runtimeMutationFencingV1` timeout preservation in authenticated
heartbeats. Manager persists that mode in Agent capabilities, retains it across
later omissions/false values and credential replacement, and exempts that Agent's
active config/operation jobs from the five-minute orphan-to-failed heuristic.
The response acknowledges the persisted policy. Experimental mutations cannot
start without this positive, identity-bound acknowledgement, so an older Manager
cannot silently use the old timeout semantics. This is **not** a credential
removal capability or a claim that all mutation/recovery paths are complete.
There is deliberately no automatic downgrade of the persisted safety mode.

The fixed state directory is `/var/lib/routegate-agent/runtime-mutations`, outside
the default artifact cleanup roots. Its stable parent must already be provisioned.
Existing state makes admission sticky even if the opt-in flag is removed on
restart. Malformed state fails closed. Diagnostics and existing platform-update
receipt reconciliation remain available; unknown kinds and credential-removal
dispatch remain disabled. Admission failures do **not** post a fabricated
terminal result for what may be an uncertain/redelivered task.

Detached update dispatch is refused while the experimental gate is active, and
the update preparation and worker entrypoints independently refuse existing
shared state. This does **not** fence an updater that already passed entry
checks: drain/stop all such workers before opting in. Dispatcher/worker atomic
handoff is still required before advertising any complete fencing capability.
Direct privileged tools and scripts remain outside the Agent admission boundary.

The isolated HTTP Manager fixture exercises actual Agent task polling, dispatch
and result acknowledgement. It covers success, handler failure, failed result
acknowledgement, a fresh Runner with the flag removed, all mutation kinds,
identity mismatch and disabled removal/updates. Config tests exercise real
temporary-file promotion and rollback with a fake service adapter while checking
that the lease stays held. These are not production tests or proof of a complete
Manager/PostgreSQL-to-Agent revocation workflow.
Focused PostgreSQL tests separately prove old in-progress jobs remain active
across repeated heartbeats, capability omission/false and Agent credential
replacement, while unfenced legacy jobs retain their existing timeout behavior.

**Recovery limitation:** legacy handlers do not yet provide phase-specific
proof (durable historical success delivery is added below). Therefore even
a validation failure conservatively retains its intent. A successful result
can also reach Manager before local fence cleanup fails; the runtime result is
not rewritten, but the local gate remains blocked. There is no supported
operator unlock/recovery route yet. Do not activate this gate on production or
remove its state to resume jobs. Implement authenticated reconciliation, safe
result replay and worker handoff before rollout or enabling the revocation UI.

### Durable historical results (sixth implementation slice)

The experimental synchronous dispatch gate saves its successful result envelope
before the first HTTP result attempt. `mutation-result.json` uses the existing
private-file atomic rename and file/directory fsync helpers under the shared
runtime lock. Receipts are bounded (512 KiB envelope, 1 MiB stored receipt),
private (0600), and bound to Manager URL, Agent ID, server ID, task ID and the
exact durable intent digest. The binding stores no token or rendered config.
The existing report itself may contain command output and must be treated as
sensitive local state; it is never included in recovery logs. A persistence
failure prevents result delivery and retains the intent.

After Manager's identity-bound fencing acknowledgement, each task poll first
attempts delivery of an unacknowledged receipt, even if Manager no longer returns
the task. Delivery uses the saved JSON without decoding/re-encoding numbers,
the current Agent token, and strict acknowledgement semantics. HTTP 404 remains
uncertain, including when Manager previously committed the result and lost its
response. This slice does not add a Manager result-lookup/idempotency contract.
Missing, malformed, substituted, publicly readable or differently bound receipts
cannot authorize replay. A missing receipt never authorizes runtime retry.

**Delivery is not recovery authorization.** Replay invokes no runtime handler
and never clears the mutation intent. After positive HTTP acknowledgement it
durably marks the receipt acknowledged to avoid repeated delivery; both receipt
and intent stay available for future authenticated reconciliation. Neither
historical success nor a fresh heartbeat proves that the runtime remains in the
same state after a restart. The original live owner can remove its unchanged
receipt and intent only after its successful handler and result acknowledgement.
A crash between those cleanup steps conservatively leaves recovery required.
Both legacy admission and scoped removal reject leftover receipt state.

Isolated tests kill an actual Agent test subprocess after the result request
arrives but before acknowledgement. A new Runner replays the identical report
with no runtime execution and no task returned by the Manager fixture. Tests
also cover receipt write failure, identity changes, corrupt/unsafe files,
competing admission, missing/changed intent, lost acknowledgement and sticky
restart protection. The mode remains off by default. Authenticated operator
reconciliation/unlock, Manager exact-result evidence and detached-worker handoff
remain prerequisites for production rollout; no revocation endpoint is enabled.

### Manager historical-result verification (seventh implementation slice)

`POST /api/v1/agent/tasks/{job_id}/result/verify` is an authenticated, read-only
observation endpoint for Agents with persisted `runtimeMutationFencingV1`.
It accepts the original bounded success envelope and compares its structured
payload with the stored successful config/service/install/maintenance job.
It requires the current Agent token and matching Agent/server ownership, an
empty stored error, and exactly one eligible job. It neither rewrites the result
nor updates job timestamps or lifecycle state. Unsupported task kinds, unknown
jobs and foreign identities cannot yield positive evidence. Responses are
`Cache-Control: no-store` and contain no stored report or credentials.

The response binds schema version, verified flag, task/Agent/server IDs and a
SHA-256 digest of the exact submitted envelope bytes. Payload comparison uses
PostgreSQL JSONB equality (object order and equivalent numeric formatting do not
matter), with `json.RawMessage` preserving numeric values during verification.
Omitted/null payload follows the existing completion contract's empty object.
This does not repair old payloads rounded by legacy completion decoding: if a
stored value differs from the Agent's durable envelope, verification fails
closed rather than guessing that they were equivalent.

When durable result delivery fails, Agent can use this observation to recognize
that Manager already committed the result before its HTTP response was lost.
It verifies all response bindings before marking the local receipt acknowledged.
An older Manager, rejected request, mismatched report, non-success job or missing
evidence leaves the receipt unacknowledged. No runtime handler is retried.

This closes the lost-response delivery ambiguity only. It does **not** authorize
clearing the local mutation marker, assert current runtime health, confirm VPN
credential revocation or terminate existing sessions. The fence remains held
logically after restart until a future authenticated reconciliation operation
checks fresh runtime evidence and explicitly authorizes recovery. No operator
unlock route is introduced here; the experimental mutation mode remains off.

Tests cover the Agent's rejection of identity/digest/schema mismatches and prove
that verified replay retains the fence. PostgreSQL tests exercise actual Manager
completion and verification handlers: initial commit, duplicate completion 404,
successful historical readback, unchanged result/timestamps, payload mismatch,
large exact integers, non-success state, foreign token and legacy capability.

### Runtime checkpoints before recovery authorization (eighth implementation slice)

Historical result acceptance alone is insufficient for recovering a legacy
mutation fence: older success envelopes do not identify the runtime process
that was running after the operation. A later hash of a file plus an active
listener cannot reconstruct that missing fact. Old receipts stay deliverable
but cannot produce recovery observations.

The experimental heartbeat gate now records an optional `runtimeWitness` in
the same atomic private receipt as its success envelope. This first scope is
sing-box/VLESS service **restart**, or config apply selecting exactly one
sing-box/VLESS adapter with service control enabled. Shared sing-box inbounds
remain part of the same measured config; this is not account-specific traffic
or revocation evidence. Other operations/adapters and multi-runtime applies
retain the previous conservative behavior and have no recovery checkpoint.

While holding the mutation lock, Agent brackets both pre-operation and
post-operation observations with the locally configured adapter's systemd
generation/source checks and active config reads. These reuse the removal
executor's /proc executable/config-source binding, versioned runtime digest,
enablement and listener checks. A checkpoint requires a new systemd InvocationID
after execution. Config apply must match the task's sing-box candidate hash;
service restart must retain its pre-operation config hash. The checkpoint stores
both process generations and the final hash/listener port, never a config or
command output. Failure to establish supported-scope evidence retains intent
and suppresses a terminal result. The first scope requires a healthy observable
baseline; repairing an already-unobservable runtime is not covered.

`ObserveRuntimeRecovery` is an internal read-only evidence collector, with no
HTTP, task-dispatch or operator command exposure. It acquires the same lock,
requires an acknowledged result receipt and matching local Manager/Agent/server
identity, then checks that the current file, process generation and listener
still match the saved checkpoint. Changed durable state during observation,
concurrent removal, malformed/missing proof or an old receipt fails closed. It
returns bounded task/identity/state digests and runtime measurements. It never
clears a marker, rewrites a receipt, restarts a service or restores credentials.
The initial capture is only a local observation; only the verified before/after
pair is eligible to be persisted as a recovery checkpoint.

An observation is **not an authorization or a freshness token**. The next
Manager operation must authenticate explicit operator approval, reserve the
node, bind the exact task/receipt/observation and a fresh challenge, and require
Agent re-observation under the lock at commit. A crash-safe audited release
protocol is still missing. Nothing in this slice permits manual file deletion,
automatic unlock, production activation or credential-removal dispatch.

Tests cover before/after checkpoint persistence, missing restart and changed
config rejection, old/unacknowledged receipts, identity mismatch, concurrent
locks and evidence changes during health checks. The disposable systemd/Reality
test also saves a witness from a real service restart, observes the exact process,
then proves another real restart invalidates it while the fence remains intact.

### Challenge-bound observation (ninth implementation slice)

`ObserveRuntimeRecoveryChallenge` is an internal Agent-only read-only primitive,
not a recovery endpoint or unlock grant. A typed request contains schema version,
operation UUID, a 256-bit nonce encoded as lowercase hex, issue/expiry timestamps,
and the exact expected observation from the previous slice. The future Manager
issuer must generate the nonce cryptographically and persist the request under
an authenticated operator approval and node reservation. The Agent checks nonce
shape, not entropy or uniqueness. No issuer or network exposure exists yet.

The request has at most two minutes of validity. Future-issued, expired and
overlong requests fail closed, without clock-skew grace. The collector propagates
the expiry as a context deadline, reuses the shared-lock observation and compares
every observation field, including the historical and current process identities.
It rechecks time and cancellation after observation and rejects a backwards clock
step visible between its start and finish. Manager/Agent clock synchronization is
an operational prerequisite, not an inferred property of heartbeat success.

The response includes the operation ID, observation time, measured observation
and SHA-256 of Go `encoding/json` serialization of the complete typed challenge.
This is a request-binding digest, not a signature or digest of arbitrary raw HTTP
JSON. A future cross-module protocol must pin serialization with shared golden
vectors before transport is added. The local Manager URL is checked through the
receipt binding and is not exposed in the response.

Read-only retries of the same nonce are allowed. This primitive does not consume
nonces, persist authorization, change runtime files, clear fences or guarantee
that the observed process will remain unchanged after it returns. A future
commit protocol must reserve the node, authenticate explicit approval, persist
one-time challenge consumption and audit, re-observe under the lock at commit,
and recover safely from every partial write. Historical delivery acknowledgement
and this response remain insufficient authorization for release. All dispatch,
operator UI and production activation remain absent.

Tests cover exact request binding, identical retries, nonce changes, every
observation identity/hash/generation mismatch, deadlines, cancellation, clock
regression, concurrent admission, unchanged durable files and retained fences.

### Manager recovery preparation and admission (tenth implementation slice)

Migration 000161 adds an internal `runtime_recovery_preparations` ledger with
only `prepared` and `cancelled` states. `PrepareRuntimeRecovery` reserves a node
for a previously successful config apply or VPN service restart. It checks the
current Agent identity/generation, recent authenticated heartbeat, persisted
experimental fencing capability, exact historical success payload and absence
of competing jobs, transfers, revocations and Manager updates. JSON numbers are
preserved, duplicate keys are rejected, and the full supplied result is compared
but never stored in this ledger or logged. The ledger retains only identifiers,
claimed marker/receipt hashes, exact envelope digest and operator attribution.

The sticky `runtimeMutationFencingV1` timeout-protection policy is not a current
capability advertisement. A separate `current_runtime_mutation_fencing` column
defaults to false and is set only from an explicit true value in the latest
bearer-authenticated heartbeat. Omission, false, identity-only heartbeat and
Agent replacement clear current support without removing sticky protection.
Preparation requires both. Heartbeat freshness uses `clock_timestamp()`, not
transaction-start `now()`, so lock waits cannot extend the admission window.

This is **not operator approval or authenticated Agent evidence**. No HTTP route,
challenge issuer, outbox, dispatcher or release exists here. The future caller
must authenticate the administrator and Agent evidence independently; it must
not derive the actor from untrusted request content. Preparation hashes are
claims to pin a later operation, not proof that the Agent holds those files.
Current process/file evidence must still be obtained with the challenge protocol.

Existing database admission guards now also reject mutations while recovery is
prepared: jobs, config versions, account/config-affecting preferences, node
settings, transfers, Agent rotation, routing and Manager updates. Revocation
ledger insertion has an explicit guard because it does not create a task.
The reservation serializes through the shared Manager-update advisory lock,
routing table locks and node row, with Agent identity locked by the service.
Heartbeat remains writable. Exact retries return the original preparation;
they do not renew readiness or assert that runtime is healthy.

Insertion and cancellation append immutable audit events in the same transaction.
Metadata cannot be rebound, a false confirmed state cannot be stored, and rows
or events cannot be deleted. There is no expiry worker or timeout release.
`CancelRuntimeRecovery` only releases never-dispatched Manager admission, never
an Agent fence, runtime credential, subscription token or account status. A
downgrade refuses to discard even cancelled recovery history. Migration 000160
guards are restored on an empty-ledger downgrade.

Tests cover exact retry across service instances, concurrent idempotent requests,
payload precision and malformed input, stale/unsupported Agent and task states,
conflicting jobs/revocations, immutable history, heartbeat availability and
unchanged accounts/tokens/config versions/jobs. Authenticated approval, full
observation binding, challenge issuance/consumption and crash-safe release remain
unfinished. No production schema migration or feature activation is authorized.

### Authenticated approval of read-only observation (eleventh implementation slice)

`ApproveRecoveryObservation` is an internal service method with no HTTP route,
UI, Agent dispatcher or unlock command. It requires explicit confirmation to
collect **read-only evidence**, an authenticated active human admin/super-admin
context with `configs:apply`, and a current matching database session. User,
session, role and permission records are stabilized under short transaction
locks, then rechecked against the database rather than trusting stale context
claims. Actor and session IDs come from authentication, not input fields.
This conservative first scope excludes operator/read-only and portal identities.
Only the approving user's/session's rows are locked; unrelated session updates
remain writable. RBAC mappings are table-locked to stabilize permission removal,
and lock waits have a two-second limit. There is no network I/O inside approval.
Node/preparation/Agent locks are acquired before authority locks, so a busy node
does not block unrelated session updates or role maintenance while admission waits.

Under node/preparation/Agent locks, it requires an active reservation, exact
task/Agent/server/marker/receipt/envelope binding and current authenticated Agent
readiness. The expected runtime witness is strictly parsed, bounded and checked
for valid identifiers/hashes/generations. It remains a **claimed observation**,
not verified current runtime evidence. Agent must independently collect and
compare it; permission to request evidence is not permission to release a fence.

Migration 000162 persists the approval as its own immutable audit record, with
actor/session attribution, complete typed challenge bytes and digest. A random
256-bit nonce and a two-minute deadline are created once per preparation.
Exact retries by the same currently authorized user return identical bytes and
never renew the deadline. Expired, cancelled, rebound or foreign-user requests
fail closed. Cancellation is still safe because no mutation dispatch exists;
it invalidates Manager-side approval retrieval. A previously copied request
could still elicit a read-only observation until expiry; it cannot change state.
No nonce consumption or mutation grant is implied by issuing this request.

The shared `testdata/recovery-challenge-v1.json` golden vector pins the Manager
and Agent typed JSON serialization and SHA-256. PostgreSQL tests cover live
session/permission rejection, exact binding, concurrent idempotence, immutable
audit, cancellation, expiry without renewal and retained reservations. Downgrade
refuses to delete even expired/cancelled approval history. No secrets or runtime
configs are stored in this approval record.

Authenticated transport, response verification, one-time mutation authorization,
commit-time re-observation and crash-safe audited release remain unimplemented.
Issuance does not contact an Agent, revoke credentials, terminate sessions, change
account status, clear a fence or activate the experimental mode in production.

### Authenticated observation transport and retained evidence (twelfth slice)

The existing bearer-authenticated `GET /api/v1/agent/tasks/next` pull transport
now returns an optional `recoveryChallenge` in place of a runtime task. There is
no second poller, inbound Agent listener, job producer or mutation dispatch.
The composition root injects the recovery service into the Agent handler to
avoid a dependency cycle through config rendering and traffic services. The
existing HTTP client, Manager URL and deployment transport trust are reused;
production requires the configured authenticated HTTPS Manager connection.
No operator preparation/approval endpoint or UI is exposed by this slice.

Polling reads an already approved challenge. Under server, preparation and Agent
locks it checks prepared state, current token, credential generation, current
explicit capability, sticky timeout protection and recent authenticated heartbeat.
Approval, delivery and acceptance also require distinct current
`runtimeRecoveryObservationV1` support. It is advertised only by the new Agent
with fencing mode/state and a usable local VLESS observation adapter. Manager
retains it only from an explicit true authenticated heartbeat; false, omission,
identity-only heartbeat and replacement clear it. Sticky fencing never implies
transport support. Old Agents cannot consume a challenge they cannot answer.
In fencing mode or with retained fence state, the existing heartbeat/poll loop
caps its interval at 30 seconds, preserving shorter configured intervals and
ordinary unfenced cadence. There is no extra polling loop. Long configured
intervals therefore cannot outlive the challenge or authenticated readiness.
Clock checks use `clock_timestamp()` after lock waits. Responses are `no-store`.
Cancelled, expired or accepted preparations are not delivered. A challenge copied
before cancellation can still collect read-only data until expiry, but cannot
have its evidence accepted after cancellation. No runtime authority is conveyed.

The Agent loop calls the existing locked process/config/receipt observer with
its local Manager/Agent/server binding and configured VLESS adapter. It sends
`POST /api/v1/agent/recovery-observations/{operation_id}/evidence` only after the
collector proves the complete expected observation. This is a separate result
type: it cannot terminalize the old runtime job through ordinary task completion.
Heartbeat is readiness metadata, not evidence of working VPN traffic.

Acceptance repeats token/generation/readiness and cancellation checks under the
same reservation locks. The bounded 8 KiB response rejects duplicate keys,
unknown or aliased field names, trailing input and malformed observations. It
must bind the exact operation, typed challenge digest (including nonce), task,
Agent/server, marker/receipt/envelope and every witness field. Observation time
must be within the challenge interval and no later than Manager wall time.
Manager and Agent clocks must be synchronized; there is no skew grace.

Migration 000163 retains the first accepted typed response, its digest, challenge
digest, observed time and database acceptance time as immutable history. The
database guard rechecks reservation, bindings, time and Agent readiness at insert.
The reservation lock serializes cancellation and concurrent acceptance. Identical
typed retries acknowledge the same record; changed observations cannot overwrite
it. Reordering transport object keys does not change the typed response digest.
Acknowledgements bind schema, operation, challenge and typed response digest.
Retries repeat identical bytes; an in-memory Agent cache also preserves those
bytes across subsequent polls. Restart may re-observe but cannot replace accepted
evidence. A lost acknowledgement does not clear the fence. Late retries after
expiry/cancellation fail closed; the stored record remains available for future
authenticated readback work. Downgrade refuses to discard retained evidence.

This record is authenticated historical observation, not a mutation grant,
nonce consumption or proof that the runtime stays unchanged afterward. Agent
durable state, runtime config/process, account credentials and subscription
tokens are unchanged. No release, re-observation-at-commit, crash-safe unlock or
end-to-end credential revocation exists yet; #545 remains open. No production
activation, migration or deployment is authorized by this development slice.

### Internal mutation-grant issuance (thirteenth slice)

`IssueRecoveryMutationGrant` is an internal Manager service method, with no HTTP,
polling, CLI, worker, operator UI or Agent consumer. It creates at most one
immutable grant per recovery preparation, only after authenticated evidence has
been retained. This slice implements single issuance, not one-time execution:
nonce consumption, delivery and actual release remain absent and must not be
inferred from the ledger or its JSON contract.

The caller explicitly confirms fence recovery and supplies the exact challenge
and accepted-response hashes. Actor/session identity comes only from current
human admin/super-admin authentication with `configs:apply`. Read-only approval
does not authorize issuance. The same human who approved observation must issue
the grant in this conservative scope; current authority is rechecked against
database user/session/RBAC records. Shared authority locking is reused from
observation approval, after server/preparation/Agent admission. Unrelated
sessions stay writable while a contended node waits.

Issuance rechecks prepared reservation, current Agent token hash and credential
generation, current fencing/observation capability, authenticated heartbeat
freshness and the original observation deadline. It strictly decodes and hashes
the immutable accepted response, compares the exact preparation/challenge,
measured observation, process identities, config and receipt hashes. It never
uses an operator-supplied expected snapshot as authenticated evidence. Heartbeat
remains readiness metadata and does not prove VPN traffic or continued process
stability. Session expiry and wall time are checked again after authority waits.

The typed contract pins schema/purpose, preparation ID, a new cryptographically
random 256-bit nonce, Agent credential generation, challenge/response hashes,
issue/expiry times and the complete expected observation. Grant expiry is the
original challenge expiry, so issuance cannot renew observation freshness.
Retries by the same currently authorized actor return identical bytes/hash;
concurrent issuers serialize on the node. Expired, cancelled, unsupported,
unattributed or mismatched requests fail with generic secret-safe errors. The
nonce is future authorization material and must not be logged or exposed through
ordinary task/result, diagnostic or subscription APIs.

Migration 000164 retains exact grant bytes, digest, actor/session and timestamps
as immutable audit history. The database guard independently checks evidence,
bindings, current readiness, deadline and human authority. Unique preparation
and nonce constraints prevent multiple issuances. Update/delete and destructive
downgrade are refused. A grant-bearing reservation cannot use ordinary Cancel,
even after expiry: timeout cannot prove a copied authorization was unused.
Future abort/reconciliation must establish a safe release before admitting new
node mutations; there is no automatic expiry/unlock or manual bypass here.

A future transport must negotiate distinct current grant/commit support rather
than assume observation capability implies mutation support. A consumer must
validate the exact grant and current identities, re-observe under the Agent
mutation lock, hold that lock through commit, durably consume the nonce and
recover safely from every partial write. Neither this grant record nor its
issuance acknowledgement can release Agent state. No config/credential/account,
subscription or runtime job is changed. #545 remains open; no production
migration, activation, merge or deployment is authorized.

### Agent locked re-observation and local grant consumption (fourteenth slice)

`BeginRuntimeRecoveryCommit` is a dormant internal primitive. There is no grant
HTTP delivery, dispatcher, advertised commit capability, Manager consumption
endpoint or fence-release method. Callers must first authenticate the Manager
grant and independently establish current Agent credential generation; copying
the generation from grant JSON into the local binding is forbidden. The current
Agent credential-generation negotiation still needs integration before activation.
The shared grant golden vector now pins both Manager and Agent typed JSON/hash.

The bounded strict decoder rejects duplicate/unknown/aliased fields, trailing
input, malformed identities/hashes/witnesses, wrong purpose/generation and
overlong lifetime. The local Manager/Agent/server binding must match the durable
acknowledged task receipt and grant. Clock synchronization remains required;
future-issued, expired and backwards wall-clock observations fail closed.

The primitive acquires the existing private shared admission flock and reuses
the existing observer without releasing/reacquiring the lock. It verifies the
exact marker, acknowledged receipt, envelope, running process/config/listener
and historical/current invocation identities against the grant. No runtime
validation/apply/restart/credential mutation is invoked by this code.

Before returning a live owner or proof it atomically writes and fsyncs private
`recovery-consumption.json`, binding the complete grant and digest, current local
identity/generation, observed time and measured state. It re-observes after
persistence while still holding the lock. The retained record gates duplicate
consumption, including a different grant: there is no auto retry, cleanup or
resume of an interrupted consumption. Failure after publication retains the
record and original fence even when no caller received a proof.

The returned lease keeps flock until explicit `Close`, which only releases the
process lock. `Proof` rechecks the unchanged durable record and independently
re-observes current runtime under that same lock; it returns only operation,
grant/consumption digests, time and measured observation. No nonce, Manager URL,
local path, runtime config or credential is exposed. Drift, cancellation, expiry,
lost state or a visible backwards clock step permanently poisons the live owner.
Future network commit must keep this owner alive and re-observe through it;
historical proof alone cannot authorize a later unlock.

Nonce consumption is local durable intent, not Manager commit or successful
recovery. Process exit drops kernel flock but preserves consumption/receipt/fence.
Ordinary mutation admission/completion, result persistence/replay and scoped
removal reject even malformed or isolated consumption records. This protection
survives flag removal because the shared state directory remains present.
Read-only observation stays possible; mutation admission never becomes a fallback
when consumption cannot be validated. No Complete/Unlock/delete API is exposed.

Tests cover exact wire/hash, shared-lock ownership, repeat and concurrent
consumption, generation/identity/witness/time rejection, drift before and after
persistence, poisoned owners, unchanged runtime/receipt/fence, consumption-only
admission guards, subprocess exit and SIGKILL. The existing disposable systemd/Reality
test exercises this primitive against the real process, holds the lock, then
intentionally simulates an external service restart and requires proof rejection
while preserving consumption and fence. Test restarts are isolated fault injection,
not commands performed by the commit primitive.

Authenticated grant transport/current-generation negotiation, Manager commit
acceptance, reconciliation/abort and crash-safe fence release remain unfinished.
No production activation/deployment or real-user revocation is authorized; #545
remains open. No schema migration is added; Manager schema remains 000164.

### Independently verified read-only grant delivery (fifteenth slice)

`POST /api/v1/agent/identity/verify` checks current bearer credentials without
renewing heartbeat, claiming tasks or measuring VPN health. A strict bounded
request binds configured Agent/server IDs and a fresh Agent-generated 256-bit
nonce. Manager returns current positive credential generation and SHA256 of
the exact request bytes. Replaced tokens/foreign IDs cannot establish binding.
Authenticated Manager transport remains the existing trust boundary; the
request hash binds freshness and is not an independent signature.

`POST /api/v1/agent/recovery-observations/{operation_id}/grant` returns only an
existing immutable grant. A separate nonce-bound request includes independently
verified generation. Under existing server/preparation/Agent locks, Manager
rechecks current token/generation, prepared reservation, exact challenge/evidence
and current authenticated heartbeat readiness. Distinct current capability
`runtimeRecoveryGrantReadV1` is required. False/omitted/nil/identity-only heartbeat
and replacement clear it; sticky fencing remains. This advertises read support,
never commit/unlock support.

Grant issuer/session come from immutable attribution and undergo current DB
active-human/admin/configs:apply checks. Authority locks follow node locks and
cover only that user's/session's rows plus role/permission mapping tables.
Deadline/session expiry/heartbeat freshness use wall time after waits. Refusal
does not cancel grant-bearing reservations. Retries/concurrent reads return
identical authority/hash/nonce/deadline, with individual request hashes. Read
nonces are not consumed or stored; no Agent/account/token/config/job is mutated.

`Client.FetchRecoveryGrant` verifies identity first; generation is never copied
from grant JSON into the trusted binding. Both reads reuse configured HTTP
transport/timeout, refuse redirects, bound response size and reject
duplicate/unknown/aliased/missing fields, trailing JSON, invalid bindings/hashes
and expired/future authority. Errors expose no bearer, nonce, response body or
network URL. `ValidateRuntimeRecoveryGrant` checks wire data without local state.
No Runner or dispatcher automatically invokes fetch or local consumption.

Delivery is historical authority, not a live-owner commit or permission to
release a fence. Credentials/issuer/runtime can change after the reply; future
Manager commit must recheck them while Agent retains its admission owner and
re-observes. Manager commit acceptance/readback, automatic consumption integration,
abort/reconciliation and crash-safe fence release remain absent. Existing golden
wire/hash contracts and schema 000164 are unchanged. #545 remains open; no
production activation, real-user revocation, merge or deployment is authorized.

### Manager commit-proof recording and historical readback (sixteenth slice)

`POST /api/v1/agent/recovery-observations/{operation_id}/commit` records the
Agent's typed live-owner proof once in immutable `runtime_recovery_commit_evidence`
(migration 000165). The shared `recovery-commit-v1.json` golden vector pins field
order and hash against Agent `RuntimeRecoveryCommitProof`. No Agent submission
client, Runner dispatch or advertised commit/unlock capability is added here.

Admission reuses grant delivery's server/preparation/Agent and issuer authority
locks, current bearer/generation, exact immutable challenge/evidence, prepared
reservation and current fencing/observation/read support. Heartbeat is readiness
only. Grant hash pins the nonce and complete grant; observation must match its
exact expected task/Agent/server/runtime/process/listener/receipt state.
`observedAt` must be within the grant lifetime and no later than Manager wall
time. Session/deadline/heartbeat are rechecked after waits and before commit.

One record per preparation binds grant hash, local durable consumption digest,
exact typed proof bytes/hash, indexed observation time and immutable acceptance
time. Consumption digest is unique. The DB trigger independently enforces
current readiness, binding and issuer authority in the same lock order, rejects
updates/deletes, and downgrade refuses retained history. JSON timestamp parsing
preserves sub-microsecond wire precision without pgx truncation mismatches.
Identical concurrent/retry requests reuse the same record without renewing time;
different observation time/consumption/grant/state cannot replace it. Fresh
admission still applies to retries; expiry or authority loss cannot be bypassed.

`POST /api/v1/agent/recovery-observations/{operation_id}/commit/verify` observes
the exact retained proof using current bearer/generation and Agent/server IDs.
It performs no writes/admission locks and deliberately remains historical after
grant expiry, issuer revocation or heartbeat/capability loss. Different or absent
proofs and replaced/foreign credentials fail closed. Both responses include
`recorded`, canonical `proofHash` and SHA256 of exact request bytes, with no-store
and bounded strict payloads; errors/logs expose no raw data.

Missing/conflicting proof is a definitive rejection. Query/connection/time-out
failures preserve uncertainty and produce a sanitized 500, never a false 409
claim of absence. Shared admission also preserves infrastructure errors. A
caller must retain durable state through any indeterminate acknowledgement.

Recorded evidence is an authenticated Agent claim, not remote attestation of
continued local flock ownership or working VPN traffic. Receipt/readback is
storage acknowledgement, never unlock permission. Reservation/grant/Agent fence,
accounts, subscription tokens, config and jobs are retained. Future Agent client
must freeze submitted proof, keep its live local owner through uncertain HTTP
and historical readback, and re-observe before any separately designed crash-safe
release. Automatic consumption/submission integration, abort/reconciliation and
fence release remain unfinished. #545 remains open. Schema 000165 applies only
to isolated development/tests; no production migration, merge or deployment.

### Dormant Agent commit submission with a live owner (seventeenth slice)

`Client.SubmitRecoveryCommit` accepts only an existing concrete
`RuntimeRecoveryCommit` and a previously independently verified grant binding.
Manager URL, exact grant hash, identity/generation and lifetime must match.
It adds no Runner dispatch, operator UI or advertised commit/unlock capability.

`WithSubmission` holds the owner's mutex and admission flock through synchronous
HTTP, retries and readback. Concurrent `Close` waits. A short-lived guard exposes
only measured proof and durable pinning; escaped/zero guards fail closed. Fresh
observation checks original consumption and current config/process/listener;
drift, cancellation, expiry, backwards time or changed submission bytes poison
the owner. Callback errors are sanitized and never erase state.

Before first HTTP, `PinProof` atomically writes/fsyncs private
`recovery-submission.json` with exact typed proof bytes. Consumption remains
unchanged, so its digest stays stable. The same live owner returns identical
pinned proof across retries and separate submit calls while independently
re-observing current state. No rewrite, nonce renewal or automatic process resume
exists. Publication errors retain any durable files and prevent HTTP.
Malformed/standalone submission state also blocks ordinary mutation admission,
completion, result save/replay and scoped removal, including after flag removal.

The client reuses bounded strict recovery transport, rejecting redirects,
duplicate/unknown/aliased/missing fields and trailing/oversized replies. It sends
one frozen body to commit and exact historical readback; changing observation
time on retries would conflict with immutable Manager evidence and is forbidden.
Every exchange is bracketed by fresh local proof checks, and a final check occurs
before the owner guard returns. All receipt hashes/bindings must match exactly.
Retries are bounded by the original grant deadline and existing attempt/backoff
limits. HTTP/network/parse uncertainty remains unconfirmed, never safe absence.
Readback can confirm storage after an uncertain or rejected POST but cannot heal
local drift or turn a historical receipt into release permission.

Neither success nor failure closes the owner, applies config, restarts runtime,
deletes consumption/submission/receipt/fence or consumes a second grant. Exit and
SIGKILL drop kernel flock but retain durable submission and consumption; a new
owner cannot automatically resume. Manager acknowledgement is not persisted as
an Agent release receipt here. Abort/reconciliation and crash-safe fence release,
operator workflow and end-to-end account credential revocation remain unfinished.
#545 stays open. Schema remains 000165; no production activation/merge/deploy.

### Durable Agent historical storage acknowledgement (eighteenth slice)

After strict transport verification, `Client.SubmitRecoveryCommit` calls the
still-live submission guard's `RecordReceipt` before returning success. Manager
receipt fields now share the Agent tasks type; a common acknowledgement golden
vector pins unchanged Manager/Agent field order and hash. Schema remains 000165.

The guard independently matches schema/recorded/operation/grant/consumption and
proof/request hashes against exact private pinned bytes. An unpinned or mismatched
receipt cannot be recorded. It re-observes current runtime before and after
atomic write/fsync of private `recovery-acknowledgement.json`. Same-owner retries
reuse identical retained bytes without renewal/overwrite. Concurrent Close waits
through acknowledgement persistence and the final observation.

The owner checks unchanged acknowledgement alongside consumption/submission on
every proof. Unexpected, malformed, removed or changed acknowledgement poisons
ownership; publication uncertainty retains files and never returns a confirmed
result. Standalone acknowledgement also blocks mutation admission/completion,
result save/replay and scoped removal, including after experimental flag removal.
Callback/transport/storage errors expose no sensitive payloads or paths.

Receipt persistence only records Manager's historical storage statement. It
proves neither current issuer authority nor VPN traffic nor future release
permission. No marker/receipt/consumption/submission/acknowledgement is deleted,
and no config validation/apply/restart or account/token mutation is invoked.
Process exit/SIGKILL retain acknowledgement and all fence state; a new owner
cannot resume even with a valid acknowledgement. Read-only crash reconciliation,
fresh release authority and the crash-safe release protocol remain future work.
No Runner dispatch/operator activation, production access/change, merge or deploy
is authorized. #545 remains unfinished.

### Read-only crash reconciliation (nineteenth slice)

Dormant `Client.ReconcileRecoveryCommit` independently authenticates current
Agent identity/generation using the existing nonce-bound identity exchange, then
reads a crashed owner's exact pinned proof under the shared admission flock.
It calls only the existing historical `/commit/verify` endpoint, never commit,
grant delivery or mutation dispatch. Token/generation replacement fails closed;
Manager independently rechecks current credentials during readback.

The existing lock implementation now has a non-provisioning read mode: private
directory and lock must already exist; neither is created. Consumption and
submission must be exact typed JSON, with valid original grant structure and
historical timestamps, matching Manager/Agent/server/generation, grant and
consumption hashes, exact expected observation, original acknowledged task
receipt/intent and witness bindings. Optional local acknowledgement must exactly
match the pinned proof. Malformed/standalone/foreign state is not adopted.
All retained bytes and absence of a competing removal intent are checked before
and after synchronous HTTP. A live owner or concurrent reader retains exclusive
admission; changes/cancellation/transport failures return sanitized uncertainty.

Grant expiry and current runtime drift do not invalidate historical storage.
No current runtime observation or heartbeat is treated as VPN traffic proof.
The returned receipt is checked against every exact pinned proof/hash field;
absence, credential rotation or network uncertainty never mean safe absence.
There are no retries that submit new authority and no local write, deletion,
acknowledgement adoption, lease resume or fence release. Original state remains
blocked, including after exit/SIGKILL. Caller can retry this read independently.

Fresh release authority, crash-safe fence release, operator workflow and
end-to-end account credential revocation remain future work. Schema remains
000165; #545 remains unfinished. No production activation/merge/deploy.

### Fresh conditional final release authorization (twentieth slice)

Internal `IssueRecoveryReleaseAuthorization` requires a new explicit
`ConfirmFinalFenceRelease` command from an active human admin/super_admin with
`configs:apply` and a current authenticated session, independently rechecked
under database locks. The actor may be a different currently authorized human:
this is a new authorization with its own immutable user/session attribution,
not adoption or renewal of the original consumption grant. Repeats require the
same issuing user/session and recheck current authority/readiness.

The command supplies exact preparation, original grant, consumption and stored
commit-proof hashes. Manager validates the retained typed proof/grant hashes,
expected observation, historical timestamps and preparation bindings; expected
is derived from authenticated immutable evidence, not caller-supplied runtime
data. Current Agent credentials/generation, current fencing and observation/read
capabilities and fresh authenticated heartbeat are rechecked. Heartbeat indicates
transport readiness only, never VPN traffic or current measured runtime.

Schema 000166 retains one immutable conditional final authorization per
preparation, with a distinct purpose, random nonce and exact
grant/consumption/commit-proof/Agent-generation/expected bindings. Lifetime is at
most two minutes, capped by the issuing session's current expiry. Expired
original authority can be referenced as history, but no original deadline,
nonce, evidence or attribution is renewed. Identical repeats keep original
bytes/hash/deadline; different actor/session cannot adopt them. An expired final
authorization is never silently renewed; future explicit abort/reconciliation
is still required for such a blocked reservation.

Admission reuses existing server/preparation/Agent locking. Only after node
admission are role mappings and the confirming user/session locked. SQL guards
repeat current authority/readiness/exact-binding checks, reject update/delete
and prevent bypass through direct inserts. Downgrade takes ACCESS EXCLUSIVE
before checking retained history, so a raced insert cannot be discarded.
Issuance changes no Agent, reservation, account, token, config or runtime job.

This authorization is conditional on a future Agent consumer independently
authenticating current generation, freshly re-observing exact durable state and
config/process/listener while holding the shared admission lock, durably
consuming the final nonce and using a crash-safe release protocol. Historical
commit proof/acknowledgement cannot substitute for that fresh local observation.
No delivery endpoint, Runner invocation, consumer, file cleanup or fence release
is implemented/activated here. Operator workflow and end-to-end credential
revocation remain unfinished; #545 stays open. No production/merge/deploy.

### Read-only final authorization delivery (twenty-first slice)

`POST /api/v1/agent/recovery-observations/{operation_id}/release-authorization`
delivers only existing immutable conditional authority. It reuses bounded
recovery HTTP handling and current bearer authentication. Exact operation,
Agent/server/current generation, original grant/consumption/commit-proof hashes
and a per-request nonce are required; response hashes bind exact request bytes.
Server/preparation/Agent admission precedes role mapping and issuer user/session
locks. Current fencing/observation/grant-read capabilities, authenticated
heartbeat and issuing human authority/session are rechecked. The final wall-time
check after lock waits rejects expiry, backwards time or shortened session
deadline. Original grant expiry does not renew historical authority.

Dormant `Client.FetchRecoveryReleaseAuthorization` independently negotiates
generation through existing identity verification, then reads exact private
historical state under flock throughout delivery. Existing strict bounded,
no-redirect recovery transport rejects ambiguous fields/payloads; the Agent
validates a separate final-authority contract against exact pinned proof bytes,
generation, observation, hashes and lifetime. Shared golden pins unchanged
Manager/Agent field order/hash. File changes or context/HTTP/parse uncertainty
remain sanitized failures; no error/log contains sensitive payloads.

No read issues/renews authority, consumes a nonce, resumes a mutation owner,
writes acknowledgement, changes config or releases a fence. No Runner calls this
method. Heartbeat is transport readiness only. Fresh local runtime observation,
durable final nonce consumption, crash-safe release/abort and operator workflow
remain future work. Schema stays000166 and #545 remains unfinished.

### Agent durable final consumption (twenty-second slice)

Dormant `BeginRuntimeRecoveryFinalization` owns the existing shared admission
flock through fresh observation and final authorization consumption. It reuses
the internal exact historical snapshot validator without reopening/releasing
the lock. Binding/generation must come from independently authenticated current
delivery, not authorization JSON. Expected original grant/consumption/pinned
commit proof and optional acknowledgement must remain exact. Final nonce must
be distinct from the original consumption grant nonce.

The consumer freshly verifies config/process/listener against the exact expected
observation before atomic write/fsync of private `recovery-final-consumption.json`,
then repeats observation and byte checks after publication. The retained typed
record binds authorization/hash, current identity/generation and measured state.
Expiry, cancellation, backwards wall time, file/runtime drift or publication
uncertainty retain files and prevent confirmed ownership. Repeated `Proof`
observations remain under the held flock; any drift poisons the owner permanently.
Proof exposes measured hashes/observation only, never nonce, credentials or paths.

There is no Complete/Release/resume method. Close serializes with observations
and drops only the lock, preserving original and final consumption plus all
fences. A new owner cannot consume/resume an existing final record. Public
historical delivery also refuses final-consumed state; no state is adopted or
overwritten. Standalone/malformed final state blocks normal mutation admission,
completion/result save/replay and scoped removal, including after flag removal.
Exit/SIGKILL release only kernel flock; final consumption/fences survive.

Disposable systemd/Reality regression consumes final authority under the actual
current process, deliberately restarts externally, then rejects stale final
proof while retaining all state. This is not Manager+Agent final release E2E.
No Runner/HTTP consumer activation, config apply/restart, file deletion, fence
release or Manager reservation release is implemented here. Crash-safe final
release/abort and operator credential-revocation workflow remain future work;
schema stays000166 and #545 remains unfinished. No production/merge/deploy.

### Manager final-consumption evidence (twenty-third slice)

Authenticated bounded `POST /api/v1/agent/recovery-observations/{operation_id}/finalization`
accepts the Agent's measured final-consumption proof. It reuses final-authorization
delivery admission and server/preparation/Agent then role mappings and only the
issuer user/session locks. Current credentials/generation, exact operation and
Agent/server bindings, current fencing/observation/read capabilities, authenticated
heartbeat, active human issuer authority/session and final lifetime are required.
Observation must exactly match the retained authorization and fall within its
window, never in Manager's future. Heartbeat remains transport readiness only.

Schema 000167 retains one immutable typed proof per preparation, with unique
final-consumption hash, exact authorization/proof hashes and measured/accepted
timestamps. SQL guards repeat current admission and exact-binding checks; history
cannot be updated/deleted or lost through a raced downgrade.
SQL retention checks raw JSON before jsonb conversion: duplicate keys at every
depth (including escaped names) are rejected, and compact typed field order,
scalar encoding and canonical RFC3339Nano timestamps are required. Direct inserts
cannot poison immutable history with wire bytes the Manager cannot confirm.
Identical retries recheck current authority, keep original acceptance time and
cannot replace proof.
Receipts separately bind canonical typed proof and exact request bytes. Shared
Manager/Agent golden pins final-proof field order/hash and nanosecond wire time;
indexed timestamps use PostgreSQL's JSON parser consistently.

`POST .../finalization/verify` reads only the exact retained proof under current
Agent credentials/generation and Agent/server bindings. Historical readback can
outlive issuer/session/heartbeat/authorization expiry; it never renews authority.
Database/context uncertainty remains a sanitized failure, not definitive absence.
No sensitive proof, nonce or credential is included in errors/logs.

Recording/readback proves immutable Manager storage only, not continued Agent
flock ownership or permission to release. No Agent final-submit consumer, Runner
activation, file cleanup, configuration mutation, fence or reservation release
is implemented here. Durable Agent confirmation, crash-safe release/abort and
operator end-to-end credential revocation remain future work. #545 stays open.
No production access/change, real-user revocation, merge or deploy.

### Agent final-proof submission and durable confirmation (twenty-fourth slice)

Dormant `Client.SubmitRecoveryFinalization` requires the concrete live final
owner and independently delivered binding/authorization hash. The owner retains
its mutex and existing admission flock throughout a synchronous submission
guard, HTTP retries/readback and receipt persistence. Escaped/copied/zero guards
cannot retain authority after the callback; Close waits for completion.

Before HTTP, atomic write/fsync of private `recovery-final-submission.json` pins
one exact measured proof. Every retry and `/finalization/verify` sends these same
bytes, including original observedAt. Existing strict bounded no-redirect recovery
exchange and shared commit retry policy are reused. Fresh locked observations
before/after HTTP check original/final private state, exact runtime witness,
binding, authorization lifetime, cancellation and backwards wall time. Readback
proves exact historical storage only and cannot repair local drift or revive an
expired owner. Bounded uncertain transport failure retains the pin for live-owner
retry without changing proof; no new process can resume it.

The verified seven-field receipt is independently matched to the pinned proof
and atomically/fsync retained as `recovery-final-acknowledgement.json`, followed
by fresh observation and byte checks. Shared Manager/Agent golden pins receipt
field order/hash. Repeated acknowledgement preserves original bytes; tamper,
publication uncertainty or runtime drift poisons the owner and retains fences.
Standalone/malformed final pin or acknowledgement guards all normal mutation
admission/completion/result replay/scoped removal and old historical delivery.
Exit/SIGKILL after pin or acknowledgement retains durable history and fencing.

Disposable systemd/Reality regression pins final proof and persists a synthetic
historical receipt under the actual stable process; injected external restart
rejects the old proof while retaining files. This is not Manager+Agent HTTP E2E.
No Runner invocation, file deletion, Complete/Release/resume method, configuration
mutation, fence or Manager reservation release is activated. Crash-safe release,
read-only final crash reconciliation/abort and operator credential-revocation
workflow remain future work. Schema remains000167 and #545 stays unfinished.
No production access/change, real-user revocation, merge or deploy.

### Read-only final crash reconciliation (twenty-fifth slice)

Dormant `Client.ReconcileRecoveryFinalization` independently authenticates current
Agent credentials/generation through the existing identity endpoint, then reads
exact private history under the existing admission flock. It calls only the
existing `/finalization/verify` with unchanged pinned proof bytes; no new proof,
authorization fetch, submission retry, receipt persistence or owner resume occurs.

`WithHistoricalRecoveryFinalization` reuses the original locked historical
intent/result/consumption/pinned-commit/optional-acknowledgement validator. It also
checks exact typed final consumption, final authorization/hash and its historical
binding to original commit, distinct nonce, measured timestamps/window, final
proof/hash/observation and optional final receipt. Original and final files are
checked before/after the synchronous callback; optional acknowledgements must
remain absent or unchanged. The same flock excludes a live owner or another
reader. Missing/unsafe/malformed private history never reaches HTTP and no
directory/lock/history file is created to repair it.

Final authorization is validated at its original issuedAt only for historical
consistency, not at current time as renewed authority. Expiry or actual current
runtime drift can coexist with exact historical Manager storage confirmation.
No runtime adapter is invoked and no current VPN readiness/traffic is inferred.
Absent Manager evidence, rotated credentials, malformed/foreign replies,
redirects, context/transport uncertainty or file drift remain sanitized
unconfirmed failures. A crash after consumption but before pin remains blocked;
readback cannot synthesize missing proof or adopt a mutation owner.

Exit/SIGKILL regressions now read exact final history after process death; real
systemd/Reality regression reads historical final proof after injected restart
while live proof is rejected. Both retain all files/fences. No Runner activation,
cleanup, configuration mutation, fence/reservation release or operator workflow
is implemented here. Coordinated explicit abort/crash-safe release and end-to-end
credential revocation remain future work. Schema stays000167; #545 stays open.
No production access/change, real-user revocation, merge or deploy.

### Agent prepared retirement intent (twenty-sixth slice)

Dormant `RuntimeRecoveryFinalizationGuard.PrepareRetirement` stages the durable
barrier required before future multi-file retirement. Only a still-live final
guard holding the original mutex/flock, unexpired conditional final authority,
exact pinned proof and verified historical receipt can prepare it. Fresh locked
config/process/listener and original/final byte checks occur before manifest
capture, immediately before atomic publication/fsync and after publication.
Receipt/readback alone cannot substitute for current final-owner observation.

Private `recovery-retirement-intent.json` binds explicit prepared purpose,
operation, independent identity/generation, final authorization/consumption/proof/
receipt hashes, measured observation/time and ordered manifest of eight exact
source files. Optional original acknowledgement records its exact absence; final
acknowledgement is required. Filenames are a fixed internal list, never caller
paths. The only returned value is the intent digest, not release authority or
completion evidence. Repeats preserve original bytes/time/hash and recheck live
authority/runtime; no nonce or deadline is renewed.

Publication uncertainty, expiry, backwards wall time, source/runtime/file drift
retain all state and prevent confirmed preparation. Owner poison is permanent.
Standalone/malformed intent guards normal mutation admission, completion/result
save/replay, scoped removal and old public historical delivery. New final owner
cannot resume existing intent. Close releases only flock and deletes nothing.

Read-only final crash reconciliation validates optional intent and its exact
manifest/receipt/window against retained history and includes it in before/after
snapshots. It can confirm final-proof storage only, never report completed
retirement or current runtime readiness. Exit/SIGKILL after intent retain fences;
real systemd/Reality restart invalidates live proof while exact prepared history
remains readable. No Agent config mutation, deletion, fence/reservation release,
Runner invocation, Manager endpoint/schema change or operator workflow exists in
this slice. Atomic terminal retirement and coordinated abort/crash-safe release
remain future work. Schema stays000167; #545 remains unfinished.
No production access/change, real-user revocation, merge or deploy.

### Agent terminal archival retirement (twenty-seventh slice)

Dormant `RuntimeRecoveryFinalization.RetirePrepared` may run only on the original
still-live final owner after its submission callback returns. It retains the
same mutex/flock and requires exact prepared intent, pinned proof/receipt,
current conditional authority/window and fresh bracketed config/process/listener
observation. No new owner or crashed process can resume the operation.

Copy every fixed manifest source plus the prepared journal into a new private
staging directory using atomic file write/fsync. Validate the complete typed
historical chain, exact manifest and private ownership/mode/link counts; fsync
and atomically rename the directory to `retired-{operationId}` under the stable
admission root. The root directory and `runtime.lock` inode are never renamed or
recreated. Unknown paths/files, existing archive or publication uncertainty fail
closed without automatic overwrite or staging cleanup.

Only after complete archive durability and another fresh observation is private
`recovery-retirement-terminal.json` atomically/fsync retained. This is an archive
record, not fence-release authority or Manager acceptance. Then redundant active
source copies are individually hash-checked and removed with directory fsync,
with current authority/time/context and fresh archived/runtime checks through
cleanup. Original bytes remain recoverable in the complete private archive.
Keep active `mutation-inflight.json` unchanged as a compatibility fence for older
Agent binaries. Prepared journal and terminal record also remain sticky barriers,
including when standalone/malformed or experimental dispatch is disabled.

Failure at any copy/publish/cleanup boundary retains barriers and available
history; owner poison prevents automatic continuation. Same live successful
owner repeats recheck authority/runtime and preserve terminal bytes/time/hash.
Ordinary final proof/submission methods cannot adopt terminal state. Close drops
only flock; there is no actual Agent unlock or Manager reservation release.

Historical final reconciliation selects a terminal archive only through its
exact private record and current credential-generation binding. Validate archive
chain, journal equality, terminal hashes/window/observation and any remaining
active copies, retaining before/after snapshots under the original root flock.
Read-only confirmation never completes interrupted cleanup or infers current
VPN traffic/readiness. Root phase changes during an active-history read fail
closed. Expired historical authority remains history only.

Crash-cut regressions cover every copy/cleanup plus archive/terminal publication
with SIGKILL and representative exit points; legacy/new fences survive. Real
systemd/Reality performs terminal archival, then injected external restart rejects
fresh terminal-owner checks while historical readback retains all archived state.
This is not Manager+Agent release E2E. No Runner activation, config mutation,
credential revocation, actual fence release, Manager terminal-evidence endpoint/
ledger, reservation release or operator workflow is implemented here. Those and
coordinated abort/release remain future work. Schema stays000167; #545 stays open.
No production access/change, real-user revocation, merge or deploy.

### Manager terminal archival evidence (twenty-eighth slice)

Reuse Agent bearer-authenticated evidence transport at
`POST /api/v1/agent/recovery-observations/{operation_id}/terminal` and
`/terminal/verify`. Strict bounded typed proof has explicit archival-evidence
purpose, preparation ID, credential generation, authorization/final consumption/
final proof/final receipt hashes, private intent/terminal commitments, measured
timestamp and exact observation. Golden fixture pins typed field order/hash.

Acceptance serializes server, prepared reservation and current Agent rows before
role mappings and only the issuing human/session rows. Reuse release-delivery
admission to recheck current credentials/generation, readiness/capabilities,
authenticated heartbeat, original grant/commit chain, issuer authority and the
unexpired conditional window. Exact final proof must already be retained;
final receipt digest must match the canonical Manager receipt for that proof.
Terminal observation cannot predate final observation or exceed current time.
SQL ordering compares complete wire instants, including nanoseconds that the
PostgreSQL timestamp index cannot represent.
Repeat deadline/session/heartbeat checks after bounded work and waits.

Migration000168 retains one immutable canonical terminal proof per preparation.
SQL guard independently enforces bindings, final proof/receipt, current Agent/
issuer readiness and time window, rejecting duplicate raw keys/noncanonical
wire before JSONB conversion. Same-proof retries preserve original acceptance
time; conflicting evidence cannot replace it. Nonempty history blocks downgrade.
Readback authenticates current Agent/server/generation and exact retained proof
without renewing expired authority or requiring fresh heartbeat/admin rights.

IntentHash and TerminalHash are opaque commitments declared by the Agent. The
Manager does not inspect private disk, prove complete cleanup, continued flock
ownership, runtime readiness or VPN traffic. The record is archival history,
never release authorization. Acceptance/readback change no reservation, job,
config, credentials, tokens or fence. Agent projection/pinning/submission and
terminal acknowledgement remain a future slice; no Runner activation here.
Actual coordinated crash-safe release/abort and credential revocation E2E remain
unfinished; #545 stays open. No production access/change, merge or deploy.

### Agent terminal submission and durable confirmation (twenty-ninth slice)

Dormant `Client.SubmitRecoveryTerminal` requires independently delivered current
release authorization and the original still-live final owner after successful
terminal archival. `WithTerminalSubmission` holds the same owner mutex/root flock
through synchronous transport, durable acknowledgement and fresh final runtime
observation. Close waits; all escaped/zero/copied guards expire with the callback.
No Runner invokes this API and no new/crashed owner can adopt terminal state.

Project the exact private terminal record plus retained final chain into the
Manager terminal wire. ObservedAt stays the original archival observation time;
fresh checks do not renew its bytes, nonce or authority. Guard PinProof validates
private archive/root barriers/runtime/context/window before and after atomic
write/fsync of `recovery-terminal-submission.json`. Same-owner retries use the
same pinned proof. Never overwrite/adopt preexisting unknown files. Receipt
independently binds schema, recorded flag, operation, terminal digest and exact
proof/request hashes, then atomic write/fsync retains
`recovery-terminal-acknowledgement.json` with another fresh observation.

Both publications use the existing durable metadata writer with atomic
no-replace link publication, temporary-link removal and parent-directory fsync.
Any raced destination, even byte-identical state, fails without replacement or
owner adoption. Uncertainty poisons the owner and retains published evidence.
A crash during the temporary two-link interval fails strict single-link reads;
neither name becomes release authority and no automatic repair is attempted.

Reuse strict bounded authenticated recovery exchange and exact-byte retry/
readback policy at /terminal and /terminal/verify; redirects, duplicate/alias/
unknown JSON and invalid receipts fail closed. Error/log payloads stay generic.
Both new files are sticky admission barriers, including standalone/malformed
state with experimental dispatch off. Private confirmation files require exact
0600 owner metadata, single link, no symlink and stable inode across reads.
Historical final readback validates optional new pin/receipt before and after
its callback without adopting authority or completing cleanup.

Tests cover immutable bytes, escaped guards, lost/uncertain HTTP acknowledgement,
concurrent retries, Close serialization, malformed/foreign receipts and private
file drift. Exit/SIGKILL after pin and after receipt preserve archive and all
fences, never auto-resume. Disposable real systemd/Reality persists confirmation,
then an external restart invalidates the live owner while historical bytes
remain readable. Shared Manager/Agent proof and receipt golden vectors pin
field order/hash. Schema remains000168; no Backend runtime change in this slice.

Receipt is only Manager storage confirmation. No marker, archive or root lock
is removed, no reservation/config/credentials/tokens change, and heartbeat is
never VPN-traffic proof. Read-only reconciliation of terminal storage after a
crash, coordinated abort/actual crash-safe release and credential revocation E2E
remain future work; #545 stays unfinished. No production access/change,
real-user revocation, merge/deploy, Manager relocation, FI diagnostics or logos.

### Agent terminal crash reconciliation (thirtieth slice)

Dormant `Client.ReconcileRecoveryTerminal` first verifies current Agent
credentials/server/credential generation through existing identity transport,
then retains the original admission flock through historical /terminal/verify.
It sends only the exact previously pinned terminal proof, never a new proof,
grant request or /terminal submission. Strict existing bounded/no-redirect
transport validates schema, recorded flag, operation, terminal/proof/request
digests. All uncertainty returns an empty unconfirmed result, including evidence
absence, credential rotation and any local history change during HTTP.

`WithHistoricalRecoveryTerminal` shares the complete existing terminal archive,
binding, final chain, legacy fence, journal and optional acknowledgement
validation. The immutable snapshot exposes its validated terminal projection
only when a private durable pin was present. It never reconstructs an absent
pin into submit-capable evidence or rereads an unrelated record for the callback.
Before/after snapshots remain under the same root flock, including optional
receipt presence. Concurrent live owner/readers reject; root/runtime.lock inode
and every history byte remain in place.

Expired original authority is checked as history at its original issue time,
not renewed. A missing terminal acknowledgement is valid uncertain-storage
history; an existing malformed or mismatched acknowledgement still rejects.
Readback does not capture runtime, assert cleanup completion, create a receipt,
resume a live owner, remove any file, release a reservation or infer VPN traffic.
Even successful readback leaves a missing acknowledgement absent.

Tests reuse final-history fixtures for expired full/partial terminal state and
optional acknowledgement; exact identity+verify requests, strict receipt and
local history corruption, snapshot changes during HTTP, and no file writes.
Real expiry/runtime drift, nil/missing/foreign state, callback/context failures,
concurrent flock and nested readers are covered. Existing exit/SIGKILL cuts now
read terminal history without changing the file tree. Disposable systemd/Reality
reads exact pinned terminal history after an externally injected process restart.
Schema remains000168; no Manager runtime/HTTP/schema or Runner activation.
Coordinated abort, actual crash-safe release and credential revocation E2E/
operator workflow remain future work; #545 stays unfinished. No production
access/change, real-user revocation, merge/deploy, FI diagnostics or logo changes.

### Agent read-only fence release plan (thirty-first slice)

Terminal storage confirmation does not establish completed active-file cleanup:
historical terminal validation deliberately accepts matching redundant copies
after a crash. It also says nothing about current process/config/listener state.
Before implementing any deletion protocol, `PlanFenceRelease` distinguishes this
historical state from live local readiness under the original finalization owner.

The dormant API requires the independently authenticated binding/authorization
hash, original still-held mutex/flock and original durable terminal pin and
acknowledgement. It reuses the complete terminal/archive chain checks and fresh
locked runtime observation twice. No historical reader, reopened owner or copied
plan can acquire authority; no Runner or HTTP transport invokes the API.

Readiness requires exactly seven root entries: the unchanged admission lock,
the operation's retained archive, legacy mutation marker, retirement intent,
terminal record, terminal submission and terminal acknowledgement. Matching
redundant active receipts, temporary files, foreign archives and unknown entries
are refused rather than cleaned. Root and archive identities must remain stable;
root permissions/ownership, private single-link regular files and original lock
inode/permissions/ownership/link count are checked. Two inventories around the
second runtime observation compare every root file's digest and inode, rejecting
same-byte replacement too. Expiry, cancellation and backward clock checks retain
the existing authorization window; uncertainty poisons the original owner.

The result is a local value containing operation/binding, original expiry,
authorization/terminal proof/receipt digests, retained archive basename and five
fixed root file names/digests. It is not a wire contract, durable intent, cleanup
order, deletion capability, Manager completion or VPN-traffic proof. Repeats
revalidate readiness and return the same inventory without renewing authority.
No method consumes this value as permission. All file bytes, markers, archive,
reservation and lock survive, including after Close.

Tests cover read-only repeats/value isolation, incomplete/foreign/unsafe state,
expired/changed identity or runtime, context/clock failures, same-byte inode
replacement and concurrent Close/flock admission. Disposable systemd/Reality
checks readiness before an external process restart and refusal afterwards.
It uses a separate recovery fence directory, preserving the earlier executor's
scoped-removal receipts unchanged; mixed or older unrelated root history is
intentionally unsupported by this initial strict plan, not silently deleted.
The Manager receipt in that runtime regression remains synthetic; it is not
Manager+Agent end-to-end release. Schema stays000168; no new persistence or
network infrastructure. Coordinated abort, a durable crash-safe deletion/commit
protocol and Manager reservation completion remain future stages; #545 stays
open. No production access, actual fence release, merge or deploy.

### Agent durable prepared fence release intent (thirty-second slice)

`PrepareFenceRelease` derives its own fresh locked plan from the original live
finalization owner. A returned/caller-modified plan is never accepted as input.
The independently authenticated binding/authorization, complete archival cleanup,
durable terminal pin/acknowledgement, exact private root inventory and current
process/config/listener are revalidated using the existing plan implementation.

Before any future deletion, publish private `recovery-fence-release-intent.json`
with the existing atomic no-replace/fsync writer. Its versioned private record
binds operation, Manager/Agent/server/generation, original authorization and
terminal proof/receipt digests, original expiry, fixed archive basename and five
root file names/hashes. PreparedAt comes from the latest locked observation;
it is never a new authorization window. This is local prepared history only,
not Manager evidence, a grant, cleanup order or proof of release/VPN traffic.
No wire contract between Manager and Agent changes.

Same-owner retries revalidate live state and preserve original bytes, inode,
PreparedAt, expiry and digest. They cannot adopt pre-existing disk metadata,
including identical bytes that win the publication race. Publication is checked
again against the full plan and runtime; post-write failure poisons the owner
and retains the durable intent and every existing fence/history file. Close
serializes through the same mutex and only drops the original flock.

The new marker alone, even empty or malformed and with experimental fencing
disabled, blocks ordinary mutation admission/completion, result save/replay,
scoped removal and new recovery owners. Older terminal submission/archival APIs
cannot reenter the prepared phase. Historical final/terminal reconciliation
refuses a release intent rather than ignoring this newer phase or adopting its
authority; a dedicated read-only prepared-phase reader remains future work.
Only the still-live original owner may revalidate its exact owned intent bytes
under its held lock. The plan then expects precisely one additional root entry.

Tests exercise concurrent immutable retries, binding/window/runtime failures,
no-replace races with foreign/identical destinations, unsafe/tampered/missing
prepared state and post-publication uncertainty. Real subprocess exit/SIGKILL
cuts cover durable publication before owner adoption and after final validation;
all barriers, exact root commitments and archive survive without auto-resume.
Standalone marker tests cover admission, completion and result persistence.
Disposable systemd/Reality runs separate terminal-history and prepared-intent
scenarios: old historical readback still works in the former; preparation and
then external process drift leave the latter fenced and block live retries.
Synthetic Manager receipts in that regression are not release E2E.

Schema stays000168. No Runner, HTTP route, config/runtime mutation, file deletion,
Agent fence release or Manager reservation completion is added. Coordinated
crash-safe deletion/commit, prepared-phase crash reconciliation/abort and
credential revocation E2E/operator workflow remain unfinished under #545.
No production access/change, real-user revocation, merge/deploy, FI work or logos.

### Agent prepared-release historical reconciliation (thirty-third slice)

`WithHistoricalRecoveryPreparedRelease` reads a crashed prepared release under
the existing root admission flock. It requires the original canonical private
intent, exact current independently authenticated Manager/Agent/server/generation
binding, complete terminal/archive chain, original terminal pin and durable
acknowledgement. It compares all intent fields with a plan reconstructed from
the actual fixed root files and archived authorization. PreparedAt must be at
or after the original terminal observation and before the original authorization
expiry; expiry must match that exact archived authorization. Neither timestamp
is renewed. Expired authorization and changed/missing live runtime remain
readable history, without becoming current readiness or release authority.

The existing plan inventory is extracted into a shared filesystem-only helper,
preserving the live owner's fresh-observation and authority checks. Historical
prepared reads require exactly eight root entries and retain the original lock
through callback/HTTP. Two inventories compare every root file digest/inode,
root/archive identities and private ownership/permissions/link counts, including
same-byte replacement. Existing complete historical chain validation also spans
the callback. Unknown files, partial active cleanup, missing acknowledgements,
ambiguous JSON, rebound operations, changed expiry or contradictory intent
manifests fail closed. Old final/terminal readers still reject prepared state;
the dedicated entry point cannot create or resume a finalization owner.

Dormant `Client.ReconcileRecoveryPreparedRelease` independently authenticates the
current credentials/generation and uses the existing strict read-only
`/terminal/verify` transport. Its body is the exact previously pinned terminal
proof, never the release intent or a new observation. The terminal receipt
validator is shared with ordinary historical terminal reconciliation. Expose
separate local intent hash/original timestamps and Manager terminal receipt:
the Manager has not stored or attested the local release intent. Any transport,
context or post-HTTP filesystem failure returns a zero result and generic error.
No new HTTP route, retry infrastructure, schema, evidence submission, receipt
write, runtime capture or Runner caller is introduced.

Tests cover real expiry with missing runtime,30 local refusal cases,29 transport
modes, strict replies/current identity and changes during HTTP, exclusive flock
and nested readers, unchanged bytes/tree and refusal to resume an owner. Existing
four exit/SIGKILL cuts now read the prepared history without changing state;
their harness-only readiness sentinel lives outside the fence root so production
unknown-entry checks remain strict. The disposable systemd/Reality prepared
scenario reads exact prepared history after external restart; its Manager
receipt remains synthetic and does not establish full release E2E.

Schema stays000168. Agent/Manager fences and reservations remain retained.
Coordinated durable deletion/commit, explicit prepared recovery/abort authority,
credential revocation E2E and operator workflow remain future stages; #545 stays
open. No production access/change, real-user revocation, merge/deploy, FI work
or logo changes.

### Thirty-fourth slice: Manager prepared release evidence

Manager can now accept an authenticated commitment to the Agent's prepared
fence-release intent. This closes the storage/transport boundary missing after
the local preparation and historical reconciliation slices. The Agent producer,
guarded submission and durable receipt are still future work; no Runner caller
or automatic submission is enabled in this slice.

Use existing bounded Agent bearer transport with POST
`/api/v1/agent/recovery-observations/{operation_id}/prepared-release` and its
`/verify` readback. Proof purpose is
`runtime_mutation_fence_release_preparation_evidence_v1`; exact typed field order
is schemaVersion, purpose, operationId, agentCredentialGeneration,
authorizationHash, terminalProofHash, terminalReceiptHash, releaseIntentHash,
preparedAt, observedAt, observation. Shared golden fixtures freeze proof and
receipt wire/hash on Manager and Agent. No private intent manifest is sent.

Admission reuses the existing node-first transaction and original conditional
release authority: prepared reservation, exact current Agent/server/generation,
capabilities and authenticated heartbeat, current issuing human admin permission
and session, and original authorization expiry. Require the exact immutable
terminal proof hash and hash of its canonical Manager receipt; arbitrary request
formatting cannot replace the Agent's pinned terminal receipt. Require exact
expected observation, terminal observation <= preparedAt <= observedAt <=
Manager current time, with observedAt strictly before original expiry. SQL uses
the existing full-nanosecond instant helper for wire ordering; rounded timestamp
columns cannot erase a backwards nanosecond. Readiness is admission context,
never evidence of live VPN traffic.

Migration000169 adds append-only runtime_recovery_prepared_release_evidence
referencing retained terminal evidence. Its direct-SQL guard checks canonical
wire/duplicate keys, all bindings, times, readiness and issuer permissions using
the same node-first lock order, without globally locking users/sessions. Accepted
wire, hash and acceptance time never change. Same-proof retries serialize and
return hashes of the canonical proof and exact request independently; different
proofs conflict. Downgrade refuses retained evidence. Schema metadata advances
to000169 without changing earlier migrations.

Exact historical readback requires current Agent credentials/generation and
stored proof/terminal bindings, but can succeed after original authority expires
or readiness/issuer session is lost. It cannot create evidence or renew authority.
releaseIntentHash remains an opaque Agent statement: Manager does not attest
filesystem durability, current ownership, cleanup or current runtime. Receipt
recorded=true acknowledges storage only. No fence/archive deletion, reservation
completion, credential removal, grant issuance, config apply or operator UI is
introduced. Coordinated release and credential revocation E2E remain unfinished;
#545 stays open. Production, merge/deploy, FI and logos remain outside scope.

### Thirty-fifth slice: live Agent prepared evidence submission

The original live RuntimeRecoveryFinalization owner can now submit its prepared
release commitment to the Manager endpoint from the previous slice. No Runner
caller, operator route or UI is enabled. The client takes the existing verified
conditional release authorization and current bearer; Manager admission still
independently checks current credentials/generation and original authority.

WithPreparedReleaseSubmission holds the existing owner mutex and root flock
through its synchronous callback, durable proof pin, HTTP/retries/readback,
durable receipt and final fresh runtime validation. Guard copies share a scope
state and expire together. Proof derives from the owner's exact private intent,
original terminal pin/receipt and a fresh locked runtime observation/inventory;
caller-supplied evidence cannot be adopted. Require exact reconstructed intent
fields and original expiry, terminal observedAt <= preparedAt <= observedAt.
Frozen observedAt and proof bytes never change after pinning, even on retries.
Each refresh still checks live process/config/listener, retained history,
context, original window and backwards wall clock. Heartbeat is not substituted
for runtime observation or traffic evidence.

Publish private0600 single-link canonical recovery-prepared-release-submission
and recovery-prepared-release-acknowledgement JSON files using the existing
no-replace/fsync writer. The receipt must match schema, recorded, operation,
release-intent hash and hashes of the exact canonical request. Acceptance only
acknowledges storage, never cleanup/release. No existing file is overwritten.
An uncertain write or post-write validation failure retains barriers and poisons
the owner. Network uncertainty can retry the same pinned proof while original
live authority remains valid; no nonce, intent, proof time or expiry is renewed.

Extend the shared strict root inventory only for the live owner's exact pin/ack:
eight root entries before pin, nine after pin, ten after acknowledgement. Compare
root/archive identities and every retained root file digest/inode across callback,
HTTP and legitimate snapshot advancement after publication. Existing files cannot
be silently adopted through same-byte replacement. Foreign, ambiguous, unsafe,
partial or extra files fail closed. New confirmation files independently block
legacy mutation, completion/result save/replay, scoped removal and new recovery
owners even if the release intent is missing. Older historical readers reject
this new phase; this slice provides no crash resume or historical adoption.

Client.SubmitRecoveryPreparedRelease reuses exchangePinnedRecoveryEvidence and
the existing strict bounded no-redirect transport. Every attempt and historical
readback sends the exact pinned proof to prepared-release or prepared-release/
verify with the original deadline. Revalidate the live guard before/after HTTP,
before durable receipt and before returning success. Any failed validation returns
a zero receipt and generic error, even if Manager storage or local receipt write
has already happened. No sensitive payload is included in errors or logs.

Tests cover receipt bindings, current scope/binding, durable retries/concurrency,
Close serialization, shared guard expiry, refusal modes, changes during callback,
same-byte inode replacement, orphan confirmation barriers and strict HTTP replies,
lost acknowledgement/connection, exact readback/retries, redirects and drift.
Eight exit/SIGKILL cuts cover proof and receipt fsync before memory adoption and
after successful final validation. Their harness sentinel stays outside the
protected root. Add a dedicated focused race CI step without changing existing
security windows or watchdogs. A third disposable systemd/Reality scenario pins
and acknowledges prepared evidence, then rejects the live guard after a real
external restart while preserving all barriers/history. Its Manager receipts
are synthetic; this is not full HTTP Manager+Agent release or revocation E2E.

Schema stays000169. No fence/archive deletion, reservation completion, new grant,
configuration apply, actual credential revocation, production access/change,
real-user revocation, merge/deploy, FI work or logo change. Historical prepared
submission reconciliation and coordinated durable release remain future work;
#545 stays open.

### Thirty-sixth slice: read-only prepared submission crash reconciliation

The dedicated historical reader can inspect the prepared proof pin and optional
durable acknowledgement left by the live submission slice. The older prepared
reader retains its exact pre-submission inventory and rejects this new phase.
Both readers reuse the same full-chain validation under the original admission
flock; no owner or runtime mutation authority is reconstructed.

WithHistoricalRecoveryPreparedSubmission requires the exact private canonical
proof, prepared intent, terminal pin/receipt and complete archived finalization
chain with the authenticated current Manager/Agent/server/credential-generation
binding. Reconstruct every proof field from retained history except its frozen
observedAt; require preparedAt <= observedAt < original authorization expiry.
If a local acknowledgement exists, it must be canonical and match all proof,
intent and request hashes. A malformed, unsafe, orphan or foreign file fails
closed. Snapshot exact root entry set, root/archive identity and retained root
file inodes/digests through the callback, including presence/absence of the ack.
Existing archived history validation spans that callback as well. No partial
cleanup, same-byte replacement or extra file is silently adopted.

Client.ReconcileRecoveryPreparedSubmission independently authenticates current
credentials/generation first, then uses the existing strict bounded no-redirect
read transport for the exact pinned proof at prepared-release/verify. Require
schema/recorded/operation/intent/proof/request bindings and the exact local
receipt when present. Return a zero result and generic error on uncertainty or
post-HTTP snapshot/context failure. No automatic submission, retry of mutation,
receipt repair, new receipt file, proof/time renewal or authority issuance.

An expired authorization or missing current runtime does not invalidate a
historical storage readback. Manager still verifies current credentials and
its immutable stored evidence; no heartbeat is treated as runtime or traffic
evidence. Readback cannot certify current runtime, deletion, ownership or fence
release. APIs remain dormant with no Runner/operator/UI caller. Schema remains
000169. Coordinated durable release, reservation completion and credential
revocation E2E remain unfinished; #545 stays open. No production, real users,
merge/deploy, FI diagnostic or logo work belongs to this slice.

Validation covers exact pin-only and acknowledged history, expired authenticated
HTTP readback without local writes, full proof/receipt binding refusals, malformed
or replaced files, lock retention/live-owner exclusion, context cancellation,
strict transport failures and all eight existing exit/SIGKILL prepared-submission
cuts. Crash readback preserves all barriers/history, including missing ack files.

### Thirty-seventh slice: original live owner release readiness revalidation

Historical prepared readback is not current conditional release authority. Add
a dormant read-only CheckRecoveryFenceReleaseReadiness for the original live
finalization owner after its prepared proof and acknowledgement are durable.
AcknowledgedProof accepts only files already published by that live owner; no
historical receipt or successful readback can be adopted into its memory. The
callback-scoped guard and its copies expire together, and every refresh uses
existing fresh process/config/listener validation and exact private inventory.

Keep the original owner mutex/flock and snapshots across three existing bounded
authenticated transports: current identity/generation verification, exact pinned
prepared-release/verify, then original release-authorization read. The Manager's
last read independently checks its current prepared reservation, token/generation,
capabilities, authenticated heartbeat and issuing human/session authority. A
heartbeat remains admission context, never runtime or VPN traffic evidence.
Bind every reply to the exact operation/Agent/server/generation/request hash;
require the exact prepared proof/intent hashes and byte-identical original
authorization, including nonce and expiry. Only the read request gets a fresh
nonce; no grant or authority is renewed. Reject redirects, ambiguous/malformed
responses, denial, drift, cancellation, backwards time or original expiry. No
mutation retry or new evidence submission is attempted by this check.

Reobserve through the original live guard around exchanges and before returning;
the outer owner callback also validates its final inventory/runtime. Return zero
and a generic error on any uncertainty. The returned receipt/hash pair is only
a readiness snapshot, not a durable grant, release token or permission to delete
barriers. Remote authority can change after a read: this slice does not make
Manager and Agent atomic or solve crash-safe deletion. A future coordinated
consumer must establish its own locked commit/retained-history protocol.

No new routes, migrations, polling loops, Runner/operator/UI callers, receipt
writes, owner resume, barrier/archive deletion, reservation completion, grant
issuance or runtime apply. Schema stays000169. Test live acknowledged-owner
scope, private snapshot/runtime refusal and strict authenticated multi-stage
transport, including fresh read nonces with unchanged authority, denial after
storage confirmation, malformed replies, changed authority and runtime drift
or cancellation at each HTTP boundary. Production, real users, FI diagnostics,
logos, merge/deploy are outside this slice; #545 remains open and E2E unfinished.

### Thirty-eighth slice: immutable full release history retention

Before any future barrier deletion, retain the complete audit chain: eight
current root files (including terminal/prepared pins, receipts and release
intent) plus every file of the validated retired archive. Do not copy or move
the stable runtime.lock. Retain canonical metadata, exact original bytes and
SHA256 digests with fixed root/archive scopes and ordered safe names in one
private recovery-fence-release-retention.json, bounded at2MiB. The package can
contain sensitive private receipt data and must never be sent over HTTP or
included in errors/logs. It is a retained backup, not a release/cleanup record,
new grant or permission to delete anything.

RetainRecoveryFenceReleaseHistory reuses the previous slice's authenticated
identity, prepared storage and original authorization reads. Under that same
original owner mutex/flock and deadline, its scoped guard pins every source
identity before the first authenticated HTTP read. After the fresh reads it
derives the package from exact local state, reobserves runtime, verifies source
file identities and
bytes, then uses the existing no-replace/fsync writer. Existing files cannot be
adopted, even with identical bytes. Idempotent retries retain original retainedAt
and package bytes while repeating current Manager/local readiness checks. An
uncertain publication or local post-write validation failure returns zero, poisons the live owner and
leaves the package and all original barriers/files in place.
retainedAt is the pre-publication capture time, not an attestation of when fsync
finished or evidence that release authority survived a stalled write.

Only this owner's exact published retention bytes extend the strict inventory
from10 to11 root entries. Require both prepared pin and acknowledgement before
allowing that additional entry. Snapshot the package inode/hash and all prior
root files; retain source inode/byte checks across later live reads, including
archived files. Paired absent inventory slots stay absent, preserving old
historical phases. The retention file independently blocks legacy mutation,
completion/result save/replay and new recovery owners even when orphaned,
empty or malformed. Nothing removes the old legacy marker.

The dedicated prepared-submission historical reader optionally validates the
entire canonical package against the complete retained chain and exact current
binding, with prepared observedAt <= retainedAt < original authority expiry.
Hold all source identities/bytes and the original admission flock across exact
Manager storage readback. Missing current runtime/expired original authority
does not create a new owner or renew authority. Malformed, unsafe, incomplete,
reordered or replaced history fails closed. There is no partial-cleanup resume,
file restoration, receipt repair or deletion in this slice.

Tests cover private complete retention, unchanged repeated bytes/time, source
and package substitution, publication uncertainty, orphan barriers, historical
refusals and original readers. A real Client+live filesystem-owner fixture checks
HTTP/flock boundaries and no package on denial/runtime drift/cancellation; its
Manager and runtime observer are test doubles. Extend existing exit/SIGKILL
cuts to cover fsync before memory adoption and successful publication. In the
disposable systemd/Reality scenario, retain full history before an external
restart, then reject fresh acknowledged readiness and preserve the package.
Those Manager receipts remain synthetic; coordinated release/revocation E2E
is unfinished. No new route, migration, Runner/operator/UI caller, reservation
completion, runtime apply, actual fence release, production access/change,
real-user revocation, FI diagnostics, logos, merge or deploy. Schema stays000169;
#545 remains open.

### Thirty-ninth slice: authenticated immutable retention commitments

The complete private package from slice38 is local backup, not Manager evidence
or authority to remove any barrier. Before a future release protocol can refer
to that retained history, Manager must record an exact authenticated Agent
statement about its digest. Add POST retention and retention/verify beneath the
existing /api/v1/agent/recovery-observations/{operation_id} transport. Reuse the
bounded8192-byte evidence handler, current Agent bearer hashing, no-store,
generic errors and existing reservation/authority lock order. No new polling
channel or task infrastructure is introduced.

RecoveryRetentionProof v1 has purpose
runtime_mutation_fence_release_retention_evidence_v1. Bind operation, current
credential generation, original authorization hash, exact prepared proof hash,
canonical prepared receipt hash, retention package SHA256, retainedAt,
observedAt and the exact original observation. Field order is frozen by shared
Manager/Agent golden fixtures. Require prepared observedAt <= retainedAt <=
observedAt < original expiry and observedAt <= Manager clock. retainedAt remains
the package's capture time; observedAt is a subsequent Agent observation, not a
claim that the capture timestamp proves when fsync completed. Manager never
receives private package bytes. An authenticated digest statement does not
independently prove contents, filesystem durability, traffic or deletion.

Acceptance requires the stored prepared proof and exact original authorization,
prepared reservation, current token/generation and current Agent capability,
authenticated heartbeat, active human admin and live issuing session with
configs:apply. Heartbeat remains admission context, not VPN traffic evidence.
Reuse server -> preparation -> Agent -> roles/permissions -> issuing user/session
locks; do not take global auth locks while waiting for a node. Recheck time,
session and heartbeat after bounded work before commit. Serial retries require
identical typed proof bytes and preserve accepted_at; a conflicting digest or
observation cannot replace history. The receipt binds both the canonical proof
hash and exact request-body hash, including whitespace in a retry request.

Migration000170 adds a foreign-key dependent immutable ledger. Its SQL guard
independently checks exact canonical wire (including duplicate/nested keys and
nanosecond time ordering), prepared proof/receipt hashes, reservation, current
Agent readiness and issuing human authority. Reject UPDATE/DELETE and refuse
downgrade with retained data under an exclusive table lock. Existing schema
expectations and reverse empty-ledger downgrade tests advance to000170.

Historical verify is read-only and requires exact stored wire/hash, prepared
predecessor and current token/Agent/server/generation bindings. It may read an
expired record after issuer authority/readiness changes; it cannot grant a new
live owner, renew the old authorization, finish cleanup or cancel the
reservation. Independent denial after a successful accept still rejects a new
accept retry; historical storage verification remains available.

Tests cover concurrent/idempotent accept, immutable receipt/hash/time,
conflicting proofs, prerequisite/authority/readiness failures, direct SQL bypass,
nanosecond ordering, historical readback, cancellation, expiry during node-lock
wait without globally blocking roles, and downgrade/insertion concurrency.
Only Agent wire types and golden fixtures are added here: Agent construction,
durable submission/acknowledgement and crash readback of retention evidence are
future work. No Runner/operator/UI caller, barrier/archive deletion, reservation
completion, runtime apply, actual credential revocation, production access,
FI diagnostics, logos, merge or deploy. #545 remains open; coordinated
release/revocation E2E is unfinished.

### Fortieth slice: original-owner retention submission and crash readback

Connect the dormant Agent Client to slice39's retention/retention-verify routes.
Require the complete package already published by this original live owner;
neither existing disk bytes nor historical readback may create/resume ownership.
Reuse the same callback-scoped prepared guard, owner mutex/flock, original
authorization deadline and current identity/prepared-storage/authorization
readiness reads. Pin sources before HTTP and reobserve exact runtime/inventory
around exchanges. No new polling, task dispatch, grant or Runner/UI caller.

Pin canonical RuntimeRecoveryRetentionProof to private0600 single-link
recovery-retention-submission.json with no-replace publication and file/parent
fsync before HTTP. Bind the package digest/capture time, canonical prepared
proof/receipt, original authorization and current observation/generation. Capture
observedAt only after the retained package has already been published and freshly
validated. Once pinned, bytes and observedAt remain frozen on every retry;
subsequent runtime observations cannot renew the commitment or old authority.
The package remains private: only the bounded public proof crosses HTTP.

Use the existing strict evidence exchange and bounded exact-byte retry/readback
policy. An uncertain reply can use only the exact retention/verify request; a
storage receipt binds schema, operation, package digest, proof and exact request
hash. Persist it to recovery-retention-acknowledgement.json with the same private
no-replace/fsync primitive, then revalidate the live owner. An acknowledgement
means storage only, including when readback confirms a previously stored proof
after a later rejection. Future release still needs its own current authority
checks. Post-write uncertainty poisons the original owner and preserves every
file/barrier. No existing even-identical file can be adopted.

Extend the strict inventory from11 to13 root entries only for this owner's exact
proof/receipt; require retained package before proof and proof before receipt.
Pin all root identities and retained sources across HTTP and callbacks. Both new
files independently fence legacy mutation/completion/result replay and new
recovery owners, even when orphaned/empty/malformed. Preserve paired absent
slots for older phases. Older historical readers continue to reject newer files.
The package from slice38 stays immutable and does not recursively include its
own later submission/receipt; future cleanup must preserve these additional
audit records too. No cleanup is introduced here.

ReconcileRecoveryRetention first authenticates current Agent/server/generation,
then reads exact proof, optional receipt and the complete retained predecessor
chain under the original admission flock. Verify all package/source bytes and
identities across exact Manager readback, including nanosecond capture/observation
ordering before original expiry. Expired history remains readable without a
current runtime. This path never POSTs new evidence, repairs an absent receipt,
captures runtime, resumes ownership, renews authority or removes any barrier.

Tests exercise original live filesystem ownership over real Client HTTP, direct
flock checks at every exchange, concurrent idempotent calls, exact durable pin
before POST, uncertain delivery/readback, strict malformed/foreign replies,
source substitution, denial, cancellation, expiry/backwards time, publication
uncertainty and read-only historical refusal. Those HTTP Manager and runtime
observer fixtures are test doubles. Extend the existing exit/SIGKILL matrix to20
cuts, including eight new cuts around proof/receipt fsync and memory adoption.
In the disposable real systemd/Reality scenario, pin and acknowledge retention
before an external restart; fresh proof then fails while exact historical
readback remains valid. Manager receipts in that scenario remain synthetic.

Schema stays000170. No source/archive/fence deletion, reservation completion,
runtime apply, actual revocation, operator routes/UI, production access/change,
real-user operation, FI diagnostics, logos, merge or deploy. #545 remains open;
coordinated Manager+Agent release/revocation E2E is unfinished.

### Forty-first slice: independent complete recovery history archive

Before any future cleanup, preserve the complete retained predecessor chain plus
the later retention proof and acknowledgement in an independently validated
private copy. The original retained package stays immutable. This dormant Agent
API copies only; it removes no source, barrier or admission lock and completes no
reservation. The archive digest proves local storage, never release authority.

Reuse the original live owner mutex/flock, current identity, prepared storage,
original authorization readiness and original deadline. Require this owner's
exact durable retention proof/acknowledgement and exact Manager retention/verify
readback before copying. Reobserve runtime and pin every source identity across
publication. No new endpoint, polling, dispatch, grant or operator caller.

Derive a fixed sibling path from the absolute clean state directory and canonical
operation ID. Require an owned non-writable-by-group/others parent; create the
private archive with exclusive mkdir. Reject even identical existing/partial
directories rather than adopt, overwrite or repair them. Write all fixed
whitelisted root and retired files with private single-link no-replace writes
and file/parent fsync. Bound total copied data to4MiB and manifest to16KiB.
Manifest names are never interpreted as paths. Publish the canonical manifest
last, after complete chain validation through the existing historical reader.
Keep partial copies and all original barriers on any uncertainty; poison the
live owner. Live retries work only through the same original owner and exact
in-memory commitment, with unchanged source/copy identities and current runtime.

The original stable runtime.lock stays in place and is never copied or replaced.
The mirror contains a separate fixed read-lock stub, which only serializes
archive reads and grants no admission authority. Pin original lock, parent,
container, manifest and copied file identities across callbacks. The manifest
binds operation/current credential binding, package/proof/receipt hashes and the
ordered complete file inventory. Reuse all existing strict historical chain,
wire, hash and time-order validators rather than inventing parallel validation.

Historical reconciliation authenticates current Agent/server/credential generation
first, holds the original existing admission flock, validates the entire copy,
then performs exact retention/verify. It needs no original evidence files or
current runtime, but still requires the original private state directory/stable
lock and correct binding. It never creates, repairs, resumes or renews authority.
Future cleanup must separately preserve admission barriers and any later audit
records; this slice does not implement or authorize that protocol.

Tests cover independent readback after test-only source removal, strict manifest
and source refusal, same-byte inode substitutions, scoped live ownership, actual
flock ownership across Client HTTP, concurrent idempotency, cancellation and
runtime/authority drift. Exit/SIGKILL cuts exercise partial copy versus manifest
publication. The disposable systemd/Reality test archives before external restart
and reads the independent history afterward; Manager receipts there are synthetic.
Schema stays000170; #545 remains open and coordinated release/revocation E2E is
unfinished. No source/fence deletion, configuration apply, real-user revocation,
production access, FI diagnostics, logo change, merge or deploy.

### Forty-second slice: read-only archived source cleanup plan

The independent archive is a backup, not an executable cleanup specification.
Before implementing source removal, derive a read-only local partition of the
current original files: independently retained duplicate candidates versus the
two root admission barriers that must survive source cleanup. Always preserve
the original stable runtime.lock too. Names in this returned value are never
accepted by an executor or interpreted as authority or an execution order.

PlanArchivedSourceCleanup requires the original callback-scoped live owner,
its exact retention pin/acknowledgement and its own already published archive.
An identical external archive or historical readback cannot establish ownership.
Reuse complete current runtime/source validation, then validate the independent
copy with the existing full historical chain and compare its manifest against
the complete current original inventory. Reobserve runtime between pinned
inventories; check source/copy/container/manifest/lock identities and original
authorization expiry/context/wall-clock around the computation. Uncertainty
returns a zero plan and preserves all files/barriers; no partial candidate list
escapes. Never create or repair a missing archive while planning.

Bind operation, Manager/Agent/server/credential generation, original expiry and
authorization, manifest, retention proof/receipt hashes, and both fixed archive
basenames. Return candidate scope/name/hash plus hashes of the two preserved
root barriers: mutation-inflight.json and recovery-fence-release-intent.json.
The archived older mutation marker is a separate scoped duplicate; its basename
must not confuse it with the root admission barrier. There are nine root
candidates and eight or nine validated retired files (17/18 total): the older
commit acknowledgement remains optional under the established chain contract.
Do not invent a missing acknowledgement or silently skip an unexpected file.

Client PlanRecoverySourceCleanup reuses the same current identity, prepared
storage, exact original authorization and retention/verify exchange used for
archive publication under the original owner mutex/flock/deadline. Extract the
common helper rather than add transport or endpoints. Only bounded public proof
crosses HTTP; the private archive, manifest and candidate inventory stay local.
A copied or caller-edited plan grants nothing, changes no owned state, and is
not durable evidence, a new grant, a release result or Manager completion.

Tests cover stable read-only repeats, exact scope/hash/barrier partition, caller
value isolation, escaped/copied guards, missing/unowned/partial/substituted
sources and archive, runtime drift, cancellation, expiry/backward time and
changes between observations. Reuse the actual Client/live filesystem-owner
HTTP fixture to check current authority, actual original flock, concurrent
requests, zero results on failure, and no writes to original or copied history.
The disposable systemd/Reality test checks the plan before an external restart
and refusal afterward. HTTP/runtime mocks and synthetic Manager receipts are
not complete coordinated release/revocation E2E.

Schema stays000170. No new persistence, cleanup, fence release, reservation
completion, polling/Runner/operator/UI caller, runtime apply, production access,
actual user revocation, FI diagnostics, logos, merge or deploy. #545 stays open;
a crash-safe source removal/commit protocol and coordinated E2E remain future
work. A future executor must derive and validate its own live state rather than
consume this snapshot as permission.

### Forty-third slice: fenced duplicate-source cleanup and crash readback

Implement the source-cleanup phase as one connected slice: original-owner
execution, current authenticated Client delivery, same-owner completed retries,
historical prefix classification, crash tests and real runtime regression.
This removes only redundant ORIGINAL files already preserved in the independently
validated archive. It does not remove either root admission barrier, move/replace
runtime.lock, release the Agent fence or complete the Manager reservation.

CleanupArchivedSources derives its own fresh locked plan; no caller-supplied
candidate list or returned snapshot is authority. Require the original constructed
owner identity, complete owned archive, immutable retention pin/ack, current
process/config/listener and exact original inventory. Copying an owner value,
reopening history or finding identical disk bytes cannot acquire this phase.
WithSourceCleanupSubmission retains the original mutex/flock and original
authorization deadline across current identity, prepared storage, exact original
authorization and retention/verify reads. Reuse existing transport/readiness
validators; no new endpoint, polling, wire format or database migration.

Remove the nine fixed root duplicates, then the eight/nine validated retired
files in deterministic inventory order, then the empty original retired
directory. Use the existing unlink+parent-fsync primitive. Keep both root
mutation-inflight.json and recovery-fence-release-intent.json unchanged, as well
as the stable original lock and the entire independent archive. Archived marker
basenames are distinct scoped duplicates. Never follow a manifest path, delete
an unknown/foreign file, overwrite/repair a source or touch the active config.

Pin root/retired/lock/source identities and exact bytes. After every deletion,
accept only the precise owned missing prefix, unchanged remaining files and
preserved barriers. Reobserve current runtime against the independent archive
before/after each step under the same admission flock; recheck immutable copy,
context, expiry and wall-clock monotonicity. Any uncertainty poisons the owner,
retains the archive/barriers and performs no automatic rollback or further
deletion. A failure after unlink but before fsync is not treated as success.

For each source, open with no-follow/nonblocking flags and bind the descriptor's
private single-link regular inode and exact bytes to the validated snapshot.
Retain that descriptor through unlink, then require its original inode to have
zero links and unchanged bytes before parent fsync and prefix advancement.
Missing pathname alone is insufficient: a same-user rename/replacement between
validation and unlink, a new hardlink or changed bytes must fail closed. The
deterministic regression injects those changes after descriptor pinning and
before unlink, including moving the original outside the inspected state tree.

The completed source-cleanup phase remains attached only to the same original
live owner. Its dedicated scoped proof getters validate the retained copy,
remaining root set and actual current runtime; they preserve frozen public proof
bytes/times rather than renew authority. Client retries reauthenticate and reread
current authority/storage. Old prepared-phase APIs refuse reentry. The existing
prepared callback may finish a successful one-way transition into this phase,
but its postcondition now validates the cleaned state rather than missing old
sources. Close still drops only the lock and grants no resume/release.

No mutable progress file or new success marker is needed: the fsynced original
release intent predates deletion and survives every cut; the immutable complete
copy supplies expected bytes. WithRecoverySourceCleanupHistory authenticates via
its caller and holds the original existing lock while validating the complete
independent chain and exact original prefix/remaining tree. It reports local
removed counts and whether the original retired directory is absent. A missing
directory observed after an interrupted parent fsync does not prove that fsync
completed. Historical status is filesystem observation only, never proof of an
authorized commit, completed durability, current runtime or permission to resume.
ReconcileRecoverySourceCleanup verifies current credentials and exact Manager
retention storage without deletion, repair, runtime capture or authority renewal.
Holes, altered/missing barriers, substituted/foreign files and changes during
readback fail closed. Expired or restarted history can still be read.

Tests cover actual removal of17/18 redundant files, unchanged archive and all
three preserved root files, same-owner/concurrent authenticated retries, scoped
guard expiry, refusal of old phases, current authority/runtime/storage denial,
publication uncertainty and read-only complete/partial history. Exit/SIGKILL
tests cover every per-file fsynced prefix plus representative unlink-before-fsync
cuts for root/retired files and directory unlink before parent fsync. Add a fourth
disposable systemd/sing-box/Reality phase: clean sources, externally restart,
refuse the old live witness and read historical state while admission stays
blocked. Manager receipts in that runtime scenario remain synthetic; this does
not establish coordinated Manager+Agent credential-revocation E2E.

Schema stays000170. No fence release, reservation completion, runtime/config
apply, Runner/operator/UI caller, real-user revocation, production access/change,
FI diagnostics, logos, merge or deploy. #545 remains open. A future coordinated
commit must separately revalidate current authority and preserve retained audit
history before changing either admission barrier.

### Forty-fourth slice: durable cleaned-state commit intent, still fenced

After complete duplicate-source cleanup, persist one private immutable
recovery-cleaned-commit-intent.json under the ORIGINAL admission mutex/flock.
This is a local post-cleanup commitment, not permission to remove a barrier,
proof of Manager completion, a renewed grant or a claim of working VPN traffic.
Both root admission barriers, the stable runtime.lock and the independent full
archive remain untouched. No coordinated release or automatic resume exists.

PrepareCleanedCommit derives every field from its own fully validated retained
inventory and original live owner. Bind operation, Manager/Agent/server/current
credential generation, exact original authorization and expiry, independent
manifest, retention proof/receipt hashes, both root barrier hashes and the
measured process/config/listener observation. Require complete source cleanup;
partial cleanup, caller-edited snapshots, owner/guard copies and reopened
history cannot acquire this phase. Reobserve actual runtime and check all pinned
source/copy/lock/barrier identities, context, expiry and backward wall-clock
movement before and after publication. Same-owner retries freeze bytes/time,
but still require current authority/runtime. Never replace even identical
preexisting bytes or adopt an identical post-publication inode substitution.

Reuse the existing atomic no-replace temp-file/fsync/link/unlink-temp/parent-fsync
publication primitive. Pin the inode from the writer's own file descriptor and
compare it with the accepted durable name. The primitive's ordinary nil-hook
callers retain their existing behavior. Any uncertainty returns a zero value,
poisons the owner, preserves the intent if published and never removes either
admission barrier, repairs files or continues a release. Treat the new intent
itself, including malformed/empty bytes, as an admission blocker through the
existing recovery-state checks even when other barriers are missing.

Client PrepareRecoveryCleanedCommit reuses current authenticated identity,
prepared storage, exact original conditional authorization and retention/verify
readback under that same original owner and deadline. Extract the existing
cleanup readiness helper; add no Manager endpoint, HTTP payload, polling,
dispatch or operator caller. The full local intent stays on Agent. Only the
existing bounded public prepared/retention proofs cross HTTP.

WithRecoveryCleanedCommitHistory validates the complete independent historical
chain plus the exact cleaned original tree and intent under the original stable
admission lock. ReconcileRecoveryCleanedCommit authenticates CURRENT credentials
and verifies the exact Manager retention storage. It can read expired/restarted
history without capturing runtime, changing files, renewing authority or
reconstructing an owner. Readback reports retained bytes only: a surviving name
after interrupted parent fsync cannot prove fsync completion or an authorized
release. Missing intent, surviving temporary links, malformed/inconsistent
bindings/times/hashes, unsafe modes/links, foreign files and changes during the
callback fail closed. Existing cleanup-prefix history remains compatible.

Tests cover original-owner publication/retries, concurrency and actual Client
HTTP while the original flock is held; absent cleanup/current credentials,
authorization/storage denial, drift, expiry/cancellation/backward time,
preexisting identical files, publication races and same-byte inode substitution.
Exit/SIGKILL cuts cover before publication, fsynced temporary file, two-link
window, removed temporary name before parent fsync, durable publication and
post-validation. All cuts retain admission barriers and reject automatic owner
reconstruction. A fifth disposable systemd/sing-box/Reality phase publishes the
intent, externally restarts runtime, rejects its old live witness and reads
historical intent while admission stays blocked. Manager receipts in this
runtime scenario are synthetic, not coordinated Manager+Agent release E2E.

Schema stays000170 and existing Manager/Agent wire fixtures are unchanged.
No fence release, Manager reservation completion, config/runtime apply,
Runner/operator/UI caller, real-user revocation, production access/change,
FI diagnostics, logos, merge or deploy. #545 remains open. The next coordinated
commit must independently validate current authority and durably preserve this
new audit record before changing either older admission barrier.

### Forty-fifth slice: Manager cleaned-commit statement ledger, no release

Add bounded authenticated Agent evidence for a local cleaned commit intent.
The private intent, archive, inventories, credentials and nonce never cross
HTTP. The public statement binds operation, Agent/server/task observation and
credential generation, original authorization, exact canonical retention proof
and receipt, original prepared release-intent hash, opaque local intent,
independent manifest and root marker hashes, intent time and observed time.
The root marker digest is distinct from the archived original task marker in
the observation. Opaque local hashes are Agent assertions, not independent
proof of file contents, cleanup, filesystem durability or working VPN traffic.

Use POST /api/v1/agent/recovery-observations/{operation_id}/cleaned-commit and
the corresponding /verify route through the existing bounded, no-store Agent
evidence transport. Accept at most8192 bytes and strictly validate typed JSON,
unknown/duplicate/case-altered fields, hashes, nested observation and times.
Reuse current token/generation and exact server -> preparation -> Agent -> role
maps -> confirming human/session lock order. Require prepared reservation,
current fencing/capabilities/authenticated heartbeat and active human admin or
super_admin with configs:apply and its original live session. Heartbeat is
readiness only. Recheck original expiry/session/heartbeat after bounded work
and lock waits using clock_timestamp(), never transaction-start now().

Migration000171 creates runtime_recovery_cleaned_commit_evidence with one
immutable canonical statement per preparation and a retention-evidence foreign
key. Canonical proof hash differs from exact HTTP request hash for formatted
retries. Concurrent identical submissions serialize, preserve accepted_at and
return the same storage commitment; conflicting retries never replace bytes.
Every accept retry revalidates CURRENT authority. Nanosecond-exact intent time
must follow retained observation; observed time must follow intent and remain
within the original authorization and current wall clock. SQL independently
enforces bindings/time/current authority, canonical nested wire, duplicate-key
rejection and immutable update/delete prevention. Root and nested markerHash
use their respective typed JSON order rather than a shared ambiguous ordering.
The downgrade locks the ledger before checking emptiness and refuses to erase
any statement, including one committed while downgrade waits.
The rounded timestamptz observed_at column is indexing/binding metadata only:
authorization-window and future-observation comparisons use exact textual
nanoseconds against numeric epoch bounds. A deterministic SQL/Go regression
checks issue/expiry minus1ns, equality and plus1ns, including a value that rounds
up to the stored expiry. Current authority clock checks remain independent;
this is not a claim of live submission between microsecond clock ticks.

Readback verifies exact canonical stored bytes/hash, the complete prepared/
retention predecessor chain and CURRENT Agent credentials/server/generation.
Expired history and revoked administrator authority remain readable, but this
does not renew authority, finish reservation, consume a new nonce, trigger a
mutation or prove a successful release. Recorded=true means only storage of an
authenticated statement. No Agent receipt write or adoption is implemented.

Manager and Agent share new proof/acknowledgement golden wire/hash fixtures;
all existing wire fixtures remain unchanged. Agent gets contract types only,
not a producer, automated submission, polling/Runner/operator/UI caller or a
new filesystem transition. Manager acceptance is therefore dormant until a
future original-owner Agent submission protocol pins and delivers its own
fresh proof safely. These HTTP/unit/golden tests are not coordinated release
or credential-revocation E2E.

PostgreSQL tests exercise concurrent/idempotent acceptance, conflicts, immutable
audit and downgrade races, strict raw SQL, exact hashes and nanosecond ordering,
current token/generation, missing retention, authority/capability/heartbeat
loss, cancellation and expiry while waiting without global auth locks. Schema
baseline is171 for the isolated build; no installed database is migrated.
No fence release, Manager reservation completion, runtime/config apply,
real-user revocation, production access/change, FI diagnostics, logos, merge or
deploy. #545 stays open; durable Agent submission and coordinated commit remain
future work.

CI keeps the existing120s race watchdog and all tests. The older cleaned-intent
group exceeded its package-wide budget on a slower runner while preparing the
last historical fixture, without an assertion failure or blocked lock in its
stack. Split only its historical-refusal top-level test into a separate required
matrix job; the live shard retains the original prefix selector and skips that
one exact test. Their union preserves present and future matching tests. All
other race commands and the fail-closed aggregate gate remain unchanged; no
safety/runtime/expiry check or assertion is removed or relaxed.

### Forty-sixth slice: durable original-owner cleaned-commit submission

The dormant Agent Client can now submit the public cleaned-commit statement
to the existing Manager endpoint from the ORIGINAL live finalization owner.
An already durable cleaned intent, complete removed-source state, immutable
independent archive and both original admission barriers are prerequisites.
The existing source-cleanup readiness protocol checks current authenticated
identity/generation, prepared storage, original release authorization and
retention storage under the original mutex/flock and authorization deadline.
No new polling, Runner, operator or UI caller is connected.

Derive all public fields from the owned private intent and independently
validated archive chain, never a caller's candidate list. Reobserve the real
runtime under the lock. Publish recovery-cleaned-commit-submission.json with
no replacement, file fsync and parent fsync BEFORE HTTP. Freeze its exact typed
wire and ObservedAt for retries; IntentAt remains the original intent time.
Pin the writer inode across publication and compare it with the full new
inventory to reject same-byte substitution/inode reuse. Private binding,
inventory, paths and original authorization payload are not delivered.

Reuse exchangePinnedRecoveryEvidence and the existing bounded authenticated
no-store exchange: POST cleaned-commit, exact-body /verify after uncertainty,
bounded retries, strict response validation and fresh local locked checks
around every exchange. Record only the exact operation/intent/proof/request
hash receipt using a separate no-replace durable acknowledgement. A receipt
means Manager storage, never permission to release a fence. Every live retry
still requires original current authority and unchanged runtime/inodes; no
historical record can be adopted by a replacement owner. Publication ambiguity
poisons the owner and preserves files, both barriers and independent archive.

The exact cleanup inventory now recognizes the optional ordered proof/ack
pair and validates the complete private intent-to-public-proof-to-ack chain.
Either new filename independently blocks ordinary mutation, even malformed
or orphaned. Historical reconciliation authenticates CURRENT Agent credentials,
holds the original existing flock, checks the complete independent chain and
reads only exact Manager storage. Expired history is readable without runtime
capture. A missing local ack remains missing: no repair, new POST, owner resume,
renewed deadline or authority. Unknown files, partial publication, contradictory
receipt, changed bindings or callback-time inode/archive drift fail closed.

Tests cover live repeated/concurrent submission, six incorrect receipt fields,
publication failures and same-byte substitutions, runtime drift, cancellation,
credential/authorization rejection, uncertain delivery/readback, immutable
retry bytes, historical missing ack/expiry/identity/storage mismatch, admission
blocking and 28 exit/SIGKILL cuts across proof and ack publication. Separate
required race groups retain the existing120s watchdog and fail-closed gate.
The existing disposable systemd/Reality cleaned-intent phase additionally pins
proof and synthetic Manager ack, rejects live proof after external restart,
allows historical readback and verifies both fences remain. Synthetic receipts
are not coordinated Manager+Agent release/revocation E2E.

Schema stays171; existing wire fixtures are unchanged. No fence release,
Manager reservation completion, runtime/config apply, real-user revocation,
production access/change, FI diagnostics, logos, merge or deploy. #545 stays
open. Coordinated crash-safe release, orchestration and operator workflow
remain future work; these APIs alone do not complete credential revocation.
