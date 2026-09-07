# RG-115 Secure Config Delivery / Subscription URLs

## Status

Accepted architecture and initial implementation.

## Decision

RouteGate separates the user-facing subscription identity from protocol-specific connection material.

The primary credential shown to users is an opaque HTTPS URL:

```text
https://vpn.example.com/sub/<opaque-token>
```

The URL must not encode account IDs, node IDs, protocol names, IP addresses, ports, UUIDs, Reality parameters, or other infrastructure metadata. The token is a cryptographically random bearer credential. RouteGate stores only its hash.

Raw protocol material remains available for interoperability and troubleshooting, but it is not the primary delivery surface.

## Security boundary

This design reduces unnecessary exposure; it does not make the selected VPN endpoint permanently secret. A VPN client must eventually receive enough information to establish the connection.

The goals are to:

- avoid publishing infrastructure details in the stable user-facing URL;
- centralize configuration rendering and credential lifecycle control;
- make token revocation and rotation independent from VPN node changes;
- allow node, address, port, transport, and protocol changes without replacing the user's stable subscription identity when client compatibility permits;
- reduce accidental credential leakage through UI, screenshots, QR codes, browser caches, referrers, and reverse-proxy logs.

The subscription URL must be treated like a password. Anyone who possesses a valid token may be able to obtain usable VPN connection material.

## Endpoints

### Client delivery

```text
GET /sub/{token}
```

This is the user-facing endpoint intended for VPN clients. It does not return the RouteGate management envelope.

The initial implementation supports:

- `auto` (default): a broadly compatible Base64 subscription containing newline-separated standard share URIs when the active protocol set supports them;
- `base64`: explicit Base64 share-link subscription;
- `raw`: direct active protocol material for compatibility/debugging;
- `sing-box`: sing-box JSON for supported VLESS profiles;
- `wireguard`: WireGuard configuration when available.

Example explicit format selection:

```text
https://vpn.example.com/sub/<opaque-token>?format=sing-box
```

Client-specific adapters may be added later without changing the token model.

### RouteGate API representation

```text
GET /api/v1/subscriptions/{token}
```

The existing `routegate.subscription.v1` JSON endpoint remains available for RouteGate compatibility and diagnostics. It is not the preferred user-facing subscription URL.

## Compatibility principle

RouteGate must not require a proprietary RouteGate client or proprietary configuration format.

Delivery has three layers:

1. **Primary** — opaque HTTPS subscription URL.
2. **Client-specific compatibility** — standard or client-oriented formats selected by the delivery endpoint.
3. **Advanced fallback** — raw VLESS, Hysteria2, Shadowsocks, MTProto, WireGuard, sing-box, or other protocol-native material where supported.

This keeps RouteGate interoperable with third-party clients even when their subscription capabilities differ.

## QR policy

The primary QR code represents the opaque subscription URL, not the raw protocol credential.

Raw protocol QR codes may remain available under advanced/manual connection tooling. This distinction matters because a screenshot of a raw VLESS or similar QR code can contain the complete connection credential and infrastructure parameters.

## Token lifecycle

Subscription tokens use the existing RouteGate token lifecycle:

- cryptographically random generation;
- hash-only storage;
- one active token per VPN account;
- rotation/replacement;
- revocation;
- optional expiry;
- `last_used_at` tracking.

A rotated or revoked token must stop resolving immediately.

Future hardening may add per-token rate limiting and additional device/session policies without changing the public URL shape.

## HTTP and reverse-proxy handling

Responses from `/sub/{token}` are marked non-cacheable and use a no-referrer policy.

The standard RouteGate nginx configuration proxies `/sub/` separately with access logging disabled so the bearer token is not persisted in the normal request log.

Operators using another reverse proxy should apply the same rule: do not store the full subscription request URI in access logs.

## Multi-node and multi-protocol model

The subscription URL is a stable abstraction above individual nodes and protocols:

```text
User / VPN client
        |
        v
/sub/<opaque-token>
        |
        v
RouteGate Manager
        |
        +--> resolve VPN account
        +--> resolve active node / protocol set
        +--> render current client material
        +--> return a compatible delivery format
```

This allows RouteGate to evolve the underlying VPN topology while keeping the user-facing credential stable wherever the destination client supports subscription refresh.

## Non-goals

RG-115 does not attempt to:

- conceal an endpoint from a client that must connect to it;
- replace protocol-native credentials with security through obscurity;
- remove raw configuration access;
- create a mandatory RouteGate-specific VPN client.
