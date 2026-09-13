package vpnaccounts

import "testing"

func TestHAPPCompatibilityUsesValidatedStandardSubscription(t *testing.T) {
	assessment := clientCompatibilityFor(ClientTypeHAPP)
	if assessment.ClientType != ClientTypeHAPP {
		t.Fatalf("client type = %q, want %q", assessment.ClientType, ClientTypeHAPP)
	}
	if assessment.Status != ClientCompatibilityPartial {
		t.Fatalf("status = %q, want partial_compatibility", assessment.Status)
	}
	if assessment.PreferredDeliveryFormat != SubscriptionDeliveryFormatBase64 {
		t.Fatalf("preferred format = %q, want base64", assessment.PreferredDeliveryFormat)
	}
	if !assessment.Capabilities.URISubscriptionImport || !assessment.Capabilities.SubscriptionRefresh {
		t.Fatalf("HAPP standard subscription capabilities missing: %+v", assessment.Capabilities)
	}
	if !assessment.Capabilities.SubscriptionRoutingPolicy {
		t.Fatal("HAPP must advertise the provider-managed routing artifact once the adapter exists")
	}
	if !assessment.Capabilities.DirectRouting || !assessment.Capabilities.VPNRouting || !assessment.Capabilities.BlockRouting {
		t.Fatalf("HAPP routing action capabilities missing: %+v", assessment.Capabilities)
	}
	if assessment.Capabilities.ImportedRulePrecedence != ImportedRulePrecedenceUnknown {
		t.Fatalf("HAPP precedence = %q, want unknown until real-client overlap validation", assessment.Capabilities.ImportedRulePrecedence)
	}
}

func TestHAPPManualAcceptanceIsLimitedToVLESSAndShadowsocks(t *testing.T) {
	for _, protocol := range []string{ClientProtocolVLESS, ClientProtocolShadowsocks} {
		assessment := clientCompatibilityForProtocol(ClientTypeHAPP, protocol)
		if assessment.Status != ClientCompatibilityPartial {
			t.Fatalf("HAPP %s status = %q, want partial_compatibility", protocol, assessment.Status)
		}
		if assessment.PreferredDeliveryFormat != SubscriptionDeliveryFormatBase64 {
			t.Fatalf("HAPP %s format = %q, want base64", protocol, assessment.PreferredDeliveryFormat)
		}
		if !assessment.Capabilities.SubscriptionRoutingPolicy {
			t.Fatalf("HAPP %s should carry provider-managed routing", protocol)
		}
	}

	hysteria := clientCompatibilityForProtocol(ClientTypeHAPP, ClientProtocolHysteria2)
	if hysteria.Status != ClientCompatibilityConnectionOnly {
		t.Fatalf("HAPP Hysteria2 status = %q, want connection_only until RouteGate manual validation", hysteria.Status)
	}
	if hysteria.PreferredDeliveryFormat != SubscriptionDeliveryFormatBase64 {
		t.Fatalf("HAPP Hysteria2 format = %q, want base64 client-supported fallback", hysteria.PreferredDeliveryFormat)
	}
	if !hysteria.Capabilities.SubscriptionRoutingPolicy {
		t.Fatal("HAPP Hysteria2 share-link path may carry the routing artifact even though runtime acceptance is pending")
	}

	for _, protocol := range []string{ClientProtocolWireGuard, ClientProtocolMTProto} {
		assessment := clientCompatibilityForProtocol(ClientTypeHAPP, protocol)
		if assessment.Status != ClientCompatibilityConnectionOnly {
			t.Fatalf("HAPP %s status = %q, want connection_only", protocol, assessment.Status)
		}
		if assessment.PreferredDeliveryFormat != SubscriptionDeliveryFormatAuto {
			t.Fatalf("HAPP %s format = %q, want auto protocol-native fallback", protocol, assessment.PreferredDeliveryFormat)
		}
		if assessment.Capabilities.SubscriptionRoutingPolicy {
			t.Fatalf("HAPP %s must not claim a HAPP routing artifact on protocol-native fallback", protocol)
		}
	}
}

func TestHAPPIsFirstClassSelectedClientWithoutSpeculativeUserAgentDetection(t *testing.T) {
	if got := normalizeClientType("HAPP"); got != ClientTypeHAPP {
		t.Fatalf("normalize HAPP = %q", got)
	}
	if got := resolveSubscriptionClientType(ClientProfile{ClientType: ClientTypeHAPP}, "Unknown/1.0"); got != ClientTypeHAPP {
		t.Fatalf("selected HAPP resolved as %q", got)
	}
	if got := detectClientTypeFromUserAgent("HAPP/unknown"); got != "" {
		t.Fatalf("HAPP UA detection = %q, want empty until a real client UA is confirmed", got)
	}
}

func TestHAPPDeviceTypeIsAllowed(t *testing.T) {
	if _, ok := allowedDeviceClientTypes[ClientTypeHAPP]; !ok {
		t.Fatal("HAPP must be selectable as an Access & Devices client")
	}
}
