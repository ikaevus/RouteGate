# RG-96C3c Privileged Update Dispatch

## Purpose

RG-96C3c introduces the narrow local boundary that can hand an already staged RouteGate candidate from the unprivileged Manager side to the existing privileged verified-update gate.

This slice does not redesign host mutation. The privileged `routegate-update-verified.sh apply` path remains the only entry into the existing role-aware transaction and must repeat the complete fixed-policy release verification immediately before mutation.

## Security model

RouteGate Manager continues to run as the unprivileged `routegate` service user. Manager is not granted general `sudo`, root shell access, systemd control, arbitrary command execution, or a caller-configurable privileged executable path.

The privilege transition is a RouteGate-owned systemd local socket/service pair installed by the trusted installer/update toolchain. The socket accepts connections only from the RouteGate Manager service identity. Each connection is handled by a short-lived root service whose executable and unit definitions are root-owned and not group/world writable.

The request language is intentionally tiny: one canonical UUIDv4 stage-job ID followed by a newline. No filesystem path, release URL, repository, version, asset name, role, shell text, environment override, trust root, signer, command, or option is accepted from Manager.

## Root-side reconstruction

For a request UUID `J`, the privileged dispatcher derives the only permitted candidate directory as:

`/var/lib/routegate-manager/update-staging/J`

It rejects malformed UUIDs before path construction and does not follow a caller-provided path.

Inside that fixed directory the dispatcher requires exactly the canonical staged release inputs needed by the verified gate:

- `release-manifest.json`
- `release-manifest.attestation.json`
- `SHA256SUMS`
- `release-bundles.attestation.json`
- exactly one regular, non-symlink `routegate-*.tar.gz` bundle

Unexpected entries, symlinked required inputs, missing inputs, multiple bundle candidates, or unsafe directory state fail closed before privileged apply.

The dispatcher does not trust the Manager-side C3b verification result as authorization. It invokes only the fixed trusted executable:

`/usr/local/lib/routegate/update/routegate-update-verified.sh apply`

with the five root-reconstructed local paths and `--role auto`. The verified gate then creates its own private frozen copies, validates the pinned verifier closure, verifies manifest provenance, verifies the target contract and bundle provenance, resolves the real node role through the existing role-aware update policy, and only then enters the host transaction.

## Socket/service boundary

The production dispatch endpoint is a fixed Unix-domain systemd socket below `/run/routegate/`. It is not TCP-accessible and is not exposed through nginx or the public API.

The socket/service configuration must satisfy these invariants:

- socket path and unit names are fixed in trusted RouteGate packaging;
- only the RouteGate Manager service identity may connect;
- each accepted connection receives a bounded request and produces a bounded success/failure response;
- the root service receives no request-derived environment or command line;
- the service has a bounded runtime and one update transaction at a time remains enforced by the existing privileged host lock;
- request parsing rejects extra fields, extra lines, oversized input, embedded NULs, and non-canonical UUID text;
- disconnecting the Manager request must not turn an already-started root transaction into an unsafe half-transaction; the existing transaction remains responsible for rollback on failure.

## Manager responsibility

A later Manager orchestration slice may create a durable apply job and send only its already-successful staged candidate UUID to the socket. Manager must not mark an update successful merely because dispatch was accepted. Durable result/restart reconciliation and post-update status are separate orchestration concerns.

C3c therefore proves only the local privilege transition and trusted re-verification handoff. It does not yet add one-click UI, automatic update policy, release channels, background polling, or multi-node rollout.

## Installer and upgrade ownership

Fresh Hybrid/Management installations must install the socket, service template, and dispatcher through the existing privileged installer path. Existing installations receive them only through an already verified RouteGate update. The files are RouteGate-owned trusted host state and must participate in the same ownership/symlink/writeability checks used for other privileged updater components.

VPN-only nodes do not run Manager and therefore do not need a Manager-facing local dispatch socket in this slice.

## Required validation

Focused tests must prove at least:

- valid canonical stage UUID dispatch reaches exactly one fixed `routegate-update-verified.sh apply` invocation;
- `--role auto` is supplied by trusted code, not by Manager;
- arbitrary paths/options/roles/URLs/extra JSON or extra lines are rejected;
- malformed/oversized requests do not invoke the verified gate;
- symlinked, missing, duplicated, or unexpected staged inputs fail before apply;
- a `gh` or updater executable from `PATH` cannot replace the fixed trusted executable;
- Manager receives no sudoers rule and the Manager service remains unprivileged;
- the production socket is local-only and access-restricted to the RouteGate service identity;
- existing B2/C3a/C3b verified-gate, transaction, rollback, installer, staging, and release-bundle tests remain green.
