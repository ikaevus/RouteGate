# RG-91 Security Baseline Closeout

Status: Done

This document closes the first RouteGate security baseline workstream.

RG-91 established the minimum security foundation for RouteGate Manager, RouteGate Agent, subscription access, audit visibility, secret handling, config apply safety, and admin session boundaries.

## Product security direction

RouteGate is an independent Linux VPN Management Platform.

Security assumptions:

- RouteGate Manager is the control plane.
- RouteGate Agent is the execution plane.
- sing-box is the first VPN runtime target.
- Admin users operate Manager through authenticated Admin API routes.
- Agents operate through Agent-only API routes.
- Public subscription endpoints are bearer-secret endpoints.
- Private keys and raw tokens must not leak through API responses, audit metadata, or logs.

## Security decisions

### SD-01 Agent operations are typed

Agent must not expose generic remote shell or arbitrary script execution endpoints.

Allowed Agent operations should be typed, reviewed, and represented as explicit task actions.

### SD-02 Subscription token is a bearer secret

Subscription tokens must be treated as secrets.

Rules:

- store only hashes where possible;
- return raw token only at creation or rotation time;
- use masked previews for UI and audit;
- avoid distinguishable public errors that help enumeration.

### SD-03 Reality private key stays server-side

Reality private key must not be returned through public subscription endpoints, account credential endpoints, or Admin UI responses unless a future explicit sealed-secret workflow is designed.

Reality public key may be exposed where needed.

### SD-04 Admin auth and Agent auth are separate domains

Admin session tokens and Agent bearer tokens must not be interchangeable.

Agent-only endpoints require Agent credentials.
Admin-only endpoints require Admin/session credentials and permissions.

### SD-05 Audit is foundation

Audit is part of the security foundation, not a later optional feature.

Sensitive lifecycle events should produce audit-safe records.

### SD-06 OPNsense is excluded

OPNsense is not part of RouteGate architecture, threat model, dependency model, or active security surface.

RouteGate remains an independent Linux VPN Management Platform.

## Completed security baseline items

### Audit Log Foundation

Implemented:

- `audit_events` persistence foundation;
- audit recorder and metadata sanitizing;
- audit-safe secret masking helpers;
- login audit events;
- server lifecycle audit events;
- registration token creation audit event.

Baseline result:

RouteGate has a durable audit foundation for sensitive Manager-side events.

### Subscription Token Hardening

Implemented:

- `rgsub_` token prefix;
- hash-only token storage;
- whitespace trimming before hashing;
- token preview support;
- subscription token lifecycle audit events;
- less enumerable public subscription failures.

Baseline result:

Subscription access is treated as bearer-secret access with safer storage, safer responses, and safer audit metadata.

### Agent Registration Hardening

Implemented:

- `rg_reg_` registration token prefix;
- `rg_agent_` Agent bearer token prefix;
- one-time registration token behavior preserved;
- Agent token preview support;
- Agent registration success/failure audit events;
- raw registration token not exposed from Agent registration endpoint;
- raw Agent bearer token returned only once after successful registration.

Baseline result:

Manager to Agent onboarding has a clearer secret lifecycle and safer audit visibility.

### Secrets Handling Baseline

Implemented:

- shared `backend/internal/secrets` helpers;
- public, sensitive, and secret classification;
- shared masking and metadata sanitizing;
- audit redaction routed through shared secret handling;
- token masking routed through shared secret handling;
- baseline documentation for secrets.

Baseline result:

RouteGate has one shared baseline for classifying, masking, and redacting sensitive values.

### Config Apply Safety

Implemented:

- re-validate stored rendered config before apply;
- reject broken rendered config JSON;
- reject rendered config that no longer passes validation;
- verify stored config hash before Agent job creation;
- require validated config version before apply;
- require registered Agent before apply;
- add config hash to apply job payload;
- avoid storing rendered config body in job request payload;
- apply requested and apply rejected audit events.

Baseline result:

Manager performs safety checks before creating Agent config apply jobs.

### Manager Agent Trust

Implemented:

- Agent-only endpoints require Agent credential prefix;
- non-Agent bearer values are rejected before repository lookup;
- heartbeat, task polling, and task completion use stricter Agent bearer parsing;
- task claimed, task completed, and task completion rejected audit events;
- Agent trust boundary documentation.

Baseline result:

Manager/Admin auth and Agent API auth have a clearer runtime boundary.

### RBAC Admin Sessions

Implemented:

- shared role and permission helpers;
- admin session helper;
- admin user detection;
- logout success and failure audit events;
- legacy admin server and agent routes protected by admin session middleware;
- RBAC/admin session tests;
- admin session baseline documentation.

Baseline result:

Admin-side access control is clearer and legacy admin routes no longer bypass the admin session boundary.

## Current trust boundaries

### Public endpoints

Public endpoints must not require Admin auth but must treat tokens as bearer secrets.

Current example:

- public subscription endpoint.

### Admin API

Admin API uses authenticated user sessions and role/permission checks.

Admin API must not accept Agent bearer tokens.

### Agent API

Agent API uses Agent bearer tokens.

Agent API must not accept Admin session tokens.

### Portal API

Portal API uses authenticated user sessions with portal permissions.

Portal users are not admin users.

## Secret exposure rules

Allowed one-time raw secret exposures:

- subscription token after creation or rotation;
- Agent bearer token after successful Agent registration;
- server registration token after Admin-generated registration token creation.

Not allowed:

- raw token in audit metadata;
- raw token in logs;
- token hash in API response;
- private key in public subscription response;
- private key in account credentials response;
- rendered config body in audit metadata.

## Remaining risks accepted for now

The baseline intentionally does not yet implement:

- encryption-at-rest for selected secrets;
- MFA;
- session inventory and manual revocation UI;
- Agent token rotation;
- signed Agent payloads;
- signed config payloads;
- replay protection for task completion;
- mTLS between Manager and Agent;
- immutable audit ledger;
- full per-route permission review for every legacy endpoint;
- production log scanning checks.

These are future hardening items, not blockers for the current MVP baseline.

## Suggested future security workstreams

Potential next security tasks:

- Audit Events UI and filters;
- Admin Session Management UI;
- Agent Token Rotation;
- Secret Encryption at Rest;
- Config Rollback Safety;
- Signed Config Payloads;
- Agent Runtime Validation;
- MFA Foundation;
- Rate Limiting and Abuse Protection;
- Security Headers and Deployment Hardening.

## Closeout summary

RG-91 moved RouteGate from basic feature security to an explicit security baseline.

The most important baseline outcomes are:

- secrets are classified and masked;
- audit exists as a durable foundation;
- subscription tokens are hardened;
- Agent onboarding is hardened;
- Manager to Agent trust boundary is clearer;
- config apply is safety-checked before Agent job creation;
- Admin sessions and legacy admin routes have a clearer boundary.

This is sufficient to close RG-91 as the first security baseline.
