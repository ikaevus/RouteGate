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
	if assessment.Capabilities.SubscriptionRoutingPolicy {
		t.Fatal("HAPP must not advertise provider-managed routing while the safety disable is active")
	}
	if assessment.Capabilities.DirectRouting || assessment.Capabilities.VPNRouting || assessment.Capabilities.BlockRouting {
		t.Fatalf("HAPP must not advertise RouteGate-managed routing actions while disabled: %+v", assessment.Capabilities)
	}
	if assessment.Capabilities.ImportedRulePrecedence != ImportedRulePrecedenceUnknown {
		t.Fatalf("HAPP precedence = %q, want unknown while managed routing is disabled", assessment.Capabilities.ImportedRulePrecedence)
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
		if assessment.Capabilities.SubscriptionRoutingPolicy {
			t.Fatalf("HAPP %s must not carry provider-managed routing while disabled", protocol)
		}
	}

	hysteria := clientCompatibilityForProtocol(ClientTypeHAPP, ClientProtocolHysteria2)
	if hysteria.Status != ClientCompatibilityConnectionOnly {
		t.Fatalf("HAPP Hysteria2 status = %q, want connection_only until RouteGate manual validation", hysteria.Status)
	}
	if hysteria.PreferredDeliveryFormat != SubscriptionDeliveryFormatBase64 {
		t.Fatalf("HAPP Hysteria2 format = %q, want base64 client-supported fallback", hysteria.PreferredDeliveryFormat)
	}
	if hysteria.Capabilities.SubscriptionRoutingPolicy {
		t.Fatal("HAPP Hysteria2 must not claim provider-managed routing while disabled")
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
