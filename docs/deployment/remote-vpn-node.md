# Remote VPN Node Onboarding

## Purpose

RG-114B adds a supported one-command path for attaching an Ubuntu host to an
existing RouteGate Management or Hybrid Node.

The remote host receives only:

- RouteGate Agent;
- the Agent systemd unit;
- Agent state and configuration directories;
- checksum-verified Hysteria plus its inactive RouteGate-owned systemd unit;
- native WireGuard tooling.

It does not receive Manager, PostgreSQL, the Admin UI, or nginx. sing-box
installation remains a separate allow-listed Manager → Agent operation.
Hysteria and WireGuard stay inactive until a validated config apply.

## Guided workflow

1. In Manager, create a node with the `VPN Node` role.
2. Open the node and choose **Connect server**.
3. Generate the one-time registration token.
4. Copy the generated installation command.
5. Run it on a clean Ubuntu 24.04 LTS amd64 or arm64 host with `sudo`.
6. Keep the onboarding dialog open; Manager checks the connection every five
   seconds and shows the registered Agent after its first heartbeat.

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

## Security properties

- registration tokens are bound to one node, stored only as SHA-256 hashes,
  expire, and can be consumed once;
- the raw token appears only in the one-time Manager response and copied command;
- the installer does not print the token;
- the Agent installer URL is pinned to the exact Manager build commit;
- the downloaded Agent installer must match the SHA-256 embedded into the Manager
  binary at build time before `sudo` executes it;
- the bootstrap passes the exact Manager build version and, when needed, its
  commit-addressed bundle source to the installer;
- release or Manager-hosted bundles are verified against their `SHA256SUMS`
  file before extraction;
- the pinned Hysteria binary is verified against its upstream `hashes.txt`;
- Agent replaces the bootstrap token with its persistent dedicated credential
  and saves the config with mode `0600`;
- only Manager connects to PostgreSQL;
- Agent exposes no inbound management port and initiates HTTPS requests to
  Manager;
- Agent executes only the existing allow-listed task contract.

If registration fails after the token was consumed, create a fresh token in
Manager and run the newly generated command again.
