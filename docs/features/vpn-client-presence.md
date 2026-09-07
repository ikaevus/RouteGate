# VPN client presence

RouteGate separates two signals instead of presenting traffic as proof of a live tunnel:

- `online` is an explicit, fresh Agent observation;
- `recently_active` is a two-minute traffic heuristic and is never counted as online.

The Agent sends an authoritative snapshot to `POST /api/v1/agent/client-presence` every 30 seconds. Manager authenticates the Agent bearer token, derives the Agent and server IDs itself, accepts only active VPN accounts assigned to that server, rejects out-of-order snapshots, and expires an observation after 75 seconds. An empty snapshot clears the Agent's previous observations.

For managed VLESS inbounds, the Agent collects presence without a separate adapter process. It reads the active sing-box configuration to map the authenticated user name to its VLESS credential UUID, correlates successful authentication records from the `sing-box.service` journal with currently established TCP sockets reported by `ss`, and reports only sockets that satisfy all three signals. Manager resolves the credential UUID to an active account on the Agent's own server. An unauthenticated socket, an old journal record without a live socket, or a foreign credential is never counted as online.

## External collector contract

Other runtime adapters can write `/var/lib/routegate-agent/client-presence.json` atomically. The Agent merges those observations with its native sing-box collection. The file contains aggregated observations per VPN account and protocol:

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

Client source addresses are used only for local, in-memory correlation between the journal and the kernel socket table. No client IP address or device fingerprint is written to the presence file or sent to Manager. A runtime adapter that only has handshake or counter data must use `confidence: "heuristic"`; the UI keeps that signal out of the online total.

Administrators read the merged view through `GET /api/v1/connections`. Dashboard requests a compact list; Analytics requests the full table.
