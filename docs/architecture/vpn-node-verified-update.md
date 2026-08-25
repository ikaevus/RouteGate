# VPN node verified update boundary

## Purpose

RG-96E2 defines the narrow privileged update primitive that will run on VPN-capable nodes before RG-96E3 adds durable rolling orchestration. E2 is a host-local security boundary: it may update the RouteGate Agent platform on one VPN node, but it does not decide fleet order, create rollout records, or retry ambiguous outcomes.

## Existing privilege model

`routegate-agent` currently runs as `root`. That does not make the existing Manager -> Agent task channel sufficient authorization for arbitrary software installation. E2 therefore treats every remotely received update request as untrusted control-plane input and reconstructs all privileged details locally from fixed RouteGate policy.

The existing trusted updater installed under `/usr/local/lib/routegate/update/` remains the only host mutation implementation. E2 must reuse its verification, transaction, backup, health-check and rollback contracts rather than create a second updater.

## Request language

The future Agent task kind is `platform_update`.

The Manager may supply only a canonical RouteGate target release version plus the task identity already provided by the task protocol. The version is a selector for an official immutable RouteGate release, not authorization for an arbitrary artifact.

The request must reject additional privileged selectors. In particular, the task payload must not accept or derive behavior from caller-provided:

- shell or package-manager commands;
- executable or filesystem paths;
- release, asset or redirect URLs;
- repository owner/name;
- release asset names;
- checksums or manifest digests used as trust anchors;
- signer identity, trust root or attestation predicate;
- OS/architecture override;
- node role override;
- service names or systemd unit paths;
- environment assignments or updater flags.

Unknown fields in the update payload are rejected rather than ignored so the privileged request language cannot silently expand.

## Local reconstruction and provenance

For an accepted canonical version, the VPN node independently performs official-release discovery using the fixed RouteGate GitHub release namespace and the pinned RG-96 provenance policy already established for verified updates.

The node must independently verify, in order:

1. release-manifest attestation and fixed repository/signer/predicate policy;
2. manifest target matching the requested version;
3. bundle attestation and `SHA256SUMS` closure;
4. host OS/architecture selection from the verified manifest;
5. release bundle checksum and metadata;
6. trusted updater closure and fixed updater entrypoint;
7. detected node role, which must contain VPN capability and must not be caller-controlled.

The Manager cannot substitute its own previous verification result for these checks.

## Mutation boundary

The final host mutation is delegated only to the existing fixed verified updater. The Agent must not construct a shell command from task fields and must not resolve the updater through `PATH`.

For a VPN-only node, the transaction may update RouteGate Agent platform files and the canonical trusted updater toolchain. It must not change VPN runtime binaries, VPN configuration, user/account state, routing policy, traffic counters, or unrelated packages.

For a Hybrid node, E2 alone is not the canonical Management-plane update path. Hybrid platform updates remain ordered through the local Management RG-96C/D pipeline first; E3 must not use the VPN-node primitive to bypass that ordering.

## Completion and ambiguous outcome

A successful E2 result means the node completed the verified host transaction and returned after the updated Agent became healthy and protocol-compatible.

A deterministic pre-mutation verification failure is terminal failure and is safe to report as such.

Once mutation may have started, loss of the Manager connection, Agent process restart, task lease expiry, or inability to deliver the final result makes the remote outcome ambiguous from the Manager's perspective. Such an attempt must never be automatically replayed. E3 will persist this as an explicit unknown-outcome state and require reconciliation before another mutation attempt for that node.

The local updater's rollback behavior still applies to deterministic transaction failures observed on the node. Unknown remote acknowledgement state is distinct from local rollback success/failure.

## Concurrency

E2 supports at most one platform-update transaction on a node at a time and must preserve the existing host update lock. It must also reject a platform update while another RouteGate host update transaction owns that lock.

E2 does not implement fleet concurrency. E3 will default to one VPN node at a time and require a health gate before advancing.

## Result contract

Result data returned to the Manager is bounded and contains only non-secret operational metadata needed for E3 reconciliation, such as:

- requested target version;
- resulting Agent version;
- resulting Agent protocol version;
- terminal status code;
- whether host mutation had started;
- bounded failure/reconciliation code.

Raw verifier output, command stdout/stderr, local paths, attestation payloads, secrets, tokens, or environment values are never returned.

## E2 implementation sequence

1. add a strict `platform_update` Agent task contract and capability advertisement, without mutation;
2. add fixed-policy official release discovery/staging on the VPN node;
3. connect the staged candidate to the existing verified updater and host transaction;
4. add restart/ambiguous-outcome safeguards and focused integration tests;
5. require exact-head CI and security review before E3 may create rollout attempts.

## Explicit non-goals

E2 does not add rolling orchestration, rollout persistence, Admin UI, automatic scheduling, release channels, broad concurrency, arbitrary remote commands, generic package management, VPN runtime upgrades, or automatic retry of an ambiguous privileged mutation.