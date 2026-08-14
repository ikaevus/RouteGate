# RouteGate Prometheus Metrics Surface

Status: supported public observability interface.

RouteGate exposes Prometheus-compatible text metrics without requiring a Prometheus server for RouteGate health, alerts, diagnostics, or normal administration.

Prometheus is optional. RouteGate remains operational when this surface is disabled.

## Security

The metrics surface is disabled by default.

Enable it with:

```text
ROUTEGATE_MONITORING_ENABLED=true
ROUTEGATE_MONITORING_TOKEN=<strong dedicated token>
```

Both conditions are required. If monitoring is disabled or the token is absent, RouteGate does not expose the metrics surface. When enabled, requests must use a dedicated Bearer credential:

```text
Authorization: Bearer <monitoring token>
```

The monitoring token is independent from Admin Web sessions and Agent credentials. Do not expose `/metrics` or `/metrics/fleet` without HTTPS or an equivalent trusted network boundary.

A future RouteGate-managed Prometheus installation may generate and configure this credential automatically. The public metrics contract does not depend on that installation mode.

## Endpoints

### `GET /metrics`

Manager and control-plane metrics.

Initial metrics:

- `routegate_manager_info{version,git_commit}`
- `routegate_manager_up`
- `routegate_postgresql_up`
- `routegate_metrics_collection_success`
- `routegate_database_schema_version`
- `routegate_database_schema_expected_version`
- `routegate_agents{status}`
- `routegate_alerts_active{severity,state}`
- `routegate_diagnostic_runs{profile,status}`
- `routegate_delivery_requests{status}`

`routegate_manager_up` confirms that the Manager process is serving the scrape. `routegate_postgresql_up` separately reports database reachability so a database outage does not have to masquerade as a complete Manager outage.

### `GET /metrics/fleet`

Latest managed-infrastructure observations and RouteGate health semantics.

Initial metrics:

- `routegate_agent_up{server_id}`
- `routegate_agent_observation_age_seconds{server_id}`
- `routegate_agent_observation_fresh{server_id}`
- `routegate_host_load1{server_id}`
- `routegate_host_load5{server_id}`
- `routegate_host_load15{server_id}`
- `routegate_host_logical_cpus{server_id}`
- `routegate_host_memory_total_bytes{server_id}`
- `routegate_host_memory_available_bytes{server_id}`
- `routegate_host_memory_usage_ratio{server_id}`
- `routegate_host_root_fs_total_bytes{server_id}`
- `routegate_host_root_fs_free_bytes{server_id}`
- `routegate_host_root_fs_usage_ratio{server_id}`
- `routegate_host_uptime_seconds{server_id}`
- `routegate_vpn_core_info{server_id,core,version}`
- `routegate_vpn_core_up{server_id,core}`
- `routegate_health_check{server_id,check,state}`
- `routegate_server_health{server_id,state}`

Fleet metrics are exported from the latest bounded Agent observation held by Manager. PostgreSQL is not used as a high-frequency metrics history database.

### Freshness semantics

A numeric observation can exist while being stale. Consumers must not interpret the presence of a CPU, memory, disk, or VPN Core sample as proof that an Agent is currently reachable.

Use:

- `routegate_agent_observation_age_seconds` to see observation age;
- `routegate_agent_observation_fresh` for RouteGate's freshness policy;
- `routegate_agent_up` for the combined Agent online + fresh signal.

The initial RouteGate Agent telemetry freshness window is 90 seconds.

## Cardinality policy

The Prometheus surface is intentionally stricter than internal product state.

Allowed infrastructure identity labels include `server_id`. Bounded semantic labels include values such as `status`, `severity`, `state`, `profile`, and `core`.

The following must not be exposed as Prometheus labels by the observability surface:

- VPN account IDs or usernames;
- user IDs or email addresses;
- Delivery recipients;
- request IDs;
- job IDs;
- free-form error messages;
- arbitrary URLs;
- arbitrary runtime IP addresses.

New health checks are not automatically exported as `check` label values. Each public check must be explicitly reviewed and added to the Prometheus health-check allow-list.

## Health model integration

Metrics and health remain separate concepts.

For example, disk capacity metrics expose numeric values while `routegate_health_check` exposes RouteGate's evaluated operational state. `routegate_server_health` exposes the aggregate state derived from required health checks.

Prometheus consumers may build their own rules directly from raw metrics. They are not required to use RouteGate's health interpretation.

## Example Prometheus configuration

Use a token file rather than placing the monitoring credential directly in a world-readable configuration file where practical.

```yaml
scrape_configs:
  - job_name: routegate-manager
    scheme: https
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/routegate.token
    static_configs:
      - targets: [routegate.example.com]

  - job_name: routegate-fleet
    scheme: https
    metrics_path: /metrics/fleet
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/routegate.token
    static_configs:
      - targets: [routegate.example.com]
```

Grafana, Alertmanager, remote storage, and other external monitoring components remain optional and independently controlled by the administrator.
