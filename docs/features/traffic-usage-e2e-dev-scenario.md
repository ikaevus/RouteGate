# RG-73 — Traffic Usage E2E / Dev Scenario

Status: dev scenario foundation

## Goal

Prove the current traffic usage path in local development without adding real sing-box statistics integration yet.

```text
file-based absolute counters
        ↓
RouteGate Agent file collector
        ↓
Agent in-memory delta tracker
        ↓
Manager agent traffic report API
        ↓
traffic_usage_events
        ↓
Admin UI TrafficStatsPanel / Admin API summary
```

This scenario intentionally uses the safe file-based collector from RG-72.

## Important behavior

The dev traffic file stores **absolute counters**.

The Agent reports **deltas** between consecutive snapshots while the same Agent process is running. The first snapshot establishes the in-memory baseline and does not create a usage event.

Do not use `-once` for this scenario. Persistent Agent-side counter state across restart is intentionally not implemented yet.

## Files

- `deploy/examples/routegate-agent-dev-traffic.yaml` — Agent config example with file-based traffic collection enabled.
- `scripts/dev-traffic-usage.sh` — helper that writes a local file-collector counter payload.
- `/tmp/routegate-dev-traffic-usage.json` — default local traffic counter file used by the example config.

## Local scenario

### 1. Start the dev stack

```bash
make dev
```

The dev stack starts Manager, frontend, and PostgreSQL.

Admin UI:

```text
http://127.0.0.1:5173
```

Default dev credentials:

```text
admin@routegate.local / admin
```

### 2. Create or select a server and VPN account

You can use the Admin UI, or the API directly.

API helper login:

```bash
AUTH_RESPONSE=$(
  curl -s -X POST "http://127.0.0.1:8080/api/v1/auth/login" \
    -H "Content-Type: application/json" \
    -d '{"email":"admin@routegate.local","password":"admin"}'
)

AUTH_TOKEN=$(echo "$AUTH_RESPONSE" | jq -r '.token // .accessToken // .sessionToken')
```

Create a dev server when needed:

```bash
SERVER_RESPONSE=$(
  curl -s -X POST "http://127.0.0.1:8080/api/v1/servers" \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{
      "name":"RG-73 Dev Server",
      "description":"Traffic usage dev scenario",
      "location":"local-dev",
      "provider":"routegate-dev",
      "publicIp":"127.0.0.1"
    }'
)

SERVER_ID=$(echo "$SERVER_RESPONSE" | jq -r '.id')
```

Create a dev VPN account when needed:

```bash
ACCOUNT_RESPONSE=$(
  curl -s -X POST "http://127.0.0.1:8080/api/v1/vpn-accounts" \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"displayName\":\"RG-73 Dev Account\",\"email\":\"rg73@example.local\",\"status\":\"active\",\"serverId\":\"$SERVER_ID\"}"
)

VPN_ACCOUNT_ID=$(echo "$ACCOUNT_RESPONSE" | jq -r '.id')
```

### 3. Create an Agent registration token

```bash
TOKEN_RESPONSE=$(
  curl -s -X POST "http://127.0.0.1:8080/api/v1/servers/$SERVER_ID/registration-token" \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -H "Content-Type: application/json"
)

REGISTRATION_TOKEN=$(echo "$TOKEN_RESPONSE" | jq -r '.registrationToken')
```

Copy the example config and inject the token:

```bash
cp deploy/examples/routegate-agent-dev-traffic.yaml /tmp/routegate-agent-dev-traffic.yaml
sed -i "s/replace-with-server-registration-token/$REGISTRATION_TOKEN/" /tmp/routegate-agent-dev-traffic.yaml
```

### 4. Write the baseline traffic counters

```bash
bash scripts/dev-traffic-usage.sh "$VPN_ACCOUNT_ID" 1000000 2000000
```

You can also use the Make target:

```bash
VPN_ACCOUNT_ID="$VPN_ACCOUNT_ID" RX_BYTES=1000000 TX_BYTES=2000000 make dev-traffic-usage
```

### 5. Start the Agent and keep it running

Run this in a separate terminal:

```bash
cd agent
go run ./cmd/routegate-agent -config /tmp/routegate-agent-dev-traffic.yaml
```

Wait until the Agent registers and sends at least one heartbeat. The first traffic snapshot only establishes the baseline.

### 6. Increase the counters

In the first terminal, write higher absolute counters:

```bash
bash scripts/dev-traffic-usage.sh "$VPN_ACCOUNT_ID" 1500000 2600000
```

Or with Make:

```bash
VPN_ACCOUNT_ID="$VPN_ACCOUNT_ID" RX_BYTES=1500000 TX_BYTES=2600000 make dev-traffic-usage
```

Within the next Agent heartbeat cycle, the Agent should report this delta:

```text
rxBytes: 500000
txBytes: 600000
totalBytes: 1100000
```

### 7. Verify through the Admin API

```bash
curl -s "http://127.0.0.1:8080/api/v1/vpn-accounts/$VPN_ACCOUNT_ID/traffic" \
  -H "Authorization: Bearer $AUTH_TOKEN" | jq
```

Expected result: the summary usage includes the reported delta.

You can also open the VPN account details in the Admin UI and check the existing `TrafficStatsPanel`.

## Notes

- The Agent also supports the route alias `POST /api/v1/agent/traffic-reports`, while the current client uses `POST /api/v1/agent/traffic-usage`.
- This scenario does not test real network traffic.
- This scenario does not test sing-box stats API integration.
- This scenario does not enforce traffic limits or suspend accounts.
- This scenario does not persist Agent-side counter state across restart.

## Follow-up candidates

- Real sing-box stats collector.
- Persistent Agent-side counter baseline.
- Traffic aggregation tables.
- Limit enforcement in config rendering/apply flow.
- User Portal traffic usage screen.
