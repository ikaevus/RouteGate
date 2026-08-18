# RG-114 Platform Expansion Architecture

## Purpose

RG-114 removes post-MVP topology and protocol limitations while preserving the
validated RouteGate control boundary:

```text
RouteGate Manager -> RouteGate Agent -> VPN Core
```

It is an extension of the current architecture, not a rewrite.

## Invariants

1. PostgreSQL belongs to the management plane. Remote Agents never connect to
   it directly.
2. Manager owns desired state, credentials, audit records, node inventory, and
   aggregated operational state.
3. Agent owns allow-listed host operations, local validation, apply,
   healthcheck, rollback, diagnostics, and runtime reporting.
4. VPN Core remains replaceable behind Agent and adapter boundaries.
5. A detected runtime feature is not considered supported until RouteGate can
   manage its complete lifecycle safely.
6. The current VLESS/Reality and Clean VPS All-in-One paths must continue to
   work throughout the expansion.

## Node model

The existing `servers` resource remains the compatibility name for the node
inventory during RG-114. Each record now carries `deploymentRole`.

| Role | Intended components | Agent registration | VPN config deploy |
| --- | --- | --- | --- |
| Management Node | Manager, PostgreSQL, Web UI/nginx | Not required | Rejected |
| VPN Node | Agent, VPN Core | Required | Supported |
| Hybrid Node | Both sets | Required for VPN plane | Supported |

Role and capability answer different questions:

- **Role:** what this installation is intended to host.
- **Capability:** what this Agent build and host can currently manage.
- **Runtime state:** what is installed, running, degraded, or unavailable now.

The Manager must evaluate all three before offering an action.

## Distributed onboarding sequence

The existing one-time token design remains the foundation:

1. Administrator creates a VPN Node in Manager.
2. Manager issues a short-lived, single-use registration token bound to that
   node.
3. The UI presents a copyable bootstrap command as the next action.
4. Agent registers, exchanges the bootstrap token for its persistent credential,
   and reports identity, versions, and capabilities.
5. Manager marks the node connected and guides the administrator to VPN Core
   installation or protocol configuration.
6. Agent continues to report heartbeat, capabilities, telemetry, and task
   results through the Manager API.

The bootstrap command and transport hardening are follow-up implementation
slices; no direct database access or arbitrary remote shell is introduced.

## Protocol adapter contract

The current code renders `singBox` directly inside `routegate.config.v1`.
RG-114 will move this behavior behind an adapter contract in compatible steps.
An adapter is not complete until it provides:

- settings schema and validation;
- server credential lifecycle;
- account/client credential lifecycle;
- VPN Core rendering;
- pre-apply validation;
- apply and rollback compatibility;
- listener and service health evaluation;
- client profile/share/subscription rendering;
- redaction and diagnostics rules;
- routing integration where meaningful.

The capability shape is intentionally a list of supported combinations instead
of a claim that every protocol can freely mix with every transport or security
mode.

## Initial managed capability

RG-114A advertises only the path RouteGate already manages end to end:

| VPN Core | Protocol | Transport | Security modes |
| --- | --- | --- | --- |
| sing-box | VLESS | TCP | None, Reality |

Other sing-box features and detected binaries remain unadvertised as managed
adapters until their full lifecycle is implemented.

## Planned slices

1. **RG-114A — Node Roles & Protocol Capability Foundation**
2. **RG-114B — Remote VPN Node Bootstrap & Inventory**
3. **RG-114C — VPN Core Adapter Boundary**
4. **RG-114D — Certificate Lifecycle & Recovery**
5. **RG-114E — WireGuard Adapter**
6. **RG-114F — Hysteria2 Adapter**
7. **RG-114G — Shadowsocks and MTProto Adapters**
8. **RG-114H — Node Groups, Balancing Targets & Routing Extensions**
9. **RG-114I — Automatic Selection Foundation**

Exact slice names may evolve, but dependency order should remain: role and
capability contracts before distributed onboarding; adapter boundary before new
protocol implementations; explicit health signals before automatic selection.
