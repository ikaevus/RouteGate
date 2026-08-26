# VPN node update receipt and reconciliation contract

## Purpose

RG-96E2d adds the durable host-local evidence required before the E2c detached VPN mutation primitive can be wired to Manager tasks. The receipt exists to answer one question after Agent restart or control-plane disconnect: what is safely known about this specific mutation attempt?

## Receipt ownership and location

Receipts live only under the fixed root-owned directory:

`/var/lib/routegate-agent/update-receipts/`

Each attempt has one file derived only from its canonical UUIDv4 task identity:

`/var/lib/routegate-agent/update-receipts/<task-id>.json`

The directory and receipt must be root-owned, non-symlinked and not group/world writable. Writes use a private temporary file in the same directory, `fsync`, atomic rename and directory sync. Existing receipt files are never silently replaced by a different task identity.

## Bounded schema

The receipt is a small schema-versioned JSON document containing only bounded operational data:

- schema version;
- canonical task ID;
- canonical requested RouteGate version;
- phase/status code;
- whether privileged mutation may have started;
- bounded terminal/reconciliation code;
- UTC timestamps for creation and last transition.

It must not contain local filesystem paths, URLs, tokens, attestation payloads, raw verifier/updater output, environment values or arbitrary Manager-provided text.

## State machine

The initial E2d state machine is monotonic:

1. `prepared` — staged candidate was accepted for detached execution, mutation has not started;
2. `mutation_started` — detached worker is about to invoke the verified updater; automatic replay is forbidden from this point;
3. `succeeded` — the verified updater returned success and the updated Agent became healthy under the existing transaction contract;
4. `failed` — the updater returned a deterministic failure after completing its own rollback path;
5. `outcome_unknown` — restart/recovery cannot prove a terminal result for an attempt whose receipt reached `mutation_started`.

Terminal states never transition back to a runnable state. An interrupted `prepared` attempt may be safely rejected/recreated only by an explicit higher-level orchestration decision; E2d does not add automatic retry.

## Worker lifecycle

E2c used process replacement so updater signals reached existing rollback traps directly. E2d needs a trusted supervisor to record the terminal result after the updater exits. Therefore the detached Agent worker becomes a minimal fixed-policy supervisor inside the same transient systemd unit:

- write/flush `mutation_started` before spawning the updater;
- start only the absolute trusted verified updater with fixed reconstructed arguments and `--role vpn`;
- forward INT/TERM to the updater process;
- wait for updater termination rather than abandoning it;
- atomically persist `succeeded` or deterministic `failed` after the updater exits;
- never return raw updater output through the task channel.

The supervisor does not implement provenance, host mutation, rollback or health checks itself; those remain exclusively in the existing verified updater/transaction.

## Restart reconciliation

On Agent startup, receipt reconciliation is fail-closed:

- `succeeded` and `failed` remain terminal evidence;
- `mutation_started` without a terminal transition becomes `outcome_unknown` unless a later narrowly defined host-local proof can establish success safely;
- `outcome_unknown` is never automatically replayed;
- the Manager must surface/reconcile the unknown state before another mutation attempt is permitted for that node.

E2d does not infer success merely because the Agent process is running. A future reconciliation proof may compare signed release/version evidence, but that proof must be explicit and independently validated.

## Task wiring boundary

Remote `platform_update` task execution remains disabled until the receipt implementation proves:

- receipt creation is atomic and durable;
- a duplicate task ID cannot start a second mutation after `mutation_started`;
- interrupted receipt states recover monotonically;
- signal forwarding preserves updater rollback semantics;
- output and persisted data remain bounded;
- exact-head CI and review are green.

## Explicit non-goals

No fleet rollout persistence, Admin UI, release channels, automatic retry, generic remote commands, arbitrary receipt paths, caller-controlled updater flags, VPN runtime upgrades or broad concurrency are introduced in E2d.
