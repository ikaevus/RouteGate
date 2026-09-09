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
		{ClientTypeV2Box, ClientCompatibilitySetupRequired, SubscriptionDeliveryFormatBase64, true},
		{ClientTypeV2RayTun, ClientCompatibilityPartial, SubscriptionDeliveryFormatBase64, true},
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
	if !clientCompatibilityFor(ClientTypeV2RayTun).Capabilities.SubscriptionRoutingPolicy {
		t.Fatal("expected V2RayTun subscription routing policy support")
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
	if got := preferredDeliveryFormatForClient(ClientTypeV2RayTun, ClientProtocolMTProto); got != SubscriptionDeliveryFormatAuto {
		t.Fatalf("V2RayTun MTProto format = %q", got)
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
		{"V2Box MTProto", ClientTypeV2Box, ClientProtocolMTProto},
		{"V2RayTun WireGuard", ClientTypeV2RayTun, ClientProtocolWireGuard},
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
	if got := clientCompatibilityForProtocol(ClientTypeV2RayTun, ClientProtocolVLESS).Status; got != ClientCompatibilityPartial {
		t.Fatalf("V2RayTun VLESS status = %q", got)
	}
	if got := clientCompatibilityForProtocol(ClientTypeV2RayN, ClientProtocolShadowsocks).Status; got != ClientCompatibilitySetupRequired {
		t.Fatalf("v2rayN Shadowsocks status = %q", got)
	}
}

func TestDetectClientTypeFromUserAgentIsConservative(t *testing.T) {
	cases := map[string]string{
		"Hiddify/2.5":           ClientTypeHiddify,
		"v2rayN/7.0":            ClientTypeV2RayN,
		"V2RayTun iOS":          ClientTypeV2RayTun,
		"V2Box/4.2":             ClientTypeV2Box,
		"sing-box/1.12":         ClientTypeSingBox,
		"Mozilla/5.0 Safari/18": "",
	}
	for ua, want := range cases {
		if got := detectClientTypeFromUserAgent(ua); got != want {
			t.Fatalf("detect %q = %q, want %q", ua, got, want)
		}
	}
}

func TestSelectedClientProfileWinsOverUserAgent(t *testing.T) {
	profile := ClientProfile{ClientType: ClientTypeV2Box}
	if got := resolveSubscriptionClientType(profile, "Hiddify/2.5"); got != ClientTypeV2Box {
		t.Fatalf("resolved client = %q", got)
	}
	if got := resolveSubscriptionClientType(ClientProfile{ClientType: ClientTypeOther}, "Hiddify/2.5"); got != ClientTypeHiddify {
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
