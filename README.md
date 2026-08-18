<p align="center">
  <a href="https://routegate.org">
    <img src="brand/logos/svg/logo-primary-horizontal-dark.svg" alt="RouteGate" width="620">
  </a>
</p>

<p align="center">
  <strong>Open-source, self-hosted Linux VPN Management Platform.</strong>
</p>

<p align="center">
  Manage VPN servers, accounts, routing profiles, configuration deployment, client access, delivery, and operational visibility from one web interface.
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

RouteGate is an independent open-source platform for operating self-hosted Linux VPN infrastructure.

```text
RouteGate Manager → RouteGate Agent → VPN Core
```

- **Manager** provides the Admin UI, API, PostgreSQL-backed state, configuration lifecycle, operational workflows, delivery, and observability.
- **Agent** runs on managed Linux servers and performs authenticated, allow-listed infrastructure operations.
- **VPN Core** provides the VPN runtime. The current supported core is **sing-box**, with **VLESS / Reality** as the validated public path.

RouteGate is designed for administrators who want a coherent infrastructure product instead of a collection of scripts, disconnected panels, and manual VPN configuration steps.

## Current project state

**RouteGate v0.1.0 is the first public MVP release. Development on `main` continues beyond the release baseline.**

The canonical clean-host flow has been validated end to end on Ubuntu 24.04 LTS:

```text
Clean Ubuntu VPS
→ one-command RouteGate installer
→ PostgreSQL + Manager + Admin UI + nginx/HTTPS + local Agent
→ secure one-time /setup activation
→ Guided Workflow / Next Action First
→ install sing-box through RouteGate
→ configure VLESS / Reality
→ create VPN account
→ render → validate → apply → health check → rollback boundary
→ persistent client profile / QR / VLESS link / subscription
→ working VPN connection
→ reboot
→ automatic service recovery
→ working VPN connection after reboot
```

The production-like validation environment is operated at `us.routegate.org` and uses the same native systemd/PostgreSQL/nginx model as the supported clean-host deployment.

Since the v0.1.0 release baseline, `main` has continued to evolve with scalable VPN-account management, real Dashboard runtime data, stronger operational visibility, and the Delivery / external-integration domain direction.

## Install RouteGate

The supported public installation contract is a single-node **Ubuntu 24.04 LTS amd64** host using native systemd services, local PostgreSQL, nginx, and Let's Encrypt TLS.

Before installation:

- create a DNS `A` record for the RouteGate hostname pointing to the VPS;
- make TCP ports 80 and 443 reachable;
- connect as `root` or a user with working `sudo`.

Run:

```bash
curl -fsSL https://raw.githubusercontent.com/ikaevus/RouteGate/main/install.sh \
  | sudo bash
```

The installer downloads the published RouteGate release bundle and verifies it against `SHA256SUMS`. To install v0.1.0 explicitly, use `--version v0.1.0` as documented in the [Clean VPS Installer guide](docs/deployment/clean-vps-installer.md).

After installation, open the single-use `/setup` link printed by the installer, choose the administrator password, and continue through the guided Dashboard workflow.

Additional VPN Nodes are attached from the Manager UI. Create a VPN Node, choose
**Connect server**, and copy the generated one-command Agent installer to the
target Ubuntu 24.04 host. See the [Remote VPN Node guide](docs/deployment/remote-vpn-node.md).

In the canonical All-in-One layout, nginx/HTTPS owns TCP `443`; the recommended VLESS / Reality listener uses TCP `8443`.

## Product principles

- **Self-hosted by design** — infrastructure and data remain under the operator's control.
- **Open source** — security-sensitive and operationally critical code stays inspectable and self-buildable.
- **Guided Workflow / Next Action First** — every major workflow should expose the current state and the most logical next action.
- **Product model over implementation model** — administrators manage infrastructure concepts, not internal service plumbing.
- **Operational safety** — configuration deployment follows render, validation, apply, health-check, and rollback boundaries.
- **Provider-independent integrations** — external communication is modeled through a Delivery domain rather than scattered provider-specific logic.
- **No artificial product limits** — RouteGate is one open-source self-hosted product, not a crippled community edition around closed critical features.
- **Protected brand** — source code is AGPLv3-or-later; the RouteGate name and official branding are governed separately.

