# Subscription HTTP 503: safe Manager triage (RG-140)

A client (Hiddify, v2rayN, etc.) importing `https://<manager-host>/sub/<opaque-token>` can receive HTTP 503 while its previously imported VPN profile still connects. A saved client profile may keep working even if a *fresh* subscription cannot be rendered. These are not proof of a node outage, token corruption or configuration drift.

**Deployment boundary:** US stays Hybrid/Manager; the FI and RU VPN nodes remain separate. The Manager is the correct host for the subscription URL even when the client tunnel connects directly to FI. This procedure is read-only: no config apply, service restart, token rotation, migration or client deletion.

## 1. Correlate the failing request without disclosing its bearer

On the **US Manager**, soon after the user reports a 503:

```bash
sudo journalctl -u routegate-manager --since '-15 minutes' --no-pager -o cat \
  | grep -E 'render client subscription( payload| headers)? failed' \
  | tail -n 15
```

Do not paste the full subscription URL, reverse proxy access-log URI, raw config, VLESS UUID, Reality private key, or subscription token. Redact other sensitive fields from diagnostic output.

A Manager build with this change includes a secret-safe `reason_code` in the existing warning events:

| `reason_code` | Meaning | Admin next action |
| --- | --- | --- |
| `awaiting_first_apply` | Node has no confirmed first apply | Inspect the node's Deployments; do not share the default public subscription yet |
| `awaiting_apply` | Account/protocol not in applied access | Inspect applied vs desired protocol set before considering a scoped apply |
| `unavailable` | Connection generation or topology is unavailable for another reason | Inspect the accompanying error on US; validate applied data, never guess a key |
| `unassigned` | Account has no assigned node | Check account assignment |
| `payload_render_failed` | Delivery format rendering failed | Check client/format and last applied state |
| `headers_render_failed` | Subscription metadata failed | Investigate Manager delivery metadata |
| `internal_error` | Unknown Manager/DB error | Investigate Manager/service status without changing VPN nodes |

Older builds log the same three warning messages but do **not** include `reason_code`; they require inspecting their existing `error` field. If no matching Manager warning appears, **do not infer that nginx caused the 503**. Verify upstream origin using safe, request-correlated service/proxy diagnostics without dumping bearer URLs.

## 2. Compare desired and last applied (read-only)

Use the Manager's existing authenticated client readiness/status view or the version-matched read-only diagnostics (see `docs/operations/applied-client-settings/README.md` and the read-only US readiness workflow in PR #517). Respect the live schema and release version; old pre/postflight SQL is **not** automatically valid on every deployment.

Check: account active and assigned, primary + active protocol set, active applied config version, presence of account/protocol in the applied snapshot, and any pending changes. Never use an unapplied saved key to generate a default public subscription. The presence of an active VPN session from an older imported client is independent evidence about the runtime, **not** proof of current Manager delivery readiness.

## 3. Classify before making changes

- If the account is actually served by its last applied version but the Manager withholds the entire subscription because a different protocol is pending, document the exact protocol mismatch; address continuity only with scoped tests and security review.
- If the account was never deployed, keep the normal subscription fail-closed. Any **explicit** pre-import must be designed as an authenticated admin-only flow with truthful not-ready labeling (issue #538).
- If the error comes from the Manager or nginx for another reason, fix that component rather than applying the FI VPN configuration blindly.
- Verify recovery with a fresh subscription import and separate route validation (external IP through FI vs through home ISP); Hiddify's UI may show a timeout even while traffic works.

Related: [RG-140 readiness and preliminary import #538](https://github.com/ikaevus/RouteGate/issues/538), [subscription continuity #509](https://github.com/ikaevus/RouteGate/issues/509).
