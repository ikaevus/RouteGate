# ADR-0007: Native Shadowsocks and MTProto Adapters

- Status: Accepted
- Date: 2026-08-18
- Scope: RG-114G

## Context

RouteGate already separates protocol, transport, security, and VPN Core through
managed adapter descriptors. Shadowsocks and MTProto need different credential
and runtime models without changing the Manager → Agent → VPN Core boundary.

## Decision

Shadowsocks is managed as `sing-box / shadowsocks / tcp / aead-2022` with the
fixed `2022-blake3-aes-128-gcm` method. Each server has one random 16-byte
base64 PSK and every VPN account has a separate random 16-byte base64 user PSK.
The Agent reuses the atomic sing-box stage, validation, apply, rollback, service,
and TCP health lifecycle. Client delivery uses a standard `ss://` URI containing
the required server/user PSK chain.

MTProto is managed as `mtg / mtproto / tcp / faketls`. RouteGate pins the
supported runtime release in the installers, verifies the release checksum, and
installs a dedicated hardened `routegate-mtproto.service`. The adapter accepts
only RouteGate's six-field TOML policy, disables runtime auto-update, fixes
`prefer-ipv4`, and uses `www.cloudflare.com` as the FakeTLS domain. The node has
one shared secret because mtg does not provide per-user credentials. Client
delivery uses a standard Telegram `tg://proxy` URI.

Server keys and secrets are never included in protocol-settings responses.
They are returned only by protected admin credential endpoints or scoped client
connection/subscription endpoints.

## Consequences

- RouteGate advertises five complete managed adapter compositions.
- Shadowsocks shares sing-box runtime ownership with VLESS but has its own
  descriptor, renderer, validator, credentials, URI, and listener check.
- MTProto has independent config storage, backups, service ownership, binary
  detection, validation, rollback, and health checks.
- Changing the MTProto fronting domain, enabling mtg auto-update, arbitrary TOML,
  proxy chaining, ad tags, and per-user MTProto accounting remain unsupported.
- A server still selects one managed VPN protocol at a time; automatic protocol
  selection remains a later RG-114 follow-up.
