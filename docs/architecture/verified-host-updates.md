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

## B2c1: trusted updater promotion

The updater code inside a candidate bundle is not trusted merely because it is inside that bundle.

The verifier used to establish trust in version N+1 must already belong to trusted version N, or to another equally trusted local administrative context. Candidate code never establishes its own trust.

After B2b provenance succeeds, the B2a transaction treats the local updater toolchain as recoverable host state. The canonical installed locations are fixed in privileged code:

- `/usr/local/lib/routegate/update` for the trusted updater toolchain;
- `/usr/local/sbin/routegate-update` for the operator-facing entrypoint.

Before the first platform mutation, the transaction validates the current updater state and records it as either `absent` or `complete`. A complete state is backed up. Partial or unsafe state fails closed.

The transaction order is:

1. validate the current trusted updater state;
2. verify and extract the already-approved release bundle;
3. create the role/platform backup;
4. create the updater-toolchain backup;
5. apply only the platform files owned by the detected node role;
6. pass Manager/Agent health and database-schema gates applicable to that role;
7. promote the candidate updater files from the verified release bundle;
8. validate local trusted-path security and run updater self-checks;
9. complete the transaction.

If promotion or self-check fails, rollback attempts both platform recovery and updater recovery. A legacy host that entered with no local updater returns to `absent`. A host that entered with a complete trusted updater returns to that previous trusted toolchain.

The trusted updater boundary is deliberately stricter than a normal application directory. Before it is accepted, privileged code rejects:

- partial updater layouts;
- unexpected files in the trusted updater directory;
- symlinked fixed updater paths or fixed parents;
- updater files not owned by the privileged updater user (root in production);
- group- or world-writable trusted updater paths;
- non-executable privileged transaction/verifier scripts;
- non-canonical operator entrypoint content.

The fixed updater installation paths are not caller-configurable. Manager cannot redirect the privileged update boundary to an alternate executable tree.

The trust chain is therefore:

`trusted version N verifier -> verifies N+1 -> transaction updates platform -> health/schema gates -> promotes and self-checks N+1 updater -> N+1 verifier may verify N+2`.

B2c1 deliberately does not bootstrap this updater onto a fresh installation. Clean VPS and VPN Node installer bootstrap remains B2c2 work, so new hosts are not yet considered self-update-ready merely because release bundles contain the updater files.

The promotion path is recoverable for runtime failures through the transaction backup/rollback contract. It should not be described as power-loss-atomic; crash-recovery semantics for interruption at an arbitrary filesystem instruction remain a separate hardening concern if later required.

## GitHub CLI dependency

B2b deliberately reuses the RG-96A GitHub Artifact Attestation contract instead of introducing a second signing system. At this stage the privileged verifier uses `gh attestation verify` with the preserved local attestation bundles.

Therefore `gh` is an explicit verifier dependency for this B2b primitive. It is not an update-discovery mechanism: `routegate-update-verified.sh` itself does not query releases or download artifacts.

CI checks that the available GitHub CLI supports the pinned `--predicate-type` policy. Future production packaging may replace the CLI dependency with a dedicated verifier implementation or package the required verifier in another trusted way, but it must preserve the same repository, signer-workflow, predicate, subject-digest, and manifest-integrity policy.

## Manager boundary

B2a, B2b, and B2c1 remain outside Manager and Web UI control.

A future RG-96C orchestration layer may discover releases and stage candidate files, but the privileged boundary must remain narrow:

- Manager may request an update operation;
- Manager may not override trust roots;
- Manager may not choose the commit/SHA passed to the root transaction;
- Manager may not redirect the trusted updater executable tree;
- Manager may not request arbitrary shell commands;
- privileged code must continue to detect the host role itself;
- update state, progress, audit, and administrator approval belong above this boundary.

Until that orchestration exists, the Manager continues to report manual update status and `automaticUpdatesSupported: false`.

## Current RG-96 sequence

- RG-96A1 — release manifest and artifact-integrity contract: complete.
- RG-96A2 — GitHub Artifact Attestation/Sigstore provenance contract: implemented; live release-workflow acceptance remains an operational validation step when a real/manual release run is exercised.
- RG-96B1 — shared host update core proven through production-like deployment: complete.
- RG-96B2a — role-aware root-only host transaction: complete.
- RG-96B2b — fixed-policy verified release gate: complete.
- RG-96B2c1 — trusted updater promotion/rollback inside the host transaction: implemented by this change and gated on CI/review before merge.
- RG-96B2c2 — bootstrap the trusted updater on fresh Clean VPS and VPN Node installations: still required before Manager orchestration can rely on the local update boundary on newly installed hosts.
- RG-96C — durable update jobs, discovery, preflight, progress, result, audit and rollback API: planned.
- RG-96D — explicit one-click Admin UI workflow: planned.
- RG-96E — multi-node rolling updates: planned.
- RG-96F — release channels and controlled update policy: planned.
