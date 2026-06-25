# RG-60 — Routing Profiles / Split Tunnel Foundation

Status: Foundation

## Goal

Add the first backend foundation for RouteGate routing profiles and split-tunnel rendering.

This stage introduces persisted routing rules and connects them to both rendered server config and public sing-box client subscription config.

## Data model

RG-60 adds:

- `routing_profile_rules`
- `server_routing_profiles`

The existing `routing_profiles` table remains the profile root.

A routing rule can match:

- exact domains;
- domain suffixes;
- domain keywords;
- IP CIDR ranges;
- GeoSite tags;
- GeoIP tags.

A routing rule can use one of these actions:

- `direct` — send traffic directly;
- `vpn` — send traffic through the RouteGate VPN outbound;
- `block` — block matched traffic.

## Server config rendering

Server-side rendered config includes routing profile metadata.

For sing-box server config:

- `direct` rules are rendered to `singBox.route.rules` with outbound `direct`;
- `block` rules are rendered to `singBox.route.rules` with outbound `block`;
- `vpn` rules are kept as RouteGate routing profile metadata only and are not rendered as server-side route rules.

This avoids generating an invalid server route to a non-existent VPN outbound.

## Client subscription rendering

Public sing-box client subscription config renders split-tunnel rules into client-side `route.rules`:

- `direct` -> outbound `direct`;
- `vpn` -> outbound `routegate-out`;
- `block` -> outbound `block`.

The default client route remains `routegate-out`, so traffic uses VPN unless a rule routes it differently.

## Current limitations

This is a foundation step only.

Not included yet:

- Admin API CRUD for routing profiles and rules;
- Admin UI for editing routing profiles;
- per-user or per-group profile assignment;
- predefined domestic / global rule packs;
- rule validation beyond database constraints and renderer-level filtering;
- Clash / V2Ray renderers.

## Suggested next step

RG-61 should add the Admin API for routing profile CRUD and server profile assignment.
