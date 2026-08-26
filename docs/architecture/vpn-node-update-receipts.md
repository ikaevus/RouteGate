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

The receipt state machine is monotonic:

1. `prepared` — staged candidate was accepted for detached execution, mutation has not started;
2. `mutation_started` — detached worker is about to invoke the verified updater; automatic replay is forbidden from this point;
3. `succeeded` — the verified updater returned success and the updated Agent became healthy under the existing transaction contract;
4. `failed` — a bounded deterministic failure was established; `mutationStarted` distinguishes pre-dispatch from post-dispatch failure provenance;
5. `outcome_unknown` — recovery cannot prove a terminal result for an attempt whose receipt reached `mutation_started`.

Terminal states never transition back to a runnable state. An interrupted `prepared` attempt is not replayed automatically.

## Worker lifecycle

E2c used process replacement so updater signals reached existing rollback traps directly. E2d needs a trusted supervisor to record the terminal result after the updater exits. Therefore the detached Agent worker becomes a minimal fixed-policy supervisor inside the same transient systemd unit:

- reconstruct the staged candidate from only the canonical task UUID;
- accept the durable `prepared` receipt created by the dispatch handoff and reject duplicate task identities before mutation;
- write/flush `mutation_started` before spawning the updater;
- start only the absolute trusted verified updater with fixed reconstructed arguments and `--role vpn`;
- forward INT/TERM to the updater process;
- wait for updater termination rather than abandoning it;
- atomically persist `succeeded` or deterministic `failed` after the updater exits;
- never return raw updater output through the task channel.

The supervisor does not implement provenance, host mutation, rollback or health checks itself; those remain exclusively in the existing verified updater/transaction.

## Restart and orphan reconciliation

The ordinary `routegate-agent.service` startup path must **not** blindly convert `mutation_started` to `outcome_unknown`. During a successful VPN-node update the transaction intentionally restarts the Agent while the detached task-specific worker can still be alive and waiting for the updater to finish. Treating that live attempt as orphaned would corrupt otherwise valid terminal evidence.

Each task therefore uses the fixed transient unit name `routegate-vpn-update-<task-id>`. RG-96E2i extends the earlier E2d recovery mechanism because remote at-most-once dispatch intentionally refuses to create a second worker merely to inspect an orphaned receipt.

Reconciliation may query only that fixed task-specific unit through the fixed local `systemctl` path:

- if `prepared` or `mutation_started` has a live/activating unit, leave the receipt pending;
- if `prepared` exists and the unit is definitely inactive or not found, mark bounded pre-dispatch `failed`; a racing worker will then reject the terminal receipt before mutation starts;
- if `mutation_started` exists and the unit is definitely inactive or not found, move monotonically to `outcome_unknown`;
- if unit state is unavailable or malformed, leave the receipt unchanged and retry reconciliation later;
- `succeeded`, `failed`, and `outcome_unknown` remain terminal and non-runnable.

This preserves the original rule that Agent liveness alone is never proof of update success while closing SIGKILL and host-reboot orphan recovery without redispatch.

## Task wiring boundary

RG-96E2i wires the internal Manager-to-Agent dispatch path while keeping administrator-facing creation disabled. The receipt implementation must prove:

- receipt creation is atomic and durable;
- a duplicate task ID cannot start a second mutation after `mutation_started`;
- orphaned post-start receipt states recover monotonically to `outcome_unknown` only after the fixed task-specific unit is proven absent;
- ordinary Agent restart cannot race or corrupt a still-live detached worker;
- signal forwarding preserves updater rollback semantics;
- output and persisted data remain bounded;
- exact-head CI and review are green.

## Explicit non-goals

No fleet rollout persistence, Admin UI, release channels, automatic mutation retry, generic remote commands, arbitrary receipt paths, caller-controlled updater flags, VPN runtime upgrades or broad concurrency are introduced by this receipt/reconciliation boundary.
