# RG-114H — Node Groups, Balancing Targets and Routing Extensions

RG-114H adds the control-plane model required before RouteGate can select a VPN
node automatically.

## Node groups

Authenticated administrators can create node groups, select a future strategy
(`priority` or `weighted`), and add VPN/Hybrid members with:

- priority `0..10000`;
- weight `1..1000`;
- an explicit enabled flag.

Management-only nodes are rejected. Deleting a group that is assigned to an
account returns a conflict instead of cascading policy loss.

## Candidate evidence

`GET /api/v1/node-groups/{group_id}/candidates` returns a read-only ordered
candidate list. Each entry exposes eligibility, health, and stable signal codes
derived by Manager from:

- membership policy;
- node status and deployment role;
- Agent online status and heartbeat freshness;
- the versioned managed-adapter capability contract;
- reported VPN runtime state;
- normalized one-minute load per logical CPU.

`high_load` is degraded but remains eligible. Missing/stale Agent evidence,
unsupported protocol, stopped runtime, disabled membership, or inactive node
makes a candidate unavailable. The endpoint never selects or mutates a node.

## Account routing policy

The account routing API resolves profiles in this order:

1. explicit account routing profile;
2. current server routing profile;
3. default routing profile;
4. no profile.

The account override is used by protected client configuration rendering. The
server-level renderer remains server-scoped.

An account can also store one node-group target. The response reports whether
its current `serverId` belongs to that group and always returns
`automaticSelection: false` in RG-114H.

## Admin UI

The **Node Groups** screen follows the Guided Workflow / Next Action First
principle:

1. create a group;
2. add or update VPN nodes;
3. inspect candidate health and exact signals;
4. assign the target from a VPN account's Routing & node policy panel.

The UI explicitly states that priorities and weights do not yet trigger
failover.

## API surface

- `GET|POST /api/v1/node-groups`
- `GET|PATCH|DELETE /api/v1/node-groups/{group_id}`
- `PUT|DELETE /api/v1/node-groups/{group_id}/members/{server_id}`
- `GET /api/v1/node-groups/{group_id}/candidates`
- `GET /api/v1/vpn-accounts/{id}/routing-policy`
- `PUT|DELETE /api/v1/vpn-accounts/{id}/routing-profile`
- `PUT|DELETE /api/v1/vpn-accounts/{id}/node-group`

See [ADR-0008](../decisions/ADR-0008-node-groups-routing-extensions.md).
