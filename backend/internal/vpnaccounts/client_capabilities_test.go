package vpnaccounts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClientCompatibilityMatrixInitialClients(t *testing.T) {
	tests := []struct {
		clientType string
		status     string
		format     string
		setup      bool
	}{
		{ClientTypeHiddify, ClientCompatibilityFullSmartRouting, SubscriptionDeliveryFormatSingBox, false},
		{ClientTypeV2RayN, ClientCompatibilitySetupRequired, SubscriptionDeliveryFormatBase64, true},
	}
	for _, test := range tests {
		t.Run(test.clientType, func(t *testing.T) {
			assessment := clientCompatibilityFor(test.clientType)
			if assessment.Status != test.status || assessment.PreferredDeliveryFormat != test.format || assessment.RequiresClientSetup != test.setup {
				t.Fatalf("unexpected compatibility assessment: %+v", assessment)
			}
			if !assessment.Capabilities.URISubscriptionImport || !assessment.Capabilities.SubscriptionRefresh {
				t.Fatalf("official client must support subscription import/refresh: %+v", assessment.Capabilities)
			}
		})
	}
	if got := clientCompatibilityFor(ClientTypeGeneric).Status; got != ClientCompatibilityConnectionOnly {
		t.Fatalf("generic status = %q, want connection_only", got)
	}
}

// TestV2RayNGRoutingCompatibilityStaysConservativeUntilValidated guards
// against re-introducing the unvalidated claim that v2rayNG can consume the
// same native routing-rules URL as v2rayN merely because both are 2dust
// clients. Until that is independently validated on a real v2rayNG client,
// "no silent downgrade" requires connection_only for RouteGate-managed
// routing - standard subscription delivery may still be offered.
func TestV2RayNGRoutingCompatibilityStaysConservativeUntilValidated(t *testing.T) {
	assessment := clientCompatibilityFor(ClientTypeV2RayNG)
	if assessment.Status != ClientCompatibilityConnectionOnly {
		t.Fatalf("v2rayNG status = %q, want connection_only until routing is validated", assessment.Status)
	}
	if assessment.Capabilities.SubscriptionRoutingPolicy {
		t.Fatal("v2rayNG must not claim SubscriptionRoutingPolicy support")
	}
	if assessment.Capabilities.ImportedRulePrecedence == ImportedRulePrecedenceClient || assessment.Capabilities.ImportedRulePrecedence == ImportedRulePrecedenceRouteGate {
		t.Fatalf("v2rayNG imported rule precedence = %q, want unknown (no validated routing import claim)", assessment.Capabilities.ImportedRulePrecedence)
	}
	if assessment.PreferredDeliveryFormat != SubscriptionDeliveryFormatBase64 {
		t.Fatalf("v2rayNG preferred format = %q, want base64 (standard subscription delivery is still valid)", assessment.PreferredDeliveryFormat)
	}
}

func TestRetiredClientTypesNormalizeToGeneric(t *testing.T) {
	for _, legacy := range []string{ClientTypeV2RayTun, ClientTypeV2Box, ClientTypeOther, "unknown-client"} {
		if got := normalizeClientType(legacy); got != ClientTypeGeneric {
			t.Fatalf("normalizeClientType(%q) = %q, want generic", legacy, got)
		}
		if got := clientCompatibilityFor(legacy).ClientType; got != ClientTypeGeneric {
			t.Fatalf("clientCompatibilityFor(%q).ClientType = %q, want generic", legacy, got)
		}
	}
}

func TestPreferredDeliveryFormatUsesFullConfigOnlyForVLESS(t *testing.T) {
	if got := preferredDeliveryFormatForClient(ClientTypeHiddify, ClientProtocolVLESS); got != SubscriptionDeliveryFormatSingBox {
		t.Fatalf("Hiddify VLESS format = %q", got)
	}
	if got := preferredDeliveryFormatForClient(ClientTypeHiddify, ClientProtocolWireGuard); got != SubscriptionDeliveryFormatAuto {
		t.Fatalf("Hiddify WireGuard format = %q", got)
	}
	if got := preferredDeliveryFormatForClient(ClientTypeV2RayN, ClientProtocolVLESS); got != SubscriptionDeliveryFormatBase64 {
		t.Fatalf("v2rayN VLESS format = %q", got)
	}
	if got := preferredDeliveryFormatForClient(ClientTypeV2RayN, ClientProtocolWireGuard); got != SubscriptionDeliveryFormatAuto {
		t.Fatalf("v2rayN WireGuard format = %q", got)
	}
}

func TestCompatibilityDowngradesWhenProtocolCannotCarryPolicy(t *testing.T) {
	tests := []struct {
		name       string
		clientType string
		protocol   string
	}{
		{"Hiddify WireGuard", ClientTypeHiddify, ClientProtocolWireGuard},
		{"sing-box Hysteria2", ClientTypeSingBox, ClientProtocolHysteria2},
		{"v2rayN WireGuard", ClientTypeV2RayN, ClientProtocolWireGuard},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assessment := clientCompatibilityForProtocol(test.clientType, test.protocol)
			if assessment.Status != ClientCompatibilityConnectionOnly {
				t.Fatalf("status = %q, want connection_only: %+v", assessment.Status, assessment)
			}
			if assessment.PreferredDeliveryFormat != SubscriptionDeliveryFormatAuto {
				t.Fatalf("format = %q, want auto", assessment.PreferredDeliveryFormat)
			}
			if !assessment.RequiresClientSetup {
				t.Fatal("expected client setup / compatibility warning")
			}
		})
	}
}

