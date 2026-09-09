# RouteGate VPN Client Compatibility Matrix

Reviewed: 2026-09-09

This matrix distinguishes connection compatibility from Routing Profile compatibility.

Compatibility is evaluated for the **selected client and the effective protocol together**. A standard node subscription and a native routing-policy import are separate delivery concerns: a client can connect correctly while still routing DIRECT/VPN/BLOCK traffic incorrectly.

| Client | Connection delivery | RouteGate routing delivery | Smart Routing state | Remaining client-side requirement |
| --- | --- | --- | --- | --- |
| Hiddify | sing-box JSON for VLESS | Routing Profile embedded in the same sing-box config | Full smart routing on validated VLESS path | Keep client TUN/routing behavior compatible with imported profile |
| sing-box | sing-box JSON for VLESS | Routing Profile embedded in the same sing-box config | Full smart routing on validated VLESS path | Normal client/runtime setup |
| v2rayN | Base64 standard share links | Separate native custom-rules URL: `/sub/<token>?format=v2rayn-routing` | Supported with client-side setup | Import/refresh the RouteGate rules source, activate the intended routing mode/profile, and verify DNS/TUN behavior |
| V2Box | Base64 standard share links | Native route-object import helper via `v2box://routes?multi=...` | Supported with client-side setup | Import the generated route set and verify local DNS/TUN/rule precedence; deep-link format remains manual-validation gated |
| V2RayTun | Base64 standard share links + `routing` header | Routing Profile serialized to subscription routing JSON | Partial compatibility on supported share-link protocols | Verify TUN and DNS runtime settings; these remain client-side |
| Other / unknown | RG-115 auto | Not assumed | Connection only | Select a known client before relying on Smart Routing |

## Capability detail

| Capability | Hiddify | v2rayN | V2Box | V2RayTun |
| --- | :---: | :---: | :---: | :---: |
| URI/subscription import | Yes | Yes | Yes | Yes |
| Full sing-box config import used by RouteGate | Yes | No | No | No |
| TUN functionality | Yes | Yes | Yes | Yes |
| DIRECT/VPN/BLOCK capability | Yes | Yes | Yes | Yes |
| RouteGate routing-policy artifact | Embedded config | Native custom-rules JSON URL | Native route objects / deep link | Subscription `routing` header |
| DNS/split-DNS capability | Yes | Yes | Yes | Yes |
| Connection subscription refresh | Yes | Yes | Yes | Yes |
| Routing-policy refresh/import | Same config refresh | Separate routing URL refresh | Re-import currently required | Same subscription refresh |
| Imported RouteGate routing precedence known | RouteGate config | Client routing profile governs once selected | Client-local runtime still governs | Subscription routing takes precedence |

## Protocol-aware fallback rule

RouteGate must never derive a strong routing status from client name alone.

- Hiddify and sing-box are `full_smart_routing` only when the effective protocol is VLESS and RouteGate can deliver the full sing-box representation.
- v2rayN, V2Box and V2RayTun use their documented share-link path for VLESS, Hysteria2 and Shadowsocks.
- If one of those clients is paired with WireGuard, MTProto, or another protocol without a validated adapter, the default `/sub/<token>` response uses RG-115 `auto`, which may fall back to raw protocol-native connection material, and compatibility becomes `connection_only`.
- V2RayTun's subscription `routing` header is emitted only on the validated share-link path; it is not attached to unsupported protocol combinations.

This rule prevents both impossible subscription representations and false Smart Routing claims when only connectivity material was delivered.

## Native routing adapters (RG-115B)

RG-115B does **not** introduce a second routing-policy engine. Native adapters serialize the already resolved RouteGate `RoutingProfile`.

### v2rayN

RouteGate exports an array compatible with v2rayN custom routing-rule import. The adapter mechanically maps:

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

v2rayN supports importing routing rules from a subscription URL. The administrator/user still has to associate/activate the imported routing profile and verify DNS/TUN settings, so RouteGate keeps the state `client_setup_required` until the runtime behavior is validated.

The QR rendered for this URL is a **desktop v2rayN routing-source transfer helper**, not a universal mobile routing QR. Scanning it in V2RayTun or V2Box must not be presented as applying RouteGate routing. Mobile clients use their own native delivery paths.

### V2RayTun

V2RayTun does not need a second RouteGate routing URL or a separate routing QR on the validated share-link path.

The normal opaque RG-115 subscription URL remains the only user-facing credential:

```text
https://vpn.example.com/sub/<opaque-token>
```

When the selected client is V2RayTun and the effective protocol supports share-link subscription delivery, RouteGate serializes the resolved Routing Profile and sends it in V2RayTun's native subscription `Routing` response header. V2RayTun receives the connection subscription and routing policy together.

Operational consequences:

- onboarding should use the normal secure subscription URL/QR;
- after changing a RouteGate Routing Profile, refresh the same subscription in V2RayTun;
- no second routing QR should be generated or required;
- TUN and DNS remain client-local runtime concerns, so native routing delivery alone does not promote V2RayTun to full smart-routing compatibility.

### V2Box

RouteGate converts the same policy into V2Box route objects and produces:

```text
v2box://routes?multi=<base64-json>
```

The adapter maps domain keyword/suffix/exact/GeoSite and CIDR/GeoIP conditions into V2Box's `Domain`/`IP` route-object fields with `direct`, `proxy`, or `block` tags.

The deep-link shape is supported by working community tooling but is not backed by a public official V2Box format specification available to RouteGate. Therefore this remains an **import helper** and `client_setup_required` until manual validation confirms behavior on supported V2Box releases.

## Manual marketplace validation

The motivating regression scenario is a RouteGate Routing Profile in which Russian marketplace traffic is DIRECT while unmatched traffic uses the VPN.

Validation must use the same account/profile and check at minimum:

1. Hiddify baseline: Ozon and Wildberries open with the full RouteGate sing-box profile.
2. v2rayN: import/refresh the RouteGate `v2rayn-routing` URL, activate the corresponding routing profile, then verify both Ozon and Wildberries.
3. V2Box: import the generated RouteGate route deep link, verify the imported rules are enabled/ordered as expected, then verify both Ozon and Wildberries.
4. V2RayTun: add/refresh the normal secure RouteGate subscription, verify the native subscription routing is present/effective, then verify both Ozon and Wildberries.
5. Confirm ordinary VPN-routed sites still use the VPN; fixing DIRECT marketplaces must not accidentally turn the client into global DIRECT mode.
6. If routing rules match but a marketplace still fails, inspect client DNS/TUN behavior separately before changing the shared RouteGate policy.

A client may only be promoted to a stronger compatibility state after this real-client validation passes deterministically.

## Validation sources

The matrix combines RouteGate automated tests, current client source/documentation, and observed manual behavior. Client releases may change behavior, so capability changes must be reviewed before promoting a compatibility state.

- v2rayN current source models custom routing as an ordered `RulesItem` list and supports importing that list from a configured subscription URL.
- V2Box current releases expose custom routing/DNS/Xray-TUN functionality; the native route/deep-link serialization used here follows a working community converter and remains manual-validation gated.
- V2RayTun documentation supports subscription routing headers and their precedence model.
- Hiddify is the current full-config baseline for this Smart Routing scenario.
