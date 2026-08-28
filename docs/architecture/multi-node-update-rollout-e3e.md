# Proof-gated multi-node update advancement

Status: RG-96E3e design boundary

## Purpose

Define the first safe advancement step after RG-96E3d connected a durable rollout entry to exactly one existing single-node platform-update job.

E3e may convert an `updating` rollout entry into a proven terminal result and may make the next immutable snapshot entry eligible for the already-reviewed E3d admission path. It must not create a second privileged updater, weaken the single-node lifecycle, infer success from dispatch acknowledgement, or automatically retry an ambiguous or failed host mutation.

## Authoritative proof

A rollout entry may become `healthy` only when all of the following are proven in one Manager-side transaction from durable Manager-owned state:

1. the entry is still `updating` and remains immutably bound to its original single-node platform-update job;
2. the bound job is terminal `succeeded` and has a non-null completion timestamp;
3. durable heartbeat-only evidence exists for the currently registered Agent and was written exclusively by the bearer-authenticated heartbeat path; registration, replacement, inventory refresh, or any other `last_seen_at` writer must not be able to create this evidence;
4. the heartbeat evidence is bound to the exact current Agent credential/registration generation; replacement or re-registration must atomically advance that generation and invalidate any heartbeat proof from the previous bearer before the new credentials become authoritative;
5. that generation-matched authenticated-heartbeat evidence is strictly after the bound job completion timestamp and is still inside the same canonical Agent freshness window used by Manager inventory/liveness evaluation when measured against a wall-clock value captured only after all potentially blocking E3e locks, including the Agent row lock, have been acquired;
6. that Agent is currently protocol-compatible with the Manager and the Manager-derived liveness result for the same generation-matched evidence is online;
7. the server has no unresolved platform-update outcome;
8. no platform-update job history has been added for the server after the exact bound rollout job: with E3d's immutable planning watermark, the current per-server update-job count must be exactly `observed_update_job_count + 1`, and that one additional row must be the immutable `platform_update_job_id` bound to this entry.

`agents.last_seen_at` is not authoritative E3e heartbeat proof because registration also writes it. E3e therefore requires dedicated durable heartbeat provenance whose write authority is limited to the authenticated heartbeat path and whose provenance is tied to the credential/registration generation that authenticated it.

Re-registration and Agent replacement are proof-boundary events. The operation that replaces bearer credentials must atomically advance an immutable/monotonic registration generation (or equivalent identity epoch) and clear or render unusable all heartbeat evidence from the superseded generation. A replacement that reuses the same durable Agent row must not inherit the old generation's heartbeat proof merely because the row identity is unchanged. The newly registered bearer can satisfy E3e only after it sends a fresh authenticated heartbeat for the new generation.

Agent liveness by itself is not proof of update success. A `succeeded` job without a fresh post-completion authenticated heartbeat from the current credential generation is not proof of node health. Pre-update, same-timestamp, stale, registration-derived, superseded-generation, or later-job-derived Agent evidence is not sufficient.

The history-watermark check is causal protection, not merely a uniqueness check. A later direct update job, even if already terminal and even if followed by a newer heartbeat, permanently invalidates the original rollout entry's health proof because Manager can no longer attribute current node health to the exact bound rollout mutation.

## Stop semantics

If the bound single-node job reaches `failed`, E3e must atomically terminalize the entry as `failed` and the parent rollout as `failed` with bounded durable error evidence.

If the bound job reaches `outcome_unknown`, E3e must atomically terminalize the entry and parent rollout as `outcome_unknown`. This state must never authorize automatic retry, replacement job creation, or advancement to another node.

A non-terminal bound job (`pending`, `in_progress`, or `mutation_dispatched`) leaves the entry `updating` and the rollout `running`.

A `succeeded` job whose post-update health proof is incomplete, missing, stale, registration-derived, superseded-generation, or protocol-incompatible remains `updating`; these are transient proof failures and must not be converted to rollout failure merely because valid current evidence is not yet available.

An immutable history-watermark mismatch after the exact bound job is different: because update-job history is append-only, that causal proof can never become valid again. E3e must therefore atomically terminalize the current entry and parent rollout as `failed` with a bounded durable integrity reason (for example `intervening_update_history`) and must not authorize retry, replacement-job creation, or advancement. An unresolved update outcome remains its existing fail-closed stop/interlock rather than being treated as missing health evidence.

## Concurrency and locking

Progress reconciliation is part of the same platform-update admission domain as E3d. It must serialize with rollout terminalization and new mutation admission so that two Manager workers cannot prove the same entry independently and race advancement.

The implementation must use the existing short-lived global platform-update admission mutex before protected rollout/entry/server state, then lock the parent rollout and current `updating` entry, then the canonical per-server admission row/lock, and then the current Agent row before evaluating its registration generation, dedicated heartbeat evidence, protocol/liveness state, bound job, immutable history watermark, and unresolved-outcome interlock. This defines the canonical E3e order as: global update mutex -> rollout row -> entry row -> server admission lock/row -> Agent row.

The current Agent row must be held with a write-conflicting row lock (for example `SELECT ... FOR UPDATE`) from before generation/heartbeat proof is read until the healthy/failure decision commits. Agent replacement/re-registration and authenticated heartbeat updates must modify that same row transactionally, so PostgreSQL row locking serializes them against reconciliation. Replacement must not publish a new credential generation or clear prior proof outside the row-locked transaction; heartbeat must not publish generation-matched proof outside the row-locked transaction.

