<p align="center">
  <a href="https://routegate.org">
    <img src="brand/logos/svg/logo-primary-horizontal-dark.svg" alt="RouteGate" width="620">
  </a>
</p>

<p align="center">
  <strong>Open-source, self-hosted Linux VPN Management Platform.</strong>
</p>

<p align="center">
  Operate VPN nodes, accounts, protocols, routing, configuration deployment, client access, delivery, and operational state from one control plane.
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

RouteGate is an independent open-source control plane for operating self-hosted Linux VPN infrastructure.

```text
RouteGate Manager → RouteGate Agent → managed VPN runtime
```

The boundary is intentionally simple:

- **Manager** owns desired state, the Admin UI and API, PostgreSQL-backed data, account and routing policy, configuration lifecycle, audit history, delivery, and aggregated operational state.
- **Agent** runs on managed Linux nodes and performs authenticated, allow-listed host operations such as installation, validation, apply, service control, diagnostics, health checks, and rollback.
- **VPN runtimes** remain replaceable behind explicit protocol/core adapters. RouteGate manages only combinations for which it can own the complete lifecycle safely.

Remote Agents do **not** connect directly to PostgreSQL, and RouteGate does not expose an arbitrary remote-shell channel from Manager to nodes.

RouteGate is designed as an infrastructure product: administrators work with nodes, VPN accounts, protocols, routing policies, deployment state, and next actions instead of assembling those workflows from scripts and unrelated service-specific panels.

## Project state

**RouteGate v0.1.0 is the first public MVP release. `main` has moved significantly beyond that release baseline.**

The canonical clean-host path remains the compatibility and production-like validation baseline:

```text
Clean Ubuntu 24.04 LTS host
→ one-command RouteGate installation
→ PostgreSQL + Manager + Admin UI + nginx/HTTPS + local Agent
→ single-use /setup activation
→ Guided Workflow / Next Action First
→ managed VPN runtime installation
→ VLESS / Reality configuration
→ VPN account creation
→ render → validate → apply → health check → rollback boundary
→ persistent client access / QR / subscription
→ working VPN connection
→ reboot
→ service recovery
→ working VPN connection after reboot
```

The production-like validation environment is operated at `us.routegate.org` using native systemd services, PostgreSQL, nginx, and HTTPS.

Development on `main` now also includes the RG-114 platform-expansion architecture: deployment roles, remote VPN Node onboarding, explicit VPN Core adapter boundaries, multiple managed protocol families, multi-protocol account profiles, multi-runtime apply, node groups, and explainable Automatic Selection.

> **Release note:** features described as available on `main` may not exist in the published v0.1.0 release bundle. The v0.1.0 release notes remain the source of truth for that specific release.

### Validation and implementation status

| Area | Current status |
|---|---|
| Clean Ubuntu 24.04 LTS All-in-One / Hybrid deployment | Production-like validated baseline |
| VLESS / Reality | Production-like validated public VPN path |
| Remote VPN Node bootstrap and inventory | Implemented on `main` |
| Native WireGuard | Managed adapter implemented on `main` |
| Hysteria2 | Managed adapter implemented on `main` |
| Shadowsocks 2022 | Managed adapter implemented on `main` |
| MTProto / FakeTLS | Managed adapter implemented on `main` |
| Multiple protocols on one VPN node | Manager/Agent multi-runtime lifecycle implemented on `main` |
| Node groups and explainable Automatic Selection | Preview/apply workflow implemented on `main`; unattended failover is intentionally not enabled |

Implementation support and production-like validation are deliberately treated as different claims. A detected binary or upstream feature is not considered a RouteGate-managed capability until its settings, credentials, render, validation, apply, rollback, health, client-access, and redaction lifecycle are controlled by RouteGate.

## Deployment model

RouteGate uses explicit node roles rather than a permanent root-server hierarchy.

| Role | Intended components | Agent required | VPN configuration deploy |
|---|---|---|---|
| **Management Node** | Manager, PostgreSQL, Admin UI, nginx/HTTPS | No | No |
| **VPN Node** | Agent and managed VPN runtime(s) | Yes | Yes |
| **Hybrid Node** | Management plane + VPN plane | Yes for VPN plane | Yes |

The familiar single-server installation is a **Hybrid Node**. Additional VPN Nodes register with Manager using a short-lived, single-use bootstrap token and then report heartbeat, capabilities, versions, runtime state, telemetry, and task results through the Manager API.

Manager evaluates three things separately before offering an action:

1. **role** — what the node is intended to host;
2. **capability** — what the registered Agent can manage;
3. **runtime state** — what is installed and healthy now.

See [Platform Expansion Architecture](docs/architecture/platform-expansion.md) and the [Remote VPN Node guide](docs/deployment/remote-vpn-node.md).

## Managed protocol model

RouteGate separates **VPN Core**, **Protocol**, **Transport**, and **Security** instead of treating every VPN feature as one interchangeable mode.

