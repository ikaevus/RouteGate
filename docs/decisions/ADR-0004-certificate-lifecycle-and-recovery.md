# ADR-0004: Certificate Lifecycle and Recovery Boundary

- Status: Accepted
- Date: 2026-08-18
- Workstream: RG-114D

## Context

The Clean VPS installer issued a Let's Encrypt certificate through Certbot and
nginx, but RouteGate did not explicitly verify the renewal timer, expose
certificate health through its diagnostic trust boundary, or provide one
supported recovery entrypoint. Operators had to compose commands from several
runbooks, increasing the chance of unsafe paths during an incident.

Recovery cannot become an arbitrary remote shell. Certificate private keys
must also remain on their owning node and must never enter Agent diagnostic
payloads or Manager logs.

## Decision

### Manager certificate lifecycle is explicit

The All-in-One installer enables and verifies `certbot.timer`. A Certbot deploy
hook runs `nginx -t` and reloads nginx after a successful renewal. The hook has
no user-controlled arguments and manages only the already configured nginx
certificate deployment.

Manual renewal uses `routegate-recovery renew-certificate`. It selects the
certificate name from root-owned RouteGate install state, runs normal Certbot
renewal without forced rotation, validates nginx, reloads it, checks Manager
HTTPS health, and verifies that the resulting certificate is not expired.

### Certificate observation remains non-secret

Agent adds the compile-time allow-listed `manager_certificate` diagnostic. Its
target is always the Agent's configured Manager HTTPS URL. Agent performs an
explicit x509 hostname/chain verification and returns only:

- availability;
- target hostname;
- `notBefore` and `notAfter` timestamps;
- verified/unverified outcome.

Certificate bodies, serial numbers, private keys, arbitrary endpoints, and raw
connection errors are excluded. Manager distrusts Agent-supplied meaning and
derives health and recommended actions itself.

### Recovery operations are fixed and local

The release bundle installs `/usr/local/sbin/routegate-recovery`. It is a
root-only local tool with four allow-listed operations:

| Operation | Safety boundary |
| --- | --- |
| `status` | Reads fixed RouteGate services, install state, and managed certificate metadata. |
| `renew-certificate` | Uses only the validated domain in root-owned install state. |
| `restart-services` | Restarts only Manager, Agent, and nginx, with health checks. |
| `rollback-vpn-config UUID` | Resolves only a UUID-named Agent backup, validates it with sing-box, and restores the prior active config if restart fails. |

The tool uses a process lock and root-only log. It accepts no command text,
service name, domain, script, or filesystem path from the caller.

## Consequences

- Manager certificate expiry and trust failures become structured fleet
  diagnostics.
- Clean VPS deployments have both automatic renewal and one supported manual
  recovery path.
- Recovery remains auditable and local; this ADR does not add remote shell
  semantics to Manager-Agent APIs.
- The Manager HTTPS private key is not reused for VPN protocols.
- Automated Reality key rotation and certificates owned by future Hysteria2 or
  other protocol adapters remain separate follow-ups.
