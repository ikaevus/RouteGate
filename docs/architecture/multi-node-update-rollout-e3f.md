# RG-96E3f: bounded rollout step controller

Status: design boundary

## Purpose

Connect the already-merged durable rollout primitives into one internal Manager-side advancement boundary without making rolling updates administrator-reachable yet.

E3f is orchestration only. It must not add a privileged updater, a second host-mutation path, a public Admin API, a background auto-update policy, automatic retry, or caller-selected release/mutation selectors.

The controller accepts only a canonical durable rollout ID. Server identity, target version, Agent identity, update job identity, ordering, blockers, and health evidence remain Manager-owned durable state.

## Existing primitives remain authoritative

E3f composes, rather than replaces, the existing boundaries:

- E3c owns the immutable durable rollout snapshot;
- E3d owns admission of at most one existing version-only single-node VPN update job;
- E3e owns post-update health proof and stop/advance eligibility;
- E2j/E2i remain the only path from a single-node job to privileged Agent-side mutation.

The step controller may call those primitives, but it may not reproduce their SQL admission checks, reimplement their health predicates, or synthesize a replacement job.

## One durable transition per invocation

The first E3f controller is deliberately bounded to one durable orchestration transition per invocation.

A call such as `AdvancePlatformUpdateRollout(ctx, rolloutID)` determines the current durable rollout/entry state and performs at most one of the following actions:

1. `pending` rollout:
   - invoke E3d admission;
   - either bind/replay the first allowed single-node job, or complete an all-skipped no-op rollout through the existing bounded completion sentinel;
2. `running` with an `updating` entry:
   - invoke E3e reconciliation for that exact bound entry/job;
   - if proof is incomplete, return waiting without creating any job;
   - if the entry becomes `healthy`, return after that health transaction commits;
   - if it becomes `failed` or `outcome_unknown`, return the durable terminal stop;
3. `running` with no `updating` entry:
   - invoke E3d admission for at most the next persisted eligible position;
   - if no mutation remains, allow the existing no-op/finished-rollout completion path;
4. terminal rollout (`succeeded`, `failed`, `outcome_unknown`):
   - return its durable state without mutation.

A single invocation must never both prove one node healthy and admit the next node. The next node may be admitted only by a later invocation after the `healthy` commit is durable. This is stricter than merely using two SQL transactions and makes crash/replay behavior easy to reason about before a future scheduler or Admin API repeatedly invokes the controller.

## Restart and replay contract

The controller is stateless. Durable rollout, entry, and single-node job rows are the only execution memory.

On restart or repeated invocation:

- an already-bound `updating` entry is reconciled against that exact job;
- E3d replay may return the already-bound job but must never create a replacement;
- incomplete health evidence remains waiting;
- a durable `healthy` entry is never re-mutated;
- failed or unknown outcomes remain stopped;
- completed all-skipped membership remains terminal;
- no in-memory cursor or retry counter is authoritative.

## Concurrency contract

E3f must not add a new lock hierarchy. Each underlying E3d/E3e call retains its existing global admission mutex, rollout/entry/server/Agent row ordering and PostgreSQL write-boundary checks.

The controller must not hold a database transaction, rollout row lock, Agent lock, or server admission lock across calls to E3d/E3e. It may inspect state through repository methods, but any decision that can authorize mutation or health advancement must be re-proven inside the authoritative primitive transaction.

Concurrent controller invocations therefore converge through the existing durable serialization boundaries. Tests must prove that two simultaneous step calls cannot create two jobs, bind two entries, skip a required health proof, or move past a terminal stop.

## Result vocabulary

The internal result should be bounded and operational rather than exposing arbitrary errors or privileged details. A result may report:

- rollout ID;
- rollout status;
- current entry/server ID when Manager-owned durable state provides one;
- bounded action/result such as `mutation_admitted`, `mutation_in_progress`, `waiting_health`, `node_healthy`, `rollout_succeeded`, `rollout_failed`, `outcome_unknown`, or `no_change`;
- the already-bounded E3e waiting/blocker code when applicable;
- bound single-node job ID only when already persisted by E3d.

No Agent token, Agent-selected path, URL, repository, artifact/checksum, local filesystem path, command, updater argument, signer/trust root, environment, or raw host error text may cross this result boundary.

## Not in E3f

E3f does not add:

- public/Admin HTTP routes;
- UI controls;
- a timer/background scheduler;
- automatic release discovery or update policy;
- force/retry/cancel/rollback semantics;
- maintenance windows;
- configurable parallelism;
- canary groups;
- release channels;
- automatic VPN-core updates;
- Management/Hybrid-node participation in the VPN-only rolling path.

Those require separate boundaries after the one-step controller is proven.

## Validation gate

Before E3f implementation is mergeable, tests must prove at least:

- a pending rollout admits at most the first persisted runnable VPN node;
- an all-skipped pending rollout completes without a mutation;
- an `updating` entry is reconciled and cannot cause a second job while incomplete;
- a successful health proof returns after the durable `healthy` transition and does not admit the next node in the same invocation;
- a subsequent invocation admits only the next persisted position;
- `failed` and `outcome_unknown` stop without retry or next-node admission;
- concurrent step calls cannot produce more than one mutation job or more than one `updating` entry;
- restart/replay uses the exact bound job identity;
- terminal rollouts are idempotent no-ops;
- no caller-controlled privileged selector is introduced;
- direct E2j single-node behavior remains unchanged.

Exact-head CI and a focused security review are required before implementation is marked ready for merge.
