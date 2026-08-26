# Manager VPN-node update task reconciliation

Status: RG-96E2h contract

## Purpose

Connect the durable Manager-side remote VPN-node update lifecycle to the existing Agent task protocol and durable receipt reconciliation primitive without making remote host mutation reachable yet.

## Security boundary

The Manager may identify a remote software-update task only by canonical task UUID and canonical RouteGate target version. The task payload must not carry a URL, filesystem path, repository, asset name, checksum, role, command, signer, trust root, environment assignment, or arbitrary privileged selector.

The Agent must validate the existing version-only platform-update request contract before any update-specific handling. Reconciliation is read-only: it may inspect only the durable receipt for the same canonical task UUID and must require the receipt target version to exactly match the Manager job target version.

## Lifecycle mapping

Manager persistence remains authoritative for orchestration state:

- `pending` and `in_progress` are pre-dispatch states.
- `mutation_dispatched` is non-terminal and means only that a detached mutation worker was accepted.
- receipt `prepared` or `mutation_started` maps to reconciliation `pending` and must keep the Manager job in `mutation_dispatched`.
- receipt `succeeded` maps to Manager `succeeded`.
- receipt `failed` maps to Manager `failed` with only a bounded RouteGate-defined error code.
- receipt `outcome_unknown` maps to Manager `outcome_unknown` and must never trigger automatic redispatch.

A missing, malformed, mismatched, unsafe, or otherwise unclassifiable receipt must never be interpreted as success. Post-dispatch evidence that cannot be classified safely must remain pending or become `outcome_unknown` according to the existing receipt contract; it must not cause mutation retry.

## Restart behavior

After Manager restart, `mutation_dispatched` jobs are reconciliation-only. They must never be returned to a mutation-dispatch path. Agent restart recovery continues to use the existing durable receipt rules and detached worker contract.

## Scope exclusions

This slice does not expose a Manager API that creates remote update mutation jobs and does not enable the Agent detached update worker from a remote task. It only wires the bounded task/reconciliation transport needed for the later enablement slice.

## Validation

Focused tests must prove canonical version/task identity matching, pending versus terminal receipt mapping, no false success after dispatch, restart-safe reconciliation-only behavior, rejection of privileged selectors, and preservation of existing synchronous Agent operation semantics.
