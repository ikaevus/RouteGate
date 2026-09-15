# System maintenance, cleanup, and retention

RG-137 introduces one controlled maintenance workflow for RouteGate-owned
storage. Cleanup is not a generic filesystem or SQL console. Every removable
item belongs to an explicitly registered provider and follows the same
lifecycle:

`Analyze -> Plan -> Preview -> Confirm -> Cleanup -> Verify -> Report`

## Ownership inventory

| Storage | Owner | Initial cleanup behavior |
| --- | --- | --- |
| PostgreSQL expired sessions and one-time tokens | RouteGate Manager | Recommended logical cleanup |
| PostgreSQL Agent heartbeat history | RouteGate Manager | Recommended retention cleanup |
| PostgreSQL observability history | RouteGate Manager | Advanced retention cleanup |
| PostgreSQL audit history | RouteGate Manager | Advanced, explicit 365-day retention |
| Manager update staging partials | RouteGate Manager | Recommended cleanup when older than 24 hours |
| Manager verified update candidates | RouteGate update workflow | Not deleted by RG-137; existing stage retention and apply pins remain authoritative |
| Platform update rollback backups under `/root/routegate-backups` | Privileged updater | Inventory only until a typed privileged operation preserves the newest known-good rollback set |
| Agent runtime staging and backups | RouteGate Agent | Inventory only until typed Manager-to-Agent maintenance operations are available |
| RouteGate-managed Prometheus TSDB | RouteGate installer / Prometheus | Retention integration only; Manager never deletes TSDB blocks directly |
| External Prometheus, PostgreSQL, logs, or host files | Administrator | Out of scope; RouteGate never assumes ownership |

The inventory is deliberately fail-closed. Missing ownership proof, unsafe
permissions, symlinks, an unknown provider, an expired plan, or a changed plan
state stops cleanup for that provider.

## Plan and confirmation contract

The Manager creates a short-lived immutable plan containing exact provider
cutoffs and candidate counts. It returns a one-time confirmation token; only a
hash is stored. Execution requires both the plan identity and the token. A plan
can run once, and only one cleanup plan can be running at a time.

Providers re-apply their recorded cutoff during cleanup. Newer records are not
made eligible merely because execution happens later. Every provider is
verified after cleanup and produces an individual result in the final report.
Audit events record plan creation, successful completion, and failure without
recording the confirmation secret.

## Initial retention defaults

- expired authentication/setup/registration/pairing material: retain for 7
  days after expiry or use;
- Agent heartbeat history: 30 days;
- observability event and health-transition history: 90 days;
- completed diagnostic history: 30 days;
- audit history: 365 days and advanced mode only;
- incomplete Manager update staging directories: 24 hours.

Raw traffic events are intentionally excluded from the first cleanup set. The
current daily rollup trigger reacts to source-row deletion, so raw-event
retention must first gain an archival-safe aggregation boundary. Immutable
platform update rollout history is also excluded by design.

Automatic schedules remain deferred until the manual workflow has been
validated in production-like environments.

## Manager API

- `GET /api/v1/system/maintenance` analyzes all registered providers without
  changing storage;
- `POST /api/v1/system/maintenance/plans` creates a recommended or explicit
  advanced plan and returns its one-time confirmation token;
- `GET /api/v1/system/maintenance/plans/{plan_id}` returns durable plan and
  report state, never the confirmation token;
- `POST /api/v1/system/maintenance/plans/{plan_id}/execute` consumes the token,
  runs each selected provider, verifies it, and persists the report.

All endpoints require `system:manage`. Execution is bound to the administrator
who created the plan. A Manager restart never retries an interrupted cleanup;
it records the plan as failed with an unknown outcome and requires a fresh
analysis.
