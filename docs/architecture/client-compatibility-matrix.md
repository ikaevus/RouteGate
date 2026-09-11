# RouteGate VPN Client Compatibility Matrix

Reviewed: 2026-09-11

This matrix distinguishes connection compatibility from Routing Profile compatibility.

Compatibility is evaluated for the **selected client and the effective protocol together**. A standard node subscription and a native routing-policy import are separate delivery concerns: a client can connect correctly while still routing DIRECT/VPN/BLOCK traffic incorrectly.

RouteGate supports protocols broadly, but officially supports VPN clients selectively (see [RG-116 Access & Devices](access-devices.md)):

| Client | Connection delivery | RouteGate routing delivery | Smart Routing state | Remaining client-side requirement |
| --- | --- | --- | --- | --- |
| Hiddify (recommended) | sing-box JSON for VLESS | Routing Profile embedded in the same sing-box config | Full smart routing on validated VLESS path | Keep client TUN/routing behavior compatible with imported profile |
| sing-box | sing-box JSON for VLESS | Routing Profile embedded in the same sing-box config | Full smart routing on validated VLESS path | Normal client/runtime setup |
| v2rayN (desktop) | Base64 standard share links | Separate native custom-rules URL: `/sub/<token>?format=v2rayn-routing` | Supported with client-side setup | Import/refresh the RouteGate rules source, activate the intended routing mode/profile, and verify DNS/TUN behavior |
| v2rayNG (Android) | Base64 standard share links | Same `v2rayn-routing` custom-rules URL (2dust client family) | Supported with client-side setup | Same as v2rayN: import/refresh the rules source and verify DNS/TUN behavior |
| Generic (any other client: V2RayTun, V2Box, Streisand, FoXray, Amnezia, ...) | Standard VLESS/WireGuard/Shadowsocks/Hysteria2/MTProto material | Not assumed | Connection only | Select Hiddify, v2rayN, or v2rayNG before relying on Smart Routing |

## Capability detail

| Capability | Hiddify | v2rayN | v2rayNG | Generic |
| --- | :---: | :---: | :---: | :---: |
| URI/subscription import | Yes | Yes | Yes | Yes |
| Full sing-box config import used by RouteGate | Yes | No | No | No |
| TUN functionality | Yes | Yes | Yes | Client-dependent |
| DIRECT/VPN/BLOCK capability | Yes | Yes | Yes | Client-dependent |
| RouteGate routing-policy artifact | Embedded config | Native custom-rules JSON URL | Native custom-rules JSON URL | Not offered |
| DNS/split-DNS capability | Yes | Yes | Yes | Client-dependent |
| Connection subscription refresh | Yes | Yes | Yes | Client-dependent |
| Routing-policy refresh/import | Same config refresh | Separate routing URL refresh | Separate routing URL refresh | N/A |
| Imported RouteGate routing precedence known | RouteGate config | Client routing profile governs once selected | Client routing profile governs once selected | N/A |

## Protocol-aware fallback rule

RouteGate must never derive a strong routing status from client name alone.

- Hiddify and sing-box are `full_smart_routing` only when the effective protocol is VLESS and RouteGate can deliver the full sing-box representation.
- v2rayN and v2rayNG use their standard share-link subscription paths for compatible protocols, with the native routing-rules URL as an additional client-side import.
- If a client/protocol pair has no validated representation, compatibility must fall back to `connection_only` rather than claiming Smart Routing enforcement. This is the default for Generic.

This rule prevents both impossible subscription representations and false Smart Routing claims when only connectivity material was delivered.

## Native routing adapters

RouteGate does **not** introduce a second routing-policy engine. Native adapters serialize the already resolved RouteGate `RoutingProfile`.

### v2rayN / v2rayNG

RouteGate exports an array compatible with v2rayN/v2rayNG custom routing-rule import. The adapter mechanically maps:

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

### Generic clients (V2RayTun, V2Box, and others)

RouteGate does not maintain client-specific routing adapters or QR/deep-link flows for these clients. They receive standard connection material only (`format=auto`/`base64`/`raw`, or a protocol-specific format such as `sing-box`/`wireguard`) and are classified `connection_only`. This was previously RG-115B's V2RayTun/V2Box native-routing work (issue #398, PR #404); see [RG-116](access-devices.md#rg-115b-retirement) for why it was retired rather than kept as a first-class adapter.

## Manual marketplace validation

The motivating regression scenario is a RouteGate Routing Profile in which Russian marketplace traffic is DIRECT while unmatched traffic uses the VPN.

Validation must use the same account/profile and check at minimum:

1. Hiddify baseline: Ozon and Wildberries open with the full RouteGate sing-box profile.
2. v2rayN/v2rayNG: import/refresh the RouteGate `v2rayn-routing` URL, activate the corresponding routing profile, then verify both Ozon and Wildberries.
3. Confirm ordinary VPN-routed sites still use the VPN; fixing DIRECT marketplaces must not accidentally turn the client into global DIRECT mode.
4. If routing rules match but a marketplace still fails, inspect client DNS/TUN behavior separately before changing the shared RouteGate policy.

A client may only be promoted to a stronger compatibility state after this real-client validation passes deterministically.

## Validation sources

The matrix combines RouteGate automated tests, current client source/documentation, and observed manual behavior. Client releases may change behavior, so capability changes must be reviewed before promoting a compatibility state.

- v2rayN/v2rayNG current source models custom routing as an ordered `RulesItem` list and supports importing that list from a configured subscription URL.
- Hiddify is the current full-config baseline for this Smart Routing scenario.
