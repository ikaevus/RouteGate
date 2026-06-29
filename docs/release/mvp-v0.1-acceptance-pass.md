# RG-90 — MVP v0.1 Release Candidate / Acceptance Pass

## Status

Planned / Next

## Context

RouteGate has completed the main MVP feature tracks:

- RG-40 — VPN Accounts / Client Access
- RG-50 — Config Deploy
- RG-60 — Routing Profiles / Split Tunnel
- RG-70 — Traffic Stats / Limits
- RG-80 — UI Kit / Design System MVP Polish
- RG-81–RG-84 — User Portal MVP Foundation
- RG-90 — MVP Deployment Baseline
- RG-91 — Security / Threat Model Baseline

The goal now is to verify RouteGate as one coherent MVP product, not as isolated merged features.

## Goal

Prepare and verify **RouteGate MVP v0.1 Release Candidate**.

The acceptance pass should prove that a clean RouteGate checkout can be started, configured, tested, and used through the core MVP workflow.

## How to use this checklist

Use a clean branch from `main` and record evidence while progressing through each section.

Recommended evidence per section:

- commands run;
- relevant API responses;
- screenshots where UI is involved;
- notes for failures or follow-up issues;
- final pass / fail / blocked status.

A checkbox means the item was explicitly verified during this acceptance pass.

## Scope

### 1. Clean startup / deployment baseline

- [ ] Start from clean `main`.
- [ ] Verify `.env.example` is complete enough for MVP startup.
- [ ] Verify Docker Compose stack starts successfully.
- [ ] Verify PostgreSQL becomes healthy.
- [ ] Verify Manager health endpoint.
- [ ] Verify Agent health endpoint.
- [ ] Verify frontend proxy health.
- [ ] Verify migrations on a clean database.
- [ ] Verify `make check` passes.

Suggested commands:

```bash
git switch main
git pull
git status
make check
docker compose -f deploy/docker-compose.dev.yml up --build -d
docker compose -f deploy/docker-compose.dev.yml ps
curl -i http://127.0.0.1:8080/api/admin/health
curl -i http://127.0.0.1:8080/api/agent/health
curl -i http://127.0.0.1:5173/api/admin/health
```

### 2. Admin bootstrap / Auth

- [ ] Verify bootstrap admin flow.
- [ ] Verify admin login.
- [ ] Verify protected Admin UI routes.
- [ ] Verify EN/RU language switcher.
- [ ] Verify Auth Shell visual state.
- [ ] Verify Admin Shell visual state.

### 3. Server / Agent flow

- [ ] Create server.
- [ ] Generate registration token.
- [ ] Register Agent with registration token.
- [ ] Verify token is consumed.
- [ ] Verify Agent receives persistent agent token.
- [ ] Verify Agent heartbeat.
- [ ] Verify server online/offline status.

### 4. VLESS / Reality / VPN account flow

- [ ] Create VPN account.
- [ ] Verify dedicated VLESS UUID exists.
- [ ] Configure VLESS / Reality protocol settings.
- [ ] Generate or rotate Reality keypair.
- [ ] Verify Reality private key is not exposed.
- [ ] Verify VPN account credentials view.
- [ ] Verify subscription token generation.
- [ ] Verify subscription URL / QR payload.
- [ ] Verify scannable QR UI.
- [ ] Verify public subscription response.
- [ ] Verify rendered sing-box client config.

### 5. User Portal flow

- [ ] Login as portal user.
- [ ] Open `/portal`.
- [ ] Verify portal dashboard.
- [ ] Verify profile list.
- [ ] Verify profile detail.
- [ ] Generate / refresh subscription from portal.
- [ ] Verify ownership boundaries.
- [ ] Verify QR and subscription metadata.
- [ ] Verify disabled / expired profile behavior where practical.

### 6. Config render / apply flow

- [ ] Render server config.
- [ ] Verify config version creation.
- [ ] Verify config hash.
- [ ] Verify apply job creation.
- [ ] Verify Agent task pickup.
- [ ] Verify staged config validation.
- [ ] Verify apply result reporting.
- [ ] Verify apply job status UI.
- [ ] Verify rollback/safety documentation is still accurate.

