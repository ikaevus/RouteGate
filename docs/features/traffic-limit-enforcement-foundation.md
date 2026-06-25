# RG-74 — Traffic Limit Enforcement Foundation

Status: backend/UI foundation

## Goal

Persist a Manager-side traffic limit enforcement state when reported account usage reaches a configured hard limit.

This does **not** directly stop sing-box traffic yet. Runtime exclusion or account disabling should be connected later through config render/apply.

## Behavior

RouteGate evaluates traffic limits after Agent traffic usage reports are accepted by Manager.

A VPN account is marked as `over_limit` only when all conditions are true:

- `monthly_limit_bytes` is configured and greater than zero;
- `hard_limit_enabled` is true;
- total reported usage for the account is greater than or equal to the configured limit.

If the hard limit is disabled, or the limit is empty/zero, the state is `not_enforced`.

If the hard limit is enabled and usage is below the limit, the state is `within_limit`.

The first time an account crosses the hard limit, RouteGate stores `limit_exceeded_at`. Later reports keep the original timestamp, so evaluation is idempotent.

## Stored state

`traffic_limits` now stores:

- `limit_exceeded_at`
- `enforcement_status`
- `enforcement_updated_at`

Supported statuses:

- `not_enforced`
- `within_limit`
- `over_limit`

## API/UI

The existing account traffic endpoint exposes the persisted enforcement state in the traffic summary:

```text
GET /api/v1/vpn-accounts/{id}/traffic
```

The Admin UI traffic panel shows:

- current usage;
- monthly limit;
- remaining bytes;
- limit reached/over-limit badge;
- persisted enforcement status;
- first exceeded timestamp.

## Not included yet

- Real sing-box runtime blocking.
- Config renderer exclusion of over-limit accounts.
- Agent-side persistent counter baseline.
- Traffic aggregation tables.
- Monthly reset jobs.
