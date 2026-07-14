# Updates and Versioning Foundation

RouteGate components are versioned independently:

| Component | Current source of truth |
| --- | --- |
| Manager | `backend/internal/buildinfo` with linker-overridable `Version`, `GitCommit`, and `BuildDate`. |
| Web UI | Manager system version response, currently `dev` for MVP builds. |
| Agent | `agent/internal/buildinfo` with linker-overridable `Version`, `GitCommit`, and `BuildDate`. |
| Agent protocol | Numeric protocol version reported by Agents during registration and heartbeat. |
| Database schema | Expected migration version from repository migrations, currently `102`. |

## Mixed-version deployments

Manager and Agent versions do not need to match exactly. The primary compatibility boundary is the Agent protocol:

| Agent report | Manager classification |
| --- | --- |
| Missing protocol version | `unknown` for legacy Agents. |
| Protocol lower than Manager minimum | `upgrade_required`. |
| Protocol higher than Manager supports | `unsupported`. |
| Supported protocol with reliably older Agent software | `upgrade_recommended`. |
| Supported protocol otherwise | `compatible`. |

The Manager stores the most recently reported Agent software version and protocol version. Admin Agent lists expose those fields with the compatibility status and a short reason.

## Manual MVP update model

RG-96 does not implement automatic update downloads or installation. The system version endpoint reports update status as `manual`, the development channel, and `automaticUpdatesSupported: false`.

Official builds must remain trusted builds of the auditable AGPLv3-or-later open-source project. This foundation intentionally avoids hidden license checks, silent forced updates, opaque telemetry, and undocumented outbound update calls.

Future release work may add signed official build metadata and signed release manifests. That future manifest flow should remain transparent and auditable, and should still separate update discovery from any explicit administrator-approved installation mechanism.

## Database schema reporting

The current migration runner stores applied migration filenames in `schema_migrations`. The system version endpoint returns the expected schema version and the highest applied migration filename when it can be read through that table.
