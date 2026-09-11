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

	// Guidance/limitation codes are stable, machine-readable identifiers for
	// product-facing compatibility copy. RouteGate requires full EN/RU
	// localization of user-facing text with no hardcoded prose, so the
	// backend never emits English sentences here: the frontend maps each
	// code to t('clientCompatibility.guidance.<code>') /
	// t('clientCompatibility.limitation.<code>').
	GuidanceHiddifyImportAccessLink     = "hiddify_import_access_link"
	GuidanceV2RayNRoutingMode           = "v2rayn_routing_mode"
	GuidanceV2RayNTunMode               = "v2rayn_tun_mode"
	GuidanceV2RayNGStandardSubscription = "v2rayng_standard_subscription"
	GuidanceGenericStandardConnection   = "generic_standard_connection"

	LimitationV2RayNNoRoutingRules       = "v2rayn_no_routing_rules"
	LimitationV2RayNGRoutingNotValidated = "v2rayng_routing_not_validated"
	LimitationGenericNoRoutingPolicy     = "generic_no_routing_policy"
	LimitationProtocolValidatedVLESSOnly = "protocol_validated_vless_only"
	LimitationProtocolNoShareLinkFormat  = "protocol_no_share_link_format"
	LimitationNoEffectiveProtocol        = "no_effective_protocol"
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
	GuidanceCodes           []string           `json:"guidanceCodes,omitempty"`
	LimitationCodes         []string           `json:"limitationCodes,omitempty"`
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
			GuidanceCodes: []string{GuidanceHiddifyImportAccessLink},
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
			GuidanceCodes:   []string{GuidanceV2RayNRoutingMode, GuidanceV2RayNTunMode},
			LimitationCodes: []string{LimitationV2RayNNoRoutingRules},
		}
	case ClientTypeV2RayNG:
		// v2rayNG is officially selectable for standard connection/subscription
		// delivery, but - unlike v2rayN - its native custom-routing-rules import
		// has not been independently validated on a real client. RouteGate must
		// not advertise unvalidated routing-policy compatibility ("no silent
		// downgrade"), so this stays connection_only until that validation
		// happens, and it must never appear in a "use X for RouteGate-managed
		// routing" recommendation; see docs/architecture/client-compatibility-matrix.md.
		return ClientCompatibilityAssessment{
			ClientType: ClientTypeV2RayNG, DisplayName: "v2rayNG", Status: ClientCompatibilityConnectionOnly,
			PreferredDeliveryFormat: SubscriptionDeliveryFormatBase64, RequiresClientSetup: true,
			Capabilities:    ClientCapabilities{URISubscriptionImport: true, ImportedRulePrecedence: ImportedRulePrecedenceUnknown},
			GuidanceCodes:   []string{GuidanceV2RayNGStandardSubscription},
			LimitationCodes: []string{LimitationV2RayNGRoutingNotValidated},
		}
	default:
		// Generic must not recommend v2rayNG (or any connection_only client)
		// for RouteGate-managed routing; only Hiddify (full) and v2rayN
		// (with client-side setup) have a validated routing path.
		return ClientCompatibilityAssessment{
			ClientType: ClientTypeGeneric, DisplayName: "Generic", Status: ClientCompatibilityConnectionOnly,
			PreferredDeliveryFormat: SubscriptionDeliveryFormatAuto, RequiresClientSetup: true,
			Capabilities:    ClientCapabilities{URISubscriptionImport: true, ImportedRulePrecedence: ImportedRulePrecedenceUnknown},
			GuidanceCodes:   []string{GuidanceGenericStandardConnection},
			LimitationCodes: []string{LimitationGenericNoRoutingPolicy},
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
		assessment.LimitationCodes = append(assessment.LimitationCodes, LimitationProtocolValidatedVLESSOnly)
		return assessment
	}

	// v2rayN and v2rayNG both only have a valid Base64 share-link
	// representation for protocols protocolSupportsShareLinkSubscription
	// recognizes (VLESS/Hysteria2/Shadowsocks). Without this check, a
	// WireGuard or MTProto account would still resolve to
	// SubscriptionDeliveryFormatBase64 - a format RouteGate cannot actually
	// render valid material for on that protocol - even though the
	// compatibility badge correctly says connection_only. This never
	// promotes v2rayNG's routing tier: its Status is already connection_only
	// from clientCompatibilityFor and stays that way either way.
	if normalizedClient == ClientTypeV2RayN || normalizedClient == ClientTypeV2RayNG {
		if !protocolSupportsShareLinkSubscription(protocol) {
			assessment.Status = ClientCompatibilityConnectionOnly
			assessment.PreferredDeliveryFormat = SubscriptionDeliveryFormatAuto
			assessment.RequiresClientSetup = true
			assessment.Capabilities.SubscriptionRoutingPolicy = false
			assessment.LimitationCodes = append(assessment.LimitationCodes, LimitationProtocolNoShareLinkFormat)
		}
	}

	return assessment
}

// clientCompatibilityForResolvedProtocol is the single compatibility truth
// shared by subscription delivery and the Access & Devices read model.
// protocolResolved distinguishes "the effective protocol is genuinely
// unknown" (e.g. the account has no server assignment yet) from an empty
// protocol argument used defensively elsewhere: when the caller could not
// resolve a usable effective protocol, this never overclaims by falling
// back to a client's bare (protocol-less) tier - it forces connection_only,
// the same conservative posture Generic and unvalidated clients already use.
func clientCompatibilityForResolvedProtocol(clientType, protocol string, protocolResolved bool) ClientCompatibilityAssessment {
	if protocolResolved {
		return clientCompatibilityForProtocol(clientType, protocol)
	}
	assessment := clientCompatibilityFor(clientType)
	if assessment.Status == ClientCompatibilityConnectionOnly {
		return assessment
	}
	assessment.Status = ClientCompatibilityConnectionOnly
	assessment.PreferredDeliveryFormat = SubscriptionDeliveryFormatAuto
	assessment.RequiresClientSetup = true
	assessment.Capabilities.SubscriptionRoutingPolicy = false
	assessment.LimitationCodes = append(assessment.LimitationCodes, LimitationNoEffectiveProtocol)
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
