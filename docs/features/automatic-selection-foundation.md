# RG-114I — Automatic Selection Foundation

RG-114I turns the candidate evidence introduced in RG-114H into a stable,
operator-controlled node selection workflow.

## Selection behavior

- unavailable candidates are always excluded;
- ready candidates always win over degraded candidates;
- degraded fallback must be enabled explicitly per account;
- `priority` chooses the lowest priority and then the stable server ID;
- `weighted` uses account-keyed weighted rendezvous hashing;
- a per-account cooldown blocks repeated movement.

Preview exposes the selected candidate, health, signals, strategy, reason codes,
eligible count, evaluation time, and any cooldown deadline.

## Safe apply lifecycle

Apply is an explicit administrator action. RouteGate re-evaluates candidates,
checks account status and policy, locks the account assignment, and then updates
`vpn_accounts.server_id`. A concurrent manual change is rejected.

When a move occurs, the response contains both the previous and selected server
IDs in selected-node-first order and sets `configDeploymentRequired: true`. The
UI presents rendering and deploying the selected node before cleaning up the
previous node as the required next action. This keeps
the Guided Workflow / Next Action First principle at the safety boundary.

## API surface

- `PUT /api/v1/vpn-accounts/{id}/automatic-selection`
- `GET /api/v1/vpn-accounts/{id}/automatic-selection/preview`
- `POST /api/v1/vpn-accounts/{id}/automatic-selection/apply`

See [ADR-0009](../decisions/ADR-0009-explainable-automatic-selection.md).
