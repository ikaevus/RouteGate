# ADR-0009: Explainable Automatic Node Selection

- Status: Accepted
- Workstream: RG-114I

## Context

RG-114H introduced node groups, health evidence, priority/weight inputs, and an
account-level target without changing the concrete server assignment. The next
step must make a real selection without turning transient telemetry into an
opaque or unsafe background failover mechanism.

Changing `vpn_accounts.server_id` affects both the client endpoint and the
rendered configurations of the previous and selected VPN nodes. An unattended
move that does not coordinate those deployments can publish credentials before
the selected VPN Core has received them.

## Decision

1. Selection is deterministic and explainable. Ready candidates are preferred;
   degraded candidates are an explicit fallback only.
2. `priority` selects the lowest numeric priority with a stable server-ID
   tiebreaker. `weighted` uses stable weighted rendezvous hashing keyed by the
   VPN account, avoiding random movement between evaluations.
3. Each account stores an enable flag, degraded fallback policy, and a cooldown
   between actual moves. Removing its node-group target disables selection.
4. Preview never mutates assignment. Apply performs a fresh evaluation, locks
   the account row, rejects a concurrent assignment change, updates the concrete
   server, and records the operator action in the audit log.
5. Apply returns every affected server ID and explicitly requires render/apply
   of their configurations as the next administrator action.
6. RG-114I does not run a background failover loop. Fully unattended movement
   requires a deployment coordinator that can complete or roll back both VPN
   node configurations before publishing the new client endpoint.

## Consequences

- Operators can validate the exact target and reasons before changing service.
- Priority and weight now have executable semantics without random flapping.
- Cooldown and row locking protect against repeated or concurrent movement.
- Assignment and config deployment remain visibly separate lifecycle steps.

## Explicitly out of scope

- unattended health-triggered failover;
- multi-node config deployment transactions;
- session draining or live connection migration;
- client-side latency probes;
- arbitrary scoring scripts.
