# Updates, Releases, and Versioning

RouteGate separates software delivery from installation updates. CI/CD produces and validates release artifacts; the update subsystem consumes those artifacts only through explicit administrator-approved jobs and the verified host-mutation boundary.

## Component versioning

RouteGate components remain independently observable even when they are shipped in one platform bundle:

| Component | Current source of truth |
| --- | --- |
| Manager | `backend/internal/buildinfo` with linker-overridable `Version`, `GitCommit`, and `BuildDate`. |
| Web UI | Manager system-version response; release bundles ship the UI built from the same source commit. |
| Agent | `agent/internal/buildinfo` with linker-overridable `Version`, `GitCommit`, and `BuildDate`. |
| Agent protocol | Numeric protocol version reported by Agents during registration and heartbeat. |
| Database schema | Applied migration identifiers in `schema_migrations`; the current canonical migration line reaches `000144_platform_update_rollout_creation_idempotency`. |

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

`RouteGate Release` builds native Linux bundles for supported architectures from an exact Git commit. Each bundle contains the Manager, Agent, Web UI, database migrations, managed systemd units, recovery tooling, the shared host-update core, and build metadata.

Release builds use the source commit timestamp as the default `BUILD_DATE` and tar timestamp. Combined with sorted archive entries, numeric ownership, and Go `-trimpath`, this removes wall-clock time from normal release output and makes repeated builds of the same source materially more reproducible.

Each release output contains:

- `routegate-<version>-linux-amd64.tar.gz` and/or `routegate-<version>-linux-arm64.tar.gz`;
- `SHA256SUMS`;
- `release-manifest.json`;
- `release-manifest.attestation.json`;
- `release-bundles.attestation.json`.

## Release manifest contract

RG-96A introduces `release-manifest.json` format version 1 as the machine-readable contract between release CI/CD and the RouteGate update engine.

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
    "expectedMigration": "000144_platform_update_rollout_creation_idempotency"
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

`scripts/release_manifest.py` builds and verifies this contract. Verification rejects malformed metadata, unsupported platforms, duplicate artifact identities, checksum or size mismatches, disagreement with `SHA256SUMS`, bundle metadata disagreement, missing target migration files, path traversal, special archive entries such as symlinks or device nodes, and release bundles that do not contain the shared host-update core required by the update architecture.

CI exercises both positive and negative manifest cases and verifies the real amd64 release bundle. Tagged release workflows verify the complete manifest before publishing it as a GitHub Release asset.

## Release authenticity and provenance

RG-96A2 adds cryptographic build provenance using GitHub Artifact Attestations and Sigstore.

The release workflow creates separate SLSA provenance attestations for:

- the canonical `release-manifest.json`;
- all RouteGate platform bundles produced by the same workflow run.

The workflow uses GitHub Actions OIDC to obtain a short-lived Sigstore signing certificate. RouteGate therefore does not store a long-lived release-signing private key in repository or environment secrets. For the public RouteGate repository, the attestation is signed through the public-good Sigstore trust infrastructure and associated with the RouteGate repository through GitHub's attestation service.

The workflow preserves the generated Sigstore bundles as stable release assets:

- `release-manifest.attestation.json`;
- `release-bundles.attestation.json`.

The attestation step is pinned to an immutable full commit SHA rather than a mutable action tag.

### Trust policy

The RouteGate updater must not treat "has a valid Sigstore signature" as sufficient. Verification enforces all of these boundaries:

1. the artifact or manifest digest matches the attestation subject;
2. the attestation is associated with `ikaevus/RouteGate`;
3. the signer workflow is exactly `ikaevus/RouteGate/.github/workflows/release.yml`;
4. the attestation predicate is the expected SLSA build-provenance type;
5. the manifest itself passes the strict RG-96A1 structural and artifact-integrity checks;
6. the selected artifact matches the requested OS/architecture and its digest matches the verified manifest.

The release workflow enforces the same repository + signer-workflow identity immediately after creating the attestations, using the local Sigstore bundles for deterministic verification before publishing any GitHub Release assets.

An operator can perform the same offline-bundle verification with GitHub CLI, for example:

```bash
gh attestation verify release-manifest.json \
  --repo ikaevus/RouteGate \
  --signer-workflow ikaevus/RouteGate/.github/workflows/release.yml \
  --bundle release-manifest.attestation.json
```

