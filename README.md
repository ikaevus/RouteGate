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

## Project principles

- **Self-hosted by design** — infrastructure and data stay under the operator's control.
- **Open source** — source code is available for inspection, modification, and self-builds.
- **Guided workflows** — the interface should always expose the next logical operational action.
- **Product model over implementation model** — users manage servers and access, not internal service plumbing.
- **Security-sensitive code remains open** — no hidden critical components or artificial product limits.
- **Protected brand** — the code is open under AGPLv3-or-later; the RouteGate name and official branding are governed separately.

## Current capabilities

RouteGate is under active development. The repository already includes foundations and working flows for:

- Linux server registration and guided Agent onboarding;
- Manager-to-Agent task delivery and heartbeat reporting;
- VPN account lifecycle and client subscription delivery;
- VLESS / Reality credentials and configuration rendering;
- configuration render, validation, deployment, health checking, and rollback;
- routing profiles with Direct, VPN, and Block actions;
- QR and client configuration presentation;
- user portal and portal access hardening;
- traffic collection, visibility, limits, and enforcement foundations;
- VPN Core installation and service controls;
- version, schema, protocol, and compatibility reporting;
- PostgreSQL-backed persistence and migrations;
- English-first UI with Russian localization.

> RouteGate is not yet presented as a finished production release. Interfaces, installation paths, and compatibility boundaries may still change while the project approaches its first supported release.

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
     │ validate / stage / apply / restart / health check / rollback
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
| Development | Docker Compose |
| Production direction | Native systemd services, PostgreSQL, reverse proxy, TLS |
| Public website | Static React/Vite site deployed with GitHub Pages |

## Repository layout

```text
agent/       RouteGate Agent
backend/     RouteGate Manager API and database migrations
brand/       Official brand assets and design tokens
deploy/      Development and deployment resources
docs/        Architecture, API, operations, features, and release notes
frontend/    RouteGate Admin UI
scripts/     Development and operational helpers
website/     Static public website for routegate.org
```

## Development quick start

### Requirements

- Git
- Go
- Node.js and npm
- Docker with Docker Compose
- GNU Make

### Start the development stack

```bash
git clone https://github.com/ikaevus/RouteGate.git
cd RouteGate
make dev
```

Open the Admin UI:

```text
http://127.0.0.1:5173
```

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

Development defaults are documented in [`.env.example`](.env.example). Never use the example passwords or tokens for shared or production-like deployments.

## Documentation

Project documentation is organized by topic under [`docs/`](docs/):

- [Architecture](docs/architecture/)
- [API documentation](docs/api/)
- [Deployment documentation](docs/deployment/)
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
