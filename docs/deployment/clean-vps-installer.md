# RouteGate Clean VPS Installer

## Status

- Workstream: RG-100
- Initial acceptance target: Ubuntu 24.04 LTS on amd64
- Deployment model: native systemd services on one VPS
- Validation host: disposable `us.routegate.org`
- The Finland personal VPN server is explicitly out of scope

The installer is the canonical product boundary for a new self-hosted RouteGate deployment. The operator supplies a supported operating system and working SSH/sudo access. During interactive installation, RouteGate asks for the public FQDN and email addresses itself, then owns the platform installation from that point forward.

## Product contract

```text
Clean supported Ubuntu VPS
        ↓
one copy-paste command
        ↓
host, DNS, and conflict preflight
        ↓
verified RouteGate release bundle
        ↓
PostgreSQL + Manager + Admin UI + nginx + TLS + local Agent
        ↓
secure generated first login
        ↓
guided Install sing-box action in RouteGate
```

The operator must not manually install PostgreSQL, copy migrations, assemble systemd units, configure nginx, or register the local Agent.

The VPN Core remains a deliberate guided action in the RouteGate UI. The Clean VPS Installer installs the management platform; it does not silently install or activate sing-box.

## Requirements

Before running the installer:

- use a clean Ubuntu 24.04 LTS amd64 VPS;
- connect as `root` or a user with working `sudo`;
- create a DNS `A` record for the chosen FQDN pointing directly to the VPS public IPv4 address;
- ensure inbound TCP ports 80 and 443 are reachable;
- preserve a working SSH session during the first installation;
- review whether the host already operates unrelated PostgreSQL, nginx, Apache, or another service on ports 80/443.

Already installed packages are not conflicts: the installer reuses compatible APT packages and installs only missing ones. Active database or web services are treated differently because they may contain unrelated data or configuration. In that case the installer stops before mutation, explains the risk, and offers guided choices: preserve and exit, resolve the conflict in another SSH session and recheck, or display diagnostics and safe recommendations.

The installer does not provision the VPS and does not modify SSH authentication policy.

## Stable release installation

A published RouteGate release must exist because the installer downloads a versioned binary bundle and verifies it against the release `SHA256SUMS` file.

Interactive installation:

```bash
curl -fsSL https://raw.githubusercontent.com/ikaevus/RouteGate/main/install.sh \
  | sudo bash
```

The installer asks for the RouteGate domain, the Let's Encrypt contact email, and whether the same email should be used for the first RouteGate administrator. Pressing Enter accepts the recommended same-email choice.

Unattended confirmation:

```bash
curl -fsSL https://raw.githubusercontent.com/ikaevus/RouteGate/main/install.sh \
  | sudo bash -s -- \
      --domain vpn.example.com \
      --email owner@example.com \
      --yes
```

Pin a specific release:

```bash
curl -fsSL https://raw.githubusercontent.com/ikaevus/RouteGate/main/install.sh \
  | sudo bash -s -- \
      --domain vpn.example.com \
      --email owner@example.com \
      --version v1.0.0
```

## Installer options

```text
--domain FQDN          Public RouteGate hostname; prompted when omitted.
--email EMAIL          Let's Encrypt contact; prompted when omitted.
--admin-email EMAIL    Optional first administrator email.
--server-name NAME     Optional local All-in-One server display name.
--version VERSION      Release tag; defaults to latest.
--bundle-file PATH     Local release bundle for controlled E2E or offline staging.
--checksum-file PATH   Matching SHA256SUMS file for --bundle-file.
--bundle-url URL       Explicit bundle URL.
--checksum-url URL     Matching SHA256SUMS URL.
--yes                  Skip the final confirmation prompt.
--help                 Show command help.
```

Local and explicit URL modes still require checksum verification. A bundle without a matching SHA-256 entry is rejected.

## What the installer does

The installer performs these stages in order:

1. Prompts for missing domain/email values, then validates arguments, Ubuntu version, architecture, systemd, required base commands, and root privileges.
2. Shows which APT dependencies will be reused and which will be installed.
3. Detects unowned RouteGate files, active web/database services, or listeners already using TCP 80/443 and offers guided safe-resolution choices before making changes.
4. Verifies that the FQDN resolves to an IPv4 address detected for the VPS.
5. Creates a root-owned installation state and recovery area.
6. Installs required APT packages: PostgreSQL, nginx, Certbot, curl, jq, OpenSSL, and runtime utilities.
7. Resolves or accepts a RouteGate release bundle, verifies SHA-256, rejects unsafe archive paths/links, and validates its manifest.
8. Installs Manager, Agent, migrations, frontend assets, nginx configuration, and systemd units.
9. Creates a dedicated local PostgreSQL role and database with a generated password and loopback-only listening.
10. Generates a unique first-administrator password and bootstraps the first SuperAdmin.
11. Starts Manager on `127.0.0.1:8080` and verifies its health endpoint.
12. Configures nginx, preserves the existing SSH firewall policy, and requests a Let's Encrypt certificate.
13. Creates the local RouteGate server through the authenticated Manager API.
14. Creates a one-time registration token and registers the local Agent automatically.
15. Removes bootstrap administrator environment values and restarts Manager.
16. Verifies all services, HTTPS, Agent credentials, and PostgreSQL exposure.
17. Marks the installation complete and prints the next guided action.

