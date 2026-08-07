<p align="center">
  <a href="https://routegate.org">
    <img src="brand/logos/svg/logo-primary-horizontal-dark.svg" alt="RouteGate" width="620">
  </a>
</p>

<p align="center">
  <strong>Open-source, self-hosted Linux VPN Management Platform.</strong>
</p>

<p align="center">
  Manage VPN servers, accounts, routing profiles, configuration delivery, client access, and traffic visibility from one web interface.
</p>

<p align="center">
  <a href="https://routegate.org">Website</a> ·
  <a href="docs/">Documentation</a> ·
  <a href="https://routegate.org/#roadmap">Roadmap</a> ·
  <a href="SECURITY.md">Security</a> ·
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

<p align="center">
  <img alt="License: AGPLv3-or-later" src="https://img.shields.io/badge/license-AGPLv3--or--later-2563EB">
  <img alt="Backend: Go" src="https://img.shields.io/badge/backend-Go-07111F">
  <img alt="Frontend: React and TypeScript" src="https://img.shields.io/badge/frontend-React%20%2B%20TypeScript-07111F">
  <img alt="Database: PostgreSQL" src="https://img.shields.io/badge/database-PostgreSQL-07111F">
</p>

---

## What is RouteGate?

RouteGate is an independent, open-source platform for operating self-hosted Linux VPN infrastructure.

Its product model is intentionally simple:

```text
RouteGate Manager → RouteGate Agent → VPN Core
```

- **Manager** provides the web interface, API, persistence, configuration lifecycle, and administrative workflows.
- **Agent** runs on managed Linux servers and performs allow-listed infrastructure operations.
- **VPN Core** handles the actual VPN runtime. RouteGate currently integrates with **sing-box**.

RouteGate is designed for administrators who want to operate their own infrastructure without relying on scattered scripts or a closed control panel.

## RouteGate v0.1.0

**v0.1.0 is the first public RouteGate MVP release.**

The supported MVP path has been validated end to end on a clean Ubuntu 24.04 LTS VPS:

```text
Clean VPS
→ RouteGate installer
→ PostgreSQL + Manager + Admin UI + nginx/HTTPS + local Agent
→ secure /setup activation
→ Guided Workflow
→ install sing-box through RouteGate
→ configure VLESS / Reality
→ create the first VPN account
→ Config Deploy
→ persistent client profile / QR / VLESS link
→ working VPN connection
→ reboot
→ automatic service recovery
→ working VPN connection after reboot
```

The validated third-party clients are **V2Box** and **V2RayTun**. The persistent client-profile path with `fingerprint=firefox` was validated during the final clean-host acceptance.

## Install the supported MVP

The v0.1.0 public installation contract is a single-node **Ubuntu 24.04 LTS amd64** host using native systemd services, local PostgreSQL, nginx, and Let's Encrypt TLS.

Before installation:

- create a DNS `A` record for the RouteGate hostname pointing to the VPS;
- make TCP ports 80 and 443 reachable;
- connect as `root` or a user with working `sudo`.

Run:

```bash
curl -fsSL https://raw.githubusercontent.com/ikaevus/RouteGate/main/install.sh \
  | sudo bash
```

The installer downloads the latest published RouteGate release bundle and verifies it against `SHA256SUMS`. To install this release explicitly, add `--version v0.1.0` as documented in the [Clean VPS Installer guide](docs/deployment/clean-vps-installer.md).

After installation, use the single-use `/setup` link printed by the installer to choose the administrator password. RouteGate then signs the administrator in and guides the remaining VPN setup from the Dashboard.

In the canonical All-in-One layout, nginx/HTTPS owns TCP `443` and the recommended VLESS / Reality listener uses TCP `8443`.

## Project principles

- **Self-hosted by design** — infrastructure and data stay under the operator's control.
- **Open source** — source code is available for inspection, modification, and self-builds.
- **Guided Workflow / Next Action First** — the interface should always expose the next logical operational action.
- **Product model over implementation model** — users manage servers and access, not internal service plumbing.
- **Security-sensitive code remains open** — no hidden critical components or artificial product limits.
- **Protected brand** — the code is open under AGPLv3-or-later; the RouteGate name and official branding are governed separately.

## MVP capabilities

v0.1.0 includes working foundations and end-to-end flows for:

