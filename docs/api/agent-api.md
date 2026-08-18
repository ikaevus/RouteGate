# Agent API

RouteGate Agent uses the versioned Agent-to-Manager API only:

```text
POST /api/v1/agent/register
POST /api/v1/agent/heartbeat
GET  /api/v1/agent/tasks/next
POST /api/v1/agent/tasks/{job_id}/result
POST /api/v1/agent/traffic-usage
```

Administrators read registered Agents through the authenticated Manager API:

```text
GET /api/v1/agents
```

Registration uses a short-lived server registration token. A successful registration returns a persistent Agent credential, which is then used for heartbeat, task polling, task completion, and traffic reporting.

For distributed VPN Nodes, administrators normally do not build or configure
Agent manually. Manager returns a one-command Agent bootstrap instruction from
`POST /api/v1/servers/{server_id}/registration-token`. The Agent-only installer
validates the Ubuntu/systemd target, verifies the selected release bundle
checksum, writes the token with mode `0600`, starts the systemd service, and
waits for the token exchange. The raw token is never persisted by Manager.

Registration and heartbeat include Agent build and protocol metadata:

```json
{
  "agentVersion": "dev",
  "protocolVersion": 1
}
```

Manager stores the most recently reported values and uses `protocolVersion` as the compatibility boundary.

Registration and heartbeat also report a forward-compatible `capabilities`
map. RG-114 Agents include a versioned `capabilities.routegate` block that
lists the node capabilities and complete VPN Core adapters RouteGate can manage.
This is separate from VPN Core binary detection and runtime telemetry. See
`docs/architecture/platform-expansion.md` for the contract.

Agent diagnostic profiles are compile-time allow-listed and advertised through
`capabilities.diagnosticProfiles`. RG-114D adds `manager_certificate` alongside
`host_overview` and `vpn_core_status`. The certificate profile always targets
the configured Manager URL and returns only hostname, validity timestamps, and
verification outcome. Certificate bytes, private keys, arbitrary target URLs,
and raw command/network output are outside the Agent API contract.

RG-114E adds a second entry to `capabilities.routegate.vpnCoreAdapters`:
`wireguard` core, `wireguard` protocol, `udp` transport, and `wireguard`
security. `capabilities.vpnCores` reports separate sing-box and WireGuard
runtime states while the legacy singular `vpnCore` field remains available for
compatibility.

RG-114F adds a third managed adapter entry: `hysteria` core, `hysteria2`
protocol, `quic` transport, and `tls` security. The separate Hysteria runtime
state is appended to `capabilities.vpnCores`; credentials and ACME material are
never reported.

WireGuard config apply tasks use the existing `config_apply` kind and
`routegate.config.v1` envelope. `metadata.vpnCore` selects the adapter and the
`wireGuard` field carries the native config. Validation and result payloads do
not contain native command output or private key material.

Hysteria2 config apply tasks use the `hysteria2` envelope field. Agent accepts
only the strict RouteGate JSON schema, the fixed ACME and masquerade policy,
UUID usernames, and 192-bit hexadecimal account passwords.

Shadowsocks config apply tasks use the existing `singBox` envelope and select
the `sing-box / shadowsocks / tcp / aead-2022` descriptor. MTProto tasks use the
`mtproto` envelope field and select `mtg / mtproto / tcp / faketls`. The Agent
rejects unknown MTProto TOML fields and policies, validates the mtg binary,
promotes `/etc/routegate-mtproto/config.toml` atomically, checks service
persistence and the TCP listener, and rolls back on failure.

The old unversioned `/api/agent/*` compatibility endpoints and manual Agent registration UI are not part of the supported RouteGate runtime.
