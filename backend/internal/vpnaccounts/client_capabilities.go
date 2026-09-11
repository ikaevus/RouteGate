package vpnaccounts

import (
	"encoding/json"
	"strings"
)

const (
	// Officially supported client identities. RouteGate supports protocols
	// broadly, but officially supports VPN clients selectively: Hiddify is the
	// primary/recommended client, v2rayN/v2rayNG are the officially supported
	// desktop/Android members of the 2dust client family, and everything else
	// (V2RayTun, V2Box, Streisand, FoXray, Amnezia, ...) uses Generic
	// best-effort connectivity rather than a bespoke adapter.
	ClientTypeHiddify = "hiddify"
	ClientTypeV2RayN  = "v2rayn"
	ClientTypeV2RayNG = "v2rayng"
	ClientTypeSingBox = "sing-box"
	ClientTypeGeneric = "generic"

	// Deprecated (RG-115B): retained only so legacy persisted vpn_client_profiles
	// rows normalize instead of erroring. New devices must not select these;
	// normalizeClientType maps them to ClientTypeGeneric.
	ClientTypeV2RayTun = "v2raytun"
	ClientTypeV2Box    = "v2box"
	ClientTypeOther    = "other"

	ClientCompatibilityFullSmartRouting = "full_smart_routing"
	ClientCompatibilitySetupRequired    = "client_setup_required"
	ClientCompatibilityPartial          = "partial_compatibility"
	ClientCompatibilityConnectionOnly   = "connection_only"

	ImportedRulePrecedenceRouteGate = "routegate_config"
	ImportedRulePrecedenceClient    = "client_local"
	ImportedRulePrecedenceUnknown   = "unknown"
)

type ClientCapabilities struct {
	FullSingBoxConfigImport   bool   `json:"fullSingBoxConfigImport"`
	URISubscriptionImport     bool   `json:"uriSubscriptionImport"`
	TUNMode                   bool   `json:"tunMode"`
	DirectRouting             bool   `json:"directRouting"`
	VPNRouting                bool   `json:"vpnRouting"`
	BlockRouting              bool   `json:"blockRouting"`
	RemoteRuleSets            bool   `json:"remoteRuleSets"`
	DNSRouting                bool   `json:"dnsRouting"`
	SplitDNS                  bool   `json:"splitDns"`
	ClientLocalRules          bool   `json:"clientLocalRules"`
	SubscriptionRefresh       bool   `json:"subscriptionRefresh"`
	SubscriptionRoutingPolicy bool   `json:"subscriptionRoutingPolicy"`
	ImportedRulePrecedence    string `json:"importedRulePrecedence"`
}

type ClientCompatibilityAssessment struct {
	ClientType              string             `json:"clientType"`
	DisplayName             string             `json:"displayName"`
	Status                  string             `json:"status"`
	PreferredDeliveryFormat string             `json:"preferredDeliveryFormat"`
	RequiresClientSetup     bool               `json:"requiresClientSetup"`
	Capabilities            ClientCapabilities `json:"capabilities"`
	Guidance                []string           `json:"guidance,omitempty"`
	Limitations             []string           `json:"limitations,omitempty"`
}

func init() {
	// RG-115A/RG-115B extended the persisted client_type vocabulary without a
	// schema migration because vpn_client_profiles.client_type is already
	// textual. Keep legacy values accepted for existing rows while devices
	// (RG-116) use their own, narrower allow-list.
	allowedClientTypes[ClientTypeHiddify] = struct{}{}
	allowedClientTypes[ClientTypeV2RayNG] = struct{}{}
	allowedClientTypes[ClientTypeGeneric] = struct{}{}
}

// MarshalJSON enriches the existing client-connection API without changing
// persistence or duplicating routing policy. Compatibility is evaluated for
// both the selected client and the effective protocol so the API cannot claim
// full Smart Routing when the delivered representation only provides basic
// connectivity.
func (response ClientConnectionResponse) MarshalJSON() ([]byte, error) {
	type alias ClientConnectionResponse
	return json.Marshal(struct {
		alias
		ClientCompatibility ClientCompatibilityAssessment `json:"clientCompatibility"`
	}{
		alias:               alias(response),
		ClientCompatibility: clientCompatibilityForProtocol(response.Profile.ClientType, response.Protocol),
	})
}

