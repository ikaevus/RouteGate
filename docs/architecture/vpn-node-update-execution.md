# VPN node update execution boundary

## Purpose

RG-96E2c connects the fixed-policy VPN-node staging primitive to the existing verified RouteGate host updater without making the Manager -> Agent task channel a generic root execution interface.

## Self-update process boundary

`routegate-agent` currently runs as root, but the verified VPN transaction stops and replaces the Agent service itself. Therefore the Agent must not execute `routegate-update-verified.sh apply` as an ordinary child process inside the `routegate-agent.service` cgroup.

If it did, `systemctl stop routegate-agent` could terminate both the old Agent and its updater child during the transaction. That would turn an otherwise deterministic self-update into an avoidable mid-mutation interruption and weaken rollback/reconciliation guarantees.

E2c therefore requires a RouteGate-owned detached systemd execution boundary. The mutation worker must run in its own transient or fixed unit/cgroup, independently of the Agent service lifecycle, before it is allowed to stop the Agent.

## Fixed privileged request language

The detached worker receives only locally reconstructed, bounded RouteGate state:

- canonical UUIDv4 task identity;
- fixed staging directory derived as `/var/lib/routegate-agent/update-staging/<task-id>`;
- exactly the four canonical metadata/attestation files plus exactly one deterministic `routegate-<version>-linux-<arch>.tar.gz` bundle;
- fixed operation `apply`;
- fixed requested role `vpn`.

No Manager payload may provide or override a path, URL, repository, asset name, checksum, signer, trust root, role, command, executable, service name, environment assignment, or updater flag.

The worker invokes only `/usr/local/lib/routegate/update/routegate-update-verified.sh apply`. It never resolves the updater from `PATH` and never introduces a second provenance implementation.

Passing `--role vpn` is deliberate. The existing trusted role resolver must reject this primitive on a Hybrid or Management node rather than allowing the VPN-node path to bypass the Management-first ordering established by RG-96E1.

## Staging revalidation

Immediately before detached execution, the worker must reconstruct the staging directory from the canonical task ID and fail closed unless it contains exactly:

- `release-manifest.json`;
- `release-manifest.attestation.json`;
- `SHA256SUMS`;
- `release-bundles.attestation.json`;
- one regular non-symlink bundle matching the locally determined target version and architecture.

Unexpected, missing, duplicate, symlinked or non-regular entries fail before the trusted updater is invoked. A prior E2b staging success is not release authorization; the verified updater repeats frozen-copy provenance, manifest, target and bundle verification immediately before mutation.

## Lifecycle and result boundary

E2c is responsible for starting one detached mutation worker at most once for a task. It must preserve the existing host update lock and must not start a second worker when an update transaction is active.

Because the Agent can legitimately disappear while its detached worker continues, the original task connection cannot be treated as a reliable final acknowledgement channel once mutation starts. E2d must add a root-owned bounded receipt/reconciliation record that survives Agent restart and distinguishes:

- deterministic pre-mutation rejection;
- mutation started / outcome pending;
- verified success after the new Agent becomes healthy;
- deterministic transaction failure/rollback result;
- outcome unknown requiring operator or later reconciliation.

Until that receipt contract exists, E2c must not expose the detached mutation primitive through the Manager task runner.

## Security invariants

- no shell command is constructed from task payload fields;
- no caller-controlled privileged selector crosses the boundary;
- VPN-only role is fixed locally and independently revalidated by the trusted updater;
- the updater runs outside the Agent service cgroup so stopping the old Agent cannot kill the transaction worker;
- raw verifier/transaction stdout or stderr is not returned to Manager;
- ambiguous post-mutation outcomes are never automatically replayed;
- VPN runtime binaries/configuration and unrelated packages remain outside this platform-update transaction.

## E2c implementation sequence

1. add the fixed detached worker/unit contract and exact staging reconstruction;
2. prove the worker survives `routegate-agent` stop/restart in focused systemd integration tests;
3. invoke only the absolute verified updater with fixed `apply --role vpn`;
4. preserve existing transaction lock and rollback behavior;
5. keep Manager task wiring disabled until E2d adds durable result reconciliation;
6. require exact-head CI and security review before enabling remote mutation.
