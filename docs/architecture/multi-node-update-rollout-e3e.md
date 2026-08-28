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
4. that authenticated-heartbeat evidence is strictly after the bound job completion timestamp and is still inside the same transaction-time Agent freshness window used by Manager inventory/liveness evaluation;
5. that Agent is currently protocol-compatible with the Manager and the Manager-derived liveness result for the same evidence is online;
6. the server has no unresolved platform-update outcome;
7. no platform-update job history has been added for the server after the exact bound rollout job: with E3d's immutable planning watermark, the current per-server update-job count must be exactly `observed_update_job_count + 1`, and that one additional row must be the immutable `platform_update_job_id` bound to this entry.

`agents.last_seen_at` is not authoritative E3e heartbeat proof because registration also writes it. E3e therefore requires dedicated durable heartbeat provenance (for example a heartbeat-only timestamp/event) whose write authority is limited to the authenticated heartbeat path.

Agent liveness by itself is not proof of update success. A `succeeded` job without a fresh post-completion authenticated heartbeat is not proof of node health. Pre-update, same-timestamp, stale, registration-derived, or later-job-derived Agent evidence is not sufficient.

The history-watermark check is causal protection, not merely a uniqueness check. A later direct update job, even if already terminal and even if followed by a newer heartbeat, invalidates the original rollout entry's health proof because Manager can no longer attribute current node health to the exact bound rollout mutation.

## Stop semantics

If the bound single-node job reaches `failed`, E3e must atomically terminalize the entry as `failed` and the parent rollout as `failed` with bounded durable error evidence.

If the bound job reaches `outcome_unknown`, E3e must atomically terminalize the entry and parent rollout as `outcome_unknown`. This state must never authorize automatic retry, replacement job creation, or advancement to another node.

A non-terminal bound job (`pending`, `in_progress`, or `mutation_dispatched`) leaves the entry `updating` and the rollout `running`.

A `succeeded` job whose post-update health proof is incomplete, stale, registration-derived, protocol-incompatible, or causally invalidated by later update history leaves the entry `updating`; it must not be converted to failure merely because the required proof is not currently available. An unresolved update outcome remains a stop/interlock according to the existing single-node lifecycle rather than being treated as missing health evidence.

## Concurrency and locking

Progress reconciliation is part of the same platform-update admission domain as E3d. It must serialize with rollout terminalization and new mutation admission so that two Manager workers cannot prove the same entry independently and race advancement.

The implementation must use the existing short-lived global platform-update admission mutex before protected rollout/entry/server state, then lock the parent rollout and current `updating` entry before evaluating its bound job, immutable history watermark, unresolved-outcome interlock, Agent identity, dedicated heartbeat evidence, and protocol/liveness evidence.

The history count and exact bound-job identity must be evaluated while the same global admission mutex and canonical per-server admission lock prevent a concurrent platform-update admission from creating an intervening job between proof evaluation and commit.

Heartbeat freshness must be evaluated against transaction time, not merely against the persisted Agent status. The implementation must use one canonical freshness duration/source shared with Manager liveness semantics so E3e cannot drift from inventory behavior.

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
- authenticated heartbeat evidence must be strictly newer than the bound job completion;
- post-completion heartbeat evidence outside the canonical freshness window cannot become `healthy`;
- offline, missing, stale, or protocol-incompatible Agent evidence cannot become `healthy`;
- unresolved update outcome cannot become `healthy`;
- any additional per-server update-job history beyond the E3d planning watermark plus the exact bound job invalidates the proof, including later terminal jobs;
- `failed` and `outcome_unknown` atomically stop the parent rollout;
- pending/in-progress/mutation-dispatched jobs remain waiting and never advance;
- succeeded-without-valid-fresh-heartbeat remains waiting without creating a replacement job;
- concurrent reconciliation/admission cannot insert an intervening job, admit two nodes, or skip an unproven prior position;
- restart/replay after a committed healthy proof cannot create a second job for the completed entry;
- all-healthy-or-skipped membership may terminalize the parent as `succeeded`;
- a later E3d admission may pass only prior entries in durable `healthy` or `skipped` state;
- direct single-node update behavior and the privileged version-only contract remain unchanged.
