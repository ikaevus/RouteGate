# RouteGate-managed Grafana

Grafana is an optional advanced visualization layer for RouteGate historical metrics.
It is not required for RouteGate health evaluation, alerts, diagnostics, VPN operation,
or the built-in Observability page.

## Architecture

```text
RouteGate Agent
      |
      v
RouteGate Manager
      |
      +--> PostgreSQL                 current operational state
      |
      +--> /metrics + /metrics/fleet
                    |
                    v
              Prometheus              historical time series
             127.0.0.1:9090
                    |
                    v
                Grafana               advanced dashboards
             127.0.0.1:3000
                    |
                    v
        RouteGate nginx / HTTPS
        https://HOST/grafana/
```

Prometheus and Grafana remain loopback-only. Remote administrators reach Grafana
through the same HTTPS endpoint already used by RouteGate; TCP 3000 and 9090 do
not need to be exposed publicly.

## Requirements

The managed Grafana installer currently supports the canonical RouteGate All-in-One
host:

- Ubuntu 24.04 LTS, amd64;
- a complete RouteGate-owned installation;
- HTTPS already configured for `ROUTEGATE_PUBLIC_URL`;
- RouteGate-managed Prometheus installed, active, and ready;
- no existing unowned Grafana installation on the host.

RouteGate deliberately refuses to take ownership of an existing Grafana package,
configuration, data directory, or service.

## Install

Run on the RouteGate host:

```bash
curl -fsSL https://raw.githubusercontent.com/ikaevus/RouteGate/main/install-grafana.sh \
  | sudo bash
```

The installer uses the official Grafana stable APT repository and installs the
open-source `grafana` package. It verifies the expected Grafana repository signing
key fingerprint before package installation.

The installation is rollback-oriented: RouteGate backs up its installer ownership
state and nginx site before the first managed mutation. If validation fails, the
previous RouteGate nginx/state files are restored and a Grafana package introduced
by the failed operation is removed.

## What RouteGate configures

RouteGate configures Grafana with these boundaries:

- Grafana HTTP listener: `127.0.0.1:3000`;
- public path: `https://<routegate-host>/grafana/` through nginx;
- Prometheus datasource: `http://127.0.0.1:9090`;
- anonymous access: disabled;
- public sign-up and organization creation: disabled;
- secure session cookies enabled;
- Grafana usage reporting and update checks disabled;
- minimum dashboard refresh interval: 30 seconds.

RouteGate also provisions a non-editable `RouteGate Prometheus` datasource and a
`RouteGate Fleet Overview` dashboard. The initial dashboard includes Manager and
PostgreSQL status plus historical memory, disk, load, Agent availability/telemetry,
and VPN Core availability charts.

## Authentication

RG-136 phase 1 intentionally keeps Grafana authentication independent from the
RouteGate administrator session. The installer creates a unique initial Grafana
administrator password and writes it only to the root-readable file:

```text
/root/routegate-grafana-access.txt
```

Read it on the server:

```bash
sudo cat /root/routegate-grafana-access.txt
```

Then open the displayed HTTPS URL, sign in as `admin`, change the initial password,
and remove the root-only bootstrap file after verifying the new password.

The generated password is used only for Grafana's first startup. RouteGate removes
the temporary systemd environment override immediately afterward; subsequent
starts use Grafana's own password hash in its local database.

A future SSO/Auth-Proxy integration may remove the second login, but it is not part
of the initial managed Grafana boundary. RouteGate does not enable anonymous Viewer
access as a substitute for SSO.

## Ownership state and idempotency

After successful validation RouteGate records:

```text
GRAFANA_MANAGED=1
```

in `/etc/routegate/install-state.env`.

Re-running the installer against a healthy RouteGate-managed Grafana installation
performs health/ownership checks and exits without reinstalling or replacing it.

## Security boundary

Do not expose Grafana `:3000` or Prometheus `:9090` directly to the Internet. The
canonical remote path is RouteGate HTTPS on port 443. The nginx gateway also proxies
Grafana Live WebSocket traffic under `/grafana/api/live/`.

The built-in RouteGate Observability page remains the canonical interface for
current health, alerts, diagnostics, and recommended next actions. Grafana is the
advanced historical-analysis layer rather than a replacement for RouteGate
Observability.
