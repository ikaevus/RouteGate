# VPN client presence

RouteGate separates two signals instead of presenting traffic as proof of a live tunnel:

- `online` is an explicit, fresh Agent observation;
- `recently_active` is a two-minute traffic heuristic and is never counted as online.

The Agent sends an authoritative snapshot to `POST /api/v1/agent/client-presence` every 30 seconds. Manager authenticates the Agent bearer token, derives the Agent and server IDs itself, accepts only active VPN accounts assigned to that server, rejects out-of-order snapshots, and expires an observation after 75 seconds. An empty snapshot clears the Agent's previous observations.

## Collector contract

Runtime adapters write `/var/lib/routegate-agent/client-presence.json` atomically. The file contains aggregated observations per VPN account and protocol:

```json
{
  "observedAt": "2026-09-03T23:10:00Z",
  "items": [
    {
      "vpnAccountId": "523446e8-0351-4c0a-a9ec-19a269a8848f",
      "protocol": "vless-reality",
      "connectionCount": 1,
      "source": "sing-box-api",
      "confidence": "exact",
      "connectedAt": "2026-09-03T23:08:42Z",
      "lastActivityAt": "2026-09-03T23:09:58Z"
    }
  ]
}
```

No client IP address or device fingerprint is included in the Manager contract. A runtime adapter that only has handshake or counter data must use `confidence: "heuristic"`; the UI keeps that signal out of the online total.

Administrators read the merged view through `GET /api/v1/connections`. Dashboard requests a compact list; Analytics requests the full table.
