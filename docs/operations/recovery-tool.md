# RouteGate Recovery Tool

The All-in-One release installs a root-only recovery entrypoint:

```bash
sudo routegate-recovery status
```

It reads `/etc/routegate/install-state.env`, which is written by the RouteGate
installer and must have `STATUS=complete`. The domain is validated as an FQDN
before it can be used by any certificate operation.

## Status and diagnostics

```bash
sudo routegate-recovery status
```

The command reports active/enabled state for PostgreSQL, Manager, Agent, nginx,
and `certbot.timer`, plus the managed certificate expiry. It returns non-zero
when a required service is inactive or the certificate is absent/expired, so it
can be used in an operator check without parsing secrets.

From the Analytics node inspector, **Check Manager certificate** queues the
Agent's typed `manager_certificate` diagnostic. The Agent checks the Manager URL
already present in its configuration. Manager assigns the final health state:

| Result | State | Next action |
| --- | --- | --- |
| Verified, more than 30 days remaining | Healthy | None |
| Verified, 30 days or less remaining | Degraded | Renew certificate |
| Expired, not yet valid, or untrusted | Unhealthy | Repair or renew certificate |
| Certificate cannot be inspected | Unknown | Check Manager DNS/TLS reachability |

## Certificate renewal

```bash
sudo routegate-recovery renew-certificate
```

This runs normal Certbot renewal for the installer-owned domain. It does not
force a renewal that is not due. After Certbot, it validates and reloads nginx,
checks the Manager HTTPS health endpoint, and confirms the certificate is not
expired. Automatic renewal remains enabled through `certbot.timer` and the
RouteGate nginx deploy hook.

## Service recovery

```bash
sudo routegate-recovery restart-services
```

This restarts only RouteGate Manager, RouteGate Agent, and nginx. Each service
is checked before the operation proceeds to the next dependency.

## VPN config rollback

List the root-only backups under `/var/lib/routegate-agent/backups`, then pass
the config version UUID only:

```bash
sudo routegate-recovery rollback-vpn-config 123e4567-e89b-42d3-a456-426614174000
```

The tool constructs the backup path itself, rejects symlinks, validates the
backup with `sing-box check`, promotes it atomically, and restarts sing-box. If
the service does not become active, the config that was active immediately
before recovery is restored.

Recovery operations are logged to `/var/log/routegate-recovery.log` with mode
`0600`. The log contains operation state only, not passwords, tokens,
certificate bodies, or private keys.
