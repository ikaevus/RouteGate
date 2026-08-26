# VPN node update execution boundary

## Purpose

RG-96E2c connects the fixed-policy VPN-node staging primitive to the existing verified RouteGate host updater without making the Manager -> Agent task channel a generic root execution interface.

## Self-update process boundary

`routegate-agent` currently runs as root, but the verified VPN transaction stops and replaces the Agent service itself. Therefore the Agent must not execute `routegate-update-verified.sh apply` as an ordinary child process inside the `routegate-agent.service` cgroup.

If it did, `systemctl stop routegate-agent` could terminate both the old Agent and its updater child during the transaction. That would turn an otherwise deterministic self-update into an avoidable mid-mutation interruption and weaken rollback/reconciliation guarantees.

E2c therefore uses a RouteGate-owned transient systemd execution boundary. The normal Agent launches `/usr/bin/systemd-run` with a fixed unit name derived only from a canonical task UUID and a fixed executable `/usr/local/bin/routegate-agent`. The transient service starts the same trusted Agent binary in a local-only worker mode, outside the `routegate-agent.service` cgroup. The worker then replaces itself with the fixed verified updater through `exec`, so transaction signals reach the updater's existing rollback traps directly.

A second privileged worker executable or shell wrapper is deliberately not introduced. This keeps the worker lifecycle tied to the already installed and verified Agent binary and avoids a separate packaging/update trust surface.

## Fixed privileged request language

The detached worker receives only locally reconstructed, bounded RouteGate state:

- canonical UUIDv4 task identity;
- fixed staging directory derived as `/var/lib/routegate-agent/update-staging/<task-id>`;
- exactly the four canonical metadata/attestation files plus exactly one canonical `routegate-<version>-linux-<arch>.tar.gz` bundle;
- fixed operation `apply`;
- fixed requested role `vpn`.

No Manager payload may provide or override a path, URL, repository, asset name, checksum, signer, trust root, role, command, executable, service name, environment assignment, or updater flag.

The worker invokes only `/usr/local/lib/routegate/update/routegate-update-verified.sh apply`. It never resolves the updater from `PATH` and never introduces a second provenance implementation.

Passing `--role vpn` is deliberate. The existing trusted role resolver must reject this primitive on a Hybrid or Management node rather than allowing the VPN-node path to bypass the Management-first ordering established by RG-96E1.

## Staging revalidation

Immediately before detached execution, the worker reconstructs the staging directory from the canonical task ID and fails closed unless it contains exactly:

- `release-manifest.json`;
- `release-manifest.attestation.json`;
- `SHA256SUMS`;
- `release-bundles.attestation.json`;
- one regular non-symlink bundle matching the canonical RouteGate release-version grammar and supported `amd64` or `arm64` architecture suffix.

The staging root, candidate directory, staged files and trusted updater must be root-owned, non-symlinked and not group/world writable. Unexpected, missing, duplicate, symlinked or non-regular entries fail before the trusted updater is invoked. A prior E2b staging success is not release authorization; the verified updater repeats frozen-copy provenance, manifest, target and bundle verification immediately before mutation.

## Detached launcher

The detached launcher accepts only a canonical task UUID. It constructs a fixed `systemd-run` argv with:

- an instance name `routegate-vpn-update-<task-id>`;
- `--collect` and `--no-block`;
- a private `UMask=0077` and `NoNewPrivileges=yes` property;
- the fixed `/usr/local/bin/routegate-agent` executable;
- the single internal worker flag containing the canonical task UUID.

No shell is used and no task-supplied value becomes a command, environment assignment, unit path or updater option.

## Lifecycle and result boundary

E2c exposes the detached launcher and worker primitives but intentionally does not wire them to the Manager task runner. The existing host update lock inside the verified transaction remains authoritative for concurrent mutation exclusion.

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
- the trusted worker is the already installed Agent binary, not a separately managed privileged script;
- raw verifier/transaction stdout or stderr is not returned to Manager;
- ambiguous post-mutation outcomes are never automatically replayed;
- VPN runtime binaries/configuration and unrelated packages remain outside this platform-update transaction.

## E2c implementation sequence

1. add the fixed transient-systemd launcher and local Agent worker mode;
2. reconstruct and revalidate the exact staged candidate from the canonical task UUID;
3. invoke only the absolute verified updater with fixed `apply --role vpn` through process replacement;
4. preserve the existing transaction lock and rollback behavior;
5. keep Manager task wiring disabled until E2d adds durable result reconciliation;
6. require exact-head CI and security review before enabling remote mutation.
