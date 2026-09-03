# RG-96 Updates and Versioning Closeout

Status: closed through RG-96E

Closeout date: 2026-09-03

## Decision

The explicit administrator-driven RouteGate update workstream is complete through RG-96E. The implemented boundary covers official release identity and provenance, verified and recoverable host mutation, durable Manager orchestration, the local Admin update workflow, and ordered one-at-a-time updates of dedicated VPN nodes.

RG-96F is not part of this closeout. Release-channel selection, unattended policy, maintenance windows, canaries, configurable parallelism, and automatic progression remain a separate future evolution.

## Completion matrix

| Stage | Accepted result |
| --- | --- |
| RG-96A — Release Trust & Manifest | Deterministic manifests, checksums, artifact identity, GitHub Artifact Attestation bundles, and fixed repository/signer-workflow policy. |
| RG-96B — Host Update Engine | Role-aware root transaction, pinned verifier runtime, backup, migration, health validation, rollback, and trusted updater promotion. |
| RG-96C — Update Jobs & API | Durable preflight, discovery, verification, staging, privileged dispatch, apply/rollback lifecycle, progress, result, and audit. |
| RG-96D — One-Click Admin UI | Explicit local Management/Hybrid update workflow with administrator confirmation and ambiguous-outcome handling. |
| RG-96E — Multi-Node Rolling Updates | VPN-node readiness, fixed-policy at-most-once execution, durable ordered rollout state, proof-gated one-step advancement, Admin API, and RG-96E4 presentation. |

The detailed contracts remain authoritative in [Updates, Releases, and Versioning](versioning-and-updates.md), [Verified Host Update Trust Boundary](verified-host-updates.md), and [Multi-Node Update Rollout](multi-node-update-rollout.md).

## Acceptance evidence

- exact-head and [post-merge repository CI](https://github.com/ikaevus/RouteGate/actions/runs/33718535040) passed for the RG-96E4 implementation, and the [closeout baseline CI](https://github.com/ikaevus/RouteGate/actions/runs/33721156702) remained green after the deployment-workflow correction;
- the production-like deployment workflow built and deployed exact `main` commit `66415bcf2d5b91077d5020b9e9e454c7a5aa5507` successfully in [Production-like Deploy #963](https://github.com/ikaevus/RouteGate/actions/runs/33721250891);
- deployment validation confirmed the Manager and Agent control plane active, managed VPN runtimes stable, the sing-box configuration valid, the public HTTPS surface healthy, and required database invariants present;
- the deployed RG-96E4 Settings presentation was manually accepted on the mobile production-like surface, including its expected fail-closed `production-like` version state and exact VPN-role-only inventory boundary;
- the canonical workflow failure discovered during acceptance was fixed in [PR #333](https://github.com/ikaevus/RouteGate/pull/333) by replacing a `tar | grep -q` pipeline that could fail under `pipefail` with a complete archive listing check.

## Deferred operational validation

The following evidence is intentionally collected when the required environment exists. It does not keep RG-96A-E open:

1. Exercise the release workflow with a real canonical `vX.Y.Z` tag and retain the resulting live Artifact Attestation acceptance evidence.
2. Exercise the complete remote rollout against at least one disposable dedicated `vpn` node. The current production-like host is Hybrid and is deliberately excluded from the VPN-only rollout path.

A failure in either exercise is handled as a defect against the closed contract. New release-policy or automation behavior belongs to RG-96F rather than extending RG-96A-E.

## Closeout result

There are no known blocking implementation gaps in RG-96A-E. RouteGate continues to report manual updates and `automaticUpdatesSupported: false`, matching the accepted explicit-administrator safety model.