### 7. Routing Profiles / Split Tunnel

- [ ] Create routing profile.
- [ ] Add direct / vpn / block rules.
- [ ] Assign routing profile to server.
- [ ] Verify rendered server config includes routing profile metadata.
- [ ] Verify public subscription rendered sing-box config contains split-tunnel route rules:
  - [ ] direct → direct
  - [ ] vpn → routegate-out
  - [ ] block → block
- [ ] Verify default/final route behavior.

### 8. Traffic Stats / Limits

- [ ] Verify traffic limit UI on VPN account detail.
- [ ] Run documented dev traffic usage scenario.
- [ ] Verify Agent reports usage to Manager.
- [ ] Verify Manager stores traffic usage events.
- [ ] Verify Admin UI shows traffic usage summary.
- [ ] Configure monthly hard limit.
- [ ] Verify enforcement status becomes `over_limit`.
- [ ] Verify over-limit account is excluded from rendered config.

### 9. UI / Brand / i18n sanity pass

- [ ] Verify official RouteGate logo is used from `/brand`.
- [ ] Verify favicon.
- [ ] Verify Admin UI, Auth Shell, and User Portal use correct brand assets.
- [ ] Verify Dashboard visual baseline.
- [ ] Verify core feature screens visual baseline.
- [ ] Verify world map asset in Dashboard.
- [ ] Verify Russian layout does not visibly break main screens.
- [ ] Verify no obvious old/temporary UI artifacts remain.

### 10. Documentation pass

- [ ] Verify README links are current.
- [ ] Verify MVP deployment docs.
- [ ] Verify security baseline docs.
- [ ] Verify traffic docs.
- [ ] Verify routing profiles docs.
- [ ] Verify User Portal docs.
- [ ] Verify brand source-of-truth rule.
- [ ] Verify no active OPNsense references remain.

Suggested checks:

```bash
grep -Rni "OPNsense" . \
  --exclude-dir=.git \
  --exclude-dir=node_modules \
  --exclude-dir=dist \
  --exclude-dir=.vite
```

## Out of scope

- Kubernetes.
- HA.
- Appliance image.
- Auto-update implementation.
- Billing / tariff logic.
- Real sing-box stats API integration.
- Persistent Agent-side traffic counters.
- Monthly traffic reset jobs.
- Mobile apps.
- Clash / V2Ray renderers.
- OPNsense integration.

## Acceptance Criteria

RouteGate MVP v0.1 Release Candidate can be considered accepted when:

- [ ] Clean `main` starts successfully in dev Docker Compose.
- [ ] `make check` passes.
- [ ] Manager, Agent, PostgreSQL, and frontend health checks pass.
- [ ] Admin can create server, register Agent, and see heartbeat.
- [ ] Admin can create VPN account and generate usable subscription/QR.
- [ ] Public subscription returns `routegate.client_config.v1` and rendered `sing-box.config.v1`.
- [ ] User Portal user can view own profile and generate own subscription safely.
- [ ] Config render/apply path works through Manager → Agent job flow.
- [ ] Routing profile rules reach public subscription client config.
- [ ] Traffic usage dev scenario works and over-limit exclusion affects rendered config.
- [ ] UI, brand, and language switcher are visually acceptable for MVP.
- [ ] Documentation is sufficient for MVP deployment and verification.

## Acceptance results

Fill this section after the pass.

```text
Date:
Branch / commit:
Environment:
Tester:

Overall status:
Pass / Fail / Blocked

Passed sections:

Blocked sections:

Follow-up issues:

Notes:
```

## Resulting Milestone

When complete, record:

**MILESTONE — RouteGate MVP v0.1 Release Candidate**

Status: Complete / Accepted

Result:
RouteGate MVP v0.1 has passed the first full product acceptance pass across deployment, auth, Manager ↔ Agent, VPN accounts, subscriptions, User Portal, config deploy, routing profiles, traffic limits, UI/i18n, brand, and security documentation.
