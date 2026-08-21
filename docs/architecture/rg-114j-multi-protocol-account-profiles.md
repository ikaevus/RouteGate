# RG-114J — Multi-Protocol Account Profiles

## Purpose

RG-114J removes the RG-114E–G transitional limitation that treated
`servers.vpn_protocol` as the only active protocol for the whole VPN node.

A VPN client profile now owns a protocol preference:

- `auto` — inherit the node default protocol;
- `vless`;
- `wireguard`;
- `hysteria2`;
- `shadowsocks`;
- `mtproto`.

The node-level `vpn_protocol` field remains as the backwards-compatible default
for `auto` profiles and for old clients/API consumers. It is no longer the
exclusive runtime selector.

## Rendering model

Manager resolves every assigned account to an effective protocol before
rendering a node configuration. It then renders every adapter required by the
active accounts into one `routegate.config.v1` envelope.

`metadata.vpnCore` remains populated for backwards compatibility. New bundles
also carry `metadata.vpnCores`, an ordered list of all managed adapter
compositions required by the node.

VLESS and Shadowsocks share one composed sing-box configuration and therefore
one Agent apply/restart lifecycle. Native WireGuard, Hysteria2, and MTProto keep
their separate config and service ownership.

## Apply and rollback

Agent validates all required runtime configs before promoting any of them. It
then applies each unique runtime. If a later apply, restart, persistence check,
or listener healthcheck fails, previously promoted configs are rolled back in
reverse order. A runtime introduced for the first time is removed on rollback
when no prior config exists.

This is a per-node transaction boundary. Atomic deployment across multiple VPN
nodes remains explicitly out of scope for RG-114.

## UX

The protocol selector lives in the VPN account's client profile settings. After
an explicit protocol change, RouteGate exposes config deployment as the next
action. Protocol-specific VLESS/Reality controls are hidden when the selected
profile does not use VLESS.

Hysteria2 keeps the RG-114F dedicated-VPN-node certificate constraint; selecting
it on an incompatible node will not produce an apply-safe configuration until
that node satisfies the adapter requirements.
