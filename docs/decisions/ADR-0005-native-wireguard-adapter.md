# ADR-0005: Native WireGuard Adapter

- Status: Accepted
- Date: 2026-08-18
- Workstream: RG-114E

## Context

RG-114C introduced explicit Manager and Agent VPN Core adapter boundaries, but
the only complete path remained sing-box/VLESS/TCP with optional Reality.
WireGuard is an L3 tunnel with its own key, address, interface, persistence, and
health model. Treating it as another VLESS transport would collapse the
protocol/transport/security separation.

sing-box's legacy WireGuard outbound was deprecated in 1.11 and removed in
1.13. RouteGate therefore must not build a new server lifecycle on that legacy
shape.

## Decision

RouteGate manages WireGuard through the native Ubuntu `wireguard-tools` and
`wg-quick` runtime:

| Dimension | Value |
| --- | --- |
| VPN Core | `wireguard` |
| Protocol | `wireguard` |
| Transport | `udp` |
| Security | `wireguard` |

Each VPN-capable node selects one active server protocol during this slice.
Existing nodes default to VLESS, so upgrades do not switch their data plane.
The `routegate.config.v1` envelope remains compatible and gains:

- a typed `metadata.vpnCore` composition;
- an optional `wireGuard` server configuration payload.

### Credential ownership

Manager owns the server keypair, per-account keypairs, and peer address pool.
Only the server private key and peer public keys enter the Agent apply payload.
Client private keys are returned only through existing authenticated admin or
token-protected subscription delivery. Private keys never enter capabilities,
telemetry, diagnostics, validation output, or task result payloads.

### Agent apply boundary

Agent selects the adapter from the typed envelope metadata. The WireGuard
adapter:

1. accepts only the declared native WireGuard composition;
2. parses an allow-listed config grammar;
3. rejects unknown fields and any forwarding hooks that differ from RouteGate's
   fixed policy;
4. validates with `wg-quick strip` without returning command output;
5. atomically promotes mode-0600 config and preserves a rollback backup;
6. enables/restarts only `wg-quick@routegate-wg0`;
7. checks the fixed interface with `wg show ... listen-port`.

The installers provide `wireguard-tools`, `iptables`, dedicated mode-0700
storage, and IPv4 forwarding. The generated fixed hooks provide forwarding and
NAT for the validated WireGuard subnet.

## Consequences

- Manager → Agent → VPN Core remains unchanged.
- Existing VLESS/Reality rendering and plain sing-box task compatibility remain
  available.
- WireGuard client delivery uses the standard importable `.conf` form rather
  than forcing it into the sing-box client schema.
- A server protocol switch changes the next rendered config and should be
  deployed as an explicit config version.
- User-based routing, node groups, balancing, automatic selection, IPv6 server
  pools, and multi-interface WireGuard are not part of RG-114E.

## Security notes

WireGuard config supports command hooks, so accepting arbitrary `PostUp` or
`PostDown` text would create a remote-shell channel. Both Manager and Agent pin
the exact hook strings derived from a validated IPv4 prefix. The Agent rejects
all other hook text before invoking `wg-quick`.
