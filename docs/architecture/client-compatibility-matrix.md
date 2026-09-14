# RouteGate VPN Client Compatibility Matrix

Reviewed: 2026-09-14

This matrix distinguishes connection compatibility from Routing Profile compatibility.

Compatibility is evaluated for the **selected client and the effective protocol together**. A standard node subscription and a native routing-policy import are separate delivery concerns: a client can connect correctly while still routing DIRECT/VPN/BLOCK traffic incorrectly.

RouteGate supports protocols broadly, but officially supports VPN clients selectively (see [RG-116 Access & Devices](access-devices.md)). Membership in the same client family (2dust: v2rayN/v2rayNG) does **not** imply the same routing-policy compatibility tier. Each client's Smart Routing state reflects only what RouteGate can actually confirm for that exact client.

| Client | Connection delivery | RouteGate routing delivery | Smart Routing state | Remaining client-side requirement |
| --- | --- | --- | --- | --- |
| Hiddify (recommended) | sing-box JSON for VLESS | Routing Profile embedded in the same sing-box config | Full smart routing on validated VLESS path | Keep client TUN/routing behavior compatible with imported profile |
| HAPP | Standard Base64/share-link subscription; real-client validated for VLESS/Reality + Shadowsocks from one opaque RouteGate URL | **Disabled.** RouteGate does not emit HAPP provider-managed routing after failed iOS safety acceptance | Connection compatibility validated; RouteGate-managed Smart Routing not claimed | Standard HAPP subscription only |
| sing-box | sing-box JSON for VLESS | Routing Profile embedded in the same sing-box config | Full smart routing on validated VLESS path | Normal client/runtime setup |
| v2rayN (desktop) | Base64 standard share links | Separate native custom-rules URL: `/sub/<token>?format=v2rayn-routing` | Supported with client-side setup; manual runtime validation pending | Import/refresh the RouteGate rules source, activate the intended routing mode/profile, and verify DNS/TUN behavior |
| v2rayNG (Android) | Base64 standard share links | Not offered — routing-rules import has not been independently validated on this client | Connection only | Standard subscription import/refresh only; select Hiddify or v2rayN for RouteGate-managed routing |
| Generic | Standard VLESS/WireGuard/Shadowsocks/Hysteria2/MTProto material | Not assumed | Connection only | Select Hiddify or v2rayN before relying on Smart Routing |

## Capability detail

| Capability | Hiddify | HAPP | v2rayN | v2rayNG | Generic |
| --- | :---: | :---: | :---: | :---: | :---: |
| URI/subscription import | Yes | Yes — real-client validated | Yes | Yes | Yes |
| Full sing-box config import used by RouteGate | Yes | No | No | No | No |
| TUN functionality | Yes | Client feature; not managed by RouteGate | Yes | Yes | Client-dependent |
| DIRECT/VPN/BLOCK capability claimed by RouteGate | Yes | **No while safety-disabled** | Yes | No | No |
| RouteGate routing-policy artifact | Embedded config | **Not emitted** | Native custom-rules JSON URL | Not offered | Not offered |
| DNS/split-DNS capability claimed by RouteGate | Yes | No | Yes | No | No |
| Connection subscription refresh | Yes | Yes | Yes | Yes | Client-dependent |
| Routing-policy refresh/import | Same config refresh | N/A while disabled | Separate routing URL refresh | N/A | N/A |
| Imported RouteGate routing precedence known | RouteGate config | N/A | Client routing profile governs once selected | N/A | N/A |

## HAPP real-client acceptance

HAPP is a first-class RouteGate **connection client**, not a Generic guess. On 2026-09-13 a real HAPP iOS client successfully imported one opaque RouteGate HTTPS subscription URL and exposed two independent selectable profiles from the same VPN account:

- VLESS / TCP / Reality;
- Shadowsocks / TCP.

Both protocol paths were manually confirmed working. This validates the multi-profile subscription behavior RouteGate expects: one stable access URL can represent more than one active protocol without requiring separate subscriptions.

This does **not** mean HAPP sends traffic through both protocols simultaneously. They are independent profiles and the user selects the active one.

RouteGate therefore keeps HAPP as `partial_compatibility` for the manually validated VLESS/Reality and Shadowsocks connection paths. Hysteria2 remains `connection_only` until it completes real-client acceptance. WireGuard and MTProto use protocol-native fallback and are not represented as HAPP Base64 share-link paths.

### Managed-routing safety result

On 2026-09-14 RouteGate performed real-device acceptance of HAPP's documented provider-managed routing mechanism (`routing: happ://routing/onadd/<base64-json>`). The subscription and VLESS tunnel could be created, but client traffic timed out. After disconnecting VPN, the iOS device could temporarily remain unable to reach the RouteGate site until the local network state was reset with Airplane Mode.

The RouteGate server remained healthy throughout: public HTTP, Manager, Agent, nginx, sing-box and the active protocol set all validated successfully. The failure was therefore treated as a **client-side safety failure**, not as evidence that the server or VLESS transport was unavailable.

As a result:

- RouteGate no longer emits the HAPP `Routing` response header;
- HAPP remains supported for the validated standard connection subscription;
- `SubscriptionRoutingPolicy`, DIRECT/VPN/BLOCK and RouteGate-managed DNS capabilities are not advertised for HAPP;
- the existing HAPP routing serializer may remain in the codebase for isolated research/tests, but it is not part of production delivery;
- managed HAPP routing must not be re-enabled globally until a canary/opt-in implementation proves deterministic recovery and does not leave iOS networking in a failed state.