## Current capabilities

RouteGate currently includes working foundations and end-to-end flows for:

- clean Ubuntu 24.04 LTS VPS installation;
- PostgreSQL, Manager, Admin UI, nginx/HTTPS, and local Agent deployment;
- secure single-use administrator activation at `/setup`;
- automatic local All-in-One Server and Agent registration;
- state-aware Dashboard and Guided Workflow;
- real runtime Dashboard data rather than static operational placeholders;
- VPN Core detection and allow-listed sing-box installation/service management;
- VLESS / Reality settings and validated first-run configuration;
- scalable VPN-account lifecycle, search, and management;
- persistent client profiles, QR codes, VLESS links, and subscriptions;
- configuration render, validation, deployment, health checking, and rollback foundations;
- routing profiles with Direct, VPN, and Block actions;
- user portal and self-service access foundations;
- traffic collection, visibility, limits, and enforcement foundations;
- version, schema, protocol, and compatibility reporting;
- PostgreSQL-backed persistence and migrations;
- English-first UI with Russian localization;
- Delivery architecture for provider-independent external communication, with Email/SMTP, Telegram, and WhatsApp adapter boundaries defined for integration work.

## Architecture

```text
Administrator
     │
     ▼
RouteGate Admin UI
     │
     ▼
RouteGate Manager ───────── PostgreSQL
     │
     ├── Servers / Accounts / Routing / Traffic / Delivery
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

The Manager follows a **modular monolith** architecture. Domain boundaries are kept explicit, but components are extracted into separately deployed services only when demonstrated scaling or operational pressure justifies it.

### Delivery / external integrations

The accepted integration direction is:

```text
RouteGate Manager
      │
      ▼
Delivery Service
      │
      ▼
Provider Adapter
      │
      ├── SMTP → Email
      ├── Telegram Bot API → Telegram
      └── WhatsApp Business Cloud API → WhatsApp
```

Delivery owns durable requests, template selection, idempotency, lifecycle/status, bounded retries, restart-safe recovery, and audit/history. Provider adapters contain only external-system communication logic.

## Technology stack

| Area | Technology |
|---|---|
| Manager | Go |
| Agent | Go |
| Admin UI | React, TypeScript, Vite |
| Database | PostgreSQL |
| VPN Core | sing-box |
| Current validated VPN path | VLESS / Reality |
| Supported public deployment | Ubuntu 24.04 LTS, native systemd services, PostgreSQL, nginx, Let's Encrypt TLS |
| Development | Docker Compose |
| Public website | React/Vite static site on GitHub Pages |

## Repository layout

```text
agent/       RouteGate Agent
backend/     RouteGate Manager API and database migrations
brand/       Official brand assets and design tokens
deploy/      Development and deployment resources
docs/        Architecture, API, operations, features, and release notes
frontend/    RouteGate Admin UI
scripts/     Development, packaging, and operational helpers
website/     Public routegate.org website
```

## Development quick start

Docker Compose is the contributor/development path; it is not the supported public VPS installation contract.

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

Run project checks:

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

Documentation is English-first, with Russian localization developed alongside the product.

## Current boundaries

RouteGate is not:

- an OPNsense plugin;
- a firewall distribution;
- a consumer VPN service;
- a closed VPN panel;
- a Hiddify / 3x-ui / Marzban clone;
- a restriction-bypass marketing product.

The current public release path does not promise Kubernetes/HA deployment, an appliance OS, official RouteGate mobile clients, or multiple production-validated VPN cores.

## Security

Please do not report security vulnerabilities through public issues. Read [SECURITY.md](SECURITY.md) for the supported reporting process and disclosure expectations.

## Contributing

Contributions, testing, documentation improvements, and carefully scoped proposals are welcome. Before opening a pull request, read [CONTRIBUTING.md](CONTRIBUTING.md), [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md), and [SECURITY.md](SECURITY.md).

## License and trademarks

RouteGate source code is licensed under the [GNU Affero General Public License v3.0 or later](LICENSE).

The RouteGate name, logo, and official-build designation are not granted by the software license. See [TRADEMARKS.md](TRADEMARKS.md) and [NOTICE](NOTICE) for the project boundary.

**Open code. Protected brand. Trusted official builds.**
