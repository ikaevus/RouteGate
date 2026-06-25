#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  bash scripts/dev-traffic-usage.sh <vpn-account-id> <rx-bytes> <tx-bytes> [traffic-file]

Writes a RouteGate Agent file-collector traffic counter payload for local development.
The counters are absolute values. Keep the Agent process running and increase these
values between reports so the in-memory delta tracker can emit usage deltas.

Environment:
  ROUTEGATE_DEV_TRAFFIC_FILE  Optional default traffic file path.
                              Defaults to /tmp/routegate-dev-traffic-usage.json.

Example:
  bash scripts/dev-traffic-usage.sh "$VPN_ACCOUNT_ID" 1000000 2000000
  bash scripts/dev-traffic-usage.sh "$VPN_ACCOUNT_ID" 1500000 2600000
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ "$#" -lt 3 || "$#" -gt 4 ]]; then
  usage >&2
  exit 2
fi

VPN_ACCOUNT_ID="$1"
RX_BYTES="$2"
TX_BYTES="$3"
TRAFFIC_USAGE_FILE="${4:-${ROUTEGATE_DEV_TRAFFIC_FILE:-/tmp/routegate-dev-traffic-usage.json}}"

if [[ -z "$VPN_ACCOUNT_ID" ]]; then
  echo "vpn-account-id must not be empty" >&2
  exit 2
fi

if [[ ! "$RX_BYTES" =~ ^[0-9]+$ ]]; then
  echo "rx-bytes must be a non-negative integer" >&2
  exit 2
fi

if [[ ! "$TX_BYTES" =~ ^[0-9]+$ ]]; then
  echo "tx-bytes must be a non-negative integer" >&2
  exit 2
fi

json_escape() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

mkdir -p "$(dirname "$TRAFFIC_USAGE_FILE")"
ESCAPED_VPN_ACCOUNT_ID="$(json_escape "$VPN_ACCOUNT_ID")"

cat >"$TRAFFIC_USAGE_FILE" <<JSON
{
  "counters": [
    {
      "vpnAccountId": "$ESCAPED_VPN_ACCOUNT_ID",
      "rxBytes": $RX_BYTES,
      "txBytes": $TX_BYTES,
      "metadata": {
        "source": "routegate_dev_traffic_usage",
        "scenario": "RG-73"
      }
    }
  ]
}
JSON

cat <<EOF
Updated RouteGate dev traffic counters:
  file: $TRAFFIC_USAGE_FILE
  vpnAccountId: $VPN_ACCOUNT_ID
  rxBytes: $RX_BYTES
  txBytes: $TX_BYTES
EOF
