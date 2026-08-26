# Manager-side VPN Node update lifecycle

## Purpose

RG-96E2g defines the Manager-owned lifecycle required before remote VPN-node update mutation may be enabled. It connects the existing Agent `platform_update` request contract, node-local staging, detached verified apply worker, durable receipt state machine, and read-only receipt reconciliation without treating worker dispatch as update success.

## Security invariant

The Manager selects only a registered VPN node and a canonical RouteGate release version. It must not send release URLs, filesystem paths, artifact names, checksums, updater paths, commands, roles, signers, trust roots, systemd unit names, or other privileged selectors.

A Manager job is durable control-plane state, not host authorization. The Agent reconstructs privileged inputs from fixed RouteGate policy and the verified updater remains the final host-mutation gate.

## Required lifecycle

A remote update job uses these control-plane states:

1. `pending` — durable Manager job exists but has not been claimed by the target Agent.
2. `in_progress` — Agent claimed the job and is performing non-mutating validation/staging.
3. `mutation_dispatched` — the detached node-local worker was accepted for execution. This state is explicitly non-terminal and must never be reported as success.
4. `succeeded` — terminal reconciliation evidence reports durable `succeeded` receipt for the same task UUID and target version.
5. `failed` — terminal reconciliation evidence reports a deterministic durable `failed` receipt for the same task UUID and target version.
6. `outcome_unknown` — terminal reconciliation evidence reports `outcome_unknown`, or Manager can prove that mutation started but cannot safely classify the final host state.

`mutation_dispatched` and `outcome_unknown` are never automatically replayed.

## Claim and completion semantics

The existing synchronous Agent operation completion contract is insufficient for `platform_update`: returning success immediately after `systemd-run --no-block` would incorrectly convert dispatch acceptance into host-update success.

E2g therefore requires a platform-update-specific transition from `in_progress` to `mutation_dispatched`. That transition may persist only bounded evidence needed for reconciliation: canonical task UUID, canonical target version, and bounded timestamps/state metadata. It must not persist caller-controlled privileged selectors or raw updater output.

Terminal completion after dispatch is allowed only through validated reconciliation evidence. The Manager must verify that receipt task UUID and target version match the durable job before applying a terminal transition.

## Reconciliation semantics

The Agent read-only reconciliation primitive normalizes durable node receipts to:

- `pending` for receipt phases `prepared` and `mutation_started`;
- `succeeded` for durable success;
- `failed` for deterministic durable failure;
- `outcome_unknown` for ambiguous post-mutation outcome.

A reconciliation result of `pending` leaves the Manager job in `mutation_dispatched`. Agent liveness, process exit, task polling errors, transport disconnects, Manager restart, or absence of an immediate terminal receipt are not success evidence.

## Recovery

Manager restart must preserve `mutation_dispatched` jobs and resume reconciliation rather than requeueing mutation. If the Manager cannot determine whether mutation started, it must fail closed toward `outcome_unknown`, never toward retry.

Agent restart must not cause a second update worker to be launched for a job already represented by durable receipt state.

## Concurrency

The existing one-active-operation-per-server protection must treat `mutation_dispatched` as active. A second mutating operation must not be scheduled for the same VPN node until the first update reaches a terminal state.

Rolling fleet orchestration remains a later slice; E2g establishes only the durable single-node control-plane lifecycle required by it.

## Explicit non-goals

This slice does not introduce rolling scheduling, parallel node updates, release channels, Admin UI, automatic retry, generic remote shell, caller-selected artifacts, Agent privilege reduction, or any broader privileged command language.

Remote mutation dispatch must remain disabled until the Manager persistence/API/Agent-task wiring implements these lifecycle invariants and exact-head CI proves them.