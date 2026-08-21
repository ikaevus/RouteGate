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

RG-114B implements this sequence with the Agent-only `install-agent.sh`
installer. Manager returns a copyable command containing its configured public
HTTPS origin and the one-time token. The installer downloads a published
release bundle, verifies `SHA256SUMS`, installs only Agent and its systemd unit,
exchanges the token, and starts heartbeats. It does not install Manager,
PostgreSQL, Web UI/nginx, or a VPN Core.

Manager inventory responses aggregate assigned role, Agent heartbeat freshness,
Agent protocol compatibility, and the versioned RouteGate capability block into
`inventory.connectionState`, `inventory.capabilityStatus`, and
`inventory.nextAction`. This is derived control-plane state; remote nodes still
have no database access and Manager receives no arbitrary remote-shell channel.

## Protocol adapter contract

`routegate.config.v1` remains the Manager-to-Agent compatibility envelope.
RG-114C moves its existing `singBox` VLESS/TCP render, validation, and apply
lifecycle behind explicit Manager and Agent adapter contracts without changing
the serialized envelope. Atomic file promotion and rollback remain shared Agent
behavior; VPN Core payload extraction, binary validation, service control, and
listener health belong to the Agent adapter.

Manager resolves an adapter from four separate values: VPN Core, protocol,
transport, and security mode. The first registered adapter declares exactly
`sing-box` + `vless` + `tcp` with `none` or `reality`. Unsupported combinations
are rejected by the registry instead of falling through to a generic renderer.
Agent capability reporting is generated from the same Agent-side managed
adapter descriptors used by its apply path, preventing binary detection and
managed lifecycle claims from drifting apart.

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

RG-114C is intentionally behavior-preserving. It adds no protocol, database
migration, public API field, or deployment setting. Existing config hashes stay
stable for identical input and render time, old plain sing-box apply tasks stay
supported, and the Clean VPS path continues to select the default
sing-box/VLESS adapter.

## Certificate lifecycle and recovery

RG-114D keeps certificate observation, operational meaning, and recovery
authority separate:

- Agent's allow-listed `manager_certificate` diagnostic inspects only the TLS
  peer metadata of its configured Manager URL. It reports hostname, validity
  window, and verification outcome; it never returns certificate bytes, private
  keys, or raw network errors.
- Manager validates that evidence and assigns the canonical health state. A
  verified certificate with 30 days or less remaining is degraded; expired,
  not-yet-valid, or untrusted certificates are unhealthy.
- The All-in-One installer enables `certbot.timer` and installs a fixed deploy
  hook that validates and reloads nginx after renewal.
- The root-only `routegate-recovery` CLI exposes fixed status, certificate
  renewal, service restart, and UUID-scoped VPN config rollback operations. It
  accepts no arbitrary command, service name, domain, script, or file path.

This slice manages the existing Manager HTTPS certificate lifecycle. Protocol
adapters that require certificates, including Hysteria2, must add their own
certificate ownership and distribution rules without reusing Manager private
keys.

## Native WireGuard adapter

RG-114E adds `wireguard` + `wireguard` + `udp` + `wireguard` as the second
managed composition. It uses native `wireguard-tools`/`wg-quick`, not the
removed legacy sing-box WireGuard outbound. Nodes keep a single explicit active
server protocol in this slice; existing nodes default to VLESS.

Manager owns the server and account keypairs plus peer address allocation.
Agent receives only the server private key and peer public keys in the normal
config apply payload. It validates a strict allow-listed WireGuard grammar,
pins the forwarding hooks, applies atomically, controls only the fixed
`wg-quick@routegate-wg0` unit, and checks its listen port. Client private keys
are delivered through the existing authenticated/token-protected account paths
and never through telemetry or task results.

## Native Hysteria2 adapter

RG-114F adds `hysteria` + `hysteria2` + `quic` + `tls`. The upstream Hysteria
binary receives strict JSON rather than arbitrary YAML. Manager generates one
random userpass credential per account, and protected client delivery renders
standard `hysteria2://` URIs.

TLS ownership remains on the VPN plane: Hysteria performs Let's Encrypt
HTTP-01 and stores its own ACME material locally. No Manager nginx private key
is reused or distributed. This slice supports dedicated VPN Nodes only;
Hybrid-node certificate coordination remains explicit future work.

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

## Native Shadowsocks and MTProto adapters

RG-114G adds two explicit compositions to the same adapter boundary:

- `sing-box / shadowsocks / tcp / aead-2022`, with a server PSK plus a distinct
  account PSK and a standard `ss://` client representation;
- `mtg / mtproto / tcp / faketls`, with a node-shared FakeTLS secret, dedicated
  service/config ownership, and a standard Telegram `tg://proxy` link.

The descriptors do not imply interchangeable runtimes. Shadowsocks deliberately
reuses the sing-box storage and service lifecycle, while MTProto uses separate
staging, active config, backups, binary detection, systemd service, and health
checks. See [ADR-0007](../decisions/ADR-0007-shadowsocks-mtproto-adapters.md).

## Node groups and routing extensions

RG-114H adds reusable node groups with explicit priority/weight membership and
read-only candidate health. Manager evaluates Agent heartbeat, the versioned
managed-adapter contract, reported runtime state, and normalized load, but does
not select or mutate an account's server.

VPN accounts may override their inherited routing profile and may record one
node group as a future balancing target. The concrete `serverId` remains the
actual endpoint, and the routing-profile resolution order is account, server,
then default. Automatic placement and failover remain RG-114I. See
[ADR-0008](../decisions/ADR-0008-node-groups-routing-extensions.md).

Exact slice names may evolve, but dependency order should remain: role and
capability contracts before distributed onboarding; adapter boundary before new
protocol implementations; explicit health signals before automatic selection.
