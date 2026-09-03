# Verified Host Update Trust Boundary

Status: complete through the RG-96A-E boundary. See the [RG-96 closeout record](rg-96-closeout.md).

This document records the RG-96B host-update security boundary that sits between RouteGate release artifacts and privileged host mutation.

## Purpose

RouteGate separates three concerns that must not collapse into one process:

1. release production and provenance (`RG-96A`);
2. privileged host mutation and rollback (`RG-96B`);
3. administrator-facing orchestration (`RG-96C` and later).

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

1. require root and validate the fixed RouteGate-owned attestation verifier runtime;
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

The promotion path is recoverable for runtime failures through the transaction backup/rollback contract. It should not be described as power-loss-atomic; crash-recovery semantics for interruption at an arbitrary filesystem instruction remain a separate hardening concern if later required.

## B2c2: fresh-install trusted updater bootstrap

Fresh Clean VPS and VPN Node installations establish the same fixed updater layout used by B2c1 through `scripts/routegate-update-bootstrap.sh`, which is shipped inside every canonical RouteGate release bundle.

The release-bundle contract requires the bootstrap helper together with the complete runtime updater toolchain. A bundle missing any of these components is rejected by canonical manifest verification and by the installers before bootstrap.

The bootstrap helper is deliberately narrower than the normal update transaction. It does not discover RouteGate releases, verify RouteGate GitHub provenance, select a deployment role, or mutate Manager/Agent platform state. Its first responsibility is to establish or validate the local privileged updater layout after the installer has accepted the release bundle according to the installer's existing release policy. B2c3 then layers the fixed attestation-verifier runtime onto that trusted updater bootstrap.

Before any updater write, bootstrap checks the fixed parent paths and refuses symlinked or insecure parents. It then handles only two acceptable local updater states:

- `absent`: install the updater toolchain from the already accepted release bundle, validate ownership/modes, validate the canonical entrypoint, and run updater self-checks;
- `complete`: preserve the installed trusted updater and validate it instead of silently replacing it.

Partial or unsafe updater state fails closed. Ordinary first-bootstrap runtime or self-check failure removes the partially created updater files. As with B2c1 promotion, this does not claim arbitrary-instruction crash or power-loss atomicity.

Installer ordering is intentionally role-aware:

- Clean VPS / Hybrid installation completes platform configuration and final health verification first, then bootstraps the updater and verifier runtime, and only after successful bootstrap writes installer `STATUS=complete`;
- VPN Node installation installs and starts the Agent, waits for successful Agent registration, and only then bootstraps the updater and verifier runtime.

Both installer wrappers explicitly remove `RG_UPDATE_ROOT` from the helper environment. `RG_UPDATE_ROOT` exists only to virtualize filesystem paths in tests and must never redirect the production trusted updater installation.

Clean VPS conflict detection also treats the fixed updater paths as RouteGate-owned privileged state. A fresh clean-host install will not overwrite an unrelated or unexplained updater layout.

### Fresh-install trust limitation

B2c2 and B2c3 do **not** upgrade the authenticity semantics of the existing fresh RouteGate installers. At this stage they still accept their RouteGate release bundle through the existing `SHA256SUMS` installer flow. The bootstrap helper therefore does not claim that the initial RouteGate installation itself has passed the B2b GitHub Artifact Attestation provenance gate.

This distinction is intentional: B2c2 establishes the local updater trust layout from the release already accepted by the installer; B2c3 establishes a fixed local Artifact Attestation verifier runtime for future B2b operations; B2b establishes provenance for later RouteGate updates using that already trusted local verifier.

The project must therefore not describe B2c2 or B2c3 alone as changing the initial-install provenance model, enabling release discovery, or enabling automatic, one-click, or rolling updates.

## B2c3: pinned attestation verifier runtime

B2b no longer resolves `gh` from the host `PATH`. The production Artifact Attestation verifier is a separate RouteGate-owned trust object at the fixed path:

- `/usr/local/lib/routegate/verifier/gh` — the verifier executable;
- `/usr/local/lib/routegate/verifier/runtime.env` — canonical root-owned runtime metadata.

The trusted updater pins one GitHub CLI version and one official upstream release archive digest per supported architecture. The verifier URL, version, archive SHA-256, installation path, trusted RouteGate repository, signer workflow, and attestation predicate are not caller-configurable.

`sudo routegate-update install-verifier` performs the verifier bootstrap for an existing host. Fresh Clean VPS and VPN Node installations invoke the same operation automatically after the B2c2 updater bootstrap has completed successfully.

Verifier installation performs the following sequence:

