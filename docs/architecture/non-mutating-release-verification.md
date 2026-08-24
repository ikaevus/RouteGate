# Non-Mutating Release Verification Boundary

RG-96C3a separates release verification from privileged host mutation without creating a second trust implementation.

The existing `routegate-update-verified` gate remains the single owner of the RouteGate release trust policy. It must support a non-mutating `verify` operation that accepts already-local release files, snapshots those files into a private temporary working area, verifies the release manifest provenance, validates the manifest/target contract, verifies the selected platform bundle provenance, and returns a bounded trusted target descriptor.

`verify` is deliberately not a release-discovery or download mechanism. It accepts no repository, release URL, trust-root, signer-workflow, predicate-type, command, role, or host-mutation parameters. The repository, signer workflow, predicate type, supported platform contract, and pinned attestation verifier remain fixed by RouteGate-owned code.

The operation may run without root when the caller can read the supplied files. It must still validate and use only the existing root-owned pinned attestation verifier runtime. Verification must fail closed when that verifier is missing, modified, incorrectly owned, writable by an untrusted group/world, or does not provide the pinned version and policy capability.

Diagnostic messages from `verify` belong on stderr. Successful stdout is reserved for the machine-readable trusted descriptor produced from the verified release contract. The descriptor is evidence that the supplied local snapshot passed the RouteGate trust gate; it is not itself permission to mutate the host.

The existing privileged `apply` operation must continue to pass through the same verification implementation before invoking the role-aware host transaction. A future Manager staging job may therefore use `verify` to classify a staged candidate, while a later explicit privileged apply repeats verification immediately before mutation. This deliberate repetition is defense in depth against staged-file replacement or stale verification state.

RG-96C3a does not add release discovery, artifact download, Manager staging directories, Manager API or durable-job changes, sudo delegation, systemd/package mutation, rollback, Admin UI, background checks, or automatic updates.
