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

## Client and API contract

The authenticated account connection read model returns both values explicitly:

- `profile.protocol` — the stored preference, including `auto`;
- `protocol` — the resolved effective protocol used to render the current
  connection material.

The frontend uses the same protocol union as the backend contract instead of
maintaining a separate cast-based representation. Credentials, QR/URI output,
access delivery, and the client connection panel therefore resolve the same
account-level protocol.

## UX

The **Connect VPN client** panel is part of the primary VPN-account workflow and
appears immediately after account management, before routing, delivery,
credentials, and traffic details. The protocol selector is expanded by default
inside the client profile settings so it is discoverable without scanning the
bottom of a long account page.

After an explicit protocol change, RouteGate exposes config deployment as the
next action. Protocol-specific VLESS/Reality controls are hidden when the
selected profile does not use VLESS. Account-page and credentials copy is
protocol-neutral rather than describing every account as VLESS/Reality.

Hysteria2 keeps the RG-114F dedicated-VPN-node certificate constraint; selecting
it on an incompatible node will not produce an apply-safe configuration until
that node satisfies the adapter requirements.

## Compatibility

Existing accounts continue to resolve through `auto` and the node-level
`vpn_protocol` default. Existing `metadata.vpnCore` consumers remain supported,
while newer Agents use `metadata.vpnCores` to select every required managed
runtime. The clean-host VLESS/Reality path remains valid without requiring an
administrator to choose a new protocol explicitly.