| Protocol | Managed runtime | Transport / security model | Status on `main` |
|---|---|---|---|
| **VLESS / Reality** | sing-box | TCP + Reality / XTLS Vision | Managed and production-like validated |
| **WireGuard** | native `wireguard-tools` / `wg-quick` | UDP + WireGuard cryptography | Managed |
| **Hysteria2** | Hysteria | QUIC + TLS / ACME | Managed |
| **Shadowsocks 2022** | sing-box | TCP + AEAD-2022 | Managed |
| **MTProto / FakeTLS** | mtg | TCP + FakeTLS | Managed |

A VPN client profile may use `Auto` to inherit the node default or explicitly select one managed protocol. Manager resolves the effective protocol for every active account before rendering configuration, so one VPN Node can require multiple managed runtimes at the same time.

VLESS and Shadowsocks are composed into the shared sing-box runtime. Native WireGuard, Hysteria2, and MTProto retain separate runtime/service ownership. Agent validates all required runtimes before promotion and rolls already-applied runtimes back if a later step in the same node deployment fails.

Cross-node atomic deployment remains a separate problem and is not implied by multi-runtime support on one node.

## Install RouteGate

The current public clean-host installation contract is **Ubuntu 24.04 LTS amd64** with native systemd services, local PostgreSQL, nginx, and Let's Encrypt TLS.

Before installation:

- create a DNS `A` record for the RouteGate hostname pointing to the host;
- make TCP ports 80 and 443 reachable;
- connect as `root` or a user with working `sudo`.

Run:

```bash
curl -fsSL https://raw.githubusercontent.com/ikaevus/RouteGate/main/install.sh \
  | sudo bash
```

The installer downloads a published RouteGate release bundle and verifies it against `SHA256SUMS`. To install v0.1.0 explicitly, use `--version v0.1.0` as documented in the [Clean VPS Installer guide](docs/deployment/clean-vps-installer.md).

After installation, open the single-use `/setup` link printed by the installer, choose the administrator password, and continue through the guided Dashboard workflow.

On versions that include remote-node onboarding, additional VPN Nodes are attached from Manager: create a VPN Node, choose **Connect server**, and run the generated Agent bootstrap command on the target Ubuntu host.

In the canonical Hybrid layout, nginx/HTTPS owns TCP `443`; the recommended VLESS / Reality listener uses TCP `8443` to avoid the HTTPS listener conflict.

## Product principles

- **Self-hosted by design** — infrastructure and data remain under the operator's control.
- **Open source** — security-sensitive and operationally critical code remains inspectable and self-buildable.
- **Guided Workflow / Next Action First** — major screens communicate current state and expose the most logical next action.
- **Product model over implementation model** — administrators manage infrastructure concepts rather than internal service plumbing.
- **Operational safety** — deployment follows render, validation, apply, health-check, and rollback boundaries.
- **Explicit capability contracts** — detecting a binary is not the same as supporting its lifecycle.
- **Control-plane isolation** — remote Agents have no direct database access and no general remote-shell authority.
- **Provider-independent integrations** — external communication belongs to the Delivery domain rather than being scattered through product logic.
- **No artificial product limits** — RouteGate is one open-source self-hosted product, not a restricted edition around closed critical features.
- **Protected brand** — source code is AGPLv3-or-later; the RouteGate name and official branding are governed separately.

## Current capabilities on `main`

### Control plane and node management

- PostgreSQL-backed Manager state and migrations;
- React/TypeScript Admin UI with English-first product language and Russian localization;
- secure single-use administrator activation at `/setup`;
- Management, VPN, and Hybrid deployment roles;
- local Hybrid-node registration and remote VPN Node bootstrap;
- short-lived registration tokens and persistent Agent credentials;
- heartbeat, compatibility, capability, version, runtime, and telemetry inventory;
- state-aware Dashboard and operational next-action guidance;
- allow-listed Agent infrastructure operations rather than arbitrary command execution.

### VPN protocols and configuration lifecycle

- explicit Manager/Agent adapter contracts;
- VLESS / Reality, WireGuard, Hysteria2, Shadowsocks 2022, and MTProto / FakeTLS managed protocol families;
- per-node default protocol plus per-client-profile `Auto` or explicit protocol preference;
- multi-runtime rendering and apply on one VPN node;
- render, validate, apply, health-check, rollback, and configuration history boundaries;
- server/account credential lifecycle and protected client-access rendering;
- certificate observation and recovery tooling for the Manager HTTPS path;
- protocol-specific certificate ownership where required.

### VPN accounts and client access

- scalable VPN-account lifecycle, search, filtering, and management;
- persistent client profiles;
- account-level protocol selection;
- QR codes, share/client representations, and subscription access;
- User Portal and self-service foundations;
- traffic collection, visibility, limits, and enforcement foundations.

### Routing and node selection

- routing profiles with Direct, VPN, and Block actions;
- account-level routing-profile overrides with explicit inheritance;
- node groups with priority/weight membership;
- candidate health derived from role, Agent state, heartbeat freshness, protocol capability, runtime state, and load;
- explainable Automatic Selection preview/apply with cooldown and optional degraded fallback;
- protocol-aware candidate evaluation for accounts with an explicit protocol override;
- explicit deployment-required result after moving an account between nodes.

