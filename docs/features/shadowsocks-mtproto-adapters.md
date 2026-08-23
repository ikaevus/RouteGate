# RG-114G — Shadowsocks and MTProto Adapters

RG-114G adds the fourth and fifth complete RouteGate VPN adapter compositions
without replacing the Manager → Agent → VPN Core model.

## Shadowsocks 2022

Select **Shadowsocks 2022** in a VPN or Hybrid node's Protocol Settings and set
the TCP port (default `8388`). RouteGate owns the server PSK and one PSK per VPN
account, renders the sing-box multi-user inbound, and supplies `ss://` client
URIs through protected access paths.

The supported policy is intentionally narrow:

- Core: sing-box
- Protocol: Shadowsocks
- Transport: TCP
- Security: AEAD-2022
- Method: `2022-blake3-aes-128-gcm`
- Multiplex: enabled, padding disabled

## MTProto / FakeTLS

Select **MTProto / FakeTLS** and set the TCP port (default `9443`). RouteGate
renders a strict mtg TOML config, applies it through the Agent with atomic
backup/rollback, and supplies `tg://proxy` links and QR codes. The secret is
shared by the node and this is made explicit in the protected credential view.

VLESS / Reality uses `8443` in the recommended Hybrid-node setup, so MTProto
uses a distinct TCP listener by default. RouteGate also enforces distinct TCP
ports for VLESS, Shadowsocks and MTProto at the database boundary.

The supported policy is:

- Core: mtg 2.2.8
- Protocol: MTProto
- Transport: TCP
- Security: FakeTLS
- Fronting domain: `www.cloudflare.com`
- Runtime auto-update: disabled
- Dedicated service: `routegate-mtproto.service`

## Operational checks

Before applying either protocol, confirm that the Agent reports the matching
adapter descriptor. A successful apply includes stage, validation, atomic
promotion, service persistence, and TCP listener health. On failure the Agent
reports the failed stage and restores the previous active config when one exists.

For MTProto, the clean-host and remote-node installers download the architecture
specific mtg archive, verify it against the upstream checksum file, reject unsafe
archive paths and ambiguous binaries, and refuse to overwrite an unmanaged mtg
installation.
