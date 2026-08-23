# Updates, Releases, and Versioning

RouteGate separates software delivery from installation updates. CI/CD produces and validates release artifacts; a future self-update subsystem will consume those artifacts through an explicit administrator-approved update job.

## Component versioning

RouteGate components remain independently observable even when they are shipped in one platform bundle:

| Component | Current source of truth |
| --- | --- |
| Manager | `backend/internal/buildinfo` with linker-overridable `Version`, `GitCommit`, and `BuildDate`. |
| Web UI | Manager system-version response; release bundles ship the UI built from the same source commit. |
| Agent | `agent/internal/buildinfo` with linker-overridable `Version`, `GitCommit`, and `BuildDate`. |
| Agent protocol | Numeric protocol version reported by Agents during registration and heartbeat. |
| Database schema | Applied migration identifiers in `schema_migrations`; the current canonical migration line reaches `000134_distinct_tcp_listener_ports`. |

The authenticated system-version endpoint still exposes the numeric expected schema generation for operator compatibility. Release artifacts use the exact migration identifier because it is the stronger deployment contract and naturally includes repair-migration naming.

## Mixed-version deployments

Manager and Agent versions do not need to match exactly. The primary compatibility boundary remains the Agent protocol:

| Agent report | Manager classification |
| --- | --- |
| Missing protocol version | `unknown` for legacy Agents. |
| Protocol lower than Manager minimum | `upgrade_required`. |
| Protocol higher than Manager supports | `unsupported`. |
| Supported protocol with reliably older Agent software | `upgrade_recommended`. |
| Supported protocol otherwise | `compatible`. |

The Manager stores the most recently reported Agent software version and protocol version. Admin Agent lists expose those fields with the compatibility status and a localized operator-facing explanation.

## Release bundle pipeline

`RouteGate Release` builds native Linux bundles for supported architectures from an exact Git commit. Each bundle contains the Manager, Agent, Web UI, database migrations, managed systemd units, recovery tooling, and build metadata.

Release builds use the source commit timestamp as the default `BUILD_DATE` and tar timestamp. Combined with sorted archive entries, numeric ownership, and Go `-trimpath`, this removes wall-clock time from normal release output and makes repeated builds of the same source materially more reproducible.

Each release output contains:

- `routegate-<version>-linux-amd64.tar.gz` and/or `routegate-<version>-linux-arm64.tar.gz`;
- `SHA256SUMS`;
- `release-manifest.json`.

## Release manifest contract

RG-96A introduces `release-manifest.json` format version 1 as the machine-readable contract between release CI/CD and the future RouteGate update engine.

The manifest records:

- manifest format version;
- product identity;
- RouteGate release version;
- exact Git commit;
- deterministic build date;
- exact expected database migration identifier;
- every platform artifact with OS, architecture, byte size, and SHA-256 digest.

Example shape:

```json
{
  "formatVersion": 1,
  "product": "RouteGate",
  "version": "v0.2.0",
  "commit": "<40-character Git SHA>",
  "buildDate": "2026-08-23T12:00:00Z",
  "database": {
    "expectedMigration": "000134_distinct_tcp_listener_ports"
  },
  "artifacts": [
    {
      "name": "routegate-v0.2.0-linux-amd64.tar.gz",
      "os": "linux",
      "arch": "amd64",
      "sha256": "<64-character SHA-256>",
      "size": 12345678
    }
  ]
}
```

`scripts/release_manifest.py` builds and verifies this contract. Verification rejects malformed metadata, unsupported platforms, duplicate artifact identities, checksum or size mismatches, disagreement with `SHA256SUMS`, bundle metadata disagreement, missing target migration files, path traversal, and special archive entries such as symlinks or device nodes.

CI exercises both positive and negative manifest cases and verifies the real amd64 release bundle. Tagged release workflows verify the complete manifest before publishing it as a GitHub Release asset.

## Integrity is not authenticity

`SHA256SUMS` and the RG-96A manifest establish artifact integrity and internal consistency. They do **not** by themselves prove that a remote release was authorized by the RouteGate project, because an attacker controlling the publication channel could replace both an artifact and its unsigned digest/manifest.

Therefore RG-96A does not yet make remote releases trusted input for unattended installation. The next trust milestone must add a cryptographic signature over the canonical release manifest and pin the corresponding verification identity/key in the updater trust policy.

RouteGate must not represent an unsigned manifest as a signed or trusted release.

## Current deployment model

The production-like deployment path already exercises most of the eventual host-update lifecycle:

1. build an exact-commit release bundle after successful CI;
2. transfer the bundle to the host;
3. verify SHA-256 and bundle metadata;
4. check the RouteGate control-plane preflight;
5. back up Manager, Agent, Web UI, migrations, systemd units, Manager environment, and PostgreSQL;
6. replace platform files;
7. start Manager and apply migrations;
8. validate the database against the migration shipped in the bundle;
9. start Agent;
10. run production-like validation and final health checks;
11. restore the database and platform files if a mutating stage fails.

VPN runtimes are deliberately outside the platform-update rollback transaction. Updating or rolling back Manager/Agent/UI must preserve an already-running VPN data plane whenever possible.

The production-like deploy remains CI/CD infrastructure, not the product self-update API. A customer installation must not depend on RouteGate maintainers having SSH access or on a GitHub Actions workflow to update itself.

## Current product update status

The Manager continues to report:

- update status: `manual`;
- update channel: `development`;
- `automaticUpdatesSupported: false`.

No one-click or unattended product updater is enabled by RG-96A.

Official builds remain builds of the auditable AGPLv3-or-later project. Update behavior must avoid hidden license checks, silent forced updates, opaque telemetry, undocumented outbound update calls, and arbitrary remote shell execution.

## Planned RG-96 continuation

The intended sequence is:

1. **RG-96A — Release Trust & Manifest**
   - A1: manifest contract, deterministic release metadata, artifact verification and publication;
   - A2: cryptographic manifest signing and pinned verification policy.
2. **RG-96B — Host Update Engine**
   - extract the reusable backup/apply/migrate/health/rollback lifecycle from production-like deployment behind a narrow privileged host-operation boundary.
3. **RG-96C — Update Jobs & API**
   - durable discovery, preflight, progress, result and rollback state in Manager.
4. **RG-96D — One-Click Admin UI**
   - explicit administrator-approved update workflow with clear risk and progress reporting.
5. **RG-96E — Multi-Node Rolling Updates**
   - Management plane first, then compatible Agent rollout with per-node health gates and no all-at-once default.
6. **RG-96F — Release Channels & Update Policy**
   - channel selection and controlled policy only after release trust and rollback behavior are proven.

The one-click button is intentionally the final presentation layer over a verified release contract and a recoverable host-update engine, not the starting point of the update architecture.
