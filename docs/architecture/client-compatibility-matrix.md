# RouteGate VPN Client Compatibility Matrix

Reviewed: 2026-09-14

This matrix distinguishes connection compatibility from Routing Profile compatibility.

Compatibility is evaluated for the **selected client and the effective protocol together**. A standard node subscription and a native routing-policy import are separate delivery concerns: a client can connect correctly while still routing DIRECT/VPN/BLOCK traffic incorrectly.

RouteGate supports protocols broadly, but officially supports VPN clients selectively (see [RG-116 Access & Devices](access-devices.md)). Membership in the same client family (2dust: v2rayN/v2rayNG) does **not** imply the same routing-policy compatibility tier — each client's Smart Routing state reflects only what RouteGate can actually confirm for that exact client, and "adapter implemented and automated-test-covered" is not the same claim as "real-client runtime behavior independently validated":

| Client | Connection delivery | RouteGate routing delivery | Smart Routing state | Remaining client-side requirement |
| --- | --- | --- | --- | --- |
| Hiddify (recommended) | sing-box JSON for VLESS | Routing Profile embedded in the same sing-box config | Full smart routing on validated VLESS path | Keep client TUN/routing behavior compatible with imported profile |
| HAPP | Standard Base64/share-link subscription; real-client validated for VLESS/Reality + Shadowsocks from one opaque RouteGate URL | Provider-managed `routing: happ://routing/onadd/<base64-json>` header generated from the resolved Routing Profile | Native adapter implemented and automated-test-covered; real-client Smart Routing acceptance pending | Refresh/re-add the HAPP subscription and validate DIRECT/VPN/BLOCK plus DNS/TUN behavior before promotion to Full RouteGate |
| sing-box | sing-box JSON for VLESS | Routing Profile embedded in the same sing-box config | Full smart routing on validated VLESS path | Normal client/runtime setup |
| v2rayN (desktop) | Base64 standard share links | Separate native custom-rules URL: `/sub/<token>?format=v2rayn-routing` (adapter implemented, automated tests; manual real-client marketplace validation remains pending, historically tracked in #392) | Supported with client-side setup | Import/refresh the RouteGate rules source, activate the intended routing mode/profile, and verify DNS/TUN behavior |
| v2rayNG (Android) | Base64 standard share links | Not offered — routing-rules import has **not** been independently validated on this client | Connection only (Smart Routing not claimed) | Standard subscription import/refresh only; select Hiddify or v2rayN for RouteGate-managed routing |
| Generic (any other client: V2RayTun, V2Box, Streisand, FoXray, Amnezia, ...) | Standard VLESS/WireGuard/Shadowsocks/Hysteria2/MTProto material | Not assumed | Connection only | Select Hiddify or v2rayN before relying on Smart Routing |

## Capability detail

| Capability | Hiddify | HAPP | v2rayN | v2rayNG | Generic |
| --- | :---: | :---: | :---: | :---: | :---: |
| URI/subscription import | Yes | Yes — real-client validated | Yes | Yes | Yes |
| Full sing-box config import used by RouteGate | Yes | No | No | No | No |
| TUN functionality | Yes | Client feature; RouteGate runtime behavior pending acceptance | Yes | Yes | Client-dependent |
| DIRECT/VPN/BLOCK capability | Yes | Native adapter implemented; runtime acceptance pending | Yes | Client-dependent (not validated by RouteGate) | Client-dependent |
| RouteGate routing-policy artifact | Embedded config | Provider-managed HAPP routing profile in the same subscription response | Native custom-rules JSON URL | Not offered | Not offered |
| DNS/split-DNS capability | Yes | Not claimed by RouteGate yet | Yes | Client-dependent | Client-dependent |
| Connection subscription refresh | Yes | Yes | Yes | Yes | Client-dependent |
| Routing-policy refresh/import | Same config refresh | Same subscription refresh; stable profile name lets HAPP update the bound routing profile | Separate routing URL refresh | N/A | N/A |
| Imported RouteGate routing precedence known | RouteGate config | Unknown for overlapping cross-action rules | Client routing profile governs once selected | N/A | N/A |

## HAPP real-client acceptance

HAPP is a first-class RouteGate **connection client**, not a Generic guess. On 2026-09-13 a real HAPP iOS client successfully imported one opaque RouteGate HTTPS subscription URL and exposed two independent selectable profiles from the same VPN account:

- VLESS / TCP / Reality;
- Shadowsocks / TCP.

Both protocol paths were manually confirmed working. This validates the multi-profile subscription behavior RouteGate expects: one stable access URL can represent more than one active protocol without requiring the user to create separate subscriptions.

This does **not** mean HAPP sends traffic through both protocols simultaneously. They are independent profiles presented by the client, and the user selects the active one.

RouteGate therefore treats HAPP as `partial_compatibility` for the manually validated VLESS/Reality and Shadowsocks paths. HAPP's own protocol support may include additional share-link protocols, but those are not promoted to a RouteGate-validated state until they complete the same real-client acceptance. WireGuard and MTProto are not represented as HAPP Base64 share-link paths by RouteGate and fall back to protocol-native delivery rather than receiving a false HAPP compatibility claim.

The next acceptance stage is Smart Routing. RouteGate now attaches a provider-managed HAPP routing profile to the same subscription response, but implementation is not equivalent to runtime proof. HAPP remains unvalidated for actual RouteGate-managed DIRECT/VPN/BLOCK behavior, DNS behavior, and precedence when rules of different RouteGate actions overlap. A real-client acceptance pass must prove those behaviors before the routing tier can be promoted.

## Protocol-aware fallback rule

RouteGate must never derive a strong routing status from client name alone — including inferring it from *client family* (2dust) rather than the exact client.

- Hiddify and sing-box are `full_smart_routing` only when the effective protocol is VLESS and RouteGate can deliver the full sing-box representation.
- HAPP is `partial_compatibility` on the real-client validated VLESS/Reality and Shadowsocks connection paths. On share-link protocols, RouteGate can attach the provider-managed HAPP routing artifact; that fact does not promote Smart Routing until real-client behavior is confirmed. Hysteria2 remains `connection_only` until RouteGate real-client acceptance is performed. WireGuard/MTProto use protocol-native fallback and do not receive a false HAPP routing claim.
- v2rayN uses its standard share-link subscription path for compatible protocols, with the native routing-rules URL as an additional client-side import: the adapter and its rule-mapping are implemented and covered by automated tests, but real-client runtime routing behavior (does the imported profile actually make Ozon/Wildberries DIRECT on a real device) has not yet been independently validated — manual runtime validation remains pending (historically tracked in #392); see "Manual marketplace validation" below.
- v2rayNG uses the same standard share-link subscription path, but the native routing-rules import is **not** offered to it and its Smart Routing state stays `connection_only` until that is independently validated on a real v2rayNG client — it is not inherited from v2rayN just because both are 2dust clients.
- If a client/protocol pair has no validated representation, compatibility must fall back to `connection_only` rather than claiming Smart Routing enforcement. This is the default for Generic and, currently, for v2rayNG.

This rule prevents both impossible subscription representations and false Smart Routing claims when only connectivity material was delivered.

## Native routing adapters

RouteGate does **not** introduce a second routing-policy engine. Native adapters serialize the already resolved RouteGate `RoutingProfile`.

### HAPP

HAPP documents provider-managed routing profiles delivered either in a subscription body or with the HTTP `routing` response header. RouteGate uses the header so the already validated Base64 node subscription remains unchanged:

```text
routing: happ://routing/onadd/<base64-json>
```

The adapter mechanically maps the resolved RouteGate profile:

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

The profile name is deliberately stable (`RouteGate`). HAPP documents that importing a profile with an existing name updates it, and subscription-bound profiles have their own lifecycle; using a stable name prevents a RouteGate display-name change from leaving a stale HAPP routing profile behind.

RouteGate uses HAPP's documented `IPIfNonMatch` domain strategy and does not inject an independent DNS policy. GeoIP/GeoSite URLs are left empty so HAPP may use its own defaults rather than RouteGate silently choosing a third-party ruleset.

HAPP's currently published profile schema groups matchers by action but does not document a representation for RouteGate's arbitrary cross-action rule priorities. RouteGate therefore exposes `ImportedRulePrecedence=unknown` and keeps HAPP below `full_smart_routing` until overlapping-rule precedence is demonstrated on a real client. This is a deliberate no-silent-downgrade boundary rather than a claim that ordinary non-overlapping DIRECT/VPN/BLOCK rules cannot work.

Upstream references:

- https://github.com/HappDev/happ_su/blob/main/dev-docs/routing.md
- https://github.com/HappDev/happ_su/blob/main/dev-docs/app-management.md

### v2rayN

RouteGate exports an array compatible with v2rayN's custom routing-rule import. The adapter mechanically maps:

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

The administrator/user still has to associate/activate the imported routing profile and verify DNS/TUN settings, so RouteGate keeps the state `client_setup_required` until the runtime behavior is validated per device.

This format string is named after the v2rayN adapter that produces it, not a promise that every 2dust client can consume it. **v2rayNG is not offered this format as its preferred/native representation** and its compatibility tier does not assume it works there — see the next section.

### v2rayNG

v2rayNG is the Android member of the 2dust client family and shares much of its engine with v2rayN, but RouteGate has not independently validated that v2rayNG's own custom-routing-rules import behaves the same way on a real device. Per the protocol-aware fallback rule, RouteGate does not extend v2rayN's `client_setup_required` tier to v2rayNG on that assumption alone.

v2rayNG is therefore:

- officially selectable as a device client type for standard connection/subscription delivery (Base64 share links, `format=auto`/`base64`);
- classified `connection_only` — RouteGate does not claim any Routing Profile enforcement for it;
- a candidate for promotion to `client_setup_required` (reusing the same `v2rayn-routing` artifact, or a dedicated one) once that native-routing behavior is manually validated on a real v2rayNG client and this document is updated accordingly — see "Manual marketplace validation" below.

### Generic clients (V2RayTun, V2Box, and others)

RouteGate does not maintain client-specific routing adapters or QR/deep-link flows for these clients. They receive standard connection material only (`format=auto`/`base64`/`raw`, or a protocol-specific format such as `sing-box`/`wireguard`) and are classified `connection_only`. This was previously RG-115B's V2RayTun/V2Box native-routing work (issue #398, PR #404); see [RG-116](access-devices.md#rg-115b-retirement) for why it was retired rather than kept as a first-class adapter.

## Manual marketplace validation

The motivating regression scenario is a RouteGate Routing Profile in which Russian marketplace traffic is DIRECT while unmatched traffic uses the VPN.

Validation must use the same account/profile and check at minimum:

1. Hiddify baseline: Ozon and Wildberries open with the full RouteGate sing-box profile.
2. HAPP: refresh/re-add a HAPP-scoped RouteGate subscription, confirm the subscription-bound RouteGate routing profile is present/active, then verify Ozon and Wildberries are DIRECT.
3. HAPP: confirm an ordinary unmatched site still uses the VPN, and verify one safe BLOCK rule if the selected Routing Profile contains one.
4. v2rayN: import/refresh the RouteGate `v2rayn-routing` URL, activate the corresponding routing profile, then verify both Ozon and Wildberries.
5. Confirm ordinary VPN-routed sites still use the VPN; fixing DIRECT marketplaces must not accidentally turn the client into global DIRECT mode.
6. If routing rules match but a marketplace still fails, inspect client DNS/TUN behavior separately before changing the shared RouteGate policy.

A client may only be promoted to a stronger compatibility state after this real-client validation passes deterministically. **HAPP has completed connection/multi-profile acceptance and now has an implemented native routing adapter, but has not completed Smart Routing runtime acceptance. Neither v2rayN nor v2rayNG has completed Smart Routing validation yet** (for v2rayN, manual runtime validation remains pending — historically tracked in #392, which is closed; the validation itself is the open item, not the issue). v2rayN's adapter, mapping, and automated tests are implemented and keep it at `client_setup_required` on that basis; v2rayNG additionally lacks even an adapter/native-import claim and stays `connection_only`.

## Validation sources

The matrix combines RouteGate automated tests, current client source/documentation, and observed manual behavior. Client releases may change behavior, so capability changes must be reviewed before promoting a compatibility state.

- HAPP connection acceptance: real RouteGate iOS validation on 2026-09-13 confirmed one opaque subscription importing working VLESS/Reality and Shadowsocks profiles.
- HAPP routing delivery: upstream HAPP documentation explicitly supports subscription-bound routing profiles and the `routing: happ://routing/onadd/<base64-json>` HTTP response header; RouteGate's adapter and mapping are automated-test-covered, while real-client RouteGate Smart Routing behavior remains pending.
- v2rayN current source models custom routing as an ordered `RulesItem` list and supports importing that list from a configured subscription URL; RouteGate's adapter and its automated tests are built against that documented format, but real-client marketplace behavior has **not** been independently validated yet (manual runtime validation remains pending, historically tracked in #392 - the issue itself is closed).
- v2rayNG's equivalent behavior is believed similar (same upstream engine) but has **not** been validated by RouteGate at all — no adapter is offered and no claim beyond `connection_only` is made; treat this as unverified, not as evidence of compatibility.
- Hiddify is the current full-config baseline for this Smart Routing scenario.