For a platform bundle, use the same policy with `release-bundles.attestation.json`.

### What provenance does and does not prove

The RG-96A1 SHA-256 manifest proves artifact integrity and internal consistency. RG-96A2 adds a cryptographically verifiable statement that the artifact was produced and attested by the expected RouteGate GitHub Actions release workflow.

This still does not prove that a release is bug-free, secure, or appropriate for a particular installation. Release provenance is one trust gate in the update lifecycle, not a substitute for CI, compatibility checks, migration preflight, backup, health validation, or rollback.

A compromised release workflow or authorized repository change can still produce a correctly attested malicious artifact. Protecting the release workflow and branch/review policy therefore remains part of the release trust boundary.

## Shared host update core

RG-96B starts by separating reusable host-mutation mechanics from the production-like deployment wrapper. `scripts/routegate-update-core.sh` is deliberately a library rather than a network-facing or administrator-facing updater.

The common core owns bounded platform operations against known RouteGate paths:

- validate the expected local bundle SHA-256 before extraction;
- reject archive path traversal, links, and special filesystem entries before extraction;
- parse `metadata/manifest.env` as data without evaluating it as shell code;
- enforce exact target commit, OS, and architecture metadata;
- identify the exact target database migration shipped in the bundle;
- create a root-only backup of Manager, Agent, UI, migrations, systemd units, Manager environment, and PostgreSQL;
- replace RouteGate platform files while leaving VPN runtimes untouched;
- start and health-check Manager and Agent;
- validate the applied database migration;
- restore the retained backup, including PostgreSQL when a migration-capable stage may have changed the database.

The common core does **not** download releases, call GitHub, make release-channel decisions, verify Sigstore provenance itself, expose arbitrary shell execution, or decide when an update is allowed. Release discovery and provenance verification remain separate trust gates before every privileged update transaction.

The production-like deployment wrapper was the first real consumer of the common core. That is intentional: RouteGate CI/CD deployment and one-click self-update converge on one host mutation implementation instead of maintaining two subtly different backup/rollback paths.

B1 models the production-like Hybrid-node platform layout. The B2 privileged product transaction is explicitly role-aware so Management, VPN, and Hybrid nodes update only the components they own.

## Current deployment model

The production-like deployment path now exercises the eventual host-update lifecycle through the shared update core:

1. build an exact-commit release bundle after successful CI;
2. transfer the bundle and matching update core to the host;
3. verify SHA-256, archive safety, and bundle metadata;
4. check the RouteGate control-plane preflight;
5. back up Manager, Agent, Web UI, migrations, systemd units, Manager environment, and PostgreSQL;
6. replace platform files;
7. start Manager and apply migrations;
8. validate the database against the migration shipped in the bundle;
9. start Agent;
10. run production-like validation and final health checks;
11. restore the database and platform files through the same common core if a mutating stage fails.

VPN runtimes are deliberately outside the platform-update rollback transaction. Updating or rolling back Manager/Agent/UI must preserve an already-running VPN data plane whenever possible.

The production-like deploy remains CI/CD infrastructure, not the product self-update API. A customer installation must not depend on RouteGate maintainers having SSH access or on a GitHub Actions workflow to update itself.

## Manager update preflight jobs

RG-96C1 introduces the first durable Manager-side update-job contract without enabling product self-update execution.

The Manager stores typed `preflight` jobs with a durable `pending -> running -> succeeded|failed` lifecycle and exposes them through authenticated `system:manage` API endpoints. A preflight records a bounded snapshot of the current Manager build metadata, update mode/channel, automatic-update capability, and applied database migration.

The job execution status and the preflight decision are deliberately separate:

- `decision: proceed` means the Manager-side checks found no blocker;
- `decision: blocked` means the preflight completed successfully but advises against continuing;
- `status: failed` is reserved for failure of the preflight pipeline itself.

C1 is intentionally read-only. It does not discover or download releases, accept caller-controlled release URLs or trust roots, execute shell commands, invoke the privileged host updater, or mutate RouteGate installation files. Manager-side preflight is not a replacement for the RG-96B privileged trust and host-safety checks; future mutating jobs must pass through that existing boundary.

