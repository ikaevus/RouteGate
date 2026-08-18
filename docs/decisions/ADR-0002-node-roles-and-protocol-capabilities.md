# ADR-0002: Deployment Roles and Protocol Capabilities

- Status: Accepted
- Date: 2026-08-18
- Workstream: RG-114

## Context

The RouteGate MVP validated one Clean VPS All-in-One installation. Manager,
PostgreSQL, Web UI/nginx, Agent, and sing-box all ran on the same host. The
existing `Manager -> Agent -> VPN Core` boundary worked, but the persistent
model treated every registered server as the same kind of VPN host and the
configuration contract was specialized for sing-box with VLESS/Reality.

RouteGate must add remote VPN nodes and additional protocols without creating a
permanent root-server hierarchy or allowing remote nodes to access the Manager
database directly.

## Decision

### Deployment role is explicit

Every node has exactly one assigned deployment role:

| Role | Management plane | VPN plane |
| --- | --- | --- |
| `management` | Manager, PostgreSQL, Web UI/nginx | No |
| `vpn` | No | Agent and VPN Core |
| `hybrid` | Manager, PostgreSQL, Web UI/nginx | Agent and VPN Core |

Roles describe deployed responsibilities. They do not establish authority
between nodes. The Management Node is the control plane because it runs Manager,
not because it is a privileged VPN node.

Existing pre-RG-114 server records are migrated to `hybrid`, matching the only
supported historical installer topology. New API-created server records default
to `vpn`. The Clean VPS All-in-One installer explicitly creates `hybrid`.

### Capabilities are reported separately

Deployment role expresses intent. Agent capabilities express what the current
Agent build and host can safely execute.

Agent registration and heartbeat retain their forward-compatible JSON
capability map. RG-114 adds a versioned `routegate` block:

```json
{
  "routegate": {
    "schemaVersion": 1,
    "nodeCapabilities": ["vpn"],
    "vpnCoreAdapters": [
      {
        "core": "sing-box",
        "protocol": "vless",
        "transports": ["tcp"],
        "securityModes": ["none", "reality"]
      }
    ]
  }
}
```

This reports RouteGate-managed behavior, not every feature detected in the
installed VPN Core binary. A sing-box release supporting a protocol does not
make that protocol manageable until RouteGate has render, validate, apply,
health, credential, and client-profile support for it.

### Protocol composition is capability-based

RouteGate models protocol, transport, and security as distinct concepts where
the underlying protocol supports that composition. It does not require every
adapter to fit a universal three-dropdown matrix.

- VLESS may combine with supported transports and Reality/TLS modes.
- WireGuard has an integrated tunnel and cryptographic model.
- Hysteria2 couples its behavior to QUIC and TLS.
- Shadowsocks has its own cipher and transport constraints.
- MTProto proxy has a different credential and client-delivery model.

Each adapter must therefore declare its supported combinations and validate its
own configuration. The Manager presents only combinations declared by the
selected adapter and available on the target node.

### Manager-Agent boundary remains canonical

Remote node onboarding continues to use one-time registration tokens. Runtime
state, versions, capabilities, diagnostics, and operation results flow through
Manager-Agent APIs. Only Manager accesses PostgreSQL.

## Consequences

- A Management-only node cannot receive an Agent/VPN Core registration token
  or a rendered VPN configuration.
- VPN and Hybrid nodes retain the existing registration and config-deploy path.
- Existing VLESS/Reality deployments remain compatible.
- Future protocol work adds adapters behind the same Manager-Agent boundary.
- UI terminology can migrate from “server” to “node” incrementally without an
  immediate breaking rename of `/api/v1/servers` or database tables.

## Follow-up

1. Add role-selectable installation and secure remote VPN Node bootstrap.
2. Add Manager-side node inventory and capability compatibility evaluation.
3. Extract the sing-box renderer into the first VPN Core adapter.
4. Add protocol adapters incrementally with protocol-specific credentials and
   client delivery.
5. Extend routing targets to node groups and balancing policies.
