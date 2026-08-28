# Proof-gated multi-node update advancement

Status: RG-96E3e design boundary

## Purpose

Define the first safe advancement step after RG-96E3d connected a durable rollout entry to exactly one existing single-node platform-update job.

E3e may convert an `updating` rollout entry into a proven terminal result and may make the next immutable snapshot entry eligible for the already-reviewed E3d admission path. It must not create a second privileged updater, weaken the single-node lifecycle, infer success from dispatch acknowledgement, or automatically retry an ambiguous or failed host mutation.

## Authoritative proof

A rollout entry may become `healthy` only when all of the following are proven in one Manager-side transaction from durable Manager-owned state:

1. the entry is still `updating` and remains immutably bound to its original single-node platform-update job;
2. the bound job is terminal `succeeded` and has a non-null completion timestamp;
3. the currently registered Agent for the entry's server has an authenticated `last_seen_at` strictly after that job completion timestamp;
4. that Agent is currently `online`;
5. that Agent is compatible with the current Manager Agent-protocol contract;
6. there is no unresolved platform-update outcome for the server.

Agent liveness by itself is not proof of update success. A `succeeded` job without a fresh post-completion heartbeat is not proof of node health. Pre-update or same-timestamp Agent evidence is not sufficient.

## Stop semantics

If the bound single-node job reaches `failed`, E3e must atomically terminalize the entry as `failed` and the parent rollout as `failed` with bounded durable error evidence.

If the bound job reaches `outcome_unknown`, E3e must atomically terminalize the entry and parent rollout as `outcome_unknown`. This state must never authorize automatic retry, replacement job creation, or advancement to another node.

A non-terminal bound job (`pending`, `in_progress`, or `mutation_dispatched`) leaves the entry `updating` and the rollout `running`.

A `succeeded` job whose post-update health proof is incomplete leaves the entry `updating`; it must not be converted to failure merely because the next heartbeat has not arrived yet.

## Concurrency and locking

Progress reconciliation is part of the same platform-update admission domain as E3d. It must serialize with rollout terminalization and new mutation admission so that two Manager workers cannot prove the same entry independently and race advancement.

The implementation must use the existing short-lived global platform-update admission mutex before protected rollout/entry/server state, then lock the parent rollout and current `updating` entry before evaluating its bound job and Agent evidence.

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
- heartbeat evidence must be strictly newer than the bound job completion;
- offline, missing, or protocol-incompatible Agent evidence cannot become `healthy`;
- unresolved update outcome cannot become `healthy`;
- `failed` and `outcome_unknown` atomically stop the parent rollout;
- pending/in-progress/mutation-dispatched jobs remain waiting and never advance;
- succeeded-without-fresh-heartbeat remains waiting without creating a replacement job;
- concurrent reconciliation/admission cannot admit two nodes or skip an unproven prior position;
- restart/replay after a committed healthy proof cannot create a second job for the completed entry;
- all-healthy-or-skipped membership may terminalize the parent as `succeeded`;
- a later E3d admission may pass only prior entries in durable `healthy` or `skipped` state;
- direct single-node update behavior and the privileged version-only contract remain unchanged.
