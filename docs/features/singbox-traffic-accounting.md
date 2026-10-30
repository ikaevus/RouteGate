# Native sing-box traffic collection

The Dashboard traffic chart reads Manager's daily usage rollup. Previously,
Agent only read externally produced JSON snapshots and production installers
disabled collection. An online VPN session does not imply traffic accounting.

## Implementation

- Agent reads cumulative user counters from the active sing-box config's V2Ray
  gRPC statistics API. The API address must be numeric loopback with a nonzero
  port. Requests have a three-second timeout and never reset runtime counters.
- Only users explicitly enabled in the active statistics config are accepted.
  Both uplink and downlink must be present; duplicate/negative counters fail.
- Manager adds the loopback API to newly rendered sing-box configs only when
  the Agent reports `trafficAccounting.enabled=true` and the installed runtime
  advertises `with_v2ray_api`. VLESS and sing-box Shadowsocks users use account
  IDs, matching the existing Agent traffic ingestion API.
- Existing file integrations remain usable when the active runtime does not
  configure the API. An API error is not masked with unrelated file counters.

## Validation

The Agent now requires Go 1.26, matching the repository's CI/release toolchain,
to use the current fixed gRPC/HTTP2 dependencies. Dependency vulnerability checks
for the changed Agent paths pass with Go 1.26.10.

Run Agent traffic tests, plus the disposable runtime integration test:

```sh
go install -tags with_v2ray_api github.com/sagernet/sing-box/cmd/sing-box@v1.12.12
ROUTEGATE_TEST_SINGBOX="$(go env GOPATH)/bin/sing-box" \
  go test -race -v ./internal/traffic -run TestSingBoxRuntimeTraffic -count=1
```

The integration test starts separate server/client processes on loopback and
transfers an HTTP payload through VLESS before reading account counter deltas.
It also verifies that an idle poll does not repeat the same usage.

## Rollout and remaining acceptance work

This change does not enable accounting on existing production Agents or apply
VPN configs. Before rollout, verify the deployed runtime build tags, install
the updated Agent and Manager, enable `traffic_collection_enabled`, wait for a
capability heartbeat, then render/check/apply a new config through the normal
workflow. Applying a VPN config may disconnect existing client sessions.

Production acceptance requires an actual client transfer, accepted Agent
reports, database daily rollup growth, and Dashboard values from the API.
The optional local runtime test can fail in restricted sandboxes where
sing-box cannot initialize its network monitor; CI runs it on an Ubuntu host.

Agent persists its baseline and immutable pending batch to a root-readable
outbox beside its config file before sending. It retries pending reports before
collecting new counters, including after Agent restarts. An acknowledgement
advances a durable stream sequence. Manager serializes each stream checkpoint
in the same transaction as the usage events and daily rollup. Identical retries
are acknowledged without inserting usage; changed payloads, sequence gaps, and
old sequences fail closed. Checkpoints survive raw traffic retention.

The new `/api/v1/agent/traffic-batches` endpoint is intentional: an older
Manager returns 404 without inserting usage, rather than silently ignoring
idempotency fields on the legacy endpoint. Deploy Manager and migration 160
before enabling new Agents. Existing legacy producers remain supported.

Initial observations establish a baseline; a sing-box counter reset also
establishes a new baseline. Traffic before these observations is not recovered.
This is operational usage telemetry, not complete billing-grade accounting.
Native Hysteria2 and WireGuard collection are separate follow-up work.
