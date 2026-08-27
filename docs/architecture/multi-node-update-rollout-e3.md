# Durable multi-node update rollout

Status: RG-96E3a foundation

## Purpose

Define the first durable control-plane contract for rolling RouteGate platform updates after the single-node Management and VPN-node update primitives have been proven independently.

E3a is orchestration-only. It must not introduce a new privileged updater, generic remote command path, caller-selected release URL/path/artifact/checksum/signer/trust root, automatic retry of host mutation, or all-at-once fleet mutation.

## Ordering invariant

A rollout targets one canonical RouteGate release version and follows a strict control-plane order:

1. the Management plane must already be on the target release, or the rollout is blocked;
2. only VPN-role nodes are eligible for the remote VPN-node step;
3. VPN nodes are processed one at a time by default;
4. the next node may be admitted only after the previous node reaches a proven terminal healthy result;
5. `failed` stops the rollout pending operator action;
6. `outcome_unknown` stops the rollout and must never authorize an automatic retry or advancement to another node.

Hybrid nodes are not eligible for the VPN-only rolling path. Their host update remains coordinated by the Management/Hybrid platform-update transaction.

## Existing mutation boundary is authoritative

Rolling orchestration must reuse the existing single-node API and lifecycle. For each VPN node the orchestrator may create only the same version-only platform-update job already accepted by RG-96E2j.

The rollout layer may select a server ID from Manager-owned inventory and pass the rollout's canonical target version. It must not select Agent identity, repository, release endpoint, asset name, checksum, filesystem path, command, role, environment, updater argument, signer, or trust root.

The existing Agent-side fixed-policy staging, durable prepared receipt, detached worker, verified updater, rollback classification, and reconciliation state remain the only path to host mutation.

## Durable rollout model

A rollout is a durable Manager-side object with bounded state:

`pending -> running -> succeeded|failed|outcome_unknown`

Each rollout has an ordered immutable set of VPN-node entries created from a Manager-owned eligibility snapshot. An entry has bounded state:

`queued -> waiting -> updating -> healthy|failed|outcome_unknown|skipped`

Rollout membership is frozen at the `pending -> running` boundary. Entry insertion must atomically lock and verify a still-`pending` parent so a planner retry or concurrent writer cannot append a previously unsnapshotted node after execution begins.

Only one entry in a rollout may be `updating` at a time in E3a. Transitioning an entry to `updating` must atomically lock and verify the parent rollout is still `running` before binding the immutable single-node platform-update job. Parent terminalization and mutation admission therefore serialize on the same durable rollout row.

A rollout must not become terminal while any entry remains `updating`. This prevents a pending single-node mutation job from surviving behind a terminal rollout and later being claimed independently of the rollout stop state.

A rollout never infers update success from dispatch acknowledgement. An entry becomes `healthy` only after its single-node update job is `succeeded` and a post-update health gate confirms the node is operational under Manager-observed Agent evidence.

## Eligibility snapshot

Initial eligibility is fail-closed. A candidate must:

- be a server with `deployment_role = vpn`;
- not be disabled;
- have a registered Agent that is not disabled;
- advertise the exact ready software-update capability required by the single-node API;
- not already have an active or unresolved single-node platform update;
- be compatible with the current Manager Agent-protocol contract;
- not be a Management or Hybrid node.

E3a does not silently drop ineligible requested nodes. Planning must preserve bounded blocker reasons so an administrator can see why a node cannot participate.

## Health gate

The first health gate is deliberately conservative and uses existing Manager-observed evidence only. Before advancing to the next node, the updated node must have:

- terminal single-node update status `succeeded`;
- a fresh authenticated Agent heartbeat after the update completed;
- Agent status online;
- compatible Agent protocol;
- no unresolved platform-update outcome.

Future slices may add VPN-core/data-plane probes, maintenance windows, canary groups, configurable concurrency, and richer diagnostics. They must not weaken the single-node mutation/reconciliation guarantees.

## Restart and replay behavior

Manager restart must resume from durable rollout and single-node job state without recreating an already-created node update.

If an entry references a single-node job in `pending`, `in_progress`, or `mutation_dispatched`, the rollout waits for that exact job and never creates a replacement.

If that job is `outcome_unknown`, the rollout becomes `outcome_unknown` and stops. If it is `failed`, the rollout becomes `failed` and stops. Automatic retry is outside E3a.

## API boundary

The eventual Admin API for this foundation may accept only a canonical target RouteGate version and a bounded list of Manager-known VPN server IDs. Caller-selected Agent identity or privileged update selectors remain forbidden.

E3a does not add force, retry, cancel-during-mutation, rollback-all, parallelism, maintenance windows, release channels, arbitrary ordering expressions, or caller-selected health commands.

## Validation requirements

Before the first mutation-capable rollout API is enabled, focused tests must prove:

- Management-first target-version gate;
- VPN-only eligibility and Hybrid/Management rejection;
- immutable node ordering for one rollout;
- membership cannot be added after the rollout leaves `pending`;
- mutation-job admission is rejected unless the parent rollout is `running`;
- parent terminalization cannot race past an `updating` entry;
- at most one `updating` node at a time;
- creation of at most one single-node platform-update job per rollout entry;
- restart/replay cannot create a second single-node mutation for an entry;
- next-node advancement requires proven single-node success plus fresh healthy Agent evidence;
- `failed` and `outcome_unknown` stop the rollout;
- `outcome_unknown` never permits automatic retry;
- no privileged selector is added to the rollout contract;
- existing direct single-node update behavior remains unchanged.

E3a should land the durable rollout/planning state machine before a later slice makes rollout execution administrator-reachable.