Automatic Selection is intentionally operator-driven today. RouteGate does **not** silently move accounts in the background when health changes because safe unattended failover requires coordinated deployment across the previous and selected nodes.

### Operations and integrations

- runtime Dashboard data and operational visibility;
- audit-oriented state transitions;
- recovery/status tooling with fixed allow-listed actions;
- Delivery domain for durable external communication requests, templates, retries, lifecycle, history, and provider adapters;
- Email/SMTP, Telegram Bot API, and WhatsApp Business Cloud API adapter boundaries in the integration architecture.

## Architecture

```text
Administrator / User
        │
        ▼
RouteGate Admin UI / User Portal
        │
        ▼
RouteGate Manager ───────────── PostgreSQL
        │
        ├── Nodes / Accounts / Protocols / Routing
        ├── Config lifecycle / Traffic / Delivery / Audit
        │
        └── authenticated Agent tasks + aggregated telemetry
                 │
         ┌───────┴────────┐
         ▼                ▼
RouteGate Agent      RouteGate Agent
    VPN Node A           VPN Node B
         │                   │
         ▼                   ▼
 managed adapter(s)     managed adapter(s)
         │                   │
         ├─ sing-box         ├─ sing-box
         ├─ WireGuard        ├─ WireGuard
         ├─ Hysteria         ├─ Hysteria
         └─ mtg              └─ mtg
                 │
                 ▼
              VPN clients
```

The Manager follows a **modular monolith** architecture. Domain boundaries are kept explicit, but components should become separately deployed services only when real scaling or operational pressure justifies that complexity.

### Delivery / external integrations

The integration boundary is:

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

Delivery owns durable requests, template selection, idempotency, lifecycle/status, bounded retries, restart-safe recovery, and audit/history. Provider adapters contain external-system communication logic rather than leaking provider details into core product domains.

## Technology stack

| Area | Technology |
|---|---|
| Manager | Go |
| Agent | Go |
| Admin UI / User Portal | React, TypeScript, Vite |
| Database | PostgreSQL |
| Managed VPN runtimes on `main` | sing-box, native WireGuard, Hysteria, mtg |
| Production-like validation baseline | VLESS / Reality on Ubuntu 24.04 LTS |
| Clean-host deployment | native systemd services, PostgreSQL, nginx, Let's Encrypt TLS |
| Development | Docker Compose |
| Public website | React/Vite static site on GitHub Pages |

## Repository layout

```text
agent/       RouteGate Agent
backend/     RouteGate Manager API and database migrations
brand/       Official brand assets and design tokens
deploy/      Development and deployment resources
docs/        Architecture, API, operations, features, decisions, and release notes
frontend/    RouteGate Admin UI and User Portal
scripts/     Development, packaging, and operational helpers
website/     Public routegate.org website
```

## Development quick start

Docker Compose is the contributor/development path; it is not the supported public clean-host installation contract.

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

Start here:

- [Documentation index](docs/)
- [Platform Expansion Architecture](docs/architecture/platform-expansion.md)
- [Clean VPS Installer](docs/deployment/clean-vps-installer.md)
- [Remote VPN Node](docs/deployment/remote-vpn-node.md)
- [MVP Deployment Baseline](docs/deployment/mvp-deployment-baseline.md)
- [API documentation](docs/api/)
- [Feature documentation](docs/features/)
- [Architecture decisions](docs/decisions/)
- [Release documentation](docs/release/)
- [v0.1.0 release notes](docs/release/v0.1.0.md)
- [Official website](https://routegate.org)

Documentation is English-first, with Russian localization developed alongside the product.

## Current boundaries

RouteGate is not:

- an OPNsense plugin;
- a firewall distribution;
- a consumer VPN service;
- a closed-source VPN panel;
- a restriction-bypass marketing product.

The current project does not promise Kubernetes/HA orchestration, an appliance operating system, official RouteGate mobile clients, or identical production-like validation across every managed protocol and topology.

Automatic Selection is currently an explainable preview/apply workflow, not an unattended health-triggered failover service. Cross-node atomic deployment, session draining, and live connection migration remain separate future problems.

## Security

Please do not report security vulnerabilities through public issues. Read [SECURITY.md](SECURITY.md) for the supported reporting process and disclosure expectations.

## Contributing

Contributions, testing, documentation improvements, and carefully scoped proposals are welcome. Before opening a pull request, read [CONTRIBUTING.md](CONTRIBUTING.md), [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md), and [SECURITY.md](SECURITY.md).

## License and trademarks

RouteGate source code is licensed under the [GNU Affero General Public License v3.0 or later](LICENSE).

The RouteGate name, logo, and official-build designation are not granted by the software license. See [TRADEMARKS.md](TRADEMARKS.md) and [NOTICE](NOTICE) for the project boundary.

**Open code. Protected brand. Trusted official builds.**
