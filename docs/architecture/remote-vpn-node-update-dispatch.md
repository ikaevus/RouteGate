# Remote VPN-node update dispatch boundary

Status: RG-96E2i design gate

## Purpose

Prepare the final Manager-to-Agent dispatch bridge for remote VPN-node software updates without exposing a public/admin mutation API in this slice. E2i connects the already merged version-only request, fixed-policy staging, detached worker, durable receipt, Manager lifecycle, and reconciliation contracts into one internal dispatch state machine.

Because this is the first boundary that can make remote input initiate host mutation, E2i is a security gate. The implementation must remain unreachable from an administrator-facing create endpoint until its exact-head CI and focused security review are complete.

## Fixed remote language

A dispatch task may contain only:

- the existing canonical task UUID;
- operation `dispatch`;
- the existing schema-versioned `targetVersion` request.

The Manager must not send a release URL, filesystem path, repository, asset name, checksum, signer, trust root, node role, executable path, systemd unit name, command, environment assignment, updater option, or arbitrary selector.

The Agent reconstructs all of those choices from fixed RouteGate policy. The detached worker continues to accept only the canonical task UUID and invokes only the fixed verified updater with trusted `--role vpn`.

## Manager lifecycle

Dispatch is allowed only from a durable `pending` platform-update job bound to one server and one Agent. Claiming a dispatch task atomically moves:

`pending -> in_progress`

No other state may produce a dispatch task. In particular, `mutation_dispatched` is reconciliation-only and can never return to the dispatch path.

The active-job uniqueness constraint continues to block a second platform update on the same server while the first job is `pending`, `in_progress`, or `mutation_dispatched`.

## Agent dispatch sequence

For operation `dispatch`, the Agent must perform exactly this sequence:

1. Strictly decode the version-only request.
2. Stage the requested official RouteGate release using the existing fixed-policy discovery/staging primitive.
3. Verify that the staged task identity and release version match the claimed task.
4. Start the existing detached transient-systemd worker using only the canonical task UUID.
5. Return bounded dispatch evidence only after `systemd-run --no-block` accepts the fixed unit.

A successful detached launch means only `mutation_dispatched`; it is never update success. The task then becomes reconciliation-only under RG-96E2h.

The Agent dispatch handler must not call the verified updater directly and must not wait for the host update transaction to finish inside `routegate-agent.service`.

## Pre-dispatch failures

Failures before detached worker acceptance are deterministic and may transition `in_progress -> failed`, with only a bounded RouteGate-defined error code. Examples include invalid version request, official-release staging failure, unsafe staged state, and failure to create the fixed detached systemd unit before it is accepted.

Raw download errors, verifier output, command output, paths, URLs, or environment data must not be persisted in the Manager job.

## Post-dispatch rule

Once the detached worker is accepted, the Manager must atomically persist `mutation_dispatched`. From that state:

- no automatic retry or redispatch is permitted;
- Agent restart and Manager restart resume only read-only receipt reconciliation;
- terminal `succeeded`, `failed`, or `outcome_unknown` may come only from matching durable receipt evidence;
- inability to classify the post-dispatch outcome never creates a second mutation attempt.

## Reachability gate

E2i must not add an administrator-facing endpoint that creates a remote platform-update job. It may implement and test the internal pending-job claim/dispatch completion path, but there must be no supported API route that lets a remote caller create the first runnable mutation job.

A later enablement slice may expose that create operation only after E2i is merged and reviewed. This separation prevents transport implementation and public mutation reachability from landing in the same review boundary.

## Capability advertising

`capabilities.softwareUpdate.state` remains `contract_only` throughout E2i. It must not become remotely update-capable merely because internal dispatch code exists. Capability enablement belongs with the later reachability slice.

## Validation

Focused tests must prove at least:

- only `pending` may become a dispatch task;
- claiming dispatch atomically moves exactly once to `in_progress`;
- `mutation_dispatched` can produce only reconciliation tasks, never dispatch;
- Agent dispatch accepts only operation `dispatch` plus the strict version-only request;
- privileged selectors and unknown fields are rejected before staging or worker launch;
- deterministic pre-dispatch failure cannot be confused with a post-dispatch outcome;
- successful detached launch maps only to `mutation_dispatched`, never `succeeded`;
- restart/replay cannot produce a second detached worker for the same job;
- ordinary Agent task semantics remain unchanged;
- no admin/public route can create a remote platform-update mutation job in E2i.

Exact-head CI and a focused security review are required before merge.