- Clean VPS installation with PostgreSQL, Manager, Admin UI, nginx/HTTPS, and local Agent;
- secure single-use first administrator activation at `/setup`;
- automatic local All-in-One Server and Agent registration;
- state-aware Guided Workflow on the Dashboard;
- VPN Core detection, allow-listed sing-box installation, and service management;
- VLESS / Reality protocol settings with recommended first-run configuration;
- VPN account lifecycle and persistent client profiles;
- QR code and VLESS-link client delivery;
- configuration render, validation, deployment, health checking, and rollback foundations;
- routing profiles with Direct, VPN, and Block actions;
- user portal and portal access hardening;
- traffic collection, visibility, limits, and enforcement foundations;
- version, schema, protocol, and compatibility reporting;
- PostgreSQL-backed persistence and migrations;
- English-first UI with Russian localization.

## MVP boundaries

v0.1.0 intentionally does not include:

- additional VPN Cores or protocols beyond the current sing-box + VLESS / Reality path;
- an appliance image or appliance operating system;
- Kubernetes or HA deployment;
- a managed/automatic RouteGate update system;
- official RouteGate mobile clients;
- RG-101C compatibility auto-tuning and advanced client-profile controls.

The release workflow publishes both amd64 and arm64 native bundles. The **live Clean VPS support contract for v0.1.0 is amd64**; arm64 packaging is published but has not yet received the same clean-host acceptance boundary.

## Architecture

```text
Administrator
     │
     ▼
RouteGate Admin UI
     │
     ▼
RouteGate Manager ───── PostgreSQL
     │
     │ authenticated task queue
     ▼
RouteGate Agent
     │
     │ install / render / validate / apply / restart / health check / rollback
     ▼
sing-box
     │
     ▼
VPN clients
```

The backend follows a **modular monolith** architecture. Components are extracted only when real operational pressure justifies it.

## Technology stack

| Area | Technology |
|---|---|
| Manager | Go |
| Agent | Go |
| Admin UI | React, TypeScript, Vite |
| Database | PostgreSQL |
| VPN Core | sing-box |
| Supported MVP deployment | Native systemd services, PostgreSQL, nginx, Let's Encrypt TLS |
| Development | Docker Compose |
| Public website | Static React/Vite site deployed with GitHub Pages |

## Repository layout

```text
agent/       RouteGate Agent
backend/     RouteGate Manager API and database migrations
brand/       Official brand assets and design tokens
deploy/      Development and deployment resources
docs/        Architecture, API, operations, features, and release notes
frontend/    RouteGate Admin UI
scripts/     Development, packaging, and operational helpers
website/     Static public website for routegate.org
```

## Development quick start

The Docker Compose stack remains the contributor/development path; it is not the supported public VPS installation path.

### Requirements

- Git
- Go
- Node.js and npm
- Docker with Docker Compose
- GNU Make

```bash
git clone https://github.com/ikaevus/RouteGate.git
cd RouteGate
make dev
```

Open the Admin UI at `http://127.0.0.1:5173`.

Run the full project checks:

```bash
make check
```

Useful commands:

```bash
make help
make dev
make down
make restart
make logs
make ps
make backend-test
make agent-test
make frontend-build
make frontend-i18n-check
```

Development defaults are documented in [`.env.example`](.env.example). Never use example passwords or tokens for shared or production-like deployments.

## Documentation

- [Clean VPS Installer](docs/deployment/clean-vps-installer.md)
- [MVP Deployment Baseline](docs/deployment/mvp-deployment-baseline.md)
- [Release Readiness Checklist](docs/deployment/release-readiness-checklist.md)
- [v0.1.0 release notes](docs/release/v0.1.0.md)
- [Architecture](docs/architecture/)
- [API documentation](docs/api/)
- [Feature documentation](docs/features/)
- [Release documentation](docs/release/)
- [Official website](https://routegate.org)

Documentation follows the same policy as the product: **English first, with Russian localization developed alongside it**.

## Security

Please do not report security vulnerabilities through public issues.

Read [SECURITY.md](SECURITY.md) for the supported reporting process and disclosure expectations.

## Contributing

Contributions, testing, documentation improvements, and carefully scoped proposals are welcome.

Before opening a pull request, read:

- [CONTRIBUTING.md](CONTRIBUTING.md)
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
- [SECURITY.md](SECURITY.md)

## License and trademarks

RouteGate source code is licensed under the [GNU Affero General Public License v3.0 or later](LICENSE).

The RouteGate name, logo, and official-build designation are not granted by the software license. See [TRADEMARKS.md](TRADEMARKS.md) and [NOTICE](NOTICE) for the project boundary.

**Open code. Protected brand. Trusted official builds.**
