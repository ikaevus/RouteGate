# RouteGate VPN Client Compatibility Matrix

Reviewed: 2026-09-09

This matrix distinguishes connection compatibility from Routing Profile compatibility.

Compatibility is evaluated for the **selected client and the effective protocol together**. The table below describes the validated VLESS/share-link path. When the effective protocol has no validated client-specific subscription representation, RouteGate downgrades the state to `connection_only` and falls back to RG-115 `auto`/protocol-native material instead of claiming Smart Routing enforcement.

| Client | Default RouteGate delivery | Smart Routing state | RouteGate-delivered routing | Client-side requirement |
| --- | --- | --- | --- | --- |
| Hiddify | sing-box JSON for VLESS | Full smart routing on validated VLESS path | Existing sing-box Routing Profile renderer | Keep client TUN/routing behavior compatible with imported profile |
| sing-box | sing-box JSON for VLESS | Full smart routing on validated VLESS path | Existing sing-box Routing Profile renderer | Normal client/runtime setup |
| v2rayN | Base64 standard share links | Supported with client-side setup on supported share-link protocols | No complete Routing Profile in standard URI subscription | Choose routing mode/custom rules and DNS/TUN settings that preserve RouteGate DIRECT/VPN/BLOCK intent |
| V2Box | Base64 standard share links | Supported with client-side setup on supported share-link protocols | No complete Routing Profile in standard URI subscription | Configure local routing and DNS consistently with RouteGate policy |
| V2RayTun | Base64 standard share links + `routing` header | Partial compatibility on supported share-link protocols | Routing Profile is serialized to subscription routing JSON | Verify TUN and DNS runtime settings; these remain client-side |
| Other / unknown | RG-115 auto | Connection only | Not assumed | Select a known client before relying on Smart Routing |

## Capability detail

| Capability | Hiddify | v2rayN | V2Box | V2RayTun |
| --- | :---: | :---: | :---: | :---: |
| URI/subscription import | Yes | Yes | Yes | Yes |
| Full sing-box config import used by RouteGate | Yes | No | No | No |
| TUN functionality | Yes | Yes | Yes | Yes |
| DIRECT/VPN/BLOCK capability | Yes | Yes | Yes | Yes |
| Routing policy delivered by current RouteGate adapter | Yes on VLESS | No | No | Yes on supported share-link protocols |
| DNS/split-DNS capability | Yes | Yes | Yes | Yes |
| Subscription refresh | Yes | Yes | Yes | Yes |
| Imported RouteGate routing precedence known | RouteGate config | Client-local routing governs | Client-local routing governs | Subscription routing takes precedence |

## Protocol-aware fallback rule

RouteGate must never derive a strong routing status from client name alone.

- Hiddify and sing-box are `full_smart_routing` only when the effective protocol is VLESS and RouteGate can deliver the full sing-box representation.
- v2rayN, V2Box and V2RayTun use their documented share-link path for VLESS, Hysteria2 and Shadowsocks.
- If one of those clients is paired with WireGuard, MTProto, or another protocol without a validated adapter, the default `/sub/<token>` response uses RG-115 `auto`, which may fall back to raw protocol-native connection material, and compatibility becomes `connection_only`.
- V2RayTun's subscription `routing` header is emitted only on the validated share-link path; it is not attached to unsupported protocol combinations.

This rule prevents both HTTP 400 regressions from an impossible Base64 representation and false “Full Smart Routing” claims when only connectivity material was delivered.

## Deterministic RouteGate behavior

### Hiddify

For a VLESS profile, the opaque `/sub/<token>` URL resolves to the RouteGate sing-box representation. RouteGate Routing Profile rules are rendered through the same core renderer used elsewhere. This is the preferred initial full Smart Routing path.

For a non-VLESS effective protocol, RouteGate does not claim full routing-policy enforcement merely because Hiddify can establish a connection. The compatibility state becomes `connection_only` unless a protocol-specific adapter is validated later.

### v2rayN

For supported standard share-link protocols, the URL resolves to a Base64 share-link subscription. Connection settings are delivered automatically, but a standard VLESS/Hysteria2/Shadowsocks URI does not contain the complete RouteGate Routing Profile. RouteGate therefore reports `client_setup_required`. The administrator must select/configure a v2rayN routing mode and DNS/TUN behavior that implements the documented policy; Global mode must not be assumed compatible with profiles containing DIRECT rules.

For protocols without a validated share-link adapter, RouteGate falls back to protocol-native connectivity material and reports `connection_only`.

### V2Box

For supported standard share-link protocols, the URL resolves to a Base64 share-link subscription. V2Box exposes flexible routing, DNS and custom Xray/TUN settings, but RouteGate currently does not have a validated subscription-native route-policy transport for V2Box. RouteGate therefore reports `client_setup_required` and never claims the Routing Profile is automatically enforced.

For protocols without a validated share-link adapter, RouteGate falls back to protocol-native connectivity material and reports `connection_only`.

### V2RayTun

For supported share-link protocols, the URL resolves to a Base64 share-link subscription and RouteGate also emits V2RayTun's supported `routing` response header. The header is mechanically generated from the resolved RouteGate Routing Profile and uses `direct`, `proxy`, and `block` outbound tags. V2RayTun documentation states subscription routing takes precedence over local routing and Direct Service.

The status remains `partial_compatibility`: routing policy is centrally delivered, but TUN and DNS runtime behavior is not encoded by this adapter and must be verified on the client.

For protocols without a validated share-link adapter, the routing header is not emitted and RouteGate reports `connection_only`.

## Validation sources

The matrix combines RouteGate automated tests with current vendor/project documentation and observed manual behavior. Client releases may change behavior, so capability changes must be reviewed before promoting a compatibility state.

- Hiddify project/documentation: subscription import and sing-box configuration support.
- v2rayN project wiki: standard subscriptions; client-local routing/TUN configuration remains significant.
- V2Box current App Store release information: custom DNS, flexible routing, custom JSON and Xray TUN configuration.
- V2RayTun documentation: supported subscription headers, including Base64/raw body, profile refresh and subscription `routing` precedence.

A client may only be promoted to a stronger compatibility state when RouteGate can test and document the required behavior without silently relying on unspecified local settings.
