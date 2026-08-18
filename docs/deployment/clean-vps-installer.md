# RouteGate Clean VPS Installer

## Status

- Release: RouteGate v0.1.0 MVP
- Supported live installation target: Ubuntu 24.04 LTS on amd64
- Deployment model: native systemd services on one VPS
- Validation environment: disposable production-like `us.routegate.org`
- VPN Core: sing-box, installed after first login through RouteGate

The Clean VPS Installer is the canonical public installation boundary for the RouteGate MVP. The operator supplies a supported Ubuntu host, DNS, and working SSH/sudo access. RouteGate owns the management-platform installation from that point forward.

### RG-114E WireGuard runtime

Post-MVP RG-114E adds `wireguard-tools` and `iptables` to the supported
installer dependencies, creates dedicated mode-0700 WireGuard Agent storage,
and installs a RouteGate-owned sysctl setting for IPv4 forwarding. It does not
start a WireGuard interface during platform installation. The first validated
WireGuard Config Deploy owns enabling and starting the fixed
`wg-quick@routegate-wg0` unit, just as the existing VLESS path defers sing-box
startup until it has a valid config.

## Canonical product flow

```text
Clean Ubuntu 24.04 LTS VPS
        ↓
one copy-paste installer command
        ↓
host, DNS, and conflict preflight
        ↓
verified RouteGate release bundle
        ↓
PostgreSQL + Manager + Admin UI + nginx/HTTPS + local Agent
        ↓
single-use /setup administrator activation
        ↓
Guided Workflow / Next Action First
        ↓
Install sing-box through RouteGate
        ↓
recommended VLESS / Reality configuration
        ↓
first VPN account
        ↓
Config Deploy: render → validate → apply → start/restart → health check
        ↓
persistent client profile / QR / VLESS link
```

The operator does not manually install PostgreSQL, copy migrations, assemble systemd units, configure nginx, or register the local Agent.

The installer intentionally does **not** install or start sing-box. VPN Core installation remains a deliberate, allow-listed post-login action. Installing sing-box may leave the service installed but inactive/unconfigured; the first successful Config Deploy owns the first real startup with a valid generated configuration.

## Requirements

Before running the installer:

- use a clean Ubuntu 24.04 LTS amd64 VPS;
- connect as `root` or a user with working `sudo`;
- create a DNS `A` record for the chosen FQDN pointing directly to the VPS public IPv4 address;
- ensure inbound TCP ports 80 and 443 are reachable;
- preserve a working SSH session during the first installation;
- review whether the host already operates unrelated PostgreSQL, nginx, Apache, or another service on ports 80/443.

Already installed compatible APT packages are not conflicts. Active database/web services or unowned RouteGate files are treated as potential conflicts because they may contain unrelated data or configuration. The installer stops before unsafe mutation and presents safe recovery guidance.

The installer does not provision the VPS and does not modify SSH authentication policy.

## Install RouteGate

### Interactive installation

```bash
curl -fsSL https://raw.githubusercontent.com/ikaevus/RouteGate/main/install.sh \
  | sudo bash
```

The installer asks for:

- the public RouteGate FQDN;
- the Let's Encrypt contact email;
- whether the same email should be used for the first RouteGate administrator.

Pressing Enter accepts the recommended same-email choice.

By default, the installer resolves the latest published RouteGate release and verifies the selected bundle against the release `SHA256SUMS` file.

### Pin v0.1.0 explicitly

```bash
curl -fsSL https://raw.githubusercontent.com/ikaevus/RouteGate/main/install.sh \
  | sudo bash -s -- \
      --domain vpn.example.com \
      --email owner@example.com \
      --version v0.1.0
```

### Unattended confirmation

```bash
curl -fsSL https://raw.githubusercontent.com/ikaevus/RouteGate/main/install.sh \
  | sudo bash -s -- \
      --domain vpn.example.com \
      --email owner@example.com \
      --version v0.1.0 \
      --yes
```

## Installer options

```text
--domain FQDN          Public RouteGate hostname; prompted when omitted.
--email EMAIL          Let's Encrypt contact; prompted when omitted.
--admin-email EMAIL    Optional first administrator email.
--server-name NAME     Optional local All-in-One server display name.
--version VERSION      Release tag; defaults to latest.
--bundle-file PATH     Local release bundle for controlled E2E/offline staging.
--checksum-file PATH   Matching SHA256SUMS file for --bundle-file.
--bundle-url URL       Explicit bundle URL.
--checksum-url URL     Matching SHA256SUMS URL.
--yes                  Skip the final confirmation prompt.
--help                 Show command help.
```

Local-file and explicit-URL modes still require checksum verification. A bundle without a matching SHA-256 entry is rejected.

## What the installer does

The installer performs these stages in order:

1. Prompts for missing domain/email values and validates arguments, Ubuntu version, architecture, systemd, required base commands, and privileges.
2. Shows which required APT dependencies will be reused and which will be installed.
3. Detects unowned RouteGate files, active web/database services, or listeners on TCP 80/443 before mutation.
4. Verifies that the FQDN resolves to an IPv4 address detected for the VPS.
5. Creates root-owned installation state and recovery storage.
6. Installs required APT packages including PostgreSQL, nginx, Certbot, curl, jq, OpenSSL, and runtime utilities.
7. Resolves the requested RouteGate release, downloads the native bundle, verifies `SHA256SUMS`, rejects unsafe archive paths/links, and validates the manifest.
8. Installs Manager, Agent, migrations, frontend assets, nginx configuration, and systemd units.
9. Creates a dedicated local PostgreSQL role/database with a generated password and loopback-only listening.
10. Generates a unique bootstrap administrator credential used only to initialize the first SuperAdmin and local platform workflow.
11. Starts Manager on `127.0.0.1:8080` and verifies its health endpoint.
12. Configures nginx, preserves the existing SSH firewall policy, and requests a Let's Encrypt certificate.
13. Enables `certbot.timer` and installs a RouteGate nginx validation/reload hook for certificate renewal.
14. Creates the local All-in-One Server through the authenticated Manager API.
15. Creates a one-time Agent registration token, starts the local Agent, and verifies persistent Agent credentials.
16. Creates a high-entropy, single-use administrator setup token and constructs `https://<domain>/setup#token=<token>`.
17. Removes bootstrap administrator values from the Manager environment and restarts Manager.
18. Writes root-only first-access/recovery information and installs `routegate-recovery`.
19. Verifies PostgreSQL, nginx, Manager, Agent, HTTPS health, Agent credentials, and local PostgreSQL exposure.
20. Marks installation state complete and prints the `/setup` next action.

