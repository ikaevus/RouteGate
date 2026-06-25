# RG-64 — Routing Profiles Render E2E / Split Tunnel Verification

Status: Implemented

## Summary

RG-64 verifies the routing profile render path from server config rendering into persisted RouteGate rendered config and sing-box route rules.

The tested chain is:

```text
server routing profile selection
  -> configs.Service.Render
  -> buildRenderedConfig
  -> persisted config_versions.rendered_config
  -> rendered routingProfile metadata
  -> rendered singBox.route.rules
```

## Server render behavior

Server rendered config includes the routing profile returned by the config repository for the target server.

Repository selection is intentionally centralized in the config repository:

1. If the server has an explicit assignment in `server_routing_profiles`, that routing profile is selected.
2. Otherwise, the current default routing profile is selected.
3. Only enabled routing profile rules are rendered.
4. Rules are loaded by `priority ASC, created_at ASC`.

## sing-box server route behavior

Server-side sing-box config currently renders only actions that make sense on the server config:

| Routing action | Server rendered metadata | Server sing-box route rule |
|---|---|---|
| `direct` | included with outbound `direct` | rendered with outbound `direct` |
| `vpn` | included with outbound `vpn` | not rendered as a server route rule |
| `block` | included with outbound `block` | rendered with outbound `block` |

The `vpn` action is client-side split-tunnel intent. It is preserved in RouteGate rendered metadata and rendered by the public sing-box client config renderer as the RouteGate VLESS outbound.

## Client split-tunnel behavior

The public sing-box client config renderer maps routing profile actions as follows:

| Routing action | Client sing-box outbound |
|---|---|
| `direct` | `direct` |
| `vpn` | `routegate-out` |
| `block` | `block` |

The client renderer adds the block outbound only when at least one rendered rule needs it.

## Verification added

Backend tests now cover:

- `configs.Service.Render` using a server-specific assigned routing profile.
- persisted `CreateConfigVersionInput.RenderedConfig` containing routing profile metadata.
- persisted response JSON retaining the routing profile.
- direct and block rules becoming server sing-box route rules.
- VPN rules staying metadata-only in server rendered config.
- default routing profile fallback when the repository returns the default profile.
- existing client renderer split-tunnel behavior for direct / VPN / block rules.

## Current limitations

This verification does not perform real external VPN connectivity testing.

It also does not add new UI. RG-64 is intentionally backend-focused because RG-61/RG-62/RG-63 already introduced the Admin UI, server assignment, and validation foundation.
