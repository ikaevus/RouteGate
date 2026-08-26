# Controlled remote VPN-node update enablement

Status: RG-96E2j design gate

## Purpose

Enable the already reviewed RG-96E2i single-node remote update lifecycle for explicit administrator use without widening the privileged language or introducing fleet automation.

E2j adds two things together because they form one safety gate:

1. the Agent advertises remote software-update readiness only when the complete fixed local execution and verification runtime is present and trusted enough to attempt the existing verified path;
2. Manager exposes a narrow authenticated API to create and inspect one single-node platform-update job using only a canonical RouteGate target version.

The existing E2i at-most-once dispatch, detached worker, verified updater, durable receipt, and reconciliation state machine remain authoritative. E2j does not invent a second execution path.

## Administrator boundary

Creating a remote host software update is a system-management operation, not an ordinary VPN configuration edit. The create endpoint therefore requires the existing `system:manage` permission.

The request body contains exactly:

```json
{"targetVersion":"v1.2.3"}
```

Unknown fields, extra JSON values, missing/empty versions, non-canonical versions, URLs, paths, repositories, artifact names, checksums, roles, commands, signer/trust-root selectors, environment values, retry flags, force flags, and rollout options are rejected before persistence.

The server identity comes only from the canonical route path. The Manager binds the job to the currently registered Agent for that server; the caller cannot select an Agent ID.

## Readiness capability

The Agent already advertises the schema-versioned software-update contract. E2j changes its state from `contract_only` to `ready` only when the current host can satisfy the complete fixed E2i execution boundary.

Readiness requires at least:

- Agent is running as root, matching the current RouteGate Agent privilege model;
- CPU architecture is `amd64` or `arm64`;
- fixed `/usr/bin/systemd-run` and `/usr/bin/systemctl` are root-owned regular executables and not group/world writable;
- fixed `/usr/local/bin/routegate-agent` is a root-owned regular executable and not group/world writable;
- the complete canonical `/usr/local/lib/routegate/update` toolchain is present with no unexpected entries, with the expected executable/non-executable components, root ownership, and non-writable trusted parent chains;
- fixed `/usr/local/sbin/routegate-update` is a trusted root-owned executable;
- the pinned `/usr/local/lib/routegate/verifier` runtime is complete, contains only `gh` and `runtime.env`, matches the fixed architecture/version/source/archive policy, and the verifier binary digest matches its root-owned metadata;
- the pinned verifier reports the expected GitHub CLI version and the required `attestation verify --predicate-type` capability.

Every fixed executable and every directory used to reach it is checked fail-closed for symlinks, ownership, and group/world writability. This prevents a writable parent directory from turning a previously checked path into a replacement surface.

If any prerequisite is absent, partial, inconsistent, or unsafe, the Agent continues advertising `contract_only`. In particular, a partial updater bootstrap that installed the toolchain but did not successfully install the pinned attestation verifier must never advertise `ready`. The capability carries no caller-controlled path or detailed host error text.

Heartbeat readiness is discovery evidence, not host authorization, and may become stale. The Agent repeats the same complete local runtime readiness check before staging, again after staging immediately before resolving/starting `systemd-run`, and once more inside the detached worker immediately before crossing the durable `mutation_started` boundary. The verified updater still repeats release provenance and verifier-runtime validation immediately before host mutation.

Canonical ready capability:

```json
{
  "softwareUpdate": {
    "schemaVersion": 1,
    "state": "ready",
    "request": "version_only"
  }
}
```

## Manager create gate

Manager may create a runnable platform-update job only when the target server has deployment role `vpn` and a non-disabled registered Agent whose latest durable capabilities contain exactly the supported software-update contract with `state=ready` and `request=version_only`.

Hybrid nodes are deliberately excluded from this VPN-only remote path. A Hybrid update must preserve the Management-first ordering and coordinated host-update lock defined by the verified-update architecture; enabling a VPN-only `--role vpn` mutation on a Hybrid host would bypass that boundary.

Creation must fail closed when:

