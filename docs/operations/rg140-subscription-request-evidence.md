# RG-140 — what a subscription request does and does not prove

## Operator workflow: one import, safe client refresh

For routine protocol, Reality endpoint, node and staged-transfer cutover changes,
**keep the already imported device-scoped subscription URL**. Do **not** rotate
the token unless its bearer URL was lost or exposed.

In the supported third-party client, explicitly **refresh the existing
subscription**, select its updated entry, and reconnect. For a staged move,
verify the actual destination and exit IP in the client **before** authorizing
cleanup of old credentials.

In RouteGate **VPN Account → Access & Devices**, each active link now displays
the last subscription request for its **current token**, or
`No request observed`. Press **Check for new requests** to re-read this
timestamp from Manager. The button triggers a **read-only GET**, not a push
refresh, config apply, token change or client re-import.

This metadata is also shown for the older account-level subscription under
`Legacy account access`, when one is active.

## Evidence semantics and limitations

- The timestamp comes from `vpn_subscription_tokens.last_used_at` for the
  **currently active token**, not the lifetime
  `vpn_account_devices.last_used_at`. When the token is replaced, the new
  one begins with **no recorded requests**, regardless of activity on
  an older token. Revoked/absent links do not show a current request state.
- A request indicates **retrieval**, not that the downloaded response was
  valid, parsed, selected, or that the client established a usable VPN tunnel.
  Pending apply may still cause public subscription delivery to fail closed.
- A missing request can mean no refresh, delayed client polling, an outdated
  cached client configuration, an invalid/revoked URL or connectivity to the
  Manager. It is **not proof** that a client was offline.
- A successful HTTP request and a recent timestamp do **not** prove
  connection to the target node. The operator still needs to validate actual
  VPN traffic and exit address before source cleanup.
- The client controls any background refresh schedule. RouteGate cannot
  promise silent, zero-touch updates in arbitrary third-party VPN apps.
- Token revocation stops future subscription retrieval but **does not**
  by itself revoke previously downloaded VLESS/Reality credentials on a
  running node. That requires a separate, verified config apply.
- The request data contains no token plaintext, server private keys or
  subscription URL. Never post actual user links in issue logs.

## Scope

Read-model/UI improvements only; no backend schema or Agent changes, no
production deployment, and no FI instability experiment. Transfer safety,
rotation/revocation, and fail-closed public subscription delivery retain
their existing behavior.

Tracking: [RG-140 subscription continuity #509](https://github.com/ikaevus/RouteGate/issues/509)
and [RG-140 readiness #538](https://github.com/ikaevus/RouteGate/issues/538).
