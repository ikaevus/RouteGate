# Remote VPN-node update dispatch boundary

Status: implemented

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

E2i therefore requires a durable Agent-side handoff before detached launch. The later E2j enablement review tightened this handoff further so the durable receipt also covers the long staging window:

1. Strictly decode the version-only request and reject any prior local dispatch/staging evidence for the task UUID.
2. Create the root-owned no-replace durable receipt in `prepared` for that exact task UUID and target version **before staging begins**.
3. Stage the requested official RouteGate release using the fixed-policy staging primitive.
4. Verify that staged task identity and release version exactly match the claimed task; deterministic staging/identity failures monotonically transition the existing receipt `prepared -> failed`.
5. Re-read the receipt and require that it is still the same runnable `prepared` task/version before resolving or starting `systemd-run`.
6. Revalidate the complete fixed local update runtime immediately before detached launch, then start the existing transient-systemd worker using only the canonical task UUID.
7. The worker must accept only the matching pre-existing `prepared` receipt, revalidate the complete fixed runtime again, and atomically advance it to `mutation_started` before the verified updater is invoked.
8. Return bounded dispatch evidence only after `systemd-run --no-block` accepts the fixed unit.

A failure to create the durable `prepared` receipt prevents staging and worker launch. If durable receipt state is uncertain after a failed create, the attempt remains conservatively non-runnable instead of being retried. If the Agent is killed or the host reboots during staging, the already durable `prepared` receipt gives reconciliation pre-mutation evidence; a definitely absent fixed worker terminalizes it without redispatch. If the fixed detached launcher fails before a process can be started, the Agent marks the existing prepared receipt with a bounded deterministic failure code and reports `failed`.

Once `systemd-run` has been accepted, failure to deliver the Manager acknowledgement is post-dispatch ambiguity. The job must never be redispatched. A later Agent poll/restart reconciles the durable receipt instead.

## Agent dispatch sequence

For operation `dispatch`, the Agent follows the durable pre-launch handoff above. It must not call the verified updater directly and must not wait for the host update transaction to finish inside `routegate-agent.service`.

A successful detached launch means only `mutation_dispatched`; it is never update success. The task then becomes reconciliation-only under RG-96E2h.

## Pre-dispatch failures

Failures proven to occur before detached worker acceptance may transition `in_progress -> failed`, with only a bounded RouteGate-defined error code. Examples include invalid version request, stale execution readiness, official-release staging failure, unsafe/mismatched staged state, or a fixed detached launcher that cannot be started at all.

After the no-replace prepared receipt exists, every deterministic failure in the staging/launch path must terminalize that same receipt rather than creating replacement evidence. If another local path has already changed the receipt away from runnable `prepared`, dispatch fails closed and must not start a worker.

Raw download errors, verifier output, command output, paths, URLs, or environment data must not be persisted in the Manager job.

A transport disconnect or process restart while a job is `in_progress` is not by itself proof that mutation is safe to retry. Recovery inspects only the matching durable receipt plus the fixed task-specific systemd unit and must never infer permission to redispatch from the absence of a Manager acknowledgement. A missing/unreadable receipt in the narrow claim-to-first-receipt window remains conservative ambiguity, not retry permission.

## Detached-worker recovery

E2i extends the earlier receipt contract because remote at-most-once dispatch deliberately prevents a second task-specific worker from being created merely to discover an orphan.

Reconciliation is host-mutation read-only: it never stages a release, launches a worker, invokes the updater, or changes arbitrary host state. It may advance only the bounded root-owned receipt monotonically after querying the fixed unit `routegate-vpn-update-<task-id>.service` through the fixed local `systemctl` path.

The recovery rules are fail-closed:

- if a `prepared` or `mutation_started` receipt has a live/activating task-specific unit, keep the receipt pending;
- if `prepared` exists and the fixed unit is definitely inactive or not found, transition the receipt to deterministic pre-dispatch `failed`; this covers interrupted staging as well as interrupted pre-launch handoff, and a racing worker will reject the terminal receipt before mutation can start;
- if `mutation_started` exists and the fixed unit is definitely inactive or not found, transition monotonically to `outcome_unknown` because mutation may have happened but no terminal worker receipt survived;
- if the unit state cannot be read or parsed safely, do not change the receipt and do not make the Manager job runnable; retry reconciliation later.

This closes staging/worker SIGKILL and host-reboot recovery once the prepared handoff exists without ever creating a second mutation attempt.

## Post-dispatch rule

Once the detached worker is accepted, the Manager should persist `mutation_dispatched`. From that state:

- no automatic retry or redispatch is permitted;
- Agent restart and Manager restart resume only bounded receipt/unit reconciliation;
- terminal `succeeded`, `failed`, or `outcome_unknown` may come only from matching durable receipt evidence;
- inability to classify the post-dispatch outcome never creates a second mutation attempt.

If the Manager acknowledgement was lost and the durable Manager row remains `in_progress`, recovery must still be reconciliation-only. Matching `prepared`, `mutation_started`, or terminal receipt evidence proves that the task crossed the durable Agent handoff and therefore must not return to dispatch. A missing/unreadable receipt after such an interrupted `in_progress` session is conservative ambiguity, not permission to redispatch.

A locally persisted pre-dispatch failure whose Manager acknowledgement was lost is reported with the failed task envelope only while Manager still has `in_progress` state, so Manager terminalizes it without inventing `dispatched_at`. If Manager already recorded `mutation_dispatched`, later receipt reconciliation uses the successful transport envelope so known dispatch provenance cannot be erased.

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
- a matching no-replace durable `prepared` receipt exists before the staging function is entered and before detached worker launch;
- an existing receipt prevents a second staging attempt for the same task UUID;
- deterministic staging/identity/launch failures monotonically terminalize the existing prepared receipt;
- the receipt is re-read after staging so a concurrently terminalized receipt cannot be followed by worker launch;
- the detached worker accepts only the matching prepared task/version and advances it monotonically to `mutation_started`;
- deterministic pre-dispatch failure cannot be confused with a post-dispatch outcome;
- successful detached launch maps only to `mutation_dispatched`, never `succeeded`;
- loss of the post-launch Manager acknowledgement cannot make the task runnable again;
- a live task-specific unit keeps `prepared`/`mutation_started` reconciliation pending;
- a definitely absent task-specific unit converts orphaned `mutation_started` to `outcome_unknown` without launching another worker;
- a definitely absent task-specific unit converts an orphaned prepared receipt, including an interrupted staging handoff, to deterministic pre-dispatch failure;
- pre-dispatch terminal reconciliation does not invent Manager dispatch provenance;
- restart/replay cannot produce a second detached worker for the same job;
- ordinary Agent task semantics remain unchanged;
- no admin/public route can create a remote platform-update mutation job in E2i.

Exact-head CI and a focused security review are required before merge.