func clientCompatibilityFor(clientType string) ClientCompatibilityAssessment {
	switch normalizeClientType(clientType) {
	case ClientTypeHiddify:
		return ClientCompatibilityAssessment{
			ClientType: ClientTypeHiddify, DisplayName: "Hiddify", Status: ClientCompatibilityFullSmartRouting,
			PreferredDeliveryFormat: SubscriptionDeliveryFormatSingBox,
			Capabilities: ClientCapabilities{
				FullSingBoxConfigImport: true, URISubscriptionImport: true, TUNMode: true,
				DirectRouting: true, VPNRouting: true, BlockRouting: true, RemoteRuleSets: true,
				DNSRouting: true, SplitDNS: true, ClientLocalRules: true, SubscriptionRefresh: true,
				ImportedRulePrecedence: ImportedRulePrecedenceRouteGate,
			},
			Guidance: []string{"Import the RouteGate subscription URL. Keep Hiddify routing/TUN settings compatible with the imported profile when using smart routing."},
		}
	case ClientTypeSingBox:
		return ClientCompatibilityAssessment{
			ClientType: ClientTypeSingBox, DisplayName: "sing-box", Status: ClientCompatibilityFullSmartRouting,
			PreferredDeliveryFormat: SubscriptionDeliveryFormatSingBox,
			Capabilities: ClientCapabilities{
				FullSingBoxConfigImport: true, URISubscriptionImport: true, TUNMode: true,
				DirectRouting: true, VPNRouting: true, BlockRouting: true, RemoteRuleSets: true,
				DNSRouting: true, SplitDNS: true, ClientLocalRules: true, SubscriptionRefresh: true,
				ImportedRulePrecedence: ImportedRulePrecedenceRouteGate,
			},
		}
	case ClientTypeV2RayN:
		return ClientCompatibilityAssessment{
			ClientType: ClientTypeV2RayN, DisplayName: "v2rayN", Status: ClientCompatibilitySetupRequired,
			PreferredDeliveryFormat: SubscriptionDeliveryFormatBase64, RequiresClientSetup: true,
			Capabilities: ClientCapabilities{
				URISubscriptionImport: true, TUNMode: true, DirectRouting: true, VPNRouting: true,
				BlockRouting: true, RemoteRuleSets: true, DNSRouting: true, SplitDNS: true,
				ClientLocalRules: true, SubscriptionRefresh: true, ImportedRulePrecedence: ImportedRulePrecedenceClient,
			},
			Guidance: []string{
				"Use a routing mode that preserves DIRECT/VPN intent (for example a whitelist/custom rules mode rather than Global when DIRECT rules are required).",
				"Enable TUN when system-wide routing is required and verify v2rayN DNS/routing rules do not override the RouteGate intent.",
			},
			Limitations: []string{"Standard URI subscriptions do not carry RouteGate sing-box routing rules; matching routing/DNS behavior must be configured in v2rayN."},
		}
	case ClientTypeV2RayNG:
		return ClientCompatibilityAssessment{
			ClientType: ClientTypeV2RayNG, DisplayName: "v2rayNG", Status: ClientCompatibilitySetupRequired,
			PreferredDeliveryFormat: SubscriptionDeliveryFormatBase64, RequiresClientSetup: true,
			Capabilities: ClientCapabilities{
				URISubscriptionImport: true, TUNMode: true, DirectRouting: true, VPNRouting: true,
				BlockRouting: true, DNSRouting: true, SplitDNS: true, ClientLocalRules: true,
				SubscriptionRefresh: true, ImportedRulePrecedence: ImportedRulePrecedenceClient,
			},
			Guidance:    []string{"v2rayNG is the Android member of the 2dust client family. Configure routing/DNS locally so DIRECT/VPN/BLOCK behavior matches the RouteGate routing profile."},
			Limitations: []string{"Standard URI subscriptions do not carry RouteGate routing rules; matching routing/DNS behavior must be configured in v2rayNG."},
		}
	default:
		return ClientCompatibilityAssessment{
			ClientType: ClientTypeGeneric, DisplayName: "Generic", Status: ClientCompatibilityConnectionOnly,
			PreferredDeliveryFormat: SubscriptionDeliveryFormatAuto, RequiresClientSetup: true,
			Capabilities: ClientCapabilities{URISubscriptionImport: true, ImportedRulePrecedence: ImportedRulePrecedenceUnknown},
			Guidance:     []string{"Standard VLESS/WireGuard/Shadowsocks/Hysteria2/MTProto connection material only. Select Hiddify, v2rayN, or v2rayNG for RouteGate-managed routing."},
			Limitations:  []string{"Only protocol-level connectivity is assumed for generic/unrecognized clients; RouteGate routing/DNS policy is not reproduced."},
		}
	}
}