Update preflight lifecycle events use the existing audit subsystem. The durable job history is the foundation for later discovery, execution progress, result, and rollback state rather than a second parallel update mechanism.

## Manager release discovery jobs

RG-96C2 extends the same durable update-job history with a manual, read-only `discovery` operation and platform target selection.

An authenticated administrator with `system:manage` can explicitly request discovery. Only that request causes the Manager to contact the fixed official endpoint `https://api.github.com/repos/ikaevus/RouteGate/releases/latest`. C2 does not add a timer, startup check, background poller, configurable repository, release URL, trust root, channel, command, or filesystem path. Redirects are rejected, the request and response are bounded, and unsupported runtime platforms return without performing an outbound request.

Discovery parses only bounded release metadata needed for target selection: the tag, release flags, publication time, and asset names/sizes. For supported `linux/amd64` and `linux/arm64` Managers it selects the expected manifest, attestations, checksums, and matching platform bundle by name. Missing required assets produce a successful but non-actionable `incomplete_release` result rather than an update-ready claim.

Version comparison is deliberately conservative. Stable dotted versions can be classified as `update_available`, `up_to_date`, or `current_newer`; development, unknown, or otherwise uncomparable versions never imply update permission. A safe but unsupported release tag is reported as `uncomparable_release` rather than treated as a pipeline failure.

GitHub release metadata is **discovery data, not release authorization**. Persisted discovery results explicitly remain `unverified` and state that RG-96B provenance/manifest verification is still required. C2 does not download or stage release files, persist arbitrary remote URLs or raw responses, invoke the privileged updater, execute shell commands, mutate the host, or apply a product update.

Discovery jobs use the same bounded durable lifecycle and audit subsystem as C1. Interrupted `pending` or `running` discovery jobs are terminalized on Manager restart with a discovery-specific safe error code; existing preflight recovery semantics remain unchanged.

## Current product update status

The Manager continues to report:

- update status: `manual`;
- update channel: `development`;
- `automaticUpdatesSupported: false`.

The explicit RG-96D1 Admin workflow can drive the verified local Management/Hybrid update pipeline, while RG-96E3g/E4 expose the durable VPN-node rollout through ordered selection, progress, recovery, and explicit one-step-at-a-time controls. Neither path enables unattended updates: every mutation remains administrator-triggered, while automatic scheduling, release-channel policy, broad fleet concurrency, and automatic retry remain disabled.

Official builds remain builds of the auditable AGPLv3-or-later project. Update behavior must avoid hidden license checks, silent forced updates, opaque telemetry, undocumented outbound update calls, and arbitrary remote shell execution.

## RG-96 implementation status and continuation

The intended sequence is:

1. **RG-96A — Release Trust & Manifest**
   - A1 complete: manifest contract, deterministic release metadata, artifact verification and publication;
   - A2 complete: cryptographic build provenance, offline attestation bundles, and pinned repository/signer-workflow verification policy.
2. **RG-96B — Host Update Engine**
   - B1 complete: reusable backup/apply/migrate/health/rollback core proven through production-like deployment;
   - B2 complete: narrow, role-aware privileged host-operation transaction suitable for Manager orchestration.
3. **RG-96C — Update Jobs & API**
   - C1/C2 complete: durable preflight and fixed-source discovery;
   - C3a-C3d complete: non-mutating verification, bounded staging, privileged dispatch, and durable apply/rollback orchestration.
4. **RG-96D — One-Click Admin UI**
   - D1 implemented: explicit administrator-approved local update workflow with risk, progress, terminal result, and ambiguous-outcome reporting.
5. **RG-96E — Multi-Node Rolling Updates**
   - E1/E2 complete: readiness plus the fixed-policy, at-most-once VPN-node update lifecycle;
   - E3a-E3g complete: durable ordered snapshots, single-node admission, proof-gated advancement, bounded one-step controller, and the administrator-reachable API;
   - E4 complete: Admin presentation, idempotent creation recovery, durable status, and explicit one-step controls over the existing E3 contract.
6. **RG-96F — Release Channels & Update Policy**
   - separate future evolution for channel selection and controlled policy; it is not required to close the explicit administrator-driven update path.

The one-click button remains a presentation layer over the verified release contract and recoverable host-update engine, not a separate update implementation.
