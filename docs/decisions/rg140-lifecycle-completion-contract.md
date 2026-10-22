# RG-140 lifecycle completion contract

Status: implementation target, not implemented runtime authority.
Tracking: #545 (revocation), #509 (continuity), #538 (readiness).
This contract complements ADR-0011. Existing SQL guards, immutable history,
authority deadlines and negative tests remain mandatory until replaced by a
strictly scoped transition with equivalent tested protections.

## Integrated baseline

The integration candidate starts from #632 at
db50db0f5a0017b8f8cffeacb150b4aa48b1e971 (including #631), and incorporates
#633 at 18e102257dd14b33e7892e2fff343015bd06837c. It is a new draft, not a merge
of either PR. The SQL fence regression runs with both approval-only and staged
fixtures. It checks the full proposed ID tuple and immutable durable rows,
including migration 175's staging row. Neither fixture may create executable
versions/jobs, advance the counter or release the reservation via generic SQL.

## One operation owns the reservation

An operation binds one account, node, credential identity, pinned baseline,
candidate and Agent generation. Recovery belongs to that operation: it must
not create an unrelated reservation blocked by the original reservation, and
must not cancel the original reservation to gain access to generic Apply.

The following are target states, not new values permitted by migration 160.
Implementing them requires a new reviewed migration and coordinator. Until
then, preparation/staging remain non-executable.

| State | Runtime knowledge | Reservation and permitted next step |
| --- | --- | --- |
| prepared | No dispatch has been authorized for execution | Retain; explicit cancel or fresh dispatch authorization |
| dispatch_committed | Exact version/job and authorization consumption committed atomically | Retain; scoped Agent claim only |
| applying | Original Agent may have mutated runtime | Retain; collect result; no ordinary cancel or retry |
| verifying | Matching result exists; complete enforcement evidence is pending | Retain; independent verification or recovery |
| recovery_required | Failure, timeout or conflicting evidence leaves outcome uncertain | Retain; new explicitly approved recovery attempt |
| reconciling | A fresh recovery attempt observes or repairs the pinned operation | Retain; verification, another recovery_required outcome, or proven safe abort |
| confirmed | Old credential is absent, old sessions terminated and other account usable | Atomically record final evidence and persistent retired identity before releasing reservation |
| cancelled | No dispatch was committed | Release; retain history; make no runtime revocation claim |
| aborted_safe | Post-dispatch reconciliation proves no live worker can mutate and baseline is restored or unchanged | Release only via a verified terminal transition; explicitly state that VPN revocation was not confirmed |

Blocked preflight is a refusal with a reason, not evidence of removal. Errors
after dispatch never turn directly into cancelled. A lost HTTP response is
not a new operation. The same immutable job can be read back or redelivered
only with the Agent's existing measured idempotence checks.

## Dispatch and terminal atomicity

Before dispatch, derive the operator from authenticated context and check
current role/session, explicit shared-process restart consent, Agent generation
and heartbeat, baseline integrity, candidate identity and reservation under
the established locks. Recheck time-based predicates at the write boundary.
Create the exact candidate version, increment the version counter once, create
the exact scoped job, consume the authorization and record the operation state
in one transaction. Generic inserts and updates continue to fail with P0141.
Do not expose a pending generic job as an intermediate implementation.

At confirmation, bind the result to those exact IDs/hashes and Agent identity.
Receipt storage alone is not enough. Commit terminal evidence, applied
membership, the retired credential state, delivery policy and reservation
release as one coherent transition. Unknown runtime state retains the fence.
Recovery metadata, post-release evidence and a prepared ID never imply confirmed.

## Expiry, crash and new recovery attempts

Keep the existing maximum authority lifetime. Never edit, extend, replace or
reuse an expired nonce. Historical readback may outlive authority but cannot
authorize mutation. Replaying an accepted result must not renew authority.

A fresh attempt must be an append-only child of the same operation, with a
new explicit human approval, nonce, bounded deadline and current Agent/runtime
observation. Before allowing any new mutation, prove the old owner is quiescent
and cannot resume: timeout alone is insufficient. Bind the new attempt to
retained prior evidence and enforce one live mutation owner. Manager and Agent
must agree on the attempt identity; no independent recovery reservation.

The current recovery path only accepts succeeded job envelopes. It remains
appropriate for lost acknowledgements but does not solve failed restart,
partial file promotion or unknown outcome. Implement separate observation and
reconciliation paths for those outcomes. A replacement Agent identity or
changed generation requires explicit re-establishment of trust; it must not
inherit authority from an old token, receipt or expired owner.

Even a fully successful recovery must have a tested terminal reservation
transition. Migration 164 currently forbids all state changes after a grant;
the future specific terminal transition must preserve that ordinary prohibition.

## Credential lifetime and subscription behavior

Revocation targets the deployed identity, not just account.status. Before
enabling dispatch, define a persistent retired-identity invariant: a confirmed
old UUID cannot be reintroduced by ordinary rendering, activation, transfer,
rollback, stale config application or disaster recovery. A new intentional
grant of access uses a new credential generation/UUID and explicit delivery.
Reactivation alone must not resurrect an old credential. Restoring a backup
containing retired identities requires reconciliation before VPN activation;
an application table alone is not protection against restoring the entire DB.

The request/dispatch contract must specify when subscription retrieval becomes
blocked and enforce it atomically with accepted revocation intent. The present
Prepare/Stage methods intentionally do not change account status; they are not
the public revocation action. An active account plus a staged row must never be
presented as access already revoked. Failure must not silently reactivate it.
Preserve unrelated device tokens and account identities.

Shared sing-box restart interrupts existing sessions of other accounts too.
The v1 acceptance criterion is successful reconnection and unchanged unrelated
credentials, not uninterrupted connections. State this impact before consent.
One VPN account UUID shared by devices cannot provide per-device termination.

## Required vertical acceptance matrix

All rows below must use the integrated candidate. Component tests support
these gates but do not substitute for the connected Manager/PostgreSQL/Agent
and disposable sing-box scenario. No production US/FI/RU or real credentials.

| Scenario | Required observable outcome |
| --- | --- |
| Two-account normal revocation through real task HTTP | Old UUID cannot reconnect; its prior stream terminates; other account reconnects; exact operation confirmed |
| Duplicate request and lost commit response | One version, job and authorization consumption; no extra counter increment |
| Concurrent generic Apply/transfer/reactivation | Refused or serialized without unrelated changes or UUID resurrection |
| Crash before and after each DB/filesystem boundary | Durable state explains outcome; no automatic unlock, restore or unverified confirmation |
| File promotion followed by failed restart | Recovery-required state and a working explicitly authorized reconciliation path |
| Old success but lost acknowledgment | Exact receipt readback; no second restart or renewed authority |
| Approval expiry or human logout at every stage | No stale mutation; new explicit attempt can eventually reach a safe terminal state |
| Old owner resumes while recovery is attempted | At most one live mutation owner; stale owner cannot change runtime |
| Agent offline, reboot, identity/generation replacement | Fail closed with a supported recovery procedure and preserved evidence |
| Ordinary Apply/activation/rollback after confirmation | Retired UUID remains rejected; regrant requires a new identity |
| Manager restart during verification/confirmation | Final evidence, delivery policy and reservation agree after restart |
| Interrupted upgrade and binary rollback with retained history | No data loss or automatic exposure of obsolete credentials |
| Shared process and last-account removal | Honest disruption scope; remaining account reconnects, or empty listener rejects both old UUIDs |

Before release, also close the remaining #509/#538 client compatibility and
readiness acceptance evidence. Preserve the distinction between a subscription
request, a client refresh, an imported profile and successful VPN traffic.
Do not claim the historical FI 503 cause without contemporaneous evidence.

## Delivery order

1. Integrate existing branches and run full CI on one exact HEAD.
2. Implement operation state/ownership and retired-identity invariants together
   with narrow SQL transitions; maintain all negative tests.
3. Connect one complete isolated normal revocation and terminal transition.
4. Complete failure/expiry/crash recovery in the same lifecycle, including
   safe progress after a new explicit approval. Do not enable production in
   the interval between normal-path and failure-path implementation.
5. Validate UI, upgrade/restore behavior and the full acceptance matrix, then
   run an independent security audit. Keep #545 OPEN until all release gates pass.

Do not add a new evidence ledger unless a concrete transition in this model
requires it and its failure/recovery path is specified and testable.