## First administrator activation

The canonical first access is the `/setup` link printed by the installer:

```text
https://vpn.example.com/setup#token=<single-use-token>
```

Security properties:

- the setup token is high entropy;
- only its SHA-256 hash is stored in PostgreSQL;
- the token is carried in the URL fragment so it is not sent as part of normal HTTP request URLs;
- the link is single-use;
- the link expires after 30 minutes;
- the administrator chooses and confirms a new password in the browser;
- successful activation consumes the token atomically, revokes bootstrap sessions, signs the administrator in, and removes the setup token from browser history.

The installer also writes root-only recovery information to:

```text
/root/routegate-first-login.txt
```

The file is mode `0600` and contains the setup URL plus a unique bootstrap password retained only as an emergency recovery credential if activation is not completed before the setup link expires. RouteGate does not email plaintext passwords and SMTP is not configured automatically.

After successful activation and password verification, remove the recovery file:

```bash
sudo rm -f /root/routegate-first-login.txt
```

## Guided first-run VPN setup

After `/setup`, RouteGate signs the administrator in and the Dashboard exposes the current setup state and exactly one primary next action.

The canonical All-in-One flow is:

1. confirm the automatically registered local Server/Agent is connected;
2. **Install sing-box** through the existing allow-listed Agent operation;
3. configure the recommended VLESS / Reality settings;
4. create the first VPN account;
5. run **Deploy VPN**, which uses the existing render → validate → apply → restart/start → health-check lifecycle;
6. open the account and use the persistent QR code or VLESS link in a compatible client.

Recommended All-in-One network ownership:

```text
nginx / RouteGate HTTPS    TCP 443
VLESS / Reality            TCP 8443
```

The recommended Reality flow uses TCP and `xtls-rprx-vision`, generates a fresh Reality keypair and Short ID, and uses the server hostname as the initial Reality server name/handshake target. Manual protocol settings remain available as an advanced workflow.

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

These files are root-only. If a RouteGate-owned installation is marked `installing`, re-running the same command for the same domain resumes with preserved installer secrets. After successful verification, transient secrets are deleted and state becomes `complete`.

When state is `complete`, running the installer again performs health/idempotency checks and exits without reinstalling the platform or rotating credentials.

For supported post-install certificate, service, and VPN config recovery, use:

```bash
sudo routegate-recovery status
```

See [RouteGate Recovery Tool](../operations/recovery-tool.md) for the fixed
operation allow-list and rollback behavior.

The installer refuses to overwrite partial RouteGate files that do not have valid RouteGate ownership state.

## Release bundles

The v0.1.0 release workflow publishes:

```text
routegate-v0.1.0-linux-amd64.tar.gz
routegate-v0.1.0-linux-arm64.tar.gz
SHA256SUMS
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

The release workflow verifies SHA-256 checksums and required bundle structure before publication.

Both amd64 and arm64 bundles are published to keep the native packaging contract multi-architecture. **The v0.1.0 Clean VPS installation support boundary is Ubuntu 24.04 LTS amd64**, because that is the architecture that completed the production-like clean-host E2E acceptance.

## Security boundaries

- Manager listens only on loopback and is exposed through nginx/HTTPS.
- PostgreSQL is local-only.
- Release checksum verification is mandatory.
- Archive traversal and archive links are rejected.
- Secrets are not passed as normal process command-line arguments.
- Sensitive installer files use mode `0600`.
- Existing SSH settings are not weakened.
- When UFW is already active, the installer adds only nginx HTTP/HTTPS access and preserves SSH rules.
- DNS mismatch or TLS failure stops the installation rather than presenting plaintext deployment as success.
- Existing unrelated web/database services trigger safe conflict handling and are never modified automatically.
- Agent infrastructure mutations are allow-listed rather than arbitrary shell execution exposed through Manager APIs.

## Explicit non-goals for v0.1.0

- operating-system installation or SSH hardening;
- Docker/Kubernetes/HA production deployment;
- external PostgreSQL deployment;
- automatic VPN Core installation during the platform installer;
- managed/automatic RouteGate update and rollback orchestration;
- appliance image work;
- additional VPN Cores or protocols;
- RG-101C client compatibility auto-tuning;
- destructive uninstall or cleanup of unrelated host software.

## Validated MVP result

The final clean-host acceptance on `us.routegate.org` validated:

- Clean Ubuntu 24.04 LTS → RouteGate installer;
- PostgreSQL + Manager + Admin UI + nginx/HTTPS + Agent startup;
- secure `/setup` activation;
- automatic local Agent registration;
- Guided Workflow;
- sing-box installation through RouteGate;
- VLESS / Reality configuration;
- first VPN account and Config Deploy;
- persistent client profile, QR, and VLESS link;
- real client connectivity with V2Box and V2RayTun;
- persistent `fingerprint=firefox` profile behavior;
- host reboot and automatic service recovery;
- working VPN connectivity after reboot.
