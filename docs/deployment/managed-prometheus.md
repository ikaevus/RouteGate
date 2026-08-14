# RouteGate-managed Prometheus

RouteGate can optionally install and manage a local Prometheus instance on the canonical Ubuntu 24.04 LTS All-in-One host.

Prometheus remains optional. RouteGate health evaluation, alerts, diagnostics, Delivery notifications, current infrastructure state, and the Admin UI do not depend on a Prometheus process being installed.

## Quick installer

The interactive clean-host installer asks:

```text
Install Prometheus for historical infrastructure metrics? [y/N]
```

The default is **No**. Pressing Enter keeps the canonical RouteGate installation small and does not disable built-in observability.

For non-interactive deployments:

```text
--with-prometheus
```

explicitly opts in, while:

```text
--without-prometheus
```

explicitly opts out. `--yes` without either Prometheus flag retains the safe default: Prometheus is not installed.

## Package source

The canonical Ubuntu 24.04 installer uses the Ubuntu `prometheus` package rather than downloading an arbitrary release binary during installation. This keeps installation within the operating-system package trust and update model.

RouteGate does not take over an existing active Prometheus service. If an existing deployment is detected, the installer stops and recommends keeping it as an external Prometheus integration instead.

## Network exposure

A RouteGate-managed Prometheus instance is intentionally local-only:

```text
127.0.0.1:9090
```

The installer supplies a RouteGate-owned systemd override rather than accepting Prometheus's default wildcard web listener. Port 9090 is not added to nginx and is not opened in UFW.

The managed Prometheus scrapes RouteGate Manager locally at:

```text
http://127.0.0.1:8080/metrics
http://127.0.0.1:8080/metrics/fleet
```

A dedicated generated monitoring token is stored in root-controlled RouteGate state and in a Prometheus-readable token file. The token is not printed in installer output and is not embedded directly in `prometheus.yml`.

## Files owned by RouteGate

The managed integration uses separate RouteGate-specific paths:

```text
/etc/prometheus/routegate.yml
/etc/prometheus/routegate.token
/etc/systemd/system/prometheus.service.d/routegate.conf
/var/lib/prometheus/routegate
```

RouteGate does not rewrite the distro package's generic `/etc/prometheus/prometheus.yml` configuration.

## Scrape jobs

The initial managed configuration creates two jobs:

- `routegate-manager` for Manager/control-plane metrics;
- `routegate-fleet` for latest managed-host observations and RouteGate health semantics.

The initial scrape interval is 30 seconds.

## Existing Prometheus deployments

An organization may use its own Prometheus instead of the RouteGate-managed instance. That Prometheus remains outside RouteGate ownership and may scrape the supported public metrics surface over HTTPS with a dedicated monitoring credential.

The Admin UI must distinguish these modes:

- Prometheus not configured;
- RouteGate-managed Prometheus;
- external Prometheus.

A future Guided Workflow action may install the same RouteGate-managed component after initial setup. That UI action must reuse the same ownership and network-exposure rules rather than introducing a second installation model.
