# ADR-0010: Staged account transfer without subscription rotation

- **Status:** Proposed — RG-140, not implemented
- **Date:** 2026-10-05
- **Related:** [RG-140](https://github.com/ikaevus/RouteGate/issues/509), [RG-139](https://github.com/ikaevus/RouteGate/issues/510), ADR-0009

## Problem

A device-scoped `/sub/<opaque-token>` resolves a VPN account and then its
current `vpn_accounts.server_id`. The token is not tied to the node. This
correctly preserves the URL across US ↔ FI changes, but the existing
`UpdateAccount` changes `server_id` **before** rendering/applying the
target. Current client access consequently fails closed while the target
applied snapshot lacks the account. The old node may still accept stale
credentials. An installed third-party VPN client has no guaranteed
instantaneous subscription refresh.

The goal is **one initial import, a preserved bearer URL, and a
controlled/observable transfer**, not automatic link-token rotation or an
unverifiable zero-downtime promise.

## Proposed durable state machine

```text
READY_ON_SOURCE
  → REQUESTED (source+target pinned, client/device inventory snapshot)
  → TARGET_STAGING (source remains authoritative; target renders credentials)
  → TARGET_APPLYING (Agent result + applied version proof)
  → TARGET_READY (actual runtime/port/health checks)
  → CUTOVER (atomic canonical assignment change; token unchanged)
  → CLIENT_REFRESH_PENDING (source retained as fallback, refresh evidence)
  → SOURCE_CLEANUP_READY (explicit admin acknowledgement/strict policy)
  → SOURCE_CLEANUP_APPLYING (render/validate/apply old node)
  → COMPLETE

Any pre-cutover failure → FAILED / CANCELLED (source still authoritative);
post-cutover failure → bounded ROLLBACK to known-good source if it remains
deployed, otherwise guided recovery. Every transition is idempotent/durable.
```

## Invariants

1. **Only one in-flight transfer per account.** Persist stable account ID,
   previous node, target node, expected config versions, transition timestamps,
   last error and operator identity. Reject conflicting changes under
   account/server locks, including direct/manual assignment and automatic
   selection. Neither path may bypass transfer guards.
2. **Staging is not cutover.** While target is being prepared, client
   subscriptions continue to resolve the existing, proven source. Extend the
   target renderer/account deployment snapshot only through explicitly staged
   account membership; do **not** mutate `vpn_accounts.server_id` early.
   Staging must not accidentally expose saved-but-unapplied target secrets.
3. **Confirm target readiness.** Agent-confirmed apply to the expected
   config version, inclusion of the account and desired protocol(s) in the
   target's applied snapshot, a healthy runtime/listener and required public
   reachability before canonical cutover. An already-running service alone
   is insufficient. Limit initial implementation to a verified protocol
   (e.g. VLESS/Reality) and fail closed for unsupported pairs.
4. **Atomic cutover.** The canonical assignment changes only when the
   target's proof matches the staged intention. An existing active device
   token stays exactly the same; `/sub/<token>` resolves a target endpoint
   from the applied target version. Account-level and device tokens are
   unchanged, and unrelated accounts/devices are unaffected.
5. **Client update evidence is not client installation evidence.**
   A device `last_used_at` proves the subscription was requested, **not**
   that a third-party app activated the new profile. Background refresh is
   not guaranteed for Hiddify, v2rayNG, v2rayN or iOS. Explicitly show
   `refreshed / not observed / unknown` rather than asserting full cutover.
6. **Retain old credentials until cleanup is safe.** Source credentials
   from the last applied version may still serve clients on cached configs.
   A timeout by itself is not proof of client migration. Require admin
   acknowledgement with affected-device list, service/traffic signals and
   documented rollback risk before removal; never keep two active nodes
   indefinitely without an explicit lifecycle/watchdog.
7. **Secrets and identity.** Continue one-time plaintext link issuance,
   hash-only token storage, no bearer URL in audit logs, and no redirection
   from old/revoked tokens to new tokens. Signed/verified Agent apply results
   are security evidence; frontend success banners aren't enough.
8. **Durability.** Survive Manager restart, Agent disconnect, stalled
   tasks, failed apply, concurrent reassignment, rejected protocol,
   maintenance/update locks, and config rollback. Every action is scoped,
   observable, cancellable when safe, and restartable without token churn.

## UI flow (Guided Workflow / Next Action First)

One `Move account` action, with current node, destination, protocol
compatibility, expected user impact and a preflight summary; show distinct
stages and **exactly one safe next action** at a time.

- Before cutover: `Prepare target`; source stays active.
- After target proof: `Switch subscription endpoint`; no new link or QR.
- After cutover: `Check client refresh` and `Clean up source`, with explicit
  warning for devices never observed refreshing.
- On failure: show rollback/retry options and last proven source/target,
  not a misleading completed badge.
- Standard settings/routing forms must not directly write `server_id`
  behind the new safe workflow when an active deployed account is moving.

## Tests and rollout gates

- DB integration: source US→FI→US, concurrent requests and locks, staged
  target render/apply, failed target apply, successful target cutover, token
  identity, no early client material, delayed/failed source cleanup, restart.
- External tests: VLESS Reality on actual US/FI with `fi-test` only; preserve
  existing Android/iPhone subscriptions, refresh manually, inspect
  `last_used_at`, validate exit IP and observe old cached configurations.
- Failure injection: offline target Agent, unavailable Manager, DNS/port
  block, rollback, unresponsive third-party client. Do not move real users
  or call the flow seamless until verified.
- No automatic forced OS/network/agent changes as part of account transfer.

## Non-goals

- Issuing new device URLs on every move.
- Rescuing users who only possess a foreign Hiddify Manager URL without
  controlling that legacy endpoint or a separately trusted enrollment path.
- Promising push-replaced URLs in unmodified third-party VPN clients.
- Automatic unattended failover while client refresh and cleanup semantics
  remain unverified.

**Implementation is pending.** Current manual `server_id` reassignment
continues to have a subscription refresh interruption window and must remain
a guarded/manual operation until this design is implemented and tested.
