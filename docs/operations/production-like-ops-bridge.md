# Production-like Operations Bridge

## Purpose

The production-like operations bridge provides an auditable, allow-listed path
for routine RouteGate diagnostics and recovery without exposing arbitrary remote
shell execution through GitHub Actions.

The bridge reuses the existing `production-like` GitHub environment and its SSH
secrets. Secrets remain inside GitHub Actions and are never copied into issues or
operation results.

## Control surface

GitHub issue `#268` (`RouteGate Ops Bridge`) is the fixed control issue. The
workflow runs only when that exact issue is reopened and its body matches one of
the exact allow-listed requests below, or when an administrator manually uses
the workflow's typed `workflow_dispatch` choice.

Supported requests:

- `operation=diagnose`
- `operation=diagnose-sing-box`
- `operation=validate`
- `operation=restart-control-plane`
- `operation=restart-sing-box`
- `operation=restart-wireguard`
- `operation=restart-hysteria2`
- `operation=restart-mtproto`
- `operation=renew-certificate`

`diagnose-sing-box` reports only safe runtime metadata: lifecycle state, process
exit status, installed version, config validation result, and the systemd
ExecStart command. It never exposes the sing-box configuration or raw journal
output.

The issue body is never evaluated as shell code. It is mapped through a fixed
`case` statement to one fixed operation name. The remote script independently
checks the same allow-list before doing anything.

After every issue-triggered run the workflow posts a sanitized result to #268
and closes the issue again, making the next reopen a distinct audited request.

## Security boundary

The bridge deliberately does **not** support:

- arbitrary shell commands;
- arbitrary service names;
- arbitrary paths or files;
- arbitrary database queries;
- arbitrary VPN configuration rollback identifiers;
- secret, environment, configuration, or journal dumps.

Diagnostics expose only service lifecycle state, HTTP status codes, the applied
schema version, and safe VPN runtime state. Raw journals are excluded because
the repository and Actions output may be visible to people who do not need
production secrets.

Mutating operations are narrow and reversible where practical. Runtime restarts
are isolated to one known service. A sing-box restart first validates the active
configuration. Control-plane restart and certificate renewal reuse the installed
`routegate-recovery` command instead of duplicating recovery logic.

## Control plane and data plane

Platform deployment requires a healthy RouteGate Manager and Agent. VPN runtimes
are independent data-plane components. A degraded sing-box, WireGuard,
Hysteria2, or MTProto runtime must be reported and recovered independently and
must not block an otherwise healthy RouteGate platform deployment.

The production-like deploy workflow therefore performs a strict preflight only
for Manager and Agent. Runtime status is diagnostic-only in the deployment
script. Platform rollback does not restart or rewrite unrelated VPN runtimes.

## Break-glass boundary

Operations that require an arbitrary VPN backup UUID or genuinely open-ended
host investigation remain break-glass procedures. They should use the local
`routegate-recovery` tool or direct administrative access rather than widening
the GitHub bridge into a general root shell.
