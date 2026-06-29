# Development Runbook

## Start PostgreSQL

```bash
docker compose -f deploy/docker-compose.dev.yml up -d postgres
```

## Start Manager

```bash
cd backend
go mod tidy
go run ./cmd/routegate-manager
```

## Check Manager

```bash
curl http://localhost:8080/api/admin/health
```

## Start Agent

```bash
cd agent
go mod tidy
go run ./cmd/routegate-agent
```

## Dev config apply readiness

The production Agent defaults use system-owned paths and service control:

- `/var/lib/routegate-agent/configs`
- `/var/lib/routegate-agent/backups`
- `/etc/sing-box/config.json`
- `sing_box_path: "sing-box"`
- `sing_box_service_name: "sing-box"`
- `service_control_enabled: true`

For Codespaces/dev acceptance, use the dev apply example instead:

```bash
cp deploy/examples/routegate-agent-dev-apply.yaml /tmp/routegate-agent-dev-apply.yaml
sed -i "s/replace-with-server-registration-token/$REGISTRATION_TOKEN/" /tmp/routegate-agent-dev-apply.yaml

cd agent
go run ./cmd/routegate-agent -config /tmp/routegate-agent-dev-apply.yaml -once
```

The dev example uses `/tmp/routegate-agent/...` paths so the Agent can create staging, active config, and backup directories without `sudo`.

It keeps real `sing-box check -c ...` validation. Make sure `sing-box` is available in `PATH`, or set `sing_box_path` to an absolute binary path in the config.

Codespaces/dev shells usually do not run `systemd`, so the dev example sets:

```yaml
service_control_enabled: false
```

With service control disabled, the Agent still stages, validates, and promotes the config. It then reports the apply job as succeeded with restart and healthcheck marked as skipped. Production/systemd service restart and healthcheck remain enabled by default.
