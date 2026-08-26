# Remote VPN-node update enablement gate

Status: RG-96E2j design gate

## Purpose

Enable the first administrator-reachable creation path for a remote VPN-node RouteGate software update only after the RG-96E2i at-most-once dispatch and reconciliation boundary has been merged and proven.

E2j is intentionally narrow. It exposes controlled job creation and capability readiness; it does not widen the privileged task language, add arbitrary rollout concurrency, introduce automatic retry, or bypass the existing Manager -> Agent update lifecycle.

## Security significance

This is the first slice in which an authenticated administrator request can cause a durable `agent_platform_update_jobs` row to become runnable and therefore eventually initiate host mutation on a remote VPN Node. The create path is consequently a security gate and must fail closed.

The administrator-facing request may select only:

- one managed server identity;
- one canonical RouteGate target version.

It must not accept or persist a release URL, filesystem path, repository, artifact name, checksum, signer, trust root, node role override, executable path, command, environment assignment, systemd unit name, updater option, retry flag, or arbitrary selector.

## Eligibility

A remote update job may be created only when all of the following are true in one authoritative Manager-side decision:

1. the target server exists and is active;
2. the target is eligible to host the VPN capability under the canonical deployment-role model;
3. an enabled/online Agent is bound to that server;
4. the Agent advertises the exact software-update contract/version required by the Manager;
5. no active platform-update job already exists for that server;
6. the target version is canonical and differs from the currently known Agent/platform version when that comparison is authoritative;
7. the request is authenticated through the existing Admin security boundary.

The database active-job uniqueness constraint remains the final concurrency guard. API prechecks improve errors but must not replace the durable constraint.

## Capability transition

`softwareUpdate.state` may move from `contract_only` to an explicit ready state only when the shipped Agent actually contains the merged E2i dispatch + receipt + reconciliation implementation. Capability advertising is descriptive; it is never permission by itself to skip Manager eligibility checks.

The capability remains version-only. No privileged selector is added to heartbeat capabilities or task payloads.

## Creation semantics

Job creation is a durable enqueue operation only. A successful Admin response means that one update job was accepted into the Manager lifecycle; it does not mean dispatch occurred and never means the update succeeded.

The lifecycle remains:

`pending -> in_progress -> mutation_dispatched -> succeeded|failed|outcome_unknown`

The existing E2i rules remain authoritative:

- only `pending` can produce one dispatch-capable task;
- `in_progress` after transport loss is reconciliation-only;
- `mutation_dispatched` is reconciliation-only;
- no automatic redispatch occurs after the first claim;
- `outcome_unknown` is terminal for automatic execution;
- terminal update success comes only from matching durable Agent receipt evidence.

## API boundary

The initial Admin endpoint should be server-scoped and explicit about mutation intent. It must use strict JSON decoding with unknown fields rejected and a small bounded request body.

The response should return only durable job identity/lifecycle metadata needed by the Admin workflow. It must not expose Agent tokens, receipt paths, staged paths, release download URLs, updater output, commands, or other privileged implementation details.

Duplicate/active-job conflicts should return a stable conflict response rather than silently reusing or retrying an existing mutation attempt.

## Audit

Every accepted or rejected administrator attempt to create a remote platform update should produce bounded audit metadata containing only stable identifiers and the canonical target version. Secrets, URLs, local paths, raw database errors, updater output, and arbitrary request text must not be recorded.

## Rollout scope

E2j enables one-node update creation only. Fleet/rolling orchestration remains governed by the RG-96E readiness contract and is not introduced by this endpoint. The default future rollout policy remains sequential VPN-node mutation with a health gate between nodes.

## Validation

Focused tests must prove at least:

- unauthenticated callers cannot create a job;
- non-canonical versions and unknown JSON fields are rejected;
- privileged selectors cannot cross the Admin request contract;
- ineligible/non-VPN targets are rejected;
- missing, disabled, offline, or insufficient-capability Agents are rejected;
- the capability readiness state is advertised only by an Agent that contains the E2i implementation;
- two active updates for the same server cannot be created, including concurrent creation attempts;
- accepted creation persists only server/Agent identity, canonical target version, and lifecycle metadata;
- creation never reports update success or dispatch success;
- E2i at-most-once dispatch/reconciliation tests remain green;
- ordinary Agent operations and local Management-node update flows remain unchanged.

Because E2j makes the remote mutation pipeline administrator-reachable for the first time, exact-head CI and a focused security review are required before merge.
