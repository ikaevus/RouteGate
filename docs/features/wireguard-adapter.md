# RG-114E — Native WireGuard Adapter

RG-114E adds the first non-VLESS RouteGate protocol path while preserving the
existing Manager → Agent → VPN Core architecture.

## Operator flow

1. Open a VPN or Hybrid Node's Protocol Settings.
2. Choose **Configure WireGuard**. Manager generates the server keypair and
   applies the recommended UDP port, interface subnet, and client DNS.
3. Create or assign active VPN accounts. Manager allocates each account a
   unique peer keypair and IPv4 address under a serialized server-row lock.
4. Render, validate, and deploy the server config through the normal Config
   Deploy workflow.
5. Open an account connection or subscription. RouteGate returns a standard
   WireGuard config suitable for QR import.

Recommended values:

| Setting | Value |
| --- | --- |
| Interface | `routegate-wg0` |
| UDP port | `51820` |
| Server address | `10.66.0.1/24` |
| Client DNS | `1.1.1.1` |
| Persistent keepalive | `25` seconds |

## Compatibility

- Existing database rows retain `vless` as their selected protocol.
- The config envelope remains `routegate.config.v1`.
- Old plain sing-box apply tasks continue to select the VLESS adapter.
- WireGuard requires an Agent build advertising the native adapter and a host
  with `wireguard-tools`; both supported installers provide it.

## Bounded operations

The WireGuard adapter does not accept arbitrary interfaces, services, config
paths, or hook commands. Agent uses fixed paths and the fixed systemd instance
from its root-owned configuration. Native validation output is deliberately
discarded because `wg-quick strip` can include the server private key.