1. require root and validate the fixed RouteGate verifier parent path;
2. resolve only the current supported architecture (`amd64` or `arm64`);
3. construct the immutable pinned GitHub CLI release asset URL from trusted constants;
4. download that exact asset over HTTPS only;
5. compare the downloaded tar archive with the hard-coded architecture-specific SHA-256;
6. reject archive traversal, links, and special filesystem entries before extraction;
7. require the one canonical `gh_<version>_linux_<arch>/bin/gh` binary path;
8. require the exact pinned GitHub CLI version and the `attestation verify --predicate-type` capability;
9. install the binary into the fixed RouteGate-owned verifier directory;
10. record canonical metadata with format version, verifier version, architecture, upstream archive SHA-256, installed binary SHA-256, and fixed source URL;
11. validate the complete installed runtime before reporting success.

The hard-coded upstream archive SHA-256 is the verifier supply-chain anchor. The installed binary SHA-256 is derived only after the trusted archive digest has matched and is then recorded in root-owned metadata for local-integrity checks. These are intentionally different digests for different objects and must not be conflated.

Before every B2b `apply`, privileged code validates the local verifier state again. It rejects:

- a missing, partial, or symlinked verifier runtime;
- non-root-owned verifier paths;
- group- or world-writable verifier paths;
- unexpected files in the verifier directory;
- metadata with an unknown or duplicate key;
- a version, architecture, upstream archive digest, or source URL that does not equal the pinned policy;
- an installed binary whose current SHA-256 differs from the recorded root-owned binary digest;
- a verifier that does not report the exact pinned GitHub CLI version or required predicate capability.

If the fixed verifier is unavailable or invalid, B2b fails closed before provenance verification or host mutation. A `gh` executable available elsewhere in `PATH` is never a fallback trust path.

### Existing-host transition

B2c3 intentionally does not add a sixth file to the B2c1 updater-toolchain format. An existing B2c1/B2c2 host can therefore receive the B2c3 `routegate-update-verified.sh` through the existing five-file trusted-updater promotion contract. The operator then runs `sudo routegate-update install-verifier` once before the next B2b update.

Verifier rotation is a separate controlled lifecycle concern. B2c3 deliberately uses no `latest` URL, mutable package repository, caller-selected verifier version, or silent PATH fallback.

## GitHub CLI verifier dependency

B2b deliberately reuses the RG-96A GitHub Artifact Attestation contract instead of introducing a second signing system. The dependency is now operationally packaged as the pinned RouteGate-owned verifier runtime rather than treated as an arbitrary system `gh` dependency.

The fixed verifier download is not RouteGate release discovery: `install-verifier` downloads only the exact GitHub CLI asset whose version, URL pattern, and archive digest are already fixed in the trusted updater. Normal B2b `apply` performs no verifier download and no RouteGate release discovery.

CI continues to verify the required `--predicate-type` capability and additionally exercises local verifier integrity, unsafe archive rejection, fresh-install bootstrap, and the no-`PATH`-fallback boundary. A future dedicated verifier implementation may replace GitHub CLI only if it preserves the same repository, signer-workflow, predicate, subject-digest, manifest-integrity, and fixed-local-trust properties.

## Manager boundary

B2a, B2b, B2c1, B2c2, and B2c3 remain outside Manager and Web UI control.

The RG-96C orchestration layer discovers releases and stages candidate files, but the privileged boundary remains narrow:

- Manager may request an update operation;
- Manager may not override trust roots;
- Manager may not choose the commit/SHA passed to the root transaction;
- Manager may not redirect the trusted updater or verifier executable trees;
- Manager may not request arbitrary shell commands;
- privileged code must continue to detect the host role itself;
- update state, progress, audit, and administrator approval belong above this boundary.

The Manager continues to report manual update status and `automaticUpdatesSupported: false` because the implemented orchestration is explicitly administrator-triggered and does not authorize unattended policy.

## Current RG-96 sequence

- RG-96A1 — release manifest and artifact-integrity contract: complete.
- RG-96A2 — GitHub Artifact Attestation/Sigstore provenance contract: implemented; live release-workflow acceptance remains an operational validation step when a real/manual release run is exercised.
- RG-96B1 — shared host update core proven through production-like deployment: complete.
- RG-96B2a — role-aware root-only host transaction: complete.
- RG-96B2b — fixed-policy verified release gate: complete.
- RG-96B2c1 — trusted updater promotion/rollback inside the host transaction: complete.
- RG-96B2c2 — bootstrap the trusted updater on fresh Clean VPS and VPN Node installations: complete.
- RG-96B2c3 — pinned RouteGate-owned Artifact Attestation verifier runtime with no `PATH` fallback: complete.
- RG-96C — durable update jobs, discovery, preflight, verified staging, progress, result, audit and rollback API: complete through C3d.
- RG-96D — explicit one-click Admin UI workflow: implemented through D1.
- RG-96E1/E2 — rollout readiness and fixed-policy at-most-once VPN-node update lifecycle: complete.
- RG-96E3 — durable one-at-a-time rollout orchestration and administrator-reachable API: complete through E3g.
- RG-96E4 — Admin rollout presentation and explicit operator controls over E3: implemented.
- RG-96F — release channels and controlled update policy: separate future evolution.