// clientCompatibilityForProtocol narrows a client's general capability model to
// the representation RouteGate can actually deliver for the effective
// protocol. This is the enforcement truth exposed by the API and Admin UI.
func clientCompatibilityForProtocol(clientType, protocol string) ClientCompatibilityAssessment {
	assessment := clientCompatibilityFor(clientType)
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" {
		return assessment
	}

	normalizedClient := normalizeClientType(clientType)
	if (normalizedClient == ClientTypeHiddify || normalizedClient == ClientTypeSingBox) && protocol != ClientProtocolVLESS {
		assessment.Status = ClientCompatibilityConnectionOnly
		assessment.PreferredDeliveryFormat = SubscriptionDeliveryFormatAuto
		assessment.RequiresClientSetup = true
		assessment.Limitations = append(assessment.Limitations,
			"Full RouteGate smart routing delivery is currently validated for VLESS only; the selected protocol uses a connectivity fallback.")
		return assessment
	}

	if normalizedClient == ClientTypeV2RayN || normalizedClient == ClientTypeV2RayNG {
		if !protocolSupportsShareLinkSubscription(protocol) {
			assessment.Status = ClientCompatibilityConnectionOnly
			assessment.PreferredDeliveryFormat = SubscriptionDeliveryFormatAuto
			assessment.RequiresClientSetup = true
			assessment.Capabilities.SubscriptionRoutingPolicy = false
			assessment.Limitations = append(assessment.Limitations,
				"No validated client-specific subscription representation exists for the selected protocol; RouteGate falls back to protocol-native connectivity material.")
		}
	}

	return assessment
}

func protocolSupportsShareLinkSubscription(protocol string) bool {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case ClientProtocolVLESS, ClientProtocolHysteria2, ClientProtocolShadowsocks:
		return true
	default:
		return false
	}
}

// normalizeClientType maps any persisted or requested value to one of the
// officially supported client identities. Anything RouteGate does not treat
// as a first-class client (unknown values, and the retired V2RayTun/V2Box
// bespoke adapters) normalizes to Generic rather than erroring.
func normalizeClientType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case ClientTypeHiddify, ClientTypeV2RayN, ClientTypeV2RayNG, ClientTypeSingBox:
		return value
	default:
		return ClientTypeGeneric
	}
}

func detectClientTypeFromUserAgent(userAgent string) string {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	switch {
	case strings.Contains(ua, "hiddify"):
		return ClientTypeHiddify
	case strings.Contains(ua, "v2rayng"):
		return ClientTypeV2RayNG
	case strings.Contains(ua, "v2rayn"):
		return ClientTypeV2RayN
	case strings.Contains(ua, "sing-box") || strings.Contains(ua, "singbox"):
		return ClientTypeSingBox
	default:
		return ""
	}
}

func resolveSubscriptionClientType(profile ClientProfile, userAgent string) string {
	selected := normalizeClientType(profile.ClientType)
	if selected != ClientTypeGeneric {
		return selected
	}
	if detected := detectClientTypeFromUserAgent(userAgent); detected != "" {
		return detected
	}
	return ClientTypeGeneric
}

func preferredDeliveryFormatForClient(clientType, protocol string) string {
	return clientCompatibilityForProtocol(clientType, protocol).PreferredDeliveryFormat
}
