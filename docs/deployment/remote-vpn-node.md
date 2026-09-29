# Remote VPN Node Onboarding

## Purpose

RG-114B adds a supported one-command path for attaching an Ubuntu host to an
existing RouteGate Management or Hybrid Node.

The remote host bootstrap receives only the RouteGate control-plane pieces
needed to establish a managed node:

- RouteGate Agent;
- the Agent systemd unit;
- Agent state and configuration directories;
- the trusted RouteGate updater boundary.

It does not receive Manager, PostgreSQL, the Admin UI, nginx, or any VPN
protocol runtime during bootstrap.

WireGuard, Hysteria2, MTProto and sing-box are installed later through the
existing allow-listed Manager → Agent runtime installation operations, only
when RouteGate needs the runtime for the protocol the administrator selects.
Runtime installers leave a newly installed service unconfigured/inactive until
the validated configuration apply owns activation.

## Guided workflow

1. In Manager, create a node with the `VPN Node` role.
2. Open the node and choose **Connect server**. RouteGate creates the
   short-lived registration token as part of this single onboarding workflow.
3. Copy the generated installation command.
4. Run it on a clean Ubuntu 24.04 LTS amd64 or arm64 host with `sudo`.
5. Keep the onboarding dialog open; Manager checks the connection every five
   seconds and shows the registered Agent after its first heartbeat.
6. After the node is connected, choose/configure the VPN protocol. RouteGate
   installs the required VPN runtime through Agent as the next managed action;
   do not install protocol runtimes manually on the host.
7. For VLESS / Reality, explicitly choose an external TLS handshake hostname
   and check its DNS and TLS reachability from the VPN node. The node hostname
   is not an automatic Reality target. Save settings, render and review a
   validated config version, apply it, and check the Agent result. Open the
   selected inbound VPN port in host and provider firewalls before testing a
   client; Agent's Manager connection itself needs outbound HTTPS only.

The onboarding dialog is the only registration-token surface in the Admin UI.
Its generated command already includes the displayed token; the raw value and
manual Agent configuration are folded into technical details for recovery.
Reopening it in the same browser session reuses the currently displayed token
until it expires. **Generate new token** is an explicit rotation action: Manager
invalidates the previous unused token before returning the replacement. Thus a
node has at most one usable registration token after a generation request
completes.

The command is generated from `ROUTEGATE_PUBLIC_URL`, which must be a public
HTTPS origin without a path, query, or fragment.

The generated command is bound to the exact Manager build identity. Manager
embeds the full Git commit and SHA-256 of `install-agent.sh` at build time. The
onboarding command downloads the installer from that exact commit, verifies the
embedded SHA-256 locally, and only then executes it.

Published release builds download the matching GitHub Release bundle and verify
its `SHA256SUMS`. The production-like deployment additionally publishes
commit-addressed amd64/arm64 bundles under the Manager HTTPS origin; its Manager
binary embeds that immutable bundle base URL, and the Agent installer verifies
the hosted bundle against the checksum file before installation. This keeps
exact-main validation and remote-node onboarding on the same commit without
pretending a development build is a published GitHub release.

Builds that lack a complete trusted identity — public HTTPS origin, full Git
commit, installer checksum, and either a published release tag or an explicit
verified bundle source — do not expose a copyable privileged bootstrap command.
The UI must show bootstrap unavailability rather than presenting the
configuration-only snippet as an installation command.

### Retry and diagnostics

The Agent bootstrap records its current stage and result without storing the
registration token:

- state: `/var/lib/routegate-agent-installer/state.env`;
- log: `/var/log/routegate-agent-installer.log`.

Both are root-owned and the log is mode `0600`. On failure the installer prints
the failed stage plus the safe next action. The supported retry path is to return
to **Connect server**, generate a fresh command, and run that newly generated
command. If the Agent already exchanged the registration token successfully, its
existing persistent identity for the same Manager is preserved.

## Security properties

- registration tokens are bound to one node, stored only as SHA-256 hashes,
  expire, can be consumed once, and explicit rotation invalidates the previous
  unused token;
- the raw token appears only in the one-time Manager response and copied command;
- the installer does not print the token;
- the Agent installer URL is pinned to the exact Manager build commit;
- the downloaded Agent installer must match the SHA-256 embedded into the Manager
  binary at build time before `sudo` executes it;
- the bootstrap passes the exact Manager build version and, when needed, its
  commit-addressed bundle source to the installer;
- release or Manager-hosted bundles are verified against their `SHA256SUMS`
  file before extraction;
- explicit remote bundle/checksum overrides, when used, must be HTTPS URLs
  without embedded credentials, fragments, or whitespace;
- release bundle extraction accepts only regular files and directories after
  path traversal checks;
- protocol runtime downloads and checksum verification happen in the Agent's
  dedicated runtime installation operations, not in the node bootstrap;
- Agent replaces the bootstrap token with its persistent dedicated credential
  and saves the config with mode `0600`;
- only Manager connects to PostgreSQL;
- Agent exposes no inbound management port and initiates HTTPS requests to
  Manager;
- Agent executes only the existing allow-listed task contract.

If registration fails after the token was consumed, create a fresh token in
Manager and run the newly generated command again.
