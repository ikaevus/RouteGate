# Config Apply Safety

RouteGate config apply is a sensitive operation because it changes server-side VPN runtime state through the Agent.

This baseline defines the first safety rules for applying rendered configs.

## Rules

1. A config version must be `validated` before apply.
2. A config version must contain valid JSON.
3. A config version must unmarshal into `routegate.config.v1` rendered config.
4. The rendered config must pass current validation rules again immediately before apply.
5. The stored `config_hash` must match the rendered config payload.
6. A server must have a registered Agent before apply.
7. Apply creates a pending job for the Agent; it does not directly run arbitrary commands.
8. Audit metadata must not include the rendered config body.

## Audit events

Current audit events:

- `config.apply.requested`
- `config.apply.rejected`

Rejected apply events include only safe metadata:

- server id
- config version id
- rejection reason

Successful apply request events include only safe metadata:

- server id
- agent id
- config version id
- job id
- job status

## Out of scope

This baseline does not yet implement:

- rollback hardening;
- Agent-side runtime validation;
- signed config payloads;
- config diff approval;
- immutable apply ledger;
- concurrent apply locking beyond the current job model.

These are future hardening steps.
