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

For released Manager builds, the generated command is also bound to the exact
Manager build identity. Manager embeds the full Git commit and SHA-256 of
`install-agent.sh` at release build time. The onboarding command downloads the
installer from that exact commit, verifies the embedded SHA-256 locally, and only
then executes it with the matching RouteGate release version. Mutable
`main`-branch installer execution is not part of the release onboarding path.

Development or non-release builds that do not carry a release version, full Git
commit, and installer checksum do not expose a copyable privileged bootstrap
command.

## Security properties

- registration tokens are bound to one node, stored only as SHA-256 hashes,
  expire, and can be consumed once;
- the raw token appears only in the one-time Manager response and copied command;
- the installer does not print the token;
- the Agent installer URL is pinned to the exact Manager build commit;
- the downloaded Agent installer must match the SHA-256 embedded into the Manager
  binary at build time before `sudo` executes it;
- the bootstrap passes the exact Manager release version to the installer, which
  prevents onboarding from silently drifting to a different published release;
- release bundles are verified against the published `SHA256SUMS` file;
- the pinned Hysteria binary is verified against its upstream `hashes.txt`;
- Agent replaces the bootstrap token with its persistent dedicated credential
  and saves the config with mode `0600`;
- only Manager connects to PostgreSQL;
- Agent exposes no inbound management port and initiates HTTPS requests to
  Manager;
- Agent executes only the existing allow-listed task contract.

If registration fails after the token was consumed, create a fresh token in
Manager and run the newly generated command again.
