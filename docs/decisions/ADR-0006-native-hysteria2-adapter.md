# ADR-0006: Native Hysteria2 Adapter and Node-local ACME

- Status: Accepted
- Date: 2026-08-18
- Workstream: RG-114F

## Context

RouteGate's adapter boundary already manages VLESS through sing-box and native
WireGuard. Hysteria2 adds a different composition: its protocol is carried by
QUIC over UDP and requires TLS. Reusing the Manager nginx certificate would
couple management-plane private keys to the VPN plane and make remote node
onboarding unsafe.

## Decision

RouteGate manages the upstream Hysteria binary as a native VPN Core:

| Dimension | Value |
| --- | --- |
| VPN Core | `hysteria` |
| Protocol | `hysteria2` |
| Transport | `quic` |
| Security | `tls` |

The Manager renders strict JSON accepted by Hysteria. The initial schema
contains one UDP listener, Hysteria-owned Let's Encrypt HTTP-01 ACME, a fixed
ACME state directory, `userpass` authentication, and a fixed HTTPS proxy
masquerade target. Arbitrary YAML, commands, authentication backends, ACME DNS
provider secrets, port ranges, obfuscation, traffic APIs, and file paths are
not accepted.

Every VPN account receives a random 192-bit hexadecimal password. Its UUID is
the Hysteria2 username. Credentials are present only in the protected config
apply payload and existing authenticated or token-protected client delivery
paths. They are excluded from Agent telemetry and task results.

The VPN node owns its Hysteria ACME state under `/var/lib/hysteria/acme`.
Manager certificates and keys are never copied or referenced. Because HTTP-01
must own TCP port 80, RG-114F permits this adapter only on a dedicated `vpn`
node. Hybrid-node certificate coordination is deliberately deferred.

The Agent stages `/etc/hysteria/config.json` with mode 0600, validates an exact
JSON grammar, verifies the fixed Hysteria binary, applies through the shared
atomic promotion/rollback path, controls only `hysteria-server.service`, and
requires the Hysteria process to own the configured UDP listener.

Installers pin Hysteria 2.12.1 and verify its binary against the upstream
release `hashes.txt` before installation. An existing unmanaged Hysteria
installation is never overwritten.

## Consequences

- Hysteria2 gets independent server, account, client, apply, health, and
  certificate lifecycles without changing Manager → Agent → VPN Core.
- A dedicated DNS name must resolve to the VPN node, TCP 80 must be reachable
  for ACME, and the configured UDP port must be open at host and provider
  firewalls.
- Hysteria2 remains a TCP/UDP proxy protocol; it is not presented as a generic
  layer-3 tunnel and does not promise ICMP forwarding.
- Hybrid-node Hysteria2, alternate CAs, DNS-01, certificate import, obfuscation,
  port hopping, and custom masquerade targets require later explicit designs.
