# Remote VPN node update task boundary

## Purpose

RG-96E2e/E2f is the control-plane bridge from a Manager-owned `platform_update` task to the already established VPN-node update primitives: E2a's version-only request contract, E2b's fixed official-release staging, E2c's detached root worker, and E2d's durable receipt state machine.

This slice must not widen the privileged language. The Manager selects only a canonical RouteGate target version for a specific registered VPN node. The Agent reconstructs release URLs, asset names, local paths, trust policy, updater path, node role, and systemd unit identity from fixed RouteGate policy.

## Existing task-model constraint

The current Agent task protocol is synchronous at completion: a task is either completed successfully or failed through the existing completion endpoint. A detached update, however, is intentionally asynchronous because the updater restarts `routegate-agent.service` while a task-specific transient worker remains alive to finish the transaction and persist a terminal receipt.

Therefore acceptance by `systemd-run --no-block` is **not update success** and must never be reported as such.

E2e must introduce an explicit update-task lifecycle rather than mapping detached-worker acceptance onto the existing success completion semantics.

## Current Agent privilege model

`routegate-agent.service` currently runs as `User=root`. Root ownership and mode `0600` therefore protect update receipts from other host users, but they do **not** create an OS privilege boundary against the Agent process itself.

A separate local receipt-reader socket would not materially strengthen isolation while its caller is already root. Until the Agent privilege model is reduced in a separate architecture change, E2f uses a narrow in-process read-only reconciliation function over the existing strict receipt parser. That function accepts only canonical task UUID plus canonical target version and returns only bounded receipt evidence.

This does not broaden the remote privileged language: Manager input still cannot select receipt paths, updater paths, artifacts, commands, roles, signers, trust roots, or other host selectors.

## Required lifecycle

The minimum safe lifecycle is:

1. Manager creates one `platform_update` task for one VPN node and one canonical target version.
2. Agent claims that task through the existing authenticated task channel.
3. Agent strictly decodes the version-only payload and stages only the fixed official RouteGate release assets locally.
4. Agent launches the fixed task-specific detached worker. A successful launch means only `mutation_dispatched`; the task remains non-terminal.
5. The detached worker owns the durable host receipt: `prepared -> mutation_started -> succeeded|failed|outcome_unknown`.
6. A narrow reconciliation path maps validated bounded receipt evidence into the Manager task's terminal state. Neither Agent liveness nor worker dispatch is sufficient proof of success.
7. `mutation_started` or `outcome_unknown` is never automatically replayed.

## Task payload

The remote payload remains schema-versioned and version-only:

```json
{"schemaVersion":1,"targetVersion":"v1.2.3"}
```

Unknown fields, whitespace-normalized variants, extra JSON values, arbitrary URLs, paths, repositories, checksums, asset names, signers, trust roots, roles, commands, environment assignments, and updater options are rejected.

The backend task envelope should expose a dedicated bounded payload field rather than overloading rendered VPN configuration or service-operation fields.

## Dispatch result

The immediate Agent result after successful staging and detached launch is bounded operational evidence only, for example:

- task kind;
- canonical target version;
- state `mutation_dispatched`;
- canonical task UUID.

It must not contain staging paths, release URLs, verifier output, raw updater output, secrets, or arbitrary remote text. More importantly, it must not mark the Manager task `succeeded`.

## Receipt reconciliation boundary

E2d receipts remain under the fixed root-owned `/var/lib/routegate-agent/update-receipts/` directory and are parsed with strict ownership, mode, size, schema and semantic validation. The reconciliation call never accepts a filesystem path; it derives the receipt only from a canonical task UUID and also requires the expected canonical target version to match the durable receipt.

Its output is limited to task UUID, target version, normalized reconciliation state, bounded receipt code, and timestamps. `prepared` and `mutation_started` both map to non-terminal `pending`; only `succeeded`, `failed`, and `outcome_unknown` map to terminal control-plane evidence.

The reconciliation path is read-only: it does not transition a receipt, start an updater, or perform staging.

Until Manager-side non-terminal task state and this reconciliation result are wired end-to-end, remote mutation dispatch remains disabled.

## Failure semantics

- decode/staging failure before detached mutation: deterministic task failure is safe;
- failure to create the detached unit before mutation starts: deterministic task failure is safe;
- detached worker accepted: task becomes non-terminal `mutation_dispatched`, never `succeeded`;
- receipt `succeeded`: terminal task success;
- receipt `failed`: terminal deterministic failure;
- receipt `outcome_unknown`: terminal/manual-reconciliation state, never automatic retry;
- inability to read/reconcile a post-dispatch receipt: fail closed and keep the task non-runnable rather than dispatching a second mutation.

## Explicit non-goals

No rolling fleet scheduler, parallel node updates, release channels, Admin UI, generic remote shell, arbitrary artifact selection, caller-controlled privileged arguments, Agent privilege reduction, or automatic retry are introduced by this boundary.
