# RouteGate MVP v0.1 — Final Acceptance Pass

## Status

**Accepted — production-like Clean VPS E2E completed**

This document supersedes the earlier development/Codespaces MVP acceptance result as the canonical v0.1 release acceptance record.

## Final baseline

Final integrated runtime commit before release-closeout documentation:

```text
9f2cacc3995ea1d921afff8f7ba0ecc3b3fa9fbb
```

Final integration chain:

- PR #108 — RG-89C — VPN Core Installation E2E
- PR #110 — RG-100 — Clean VPS Installer MVP
- PR #112 — RG-101 — Secure First Access
- PR #114 — RG-102 — Guided First-Run Setup

Post-merge RouteGate CI #451 passed on the canonical integrated commit.

## Acceptance environment

```text
Host: us.routegate.org
Operating system: Ubuntu 24.04 LTS
Architecture: amd64
Deployment type: clean production-like VPS
VPN Core: sing-box
```

The host was used as a disposable validation environment for the supported All-in-One RouteGate lifecycle.

## Accepted lifecycle

The final MVP acceptance proved the complete supported path:

```text
Clean VPS
→ RouteGate installer
→ PostgreSQL + Manager + frontend + nginx/HTTPS + Agent
→ secure /setup activation
→ automatic local Agent registration
→ Guided Workflow
→ sing-box installation through RouteGate
→ VLESS / Reality configuration
→ first VPN account
→ Config Deploy
→ persistent client profile / QR / VLESS link
→ real desktop/mobile VPN connectivity
→ reboot
→ automatic service recovery
→ VPN connectivity after reboot
```

## Platform installation

Accepted behavior:

- [x] installer runs on a clean Ubuntu 24.04 LTS amd64 VPS;
- [x] host/DNS/conflict preflight runs before unsafe mutations;
- [x] release bundle checksum verification is required;
- [x] PostgreSQL is installed/configured locally;
- [x] Manager is installed as a native systemd service;
- [x] production frontend is installed and served through nginx;
- [x] HTTPS is configured with Let's Encrypt;
- [x] RouteGate Agent is installed as a native systemd service;
- [x] local All-in-One Server is created automatically;
- [x] local Agent registration is completed automatically;
- [x] installer state/logging/retry boundaries work for the supported path.

## Secure first access

Accepted behavior:

- [x] installer creates a high-entropy one-time setup token;
- [x] first access uses `/setup#token=...` rather than a plaintext default login flow;
- [x] the administrator chooses the operational password;
- [x] setup token is consumed once;
- [x] bootstrap sessions are revoked;
- [x] the administrator is signed in automatically after activation;
- [x] bootstrap environment values are removed from Manager configuration;
- [x] root-only recovery information remains available for the server owner until intentionally removed.

## Guided Workflow

Accepted behavior:

- [x] a new administrator is not left on a dead-end Dashboard;
- [x] the Dashboard derives progress from actual system state;
- [x] the current incomplete step exposes the next logical action;
- [x] completed prerequisites are recognized without a separate onboarding-state subsystem;
- [x] the completed guide collapses to a ready state;
- [x] the guide can recover if required infrastructure later becomes incomplete.

## VPN Core lifecycle

Accepted behavior:

- [x] sing-box absence is reported as `not_installed`;
- [x] sing-box is installed through an explicit allow-listed Agent operation;
- [x] installation does not expose arbitrary shell/package execution through Manager APIs;
- [x] installed and VPN-ready/running-with-valid-config remain distinct states;
- [x] RouteGate does not require a premature sing-box start before a valid config exists;
- [x] Config Deploy owns the first real start/restart after configuration is rendered and validated.

## VLESS / Reality and Config Deploy

Accepted All-in-One behavior:

- [x] nginx/RouteGate HTTPS owns TCP 443;
- [x] recommended VLESS / Reality uses TCP 8443 to avoid the port conflict;
- [x] TCP transport and `xtls-rprx-vision` are configured through the recommended setup path;
- [x] Reality X25519 keys and Short ID are generated;
- [x] first VPN account can be created;
- [x] Config Deploy performs render → validate → apply → restart/start → health check;
- [x] applied configuration metadata reaches the correct applied state;
- [x] VPN Core readiness is confirmed after deployment.

## Client access

Validated clients:

- [x] V2Box
- [x] V2RayTun

Accepted behavior:

- [x] persistent client profiles survive page reload/re-login;
- [x] QR code remains available from the saved profile;
- [x] VLESS link remains available from the saved profile;
- [x] `fingerprint=firefox` was validated as the working persistent Reality client fingerprint in the final path;
- [x] real desktop/mobile connectivity was demonstrated.

Advanced compatibility auto-tuning and richer profile controls remain tracked separately as RG-101C / #113 and are Post-MVP / Planned.

## Reboot persistence

Accepted behavior after host reboot:

- [x] PostgreSQL recovers automatically;
- [x] nginx recovers automatically;
- [x] RouteGate Manager recovers automatically;
- [x] RouteGate Agent recovers automatically;
- [x] configured sing-box runtime recovers automatically;
- [x] Agent connectivity returns;
- [x] client VPN connectivity works after reboot.

## Release-support boundary

The v0.1.0 public MVP support claim is intentionally narrower than the full source tree:

Supported live installer acceptance:

```text
Ubuntu 24.04 LTS + amd64 + single VPS + native systemd
```

Release packaging also builds arm64, but the first release does not claim the same live Clean VPS acceptance level for arm64.

## Non-goals confirmed

The final acceptance does not require:

- RG-101C implementation;
- additional VPN protocols;
- additional VPN Cores;
- appliance work;
- Kubernetes or HA;
- managed automatic updates;
- official mobile clients;
- opportunistic redesign/refactoring.

## Historical development acceptance

An earlier July 2026 acceptance pass validated the feature foundations in the development/Codespaces environment. That result was useful during construction, but it is no longer the final release gate because the production-like Clean VPS lifecycle has now been validated end to end.

## Result

```text
RouteGate MVP v0.1 acceptance: PASS
Release blocker from supported product path: NONE KNOWN
Next action: release closeout → v0.1.0 tag → GitHub Release → asset verification
```

The public release may be declared complete only after the final release-closeout documentation commit is green on `main` and the tag-triggered release workflow publishes and verifies the official artifacts.
