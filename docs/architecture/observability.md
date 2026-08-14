# RouteGate Observability / Monitoring Architecture v0.1

**Status:** Accepted

## Decision

Observability is a dedicated domain boundary inside the RouteGate Manager modular monolith. It remains separate from Traffic Stats and is not a separately deployed service in the initial architecture.

RouteGate owns operational meaning: health evaluation, events, alert lifecycle, diagnostics, and recommended next actions. Prometheus-compatible metrics are an open public observability surface, but Prometheus is not required for RouteGate health.

## Domain model

RouteGate keeps four concepts distinct:

- Metric: numeric runtime measurement.
- Health State: current evaluated condition.
- Event: discrete operational occurrence.
- Alert: stateful condition requiring administrator attention.

Health states are exactly `healthy`, `degraded`, `unhealthy`, and `unknown`.

Health checks carry freshness, machine-readable reason codes, evidence, and optional recommended actions for Guided Workflow / Next Action First.

## Persistence boundary

PostgreSQL stores durable product state such as current health, health transitions, operational events, alert episodes, alert transitions, acknowledgements, monitoring configuration, and durable notification state.

PostgreSQL is not the raw high-frequency metrics database. Historical time-series metrics belong to Prometheus-compatible infrastructure when enabled.

Existing Agent runtime measurements can feed Observability until the bounded Agent telemetry snapshot is expanded in RG-113B.

## Alert lifecycle

The condition lifecycle is `pending -> firing -> resolved`. A pending condition may resolve before firing.

A resolved episode does not reopen. Recurrence creates a new episode with the same logical fingerprint and a new episode ID.

Acknowledgement is independent from condition state, so an alert may remain firing while acknowledged.

The Alert Engine will add delayed firing, severity escalation, hysteresis, deduplication, and flapping protection in later RG-113 stages.

## Delivery boundary

Canonical flow:

`Observability -> Alert Engine -> Notification Intent -> Delivery -> Provider Adapter`

Observability does not own provider-specific messaging. Delivery retains provider selection, EN/RU rendering, retries, and delivery lifecycle.

## Diagnostics boundary

Diagnostics use explicit allow-listed typed Agent operations only. Manager does not gain a generic remote command execution capability.

Diagnostic results are structured and should expose reason codes and recommended actions where possible.

## Prometheus

Prometheus is first-class supported technology and remains optional.

- Internal RouteGate health does not depend on Prometheus.
- Prometheus-compatible exposition is a documented public contract.
- Manager exposes its own metrics and may expose a separate fleet surface backed by latest RouteGate observations.
- Direct Agent inbound scrape is not required for the MVP.
- Metrics use stable `routegate_` naming and controlled label cardinality.
- External Prometheus retention remains administrator-owned.

The clean-host installer offers:

`Install Prometheus? [y/N]`

The default is `No`.

Prometheus can later be installed from the Admin UI using the guided job-based component installation pattern already used for VPN Core installation.

RouteGate distinguishes `not configured`, `RouteGate-managed Prometheus`, and `external Prometheus`. RouteGate only owns lifecycle and configuration of the managed mode.

Grafana, Alertmanager, and OpenTelemetry components remain optional and are not installed automatically with Prometheus.

## Analytics UI

The Admin UI gains `Analytics` / `Аналитика` as an operational surface. It may combine read models from Observability and Traffic Stats without merging their backend ownership.

Analytics starts with a smart world map of distributed RouteGate nodes. The map is an operational fleet view with health, clustering, problem prioritization, and contextual next actions.

The intended hierarchy is `World -> Region -> City -> Node`.

The normal Dashboard remains concise and action-oriented. Analytics contains deeper fleet exploration and historical metrics. Without Prometheus, historical panels present installation as an optional enhancement rather than an error.

## Scale and openness

The architecture preserves bounded telemetry, freshness, batching, controlled collection frequency, controlled metric cardinality, idempotent alert evaluation, and no raw metric history in PostgreSQL.

Observability remains open and auditable: open code, open metrics, documented semantics, and no requirement to use the RouteGate UI as the only monitoring surface.

## Implementation milestones

- RG-113A: domain foundation and durable health/event/alert state.
- RG-113B: bounded Agent telemetry.
- RG-113C: health evaluation and aggregation.
- RG-113D: alert engine.
- RG-113E: Delivery integration.
- RG-113F: diagnostics.
- RG-113G: Prometheus surface.
- RG-113H: Analytics / Monitoring UI and world map.
- RG-113I: production-like failure and recovery validation.
