# ADR-0008: Node Groups and Account Routing Policy

- Status: Accepted
- Workstream: RG-114H

## Context

RouteGate already assigns one VPN account to one concrete server and one
routing profile to a server. Automatic connection selection needs reusable
sets of VPN nodes, explicit balancing inputs, account-level policy, and health
evidence. Introducing automatic reassignment before those contracts exist
would make failover opaque and could silently move credentials between nodes.

## Decision

1. A `node_group` is a reusable control-plane target containing VPN or Hybrid
   Nodes. It is not a deployment role and does not create a node hierarchy.
2. Membership stores `priority`, `weight`, and `enabled`. The group declares a
   `priority` or `weighted` selection strategy, but RG-114H does not execute
   either strategy.
3. `vpn_accounts.server_id` remains the actual connection endpoint. An account
   may separately reference a node group as its future balancing target.
4. Account routing profiles override server assignments, which override the
   default profile. The override is consumed by the protected client
   subscription renderer; it does not replace server-wide VPN Core policy.
5. Candidate evaluation is read-only and deterministic. It reports member and
   node enablement, Agent heartbeat, managed protocol capability, runtime
   state, and normalized load evidence with stable reason codes.
6. Candidate evaluation never mutates account assignment, credentials, server
   configuration, or node-group membership.

## Consequences

- Operators can model pools and inspect why each node is or is not eligible.
- Per-account split-tunnel policy no longer requires changing every account on
  a server.
- Existing installations and accounts keep their server and inherited routing
  behavior after migration 128.
- A group may be assigned while the account's current server is outside the
  group; the API and UI surface that mismatch instead of silently correcting
  it.
- RG-114I can build automatic selection on versioned, explainable inputs rather
  than reinterpreting raw Agent telemetry.

## Explicitly out of scope

- automatic initial placement, failover, or migration;
- cross-node credential replication;
- session-aware draining;
- latency probes from clients;
- arbitrary scoring expressions or scripts;
- direct Agent or VPN Node access to PostgreSQL.
