# RG-70 — Traffic Stats / Limits Foundation

Status: backend foundation

## Goal

Add the first Manager-owned traffic usage and limits foundation for RouteGate VPN accounts.

This feature treats traffic statistics as RouteGate domain data stored in PostgreSQL, not as transient counters owned by a specific VPN engine.

## Implemented foundation

### Storage

- `traffic_usage_events` stores append-only usage events reported by agents.
- `traffic_limits` stores per-VPN-account limit settings.

Usage events include:

- server ID;
- agent ID;
- VPN account ID;
- received bytes;
- transmitted bytes;
- generated total bytes;
- observed timestamp;
- reported timestamp;
- metadata payload.

Limits include:

- optional monthly byte limit;
- hard-limit flag;
- optional speed limit in bits per second;
- reset day.

### Agent API

`POST /api/v1/agent/traffic-usage`

Agents authenticate with their existing bearer token. The Manager hashes the raw bearer token before lookup.

A report is accepted only when the reporting agent is valid, enabled, bound to a server, and the referenced VPN account belongs to the same server.

### Admin API

`GET /api/v1/vpn-accounts/{id}/traffic`

Returns usage totals for a period. If no period is supplied, the current UTC calendar month is used. Query parameters:

- `from` — RFC3339 or `YYYY-MM-DD`;
- `to` — RFC3339 or `YYYY-MM-DD`.

`PATCH /api/v1/vpn-accounts/{id}/traffic-limit`

Updates the per-account traffic limit foundation:

- `monthlyLimitBytes`;
- `hardLimitEnabled`;
- `speedLimitBps`;
- `resetDay`.

## Current limitations

This PR does not yet enforce limits in rendered VPN configs, does not suspend accounts automatically, and does not implement frontend charts.

Those should come in follow-up slices after the backend data model and API foundation are merged.

## Follow-up candidates

- Agent-side sing-box/Xray counter collection.
- Daily/monthly aggregation tables or materialized summaries.
- Limit enforcement in config rendering/apply flow.
- User Portal traffic usage view.
- Admin dashboard traffic cards and charts.
- Alerting when usage approaches or exceeds limits.