This common Agent-row protocol is required even under `READ COMMITTED`: merely reading generation and proof in one transaction is insufficient because a concurrent replacement could otherwise advance the generation and invalidate the proof after reconciliation's read but before it commits `healthy`. With the row lock held, replacement/heartbeat must wait until reconciliation commits or reconciliation observes their committed generation/proof before deciding.

Agent registration/replacement and heartbeat paths do not need to acquire the global platform-update mutex merely to maintain Agent state, but they must never acquire rollout/entry/server update locks while holding the Agent row. Their lock dependency therefore terminates at the Agent row and cannot invert the E3e order. If a future Agent-state operation needs both domains, it must acquire them only in the canonical E3e order above.

The history count and exact bound-job identity must be evaluated while the same global admission mutex and canonical per-server admission lock prevent a concurrent platform-update admission from creating an intervening job between proof evaluation and commit.

Heartbeat freshness must not be evaluated against PostgreSQL transaction-start time. `now()` and `CURRENT_TIMESTAMP` are explicitly insufficient because a reconciliation transaction may spend longer than the freshness window waiting for the global, rollout, entry, server, or Agent locks. After all potentially blocking E3e locks have been acquired, reconciliation must evaluate freshness from a true wall-clock source (for example PostgreSQL `clock_timestamp()` or an equivalent Manager clock captured at that point) using the one canonical freshness duration/source shared with Manager liveness semantics. The same wall-clock freshness predicate must be revalidated immediately before writing `healthy`; no earlier transaction timestamp or cached online status may authorize the transition.

Agent credential replacement/re-registration must atomically invalidate prior-generation heartbeat proof with the credential-generation change. Reconciliation must read the current generation and matching heartbeat evidence while holding the current Agent row lock so a concurrent replacement cannot let stale proof survive under a new bearer.

When a healthy proof commits, the durable `healthy` entry is the only evidence that may unblock the following persisted position. E3d may then admit at most that next entry using the existing one-job-per-entry mutation boundary.

No transaction may both lose the healthy proof and nevertheless authorize the next host mutation.

## Rollout completion

After an entry becomes `healthy`, if every persisted rollout entry is terminal `healthy` or planning-time `skipped`, the parent rollout may become `succeeded` in the same transaction.

Otherwise the parent remains `running`. The next node is not mutated by the health-proof transaction itself; a subsequent E3d admission call must select it from the immutable snapshot and repeat all current single-node admission checks.

This separation preserves restart/replay safety: a Manager crash after committing `healthy` but before admitting the next node simply resumes from durable state and cannot recreate the completed node's job.

## Explicit exclusions

E3e does not add caller-selected Agent identity, URL, repository, artifact, checksum, filesystem path, command, role, environment, updater argument, signer, trust root, health command, force, retry, rollback-all, parallelism, maintenance windows, canary policy, or release-channel policy.

E3e does not make rollout creation or execution administrator-reachable if that API is not already present. It only establishes proof-gated durable progression behind the existing Manager repository boundary.

## Required validation

Before E3e is mergeable, focused tests must prove:

- `updating -> healthy` requires the exact bound job to be `succeeded`;
- registration or Agent replacement cannot manufacture heartbeat proof;
- replacing credentials advances/changes the Agent registration generation and invalidates prior-generation heartbeat proof atomically;
- a replacement Agent using the same durable row cannot become healthy until it sends a new authenticated heartbeat for the current generation;
- reconciliation holds the Agent row lock across generation/heartbeat proof evaluation and commit, so concurrent same-row replacement cannot invalidate proof between read and `healthy` commit;
- concurrent authenticated heartbeat on the Agent row either precedes reconciliation and is observed, or waits until reconciliation commits, without torn generation/proof observations;
- Agent replacement/heartbeat locking cannot invert the canonical global -> rollout -> entry -> server -> Agent order or deadlock with E3e;
- authenticated heartbeat evidence must be strictly newer than the bound job completion;
- post-completion heartbeat evidence outside the canonical freshness window cannot become `healthy`;
- a heartbeat proof that was fresh at transaction start but ages out while reconciliation waits for any E3e lock cannot become `healthy`; the test must exercise a lock wait that exceeds the freshness window and prove post-lock wall-clock revalidation rejects the stale proof;
- offline, missing, stale, or protocol-incompatible Agent evidence cannot become `healthy`;
- unresolved update outcome cannot become `healthy`;
- any additional per-server update-job history beyond the E3d planning watermark plus the exact bound job permanently invalidates the proof and atomically stops the rollout with bounded durable failure evidence, including later terminal jobs;
- `failed` and `outcome_unknown` atomically stop the parent rollout;
- pending/in-progress/mutation-dispatched jobs remain waiting and never advance;
- succeeded-without-valid-fresh-heartbeat remains waiting without creating a replacement job;
- concurrent reconciliation/admission cannot insert an intervening job, admit two nodes, or skip an unproven prior position;
- concurrent Agent replacement cannot transfer old-generation heartbeat proof to new credentials;
- restart/replay after a committed healthy proof cannot create a second job for the completed entry;
- restart/replay after permanent history invalidation remains terminal and cannot advance;
- all-healthy-or-skipped membership may terminalize the parent as `succeeded`;
- a later E3d admission may pass only prior entries in durable `healthy` or `skipped` state;
- direct single-node update behavior and the privileged version-only contract remain unchanged.