func TestCompatibilityKeepsValidatedProtocolBehavior(t *testing.T) {
	if got := clientCompatibilityForProtocol(ClientTypeHiddify, ClientProtocolVLESS).Status; got != ClientCompatibilityFullSmartRouting {
		t.Fatalf("Hiddify VLESS status = %q", got)
	}
	if got := clientCompatibilityForProtocol(ClientTypeV2RayN, ClientProtocolShadowsocks).Status; got != ClientCompatibilitySetupRequired {
		t.Fatalf("v2rayN Shadowsocks status = %q", got)
	}
	if got := clientCompatibilityForProtocol(ClientTypeV2RayNG, ClientProtocolShadowsocks).Status; got != ClientCompatibilityConnectionOnly {
		t.Fatalf("v2rayNG Shadowsocks status = %q, want connection_only (routing not validated)", got)
	}
}

// TestV2RayNGDeliveryFormatDependsOnProtocolNotJustClient guards against a
// real delivery bug: v2rayNG's compatibility badge was already correctly
// connection_only, but its PreferredDeliveryFormat stayed Base64 regardless
// of protocol. For WireGuard/MTProto, RouteGate cannot render a valid Base64
// share-link at all (subscriptionShareLinks only covers VLESS/Hysteria2/
// Shadowsocks), so selecting Base64 there produced a request that
// renderSubscriptionDeliveryPayload rejects as unavailable instead of a
// working protocol-native fallback.
func TestV2RayNGDeliveryFormatDependsOnProtocolNotJustClient(t *testing.T) {
	tests := []struct {
		name       string
		protocol   string
		wantFormat string
	}{
		{"VLESS has a valid share-link", ClientProtocolVLESS, SubscriptionDeliveryFormatBase64},
		{"Shadowsocks has a valid share-link", ClientProtocolShadowsocks, SubscriptionDeliveryFormatBase64},
		{"Hysteria2 has a valid share-link", ClientProtocolHysteria2, SubscriptionDeliveryFormatBase64},
		{"WireGuard has no share-link representation", ClientProtocolWireGuard, SubscriptionDeliveryFormatAuto},
		{"MTProto has no share-link representation", ClientProtocolMTProto, SubscriptionDeliveryFormatAuto},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assessment := clientCompatibilityForProtocol(ClientTypeV2RayNG, test.protocol)
			if assessment.Status != ClientCompatibilityConnectionOnly {
				t.Fatalf("v2rayNG status = %q, want connection_only regardless of protocol", assessment.Status)
			}
			if assessment.PreferredDeliveryFormat != test.wantFormat {
				t.Fatalf("v2rayNG %s preferred format = %q, want %q", test.protocol, assessment.PreferredDeliveryFormat, test.wantFormat)
			}
			if got := preferredDeliveryFormatForClient(ClientTypeV2RayNG, test.protocol); got != test.wantFormat {
				t.Fatalf("preferredDeliveryFormatForClient(v2rayNG, %s) = %q, want %q", test.protocol, got, test.wantFormat)
			}
		})
	}
}

func TestDetectClientTypeFromUserAgentIsConservative(t *testing.T) {
	cases := map[string]string{
		"Hiddify/2.5":           ClientTypeHiddify,
		"v2rayN/7.0":            ClientTypeV2RayN,
		"v2rayNG/1.9.20":        ClientTypeV2RayNG,
		"sing-box/1.12":         ClientTypeSingBox,
		"V2RayTun iOS":          "",
		"V2Box/4.2":             "",
		"Mozilla/5.0 Safari/18": "",
	}
	for ua, want := range cases {
		if got := detectClientTypeFromUserAgent(ua); got != want {
			t.Fatalf("detect %q = %q, want %q", ua, got, want)
		}
	}
}

func TestSelectedClientProfileWinsOverUserAgent(t *testing.T) {
	profile := ClientProfile{ClientType: ClientTypeV2RayN}
	if got := resolveSubscriptionClientType(profile, "Hiddify/2.5"); got != ClientTypeV2RayN {
		t.Fatalf("resolved client = %q", got)
	}
	if got := resolveSubscriptionClientType(ClientProfile{ClientType: ClientTypeGeneric}, "Hiddify/2.5"); got != ClientTypeHiddify {
		t.Fatalf("detected fallback client = %q", got)
	}
}

func TestClientConnectionJSONIncludesCompatibilityAssessment(t *testing.T) {
	payload, err := json.Marshal(ClientConnectionResponse{
		VPNAccountID: "account-1",
		Protocol:     ClientProtocolVLESS,
		Profile:      ClientProfile{ClientType: ClientTypeV2RayN},
	})
	if err != nil {
		t.Fatalf("marshal connection: %v", err)
	}
	body := string(payload)
	for _, want := range []string{`"clientCompatibility"`, `"clientType":"v2rayn"`, `"status":"client_setup_required"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("connection JSON missing %s: %s", want, body)
		}
	}
}

func TestClientConnectionJSONDoesNotClaimFullRoutingOnFallbackProtocol(t *testing.T) {
	payload, err := json.Marshal(ClientConnectionResponse{
		VPNAccountID: "account-1",
		Protocol:     ClientProtocolWireGuard,
		Profile:      ClientProfile{ClientType: ClientTypeHiddify},
	})
	if err != nil {
		t.Fatalf("marshal connection: %v", err)
	}
	body := string(payload)
	if !strings.Contains(body, `"status":"connection_only"`) || strings.Contains(body, `"status":"full_smart_routing"`) {
		t.Fatalf("unexpected fallback compatibility JSON: %s", body)
	}
}
