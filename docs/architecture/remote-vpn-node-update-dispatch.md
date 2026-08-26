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

`in_progress` is an at-most-once dispatch state. If the Manager/Agent transport is lost after claim, that job must never be returned as another dispatch task. Recovery may only reconcile durable Agent evidence or conservatively terminalize the ambiguous outcome; it may not retry mutation automatically.

## Durable pre-launch handoff

There is an unavoidable acknowledgement race if the Agent starts `systemd-run --no-block` first and only then tells Manager that mutation was dispatched: the detached worker may begin host mutation and restart `routegate-agent.service` before the acknowledgement reaches Manager. Manager would then still show `in_progress` even though mutation may already have started.

E2i therefore requires a durable Agent-side handoff before detached launch:

1. Strictly decode the version-only request.
2. Stage the requested official RouteGate release using the existing fixed-policy discovery/staging primitive.
3. Verify that staged task identity and release version exactly match the claimed task.
4. Create the existing root-owned durable receipt in `prepared` for that exact task UUID and target version before invoking `systemd-run`.
5. Start the existing detached transient-systemd worker using only the canonical task UUID.
6. The worker must accept only the matching pre-existing `prepared` receipt and atomically advance it to `mutation_started` before the verified updater is invoked.
7. Return bounded dispatch evidence only after `systemd-run --no-block` accepts the fixed unit.

A failure to create the durable `prepared` receipt is pre-dispatch and must prevent worker launch. If `systemd-run` fails synchronously before acceptance, the Agent may mark that prepared receipt with a bounded deterministic failure code and report `failed`.

Once `systemd-run` has been accepted, failure to deliver the Manager acknowledgement is post-dispatch ambiguity. The job must never be redispatched. A later Agent poll/restart must reconcile the durable receipt instead.

## Agent dispatch sequence

For operation `dispatch`, the Agent follows the durable pre-launch handoff above. It must not call the verified updater directly and must not wait for the host update transaction to finish inside `routegate-agent.service`.

A successful detached launch means only `mutation_dispatched`; it is never update success. The task then becomes reconciliation-only under RG-96E2h.

## Pre-dispatch failures

Failures proven to occur before detached worker acceptance are deterministic and may transition `in_progress -> failed`, with only a bounded RouteGate-defined error code. Examples include invalid version request, official-release staging failure, unsafe staged state, inability to create the durable prepared receipt, and synchronous failure to create the fixed detached systemd unit before it is accepted.

Raw download errors, verifier output, command output, paths, URLs, or environment data must not be persisted in the Manager job.

A transport disconnect or process restart while a job is `in_progress` is not by itself proof of a pre-dispatch failure. Recovery must inspect only the matching durable receipt and must never infer that it is safe to retry mutation from the absence of a Manager acknowledgement.

## Post-dispatch rule

Once the detached worker is accepted, the Manager should persist `mutation_dispatched`. From that state:

- no automatic retry or redispatch is permitted;
- Agent restart and Manager restart resume only read-only receipt reconciliation;
- terminal `succeeded`, `failed`, or `outcome_unknown` may come only from matching durable receipt evidence;
- inability to classify the post-dispatch outcome never creates a second mutation attempt.

If the Manager acknowledgement was lost and the durable Manager row remains `in_progress`, recovery must still be reconciliation-only. Matching `prepared`, `mutation_started`, or terminal receipt evidence proves that the task crossed the durable Agent handoff and therefore must not return to dispatch. A missing/unreadable receipt after such an interrupted `in_progress` session is conservative ambiguity, not permission to redispatch; it must remain non-runnable and may become `outcome_unknown` under the bounded recovery policy.

## Reachability gate

E2i must not add an administrator-facing endpoint that creates a remote platform-update job. It may implement and test the internal pending-job claim/dispatch completion path, but there must be no supported API route that lets a remote caller create the first runnable mutation job.

A later enablement slice may expose that create operation only after E2i is merged and reviewed. This separation prevents transport implementation and public mutation reachability from landing in the same review boundary.

## Capability advertising

`capabilities.softwareUpdate.state` remains `contract_only` throughout E2i. It must not become remotely update-capable merely because internal dispatch code exists. Capability enablement belongs with the later reachability slice.

## Validation

Focused tests must prove at least:

- only `pending` may become a dispatch task;
- claiming dispatch atomically moves exactly once to `in_progress`;
- an interrupted `in_progress` job is never automatically returned as dispatch;
- `mutation_dispatched` can produce only reconciliation tasks, never dispatch;
- Agent dispatch accepts only operation `dispatch` plus the strict version-only request;
- privileged selectors and unknown fields are rejected before staging or worker launch;
- a matching durable `prepared` receipt exists before detached worker launch;
- the detached worker accepts only the matching prepared task/version and advances it monotonically to `mutation_started`;
- deterministic pre-dispatch failure cannot be confused with a post-dispatch outcome;
- successful detached launch maps only to `mutation_dispatched`, never `succeeded`;
- loss of the post-launch Manager acknowledgement cannot make the task runnable again;
- restart/replay cannot produce a second detached worker for the same job;
- ordinary Agent task semantics remain unchanged;
- no admin/public route can create a remote platform-update mutation job in E2i.

Exact-head CI and a focused security review are required before merge.
