# Multi-node update rollout

## Purpose

RG-96E extends the completed local Management/Hybrid update pipeline into a safe multi-node rollout model. The first E1 slice is deliberately read-only: it defines rollout readiness and ordering before any remote Agent can be told to mutate its host.

## Canonical order

A rollout is ordered by control-plane dependency rather than by convenience:

1. update the Management plane through the existing RG-96C/D local verified-update pipeline;
2. confirm that the Management plane is healthy on the intended release and database schema;
3. build a bounded readiness view of registered VPN-capable Agents;
4. update eligible VPN nodes one at a time by default;
5. require a per-node health gate before advancing to the next node;
6. stop the rollout on an ambiguous outcome or failed health gate unless an administrator explicitly reconciles it.

RouteGate must not make all-at-once remote mutation the default.

## E1 readiness contract

E1 is non-mutating. It may inspect only Manager-owned durable state already reported by Agents and classify nodes for a future rollout.

A readiness record may include:

- Agent ID and Server ID;
- reported hostname, OS and architecture;
- reported Agent software version;
- reported Agent protocol version and Manager compatibility classification;
- current Agent status and last-seen timestamp;
- whether the node is eligible for a future update attempt;
- bounded blocker codes explaining why a node is not eligible.

E1 must not download release artifacts, create Agent update tasks, execute shell commands, restart services, change VPN runtimes, or mutate remote files.

## Readiness rules

A node is not eligible when any of these conditions is true:

- the Agent is disabled, offline, or in an error state;
- OS or architecture is missing or unsupported by the canonical release contract;
- Agent protocol compatibility is `unsupported` or `unknown` for the intended rollout contract;
- the Manager has not yet completed and confirmed its own update to the target release;
- another durable update attempt for the same node is already non-terminal;
- the node lacks a future explicitly versioned software-update capability once that capability is introduced.

`upgrade_required` is not automatically equivalent to update eligibility. It means the running Agent is too old for the current Manager protocol and therefore needs operator-visible remediation, but the privileged remote-update transport still has to be independently safe before RouteGate can mutate that node.

## Security boundary for future remote mutation

The existing Manager -> Agent task channel is not by itself sufficient authorization to install arbitrary software. A root Agent update primitive must remain narrower than general remote command execution.

Future E2+ mutation must preserve these rules:

- no caller-controlled shell command, package manager command, executable path, release URL, repository, signer, trust root, artifact path, checksum, or target role crosses the privileged boundary;
- RouteGate release provenance must be independently enforced on the VPN node before host mutation, using the same fixed repository/signer/predicate policy established by RG-96A/B;
- the node selects the platform artifact only from a verified manifest matching its own OS/architecture;
- Agent platform files may be updated, but VPN runtime configuration and active data-plane state remain outside the platform-update transaction unless a separate reviewed contract explicitly requires otherwise;
- one node is mutated at a time by default and a successful Agent heartbeat/compatibility health gate is required before advancing;
- an ambiguous connection loss after mutation may have started is never automatically replayed.

## Durable rollout model

Later RG-96E slices should use durable Manager-owned rollout and per-node attempt records rather than transient browser state. A rollout records a fixed target release descriptor and immutable node-attempt history. Each node attempt has a terminal success, terminal failure, or explicit unknown-outcome state.

The Manager may pause or stop a rollout, but it must not silently reinterpret an unknown outcome as failure and retry the same privileged mutation.

## Scope sequencing

- **E1 — Readiness / planning:** read-only inventory and eligibility classification. No remote mutation.
- **E2 — VPN-node verified update primitive:** narrow Agent-side release verification and recoverable Agent platform transaction, still not a rolling orchestrator.
- **E3 — Durable rolling orchestration:** one-at-a-time node attempts, per-node health gates, pause/stop semantics, and unknown-outcome handling.
- **E4 — Admin presentation:** rollout progress and operator controls over the durable E3 contract.

Release channels, automatic scheduling, unattended policy, and broad fleet concurrency remain RG-96F or later concerns.