## Initial credentials

No shared or permanent default password exists.

The installer generates a unique administrator password and writes the initial access information to:

```text
/root/routegate-first-login.txt
```

The file is mode `0600`. After saving the password in a password manager, remove the file:

```bash
sudo rm -f /root/routegate-first-login.txt
```

The temporary bootstrap password is removed from `/etc/routegate/manager.env` immediately after the first administrator and local Agent are verified.

## State, logs, and retry behavior

Installer log:

```text
/var/log/routegate-installer.log
```

Installation ownership/state:

```text
/etc/routegate/install-state.env
```

Interrupted-install secrets are temporarily stored under:

```text
/var/lib/routegate-installer/
```

These files are root-only. When a RouteGate-owned installation is marked `installing`, re-running the same command for the same domain resumes the workflow with the preserved secrets. After successful verification, transient secrets are deleted and state becomes `complete`.

When state is `complete`, running the same installer command performs health/idempotency checks and exits without reinstalling or rotating secrets.

The installer intentionally refuses to overwrite partial RouteGate files that do not have a valid RouteGate ownership state.

## Release bundles

Build release bundles from a trusted workspace, not on the target VPS:

```bash
VERSION=v1.0.0 \
COMMIT="$(git rev-parse HEAD)" \
scripts/build-release-bundle.sh
```

Generated assets:

```text
dist/routegate-v1.0.0-linux-amd64.tar.gz
dist/routegate-v1.0.0-linux-arm64.tar.gz
dist/SHA256SUMS
```

Each bundle contains:

```text
bin/routegate-manager
bin/routegate-agent
manager/migrations/
frontend/
systemd/
nginx/
metadata/manifest.env
```

The release workflow builds both architectures to establish a stable packaging model. The initial live installer acceptance remains amd64 only.

## Controlled branch E2E

Before the first official release, build an amd64 bundle in the trusted RouteGate workspace:

```bash
cd /workspaces/RouteGate

git switch rg-100-clean-vps-installer
git pull --ff-only

VERSION=rg-100-e2e \
COMMIT="$(git rev-parse HEAD)" \
ARCHITECTURES=amd64 \
scripts/build-release-bundle.sh
```

Transfer these two files to the disposable test VPS:

```text
dist/routegate-rg-100-e2e-linux-amd64.tar.gz
dist/SHA256SUMS
```

Then run the branch installer with the transferred bundle:

```bash
curl -fsSL https://raw.githubusercontent.com/ikaevus/RouteGate/rg-100-clean-vps-installer/install.sh \
  | sudo bash -s -- \
      --domain us.routegate.org \
      --email owner@example.com \
      --version rg-100-e2e \
      --bundle-file "$HOME/routegate-rg-100-e2e-linux-amd64.tar.gz" \
      --checksum-file "$HOME/SHA256SUMS" \
      --yes
```

Record only non-secret evidence: platform version, service status, HTTPS health, Agent connected state, second-run idempotency, and reboot persistence. Never post generated credentials, database passwords, Agent tokens, registration tokens, or TLS private keys.

## Security boundaries

- Manager listens only on loopback and is exposed through nginx.
- PostgreSQL is local-only.
- Release checksum verification is mandatory.
- Archive traversal and archive links are rejected.
- Secrets are not passed as process command-line arguments.
- Sensitive files use mode `0600`.
- Existing SSH settings are not weakened.
- When UFW is already active, the installer adds only the nginx HTTP/HTTPS application profile and preserves SSH rules.
- DNS mismatch or TLS failure stops the installation rather than presenting an accidental plaintext deployment as success.
- Existing unrelated web/database services trigger guided safe-resolution choices and are never modified automatically.

## Explicit non-goals for the MVP

- operating-system installation or SSH hardening;
- Docker, Kubernetes, HA, or external PostgreSQL deployment;
- automatic VPN Core installation;
- unattended RouteGate upgrade and rollback orchestration;
- browser-based one-time setup wizard;
- destructive uninstall or cleanup of unrelated host software.

## Post-installation next action

After signing in, open the automatically created local server. RouteGate should show the current VPN Core state and guide the administrator to **Install sing-box**. This preserves the global RouteGate principle: the platform installation completes first, then the UI communicates the next safe operational action.
