# RG-90-FOLLOWUP — Production systemd smoke test

## Status

- Status: Ready for execution on a real Linux/systemd host
- Current environment: not executed in this containerized workspace; this document captures the production-like procedure and acceptance criteria
- Acceptance target: verify the RouteGate Agent apply path with `service_control_enabled: true`

## Purpose

Verify that the RouteGate Agent can render and apply a sing-box config on a production-like Linux host where the Agent uses real systemd service control instead of the dev/Codespaces fallback.

This follow-up is intended to confirm that the Agent reaches the full runtime path:

- stage
- validate
- apply
- restart
- healthcheck

## Host requirements

The smoke test requires a real Linux host with:

- a working `systemd` init
- `systemctl` available in `PATH`
- a sing-box binary installed and executable
- a sing-box systemd unit present and manageable by `systemctl`
- writable paths for:
  - staging config directory
  - active config path
  - backup directory
- network access from the host to the RouteGate Manager
- an existing Manager server and a valid Agent registration token

Recommended host assumptions:

- the Agent runs as a user with permission to write to the configured config paths, or the host uses `sudo`/root for the Agent service
- the sing-box service unit uses `/etc/sing-box/config.json` or another path that matches the Agent config

## sing-box systemd service setup

Example unit file:

```ini
[Unit]
Description=sing-box
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/etc/sing-box
ExecStart=/usr/local/bin/sing-box run -c /etc/sing-box/config.json
Restart=on-failure
RestartSec=2s

[Install]
WantedBy=multi-user.target
```

Example setup commands:

```bash
sudo install -d -m 0755 /etc/sing-box /var/lib/routegate-agent/configs /var/lib/routegate-agent/backups
sudo install -m 0755 /path/to/sing-box /usr/local/bin/sing-box
sudo systemctl daemon-reload
sudo systemctl enable --now sing-box
sudo systemctl status sing-box --no-pager
```

## RouteGate Agent config example

The production-like Agent apply path uses the following config fields:

- `service_control_enabled: true` enables the real restart and healthcheck path
- `sing_box_path` points to the sing-box binary used for validation
- `active_config_path` is the live sing-box config path
- `config_staging_dir` is the staging path used by the Agent to write the rendered config before promotion
- `config_backup_dir` is the backup directory used before replacing the active config
- `sing_box_service_name` is the systemd service name used by the Agent

Example config:

```yaml
manager_url: "https://manager.example.internal"
registration_token: "rg_reg_..."
heartbeat_interval_seconds: 10

config_staging_dir: "/var/lib/routegate-agent/configs"
active_config_path: "/etc/sing-box/config.json"
config_backup_dir: "/var/lib/routegate-agent/backups"

sing_box_path: "/usr/local/bin/sing-box"
sing_box_service_name: "sing-box"
service_control_enabled: true
```

## Manager / Agent registration steps

1. Create or select a server in the Manager/Admin UI.
2. Generate a one-time registration token for that server.
3. Install the Agent on the target Linux host.
4. Create the Agent config file with the production-like values above.
5. Start the Agent and allow it to register with the Manager.
6. Confirm that the Agent appears as registered and sends heartbeats.

Example Agent startup:

```bash
sudo mkdir -p /etc/routegate
sudo install -m 0600 /path/to/agent.yaml /etc/routegate/agent.yaml
sudo /usr/local/bin/routegate-agent -config /etc/routegate/agent.yaml -once
```

## Config render / apply steps

1. In the Manager/Admin UI, render a config version for the target server.
2. Validate the rendered config.
3. Apply the config to the server via the Config Deploy flow.
4. Wait for the Agent job to be picked up and completed.
5. Inspect the runtime stage results in the Manager/Admin UI or Agent job payload.

## Expected runtime stage results

A successful production-systemd run should report the following stage results:

```text
stage: succeeded
validate: succeeded
apply: succeeded
restart: succeeded
healthcheck: succeeded
rollback: skipped
```

The runtime behavior is implemented by the Agent as:

- `sing-box check -c <staged-path>` validation
- atomic promotion of the staged file to the active config path
- `systemctl restart <service>`
- `systemctl is-active --quiet <service>`

## Failure scenarios to cover

If practical, verify these cases as part of the smoke-test pass/fail review:

- invalid sing-box config fails validation before the active config is replaced
- missing sing-box binary produces a clear validation error before apply
- missing systemd service produces a clear restart/healthcheck error
- failed restart triggers rollback behavior or documents the current limitation

## Current blockers

- RG-90-FOLLOWUP-BLOCKER-001: No code blocker identified for the standard systemd apply path. The implementation already supports the required stage, validate, apply, restart, and healthcheck flow.
- RG-90-FOLLOWUP-BLOCKER-002: A host without real `systemd`, a working `systemctl`, or a valid sing-box service unit cannot exercise the restart/healthcheck path. This is an environment prerequisite rather than a RouteGate code defect.

## Evidence checklist

The test is considered successful only when the following evidence is collected:

- Manager/Admin UI shows a successful apply job for the target server
- Agent job payload reports `stage: succeeded`
- Agent job payload reports `validate: succeeded`
- Agent job payload reports `apply: succeeded`
- Agent job payload reports `restart: succeeded`
- Agent job payload reports `healthcheck: succeeded`
- `rollback` is `skipped` for the successful path
- the active config at `/etc/sing-box/config.json` reflects the newly applied content
- `systemctl status sing-box` shows the service active after the apply
- the sing-box service is still healthy after the restart/healthcheck cycle

## Final acceptance decision

- Pass: when the production-like Linux/systemd apply path is verified with `service_control_enabled: true`, the apply job completes successfully, restart and healthcheck are not skipped, and the evidence checklist is satisfied
- Fail: when validation, apply, restart, or healthcheck fails, or when the Manager/Admin UI does not show the expected successful apply job

Final decision text if passed:

> RouteGate Config Deploy is verified for both dev/Codespaces apply path with `service_control_enabled=false` and production Linux/systemd apply path with `service_control_enabled=true`.