## Protocol-aware fallback rule

RouteGate must never derive a strong routing status from client name alone — including inferring it from client family rather than the exact client.

- Hiddify and sing-box are `full_smart_routing` only when the effective protocol is VLESS and RouteGate can deliver the full sing-box representation.
- HAPP is `partial_compatibility` on the manually validated VLESS/Reality and Shadowsocks connection paths, with **no RouteGate-managed routing artifact currently delivered**. Hysteria2 remains `connection_only`; WireGuard/MTProto use protocol-native fallback.
- v2rayN uses its standard share-link subscription path for compatible protocols, with the native routing-rules URL as an additional client-side import. The adapter and mapping are automated-test-covered, but real-client marketplace routing validation remains pending.
- v2rayNG uses the standard share-link subscription path, but the native routing-rules import is not offered and its Smart Routing state stays `connection_only` until independently validated.
- If a client/protocol pair has no validated representation, compatibility falls back to `connection_only` rather than claiming Smart Routing enforcement.

## Native routing adapters

RouteGate does **not** introduce a second routing-policy engine. Native adapters serialize the already resolved RouteGate `RoutingProfile`.

### HAPP

HAPP upstream documents provider-managed routing profiles delivered in a subscription body or through the HTTP `routing` response header. RouteGate implemented and automated-tested a serializer for that documented representation, but production delivery is **safety-disabled** after the failed real-device acceptance described above.

The dormant adapter maps the resolved RouteGate profile mechanically:

- `DIRECT` domains / IPs -> `DirectSites` / `DirectIp`;
- `VPN` domains / IPs -> `ProxySites` / `ProxyIp`;
- `BLOCK` domains / IPs -> `BlockSites` / `BlockIp`;
- exact domains -> `full:`;
- domain suffixes -> `domain:`;
- domain keywords -> `keyword:`;
- GeoSite values -> `geosite:`;
- CIDRs unchanged;
- GeoIP values -> `geoip:`;
- unmatched traffic -> VPN through `GlobalProxy: "true"`.

This description documents the experimental serializer only. **No HAPP routing header is emitted in the production subscription path while the safety disable is active.**

Upstream references:

- https://github.com/HappDev/happ_su/blob/main/dev-docs/routing.md
- https://github.com/HappDev/happ_su/blob/main/dev-docs/app-management.md

### v2rayN

RouteGate exports an array compatible with v2rayN's custom routing-rule import. The adapter maps:

- `DIRECT` -> `direct`
- `VPN` -> `proxy`
- `BLOCK` -> `block`
- exact domains -> `full:`
- domain suffixes -> `domain:`
- domain keywords -> `keyword:`
- GeoSite values -> `geosite:`
- CIDRs unchanged
- GeoIP values -> `geoip:`

A final all-ports `proxy` rule makes the RouteGate default (`unmatched -> VPN`) explicit instead of inheriting an arbitrary local routing mode.

The same RG-115 bearer credential is reused:

```text
https://vpn.example.com/sub/<opaque-token>?format=v2rayn-routing
```

The administrator/user still has to associate/activate the imported routing profile and verify DNS/TUN settings, so RouteGate keeps the state `client_setup_required` until runtime behavior is validated per device.

### v2rayNG

v2rayNG is officially selectable for standard connection/subscription delivery, but RouteGate has not independently validated native custom-routing-rules import on a real v2rayNG device. It therefore remains `connection_only` and does not inherit v2rayN's stronger compatibility tier simply because both are 2dust clients.

### Generic clients

RouteGate does not maintain client-specific routing adapters for Generic clients. They receive standard connection material only and are classified `connection_only`.

## Manual marketplace validation

The motivating Smart Routing regression scenario is a Routing Profile in which Russian marketplace traffic is DIRECT while unmatched traffic uses the VPN.

Current validation policy:

1. Hiddify remains the full-config baseline: Ozon and Wildberries should open through DIRECT while unmatched traffic remains VPN-routed.
2. HAPP managed-routing testing is **paused**. Do not ask normal users to import RouteGate-managed HAPP routing again while the safety disable is active.
3. Any future HAPP experiment must be opt-in/canary, isolate the routing artifact from the connection subscription, and include deterministic rollback/recovery of the client network state.
4. v2rayN may continue separate manual validation using the explicit `v2rayn-routing` URL.
5. Fixing DIRECT marketplaces must never accidentally turn a client into global DIRECT mode.

A client may only be promoted to a stronger compatibility state after real-client validation passes deterministically.

## Validation sources

The matrix combines RouteGate automated tests, current client source/documentation, and observed manual behavior. Client releases may change behavior, so capability changes must be reviewed before promoting a compatibility state.

- HAPP connection acceptance: real RouteGate iOS validation on 2026-09-13 confirmed one opaque subscription importing working VLESS/Reality and Shadowsocks profiles.
- HAPP managed-routing acceptance: failed on a real iOS device on 2026-09-14; production routing delivery is therefore disabled even though the serializer follows upstream's documented profile representation.
- v2rayN current source models custom routing as an ordered rules list and supports importing that list from a configured subscription URL; RouteGate's adapter is automated-test-covered, while real-client marketplace behavior remains pending.
- v2rayNG equivalent behavior is not validated by RouteGate; no routing adapter is offered and no claim beyond `connection_only` is made.
- Hiddify is the current full-config baseline for Smart Routing.