- server or eligible Agent cannot be found;
- server is not a VPN-only node;
- Agent is disabled;
- readiness capability is missing, malformed, wrong schema, or not `ready`;
- another platform-update job for the server is `pending`, `in_progress`, or `mutation_dispatched`;
- target version is non-canonical.

The database partial unique index remains the final concurrency authority. A race that violates the one-active-job invariant maps to a conflict response rather than creating a second mutation attempt.

E2j does not require the Agent to be currently online. A ready registered Agent may temporarily be offline and later claim the durable `pending` job. Capability readiness comes from the Agent's last authenticated registration/heartbeat and does not itself authorize host mutation; the execution-time readiness checks are authoritative for entering staging/dispatch and mutation.

## API

Initial endpoints:

- `POST /api/v1/servers/{server_id}/software-updates` — requires `system:manage`; creates one pending job from a strict version-only request;
- `GET /api/v1/servers/{server_id}/software-updates/{job_id}` — requires `servers:read`; returns bounded job status for polling/operations.

The response may expose only bounded control-plane fields such as job ID, server ID, target version, lifecycle status, bounded error code, and timestamps. It must not expose Agent token material, release URLs, local staging paths, updater output, systemd details, or privileged selectors.

No cancel, retry, force, rollback, batch, or list-all-fleet endpoint is introduced in E2j.

## Audit

Creation is an administrator-originated host mutation request and must be recorded through the existing audit mechanism with bounded metadata:

- action `server.software_update.created`;
- server/job identity;
- target version;
- result success/failure reason code where available.

Do not record request headers, tokens, release URLs, local paths, raw Agent/updater output, or arbitrary failure strings.

Agent dispatch/reconciliation audit remains under the existing task audit path.

## Lifecycle

E2j creates only the durable `pending` state. From that point the already-reviewed E2i lifecycle is unchanged:

`pending -> in_progress -> mutation_dispatched -> succeeded|failed|outcome_unknown`

A deterministic pre-dispatch failure may terminate from `in_progress`, but it must first be persisted as a bounded local `prepared -> failed` receipt before Manager acknowledgement. This includes stale execution readiness, staging/identity failure, and deterministic detached-launch failure. If the acknowledgement is lost, subsequent reconciliation reads that receipt and terminalizes the Manager job without redispatching mutation. Interrupted `in_progress` with any prior/concurrent local dispatch evidence and all `mutation_dispatched` work are reconciliation-only. No automatic mutation retry is added.

## Explicit non-goals

E2j does not add Admin Web UI, release-channel selection, automatic discovery-based update choice, rolling/canary rollout, parallel fleet scheduling, maintenance windows, auto-update, caller-selected artifacts, generic remote commands, cancellation, rollback orchestration, Hybrid remote-update orchestration, or VPN-core updates.

## Validation

Focused tests must prove:

- readiness is `ready` only for the complete fixed trusted host runtime and supported architecture;
- unsafe/missing executable paths, missing toolchain components, partial verifier installation, verifier metadata/digest mismatch, and writable/symlinked parents remain `contract_only`;
- execution-time dispatch repeats readiness before staging, after staging immediately before `systemd-run`, and inside the worker before `mutation_started`, rather than trusting stale heartbeat capability;
- deterministic pre-dispatch failures are durably receipt-backed before Manager acknowledgement so acknowledgement loss remains reconciliation-only;
- API requires `system:manage` for create and `servers:read` for status;
- strict request decoding rejects unknown fields and privileged selectors;
- Manager binds the job to the server's eligible ready Agent, never a caller-selected Agent;
- Management and Hybrid nodes are rejected by the VPN-only create path;
- missing/non-ready capability fails closed before job creation;
- a second active update for the same server returns conflict;
- creation persists only canonical target version plus Manager-owned server/Agent identity;
- status response is bounded and does not expose privileged/local data;
- create does not directly stage, invoke systemd, call the verified updater, or bypass the existing Agent task channel;
- existing E2i exact at-most-once/reconciliation semantics remain green.

Exact-head CI and a focused security review are required before merge.
