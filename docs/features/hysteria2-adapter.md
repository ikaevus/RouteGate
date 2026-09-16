# RG-114F — Native Hysteria2 Adapter

RG-114F adds RouteGate's third managed VPN Core composition while preserving
the existing VLESS and WireGuard paths.

## Operator flow

1. Create a DNS record for the Hysteria2 domain. On a Hybrid Node, it must be
   different from the Manager hostname and resolve directly to the node.
2. Allow inbound TCP 80 for ACME HTTP-01 and the selected UDP port for QUIC.
3. In Protocol Settings select Hysteria2 and enter the domain and ACME email.
4. Create or activate at least one VPN account.
5. Render, validate, and apply the server configuration.
6. Deliver the standard `hysteria2://` URI or QR code through the account page.

The Manager uses account UUIDs as usernames and database-generated 192-bit
passwords. Suspended, expired, revoked, or hard-limit-enforced accounts are
left out of the next rendered `userpass` map.

## Certificate lifecycle

Hysteria obtains and renews a dedicated Let's Encrypt certificate locally.
Its ACME data never enters Manager storage or Agent telemetry. The first apply
can fail until DNS and TCP 80 reachability are correct; after correction, a
normal apply retry restarts the fixed service and repeats acquisition.

On a dedicated VPN Node, Hysteria owns the public TCP 80 HTTP-01 listener. On a
Hybrid Node, it binds the fixed `127.0.0.1:9080` challenge listener and the
RouteGate nginx default-host bridge forwards only
`/.well-known/acme-challenge/` requests for the separate Hysteria hostname.
The Manager certificate and private key are never reused or exposed to
Hysteria. TCP 443 remains owned by nginx and UDP 443 can be owned by Hysteria.

## Client output

Protected subscription and client-connection endpoints return a standard URI
containing the userpass credentials, server domain, UDP port, and verified SNI.
Anyone holding that URI can use the account, so it must be handled like a
password.

## Boundaries

This slice does not add port hopping, Salamander/Gecko obfuscation, custom
masquerade URLs, insecure TLS, certificate pinning, TUN client provisioning,
or automatic protocol selection.
