# Verified Host Update Trust Boundary

This document records the RG-96B host-update security boundary that sits between RouteGate release artifacts and privileged host mutation.

## Purpose

RouteGate separates three concerns that must not collapse into one process:

1. release production and provenance (`RG-96A`);
2. privileged host mutation and rollback (`RG-96B`);
3. future administrator-facing orchestration (`RG-96C` and later).

A Manager process is not allowed to manufacture trust by choosing an arbitrary archive and supplying a matching digest to a root process. The privileged side must independently establish that a candidate is an official RouteGate release before any host files or database state can change.

## B2a: role-aware host transaction

`scripts/routegate-update-transaction.sh` is a root-only local transaction primitive layered over:

- `scripts/routegate-update-core.sh`;
- `scripts/routegate-update-role.sh`.

It performs no release discovery, download, GitHub lookup, or provenance verification. It accepts a local bundle only after an upstream privileged trust gate has approved the bundle identity.

The transaction resolves the local deployment role and fails closed on partial or ambiguous legacy layouts. An explicit requested role must match the detected role.

Role ownership is intentionally narrow:

| Node role | Updated platform components |
| --- | --- |
| Management | Manager, Web UI, migrations, Manager systemd unit; PostgreSQL is included in backup/rollback. |
| VPN | Agent binary and Agent systemd unit only. |
| Hybrid | Manager + Web UI + migrations + Agent through the shared update core. |

VPN runtimes such as sing-box, WireGuard, Hysteria2, Shadowsocks, and MTProto remain outside the platform-update transaction. A Manager/Agent/UI update should preserve the data plane whenever possible.

Backups and apply steps use explicit error propagation instead of depending on Bash `set -e` behavior inside conditional callers. Rollback is best-effort across the remaining restore steps but returns failure when database, file, or systemd recovery is incomplete.

## B2b: verified release gate

`scripts/routegate-update-verified.sh` is the privileged trust gate immediately above the B2a transaction.

It does not accept caller-controlled trust roots. The following policy is compiled into the script:

- repository: `ikaevus/RouteGate`;
- signer workflow: `ikaevus/RouteGate/.github/workflows/release.yml`;
- attestation predicate: `https://slsa.dev/provenance/v1`.

The caller supplies only local release material:

- `release-manifest.json`;
- `release-manifest.attestation.json`;
- `SHA256SUMS`;
- the bundle for the current host architecture;
- `release-bundles.attestation.json`;
- an optional requested deployment role, which B2a independently checks against the host.

The caller cannot supply the trusted repository, signer workflow, predicate type, target commit, or target SHA-256 used by the host transaction.

## Verification sequence

The verified gate performs the following sequence:

1. require root and fixed verifier dependencies;
2. require each supplied input to be a readable regular non-symlink file;
3. copy all supplied release material into a new root-only temporary directory;
4. stop using the caller-provided source paths after that snapshot;
5. verify the snapshotted release manifest with GitHub Artifact Attestations using the fixed repository, signer-workflow, and SLSA predicate policy;
6. run `release_manifest.py verify-target` against the snapshotted manifest, `SHA256SUMS`, and current `linux/<arch>` bundle;
7. derive the release version, exact Git commit, canonical bundle name, and SHA-256 from the verified descriptor;
8. verify provenance for the exact snapshotted target bundle using the same fixed attestation policy;
9. invoke the B2a role-aware transaction with the snapshotted bundle and the commit/SHA derived from the verified manifest.

The snapshot step prevents a verify/apply time-of-check/time-of-use race: provenance verification, digest verification, and privileged application refer to the same private copy.

## Target-only manifest verification

A canonical RouteGate release manifest can describe multiple architectures. A host should not need to download every architecture merely to update itself.

`release_manifest.py verify-target` therefore separates contract validation from local artifact validation:

- the complete manifest schema is validated;
- every manifest artifact identity, platform, size, and SHA-256 field is validated;
- duplicate artifact names and platforms are rejected;
- the complete release-bundle set in `SHA256SUMS` must exactly match the manifest artifact set;
- every manifest digest must match its corresponding `SHA256SUMS` entry, including entries for architectures not downloaded locally;
- only the selected current-host artifact must be present locally;
- that selected bundle is then hashed, size-checked, archive-inspected, and checked against its embedded metadata and expected migration.

This keeps the global release contract strict without requiring cross-architecture bundle downloads.

## Trust bootstrap and updater-tool promotion

The updater code inside a candidate bundle is not trusted merely because it is inside that bundle.

The verifier used to establish trust in version N+1 must already belong to the trusted version N installation or another equally trusted local administrative context. An unverified candidate must never be extracted and allowed to execute its own `routegate-update-verified.sh` as the mechanism that establishes the candidate's authenticity.

Every release bundle carries the manifest verifier and host-update toolchain so that a future trusted promotion step can advance the local verifier together with the platform. **B2b itself does not yet install or promote those tools on the host.** The current B2a platform apply path updates Manager/Agent/UI/migrations, not the updater toolchain.

A later privileged integration step must therefore promote the verified updater files only after candidate provenance has succeeded, and that promotion must participate in backup/rollback. Until such a step exists, the previously trusted local verifier remains authoritative.

The intended eventual chain is:

`trusted version N verifier -> verifies N+1 -> trusted transaction promotes N+1 platform + updater toolchain -> N+1 verifier may verify N+2`.

## GitHub CLI dependency

B2b deliberately reuses the RG-96A GitHub Artifact Attestation contract instead of introducing a second signing system. At this stage the privileged verifier uses `gh attestation verify` with the preserved local attestation bundles.

Therefore `gh` is an explicit verifier dependency for this B2b primitive. It is not an update-discovery mechanism: `routegate-update-verified.sh` itself does not query releases or download artifacts.

CI checks that the available GitHub CLI supports the pinned `--predicate-type` policy. Future production packaging may replace the CLI dependency with a dedicated verifier implementation or package the required verifier in another trusted way, but it must preserve the same repository, signer-workflow, predicate, subject-digest, and manifest-integrity policy.

## Manager boundary

Neither B2a nor B2b is exposed to Manager or Web UI yet.

A future RG-96C orchestration layer may discover releases and stage candidate files, but the privileged boundary must remain narrow:

- Manager may request an update operation;
- Manager may not override trust roots;
- Manager may not choose the commit/SHA passed to the root transaction;
- Manager may not request arbitrary shell commands;
- privileged code must continue to detect the host role itself;
- update state, progress, audit, and administrator approval belong above this boundary.

Until that orchestration exists, the Manager continues to report manual update status and `automaticUpdatesSupported: false`.

## Current RG-96 sequence

- RG-96A1 — release manifest and artifact-integrity contract: complete.
- RG-96A2 — GitHub Artifact Attestation/Sigstore provenance contract: implemented; live release-workflow acceptance remains an operational validation step when a real/manual release run is exercised.
- RG-96B1 — shared host update core proven through production-like deployment: complete.
- RG-96B2a — role-aware root-only host transaction: complete.
- RG-96B2b — fixed-policy verified release gate: implemented by this change and gated on CI/review before merge.
- trusted updater-tool promotion/installation: still required before Manager orchestration can safely depend on the local B2b gate.
- RG-96C — durable update jobs, discovery, preflight, progress, result, audit and rollback API: planned.
- RG-96D — explicit one-click Admin UI workflow: planned.
- RG-96E — multi-node rolling updates: planned.
- RG-96F — release channels and controlled update policy: planned.
