# RG-140: Explicit preliminary VLESS import

## Purpose

A new VPN account can be created before its assigned node successfully
applies a configuration containing the account's VLESS access. The ordinary
`/sub/<token>` endpoint correctly remains fail-closed until the node
confirms the apply. Instead of encouraging users to import a subscription
that returns HTTP 503, administrators may explicitly request a **preliminary,
non-subscription, VLESS/Reality share URI**.

This is **not** a ready access state, automatic subscription, privileged
deployment operation or a replacement for an Agent-confirmed apply.

## Flow

1. Create and activate a VPN account, assign a VPN node, ensure VLESS is the
   saved desired protocol, and save valid VLESS/Reality node settings.
2. In **VPN Accounts → Protocols**, RouteGate displays the pending
   apply guidance. The optional preliminary import control is available only
   while VLESS is desired and not currently served.
3. After acknowledging the unconfirmed/possibly obsolete credentials, the
   authorized operator may view/copy a **direct `vless://` URI**. The user
   can manually import it if the chosen client supports VLESS URI import.
   An import may succeed while traffic still cannot connect.
4. Apply the config using normal guarded RouteGate/Agent workflows. When
   the node confirms the apply, share the **normal subscription link** from
   Access & Devices. The preliminary URI is static and **does not refresh**:
   if saved endpoint/keys/protocol settings changed, it can be obsolete and
   must not be treated as a sustainable subscription.
5. The preview vanishes from the page when hidden or when navigating away;
   no subscription token is created or rotated. Previously deployed accounts
   should use the normal subscription, never the preliminary preview.

## Security invariants

- `POST /api/v1/vpn-accounts/{id}/client-connection/pre-import` requires
  authentication and **both** `vpn_users:update` and `configs:read`.
- Strict one-object JSON body:
  `{"acknowledgeUnapplied":true}`; otherwise HTTP 400.
- Fail closed (HTTP 409) for suspended, revoked, created-but-not-active,
  expired or unassigned accounts; unknown applied state; already deployed
  VLESS; no saved node settings; non-VLESS desired protocols; incompatible
  node deployment role; or unrenderable VLESS parameters.
- Only the standard VLESS Reality share URI is supported in this first
  phase. It uses saved settings for a future apply, but **always describes
  TCP**, which is the runtime transport the current sing-box adapter
  actually deploys. This response deliberately does not offer WireGuard
  private keys or any other protocol material.
- Sensitive response headers: `Cache-Control: no-store`,
  `Pragma: no-cache`, `Referrer-Policy: no-referrer`.
  The interface keeps the URI only in local ephemeral component state;
  no query cache, localStorage or browser URL parameter.
- Success audit records contain only account ID, protocol name and the
  unapplied-preview status, **never** the VLESS link/UUID/public key.
  The URI itself is a credential; don't paste it in support tickets.
- `GET /client-connection`, device links, portal access and
  `GET /sub/<token>` stay **unchanged**: last applied access only.
  Normal revocation/status/expiry checks remain in force.
- No server-side deployment, config render/apply, token rotation, protocol
  toggle or client modification occurs when previewing.

## Limitations and follow-up

- A preliminary URI is meant for an operator who understands that the
  server might never apply the saved configuration. Do not represent it as
  Ready in public or client UI. Hiddify and other VPN clients generally
  cannot distinguish a valid syntax from a actually running protocol.
- This phase does **not** promise one-time import with automatic updates
  before apply; that would require a separate explicitly permitted
  pre-import subscription lifecycle, including reliable revocation,
  readiness transition and support across multiple protocols. Keep #538
  open to evaluate that UX separately.
- FI intermittent traffic stalls under issue #541 are independent of
  this preview feature; do not equate successful import with a working
  tunnel.

Related: [RG-140 #538](https://github.com/ikaevus/RouteGate/issues/538);
[FI reliability #541](https://github.com/ikaevus/RouteGate/issues/541).
