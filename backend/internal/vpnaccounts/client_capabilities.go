package vpnaccounts

import (
	"encoding/json"
	"strings"
)

const (
	ClientTypeHiddify  = "hiddify"
	ClientTypeV2RayN   = "v2rayn"
	ClientTypeV2RayTun = "v2raytun"
	ClientTypeV2Box    = "v2box"
	ClientTypeSingBox  = "sing-box"
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
	FullSingBoxConfigImport    bool   `json:"fullSingBoxConfigImport"`
	URISubscriptionImport      bool   `json:"uriSubscriptionImport"`
	TUNMode                    bool   `json:"tunMode"`
	DirectRouting              bool   `json:"directRouting"`
	VPNRouting                 bool   `json:"vpnRouting"`
	BlockRouting               bool   `json:"blockRouting"`
	RemoteRuleSets             bool   `json:"remoteRuleSets"`
	DNSRouting                 bool   `json:"dnsRouting"`
	SplitDNS                   bool   `json:"splitDns"`
	ClientLocalRules           bool   `json:"clientLocalRules"`
	SubscriptionRefresh        bool   `json:"subscriptionRefresh"`
	SubscriptionRoutingPolicy  bool   `json:"subscriptionRoutingPolicy"`
	ImportedRulePrecedence     string `json:"importedRulePrecedence"`
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
	// RG-115A extends the persisted client_type vocabulary without a schema
	// migration because vpn_client_profiles.client_type is already textual.
	allowedClientTypes[ClientTypeHiddify] = struct{}{}
}

// MarshalJSON enriches the existing client-connection API without changing
// persistence or duplicating routing policy. The compatibility assessment is
// computed from the selected client profile at response time.
func (response ClientConnectionResponse) MarshalJSON() ([]byte, error) {
	type alias ClientConnectionResponse
	return json.Marshal(struct {
		alias
		ClientCompatibility ClientCompatibilityAssessment `json:"clientCompatibility"`
	}{
		alias:               alias(response),
		ClientCompatibility: clientCompatibilityFor(response.Profile.ClientType),
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
	case ClientTypeV2Box:
		return ClientCompatibilityAssessment{
			ClientType: ClientTypeV2Box, DisplayName: "V2Box", Status: ClientCompatibilitySetupRequired,
			PreferredDeliveryFormat: SubscriptionDeliveryFormatBase64, RequiresClientSetup: true,
			Capabilities: ClientCapabilities{
				URISubscriptionImport: true, TUNMode: true, DirectRouting: true, VPNRouting: true,
				BlockRouting: true, DNSRouting: true, SplitDNS: true, ClientLocalRules: true,
				SubscriptionRefresh: true, ImportedRulePrecedence: ImportedRulePrecedenceClient,
			},
			Guidance: []string{"Configure V2Box routing and DNS locally so DIRECT/VPN/BLOCK behavior matches the RouteGate routing profile."},
			Limitations: []string{"RouteGate cannot guarantee imported-rule precedence through a standard URI subscription; local V2Box rules may override routing intent."},
		}
	case ClientTypeV2RayTun:
		return ClientCompatibilityAssessment{
			ClientType: ClientTypeV2RayTun, DisplayName: "V2RayTun", Status: ClientCompatibilityPartial,
			PreferredDeliveryFormat: SubscriptionDeliveryFormatBase64, RequiresClientSetup: true,
			Capabilities: ClientCapabilities{
				URISubscriptionImport: true, TUNMode: true, DirectRouting: true, VPNRouting: true,
				BlockRouting: true, DNSRouting: true, SplitDNS: true, ClientLocalRules: true,
				SubscriptionRefresh: true, SubscriptionRoutingPolicy: true,
				ImportedRulePrecedence: ImportedRulePrecedenceRouteGate,
			},
			Guidance: []string{"RouteGate sends the Routing Profile through V2RayTun's subscription routing header. Verify TUN and DNS settings on the device when system-wide or split-DNS behavior is required."},
			Limitations: []string{"Routing rules are subscription-managed, but V2RayTun TUN/DNS runtime settings remain client-side and can affect deterministic DNS behavior."},
		}
	default:
		return ClientCompatibilityAssessment{
			ClientType: ClientTypeOther, DisplayName: "Other / unknown", Status: ClientCompatibilityConnectionOnly,
			PreferredDeliveryFormat: SubscriptionDeliveryFormatAuto, RequiresClientSetup: true,
			Capabilities: ClientCapabilities{URISubscriptionImport: true, ImportedRulePrecedence: ImportedRulePrecedenceUnknown},
			Guidance: []string{"Select a known VPN client profile before relying on RouteGate smart routing."},
			Limitations: []string{"Only protocol-level connectivity is assumed for unknown clients."},
		}
	}
}

func normalizeClientType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case ClientTypeHiddify, ClientTypeV2RayN, ClientTypeV2RayTun, ClientTypeV2Box, ClientTypeSingBox:
		return value
	default:
		return ClientTypeOther
	}
}

func detectClientTypeFromUserAgent(userAgent string) string {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	switch {
	case strings.Contains(ua, "hiddify"):
		return ClientTypeHiddify
	case strings.Contains(ua, "v2rayn"):
		return ClientTypeV2RayN
	case strings.Contains(ua, "v2raytun") || strings.Contains(ua, "v2ray-tun"):
		return ClientTypeV2RayTun
	case strings.Contains(ua, "v2box"):
		return ClientTypeV2Box
	case strings.Contains(ua, "sing-box") || strings.Contains(ua, "singbox"):
		return ClientTypeSingBox
	default:
		return ""
	}
}

func resolveSubscriptionClientType(profile ClientProfile, userAgent string) string {
	selected := normalizeClientType(profile.ClientType)
	if selected != ClientTypeOther {
		return selected
	}
	if detected := detectClientTypeFromUserAgent(userAgent); detected != "" {
		return detected
	}
	return ClientTypeOther
}

func preferredDeliveryFormatForClient(clientType, protocol string) string {
	assessment := clientCompatibilityFor(clientType)
	if assessment.PreferredDeliveryFormat == SubscriptionDeliveryFormatSingBox && strings.TrimSpace(protocol) != ClientProtocolVLESS {
		return SubscriptionDeliveryFormatAuto
	}
	return assessment.PreferredDeliveryFormat
}
