# VPN node update receipt and reconciliation contract

## Purpose

RG-96E2d adds the durable host-local evidence required before the E2c detached VPN mutation primitive can be wired to Manager tasks. The receipt exists to answer one question after worker loss or control-plane disconnect: what is safely known about this specific mutation attempt?

## Receipt ownership and location

Receipts live only under the fixed root-owned directory:

`/var/lib/routegate-agent/update-receipts/`

Each attempt has one file derived only from its canonical UUIDv4 task identity:

`/var/lib/routegate-agent/update-receipts/<task-id>.json`

The directory and receipt must be root-owned, non-symlinked and not group/world writable. Writes use a private temporary file in the same directory, `fsync`, atomic replacement for monotonic transitions, and directory sync. Initial receipt creation uses an atomic no-replace filesystem operation so concurrent or repeated creation cannot overwrite an existing task identity.

## Bounded schema

The receipt is a small schema-versioned JSON document containing only bounded operational data:

- schema version;
- canonical task ID;
- canonical requested RouteGate version;
- phase/status code;
- whether privileged mutation may have started;
- bounded terminal/reconciliation code;
- UTC timestamps for creation and last transition.

Unknown JSON fields, multiple JSON values, invalid phase/flag combinations, invalid terminal codes, and inconsistent timestamps are rejected fail-closed. The receipt must not contain local filesystem paths, URLs, tokens, attestation payloads, raw verifier/updater output, environment values or arbitrary Manager-provided text.

## State machine

The E2d state machine is monotonic:

1. `prepared` — staged candidate was accepted for detached execution, mutation has not started;
2. `mutation_started` — detached worker is about to invoke the verified updater; automatic replay is forbidden from this point;
3. `succeeded` — the verified updater returned success and the updated Agent became healthy under the existing transaction contract;
4. `failed` — the updater returned a deterministic failure after completing its own rollback path;
5. `outcome_unknown` — recovery cannot prove a terminal result for an attempt whose receipt reached `mutation_started`.

Terminal states never transition back to a runnable state. An interrupted `prepared` attempt is not replayed automatically.

## Worker lifecycle

E2c used process replacement so updater signals reached existing rollback traps directly. E2d needs a trusted supervisor to record the terminal result after the updater exits. Therefore the detached Agent worker becomes a minimal fixed-policy supervisor inside the same transient systemd unit:

- reconstruct the staged candidate from only the canonical task UUID;
- create the durable `prepared` receipt and reject duplicate task identities before mutation;
- write/flush `mutation_started` before spawning the updater;
- start only the absolute trusted verified updater with fixed reconstructed arguments and `--role vpn`;
- forward INT/TERM to the updater process;
- wait for updater termination rather than abandoning it;
- atomically persist `succeeded` or deterministic `failed` after the updater exits;
- never return raw updater output through the task channel.

The supervisor does not implement provenance, host mutation, rollback or health checks itself; those remain exclusively in the existing verified updater/transaction.

## Restart and orphan reconciliation

The ordinary `routegate-agent.service` startup path must **not** blindly convert `mutation_started` to `outcome_unknown`. During a successful VPN-node update the transaction intentionally restarts the Agent while the detached task-specific worker can still be alive and waiting for the updater to finish. Treating that live attempt as orphaned would corrupt otherwise valid terminal evidence.

Reconciliation is therefore tied to the task-specific detached worker identity:

- each task uses the fixed transient unit name `routegate-vpn-update-<task-id>`;
- systemd refuses a second unit with that same name while the original worker remains active;
- if a newly created worker for that exact task reaches receipt inspection and finds `mutation_started`, the prior task-specific unit is no longer alive and no terminal result was persisted, so the receipt is moved monotonically to `outcome_unknown` and mutation is refused;
- `prepared`, `succeeded`, `failed`, and `outcome_unknown` receipts are non-runnable and are rejected on duplicate worker invocation;
- ordinary Agent startup does not infer success or failure from process health and does not mutate root-owned receipt state.

A future narrow read/reconciliation bridge may expose bounded terminal evidence to the Manager, but it must not convert Agent liveness into proof of update success.

## Task wiring boundary

Remote `platform_update` task execution remains disabled until the receipt implementation proves:

- receipt creation is atomic and durable;
- a duplicate task ID cannot start a second mutation after `mutation_started`;
- orphaned post-start receipt states recover monotonically to `outcome_unknown`;
- ordinary Agent restart cannot race or corrupt a still-live detached worker;
- signal forwarding preserves updater rollback semantics;
- output and persisted data remain bounded;
- exact-head CI and review are green.

## Explicit non-goals

No fleet rollout persistence, Admin UI, release channels, automatic retry, generic remote commands, arbitrary receipt paths, caller-controlled updater flags, VPN runtime upgrades or broad concurrency are introduced in E2d.
