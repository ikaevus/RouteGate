package vpnaccounts

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderV2RayTunRoutingHeaderMapsRouteGateRules(t *testing.T) {
	profile := &RoutingProfile{
		ID: "profile-1",
		Name: "Smart routing",
		Rules: []RoutingProfileRule{
			{
				ID: "rule-direct", Name: "Direct local", Action: RoutingActionDirect,
				Domains: []string{"exact.example"}, DomainSuffixes: []string{"example.org"},
				DomainKeywords: []string{"local"}, GeoSites: []string{"private"},
				IPCIDRs: []string{"10.0.0.0/8"}, GeoIPs: []string{"private"},
			},
			{ID: "rule-vpn", Name: "Use VPN", Action: RoutingActionVPN, DomainSuffixes: []string{"blocked.example"}},
			{ID: "rule-block", Name: "Block", Action: RoutingActionBlock, Domains: []string{"ads.example"}},
		},
	}

	header, ok, err := renderV2RayTunRoutingHeader(profile)
	if err != nil {
		t.Fatalf("render routing header: %v", err)
	}
	if !ok || header == "" {
		t.Fatal("expected routing header")
	}
	decoded, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		t.Fatalf("decode routing header: %v", err)
	}
	var routing v2RayTunRouting
	if err := json.Unmarshal(decoded, &routing); err != nil {
		t.Fatalf("decode routing JSON: %v", err)
	}
	if routing.DomainStrategy != "AsIs" || routing.DomainMatcher != "hybrid" || len(routing.Rules) != 4 {
		t.Fatalf("unexpected routing: %+v", routing)
	}
	if routing.Rules[0].OutboundTag != "direct" || routing.Rules[1].OutboundTag != "proxy" || routing.Rules[2].OutboundTag != "block" {
		t.Fatalf("unexpected outbound tags: %+v", routing.Rules)
	}
	fallback := routing.Rules[3]
	if fallback.OutboundTag != "proxy" || fallback.Port != "0-65535" {
		t.Fatalf("expected explicit VPN fallback, got %+v", fallback)
	}
	wantDomains := []string{"full:exact.example", "domain:example.org", "keyword:local", "geosite:private"}
	for i, want := range wantDomains {
		if routing.Rules[0].Domain[i] != want {
			t.Fatalf("domain condition %d = %q, want %q", i, routing.Rules[0].Domain[i], want)
		}
	}
	if routing.Rules[0].IP[0] != "10.0.0.0/8" || routing.Rules[0].IP[1] != "geoip:private" {
		t.Fatalf("unexpected IP conditions: %v", routing.Rules[0].IP)
	}
}

func TestRenderV2RayTunRoutingDeepLink(t *testing.T) {
	profile := &RoutingProfile{
		Name: "Runet",
		Rules: []RoutingProfileRule{{
			Name: "Direct marketplaces", Action: RoutingActionDirect,
			Domains: []string{"ozon.ru"}, DomainSuffixes: []string{"wildberries.ru"},
		}},
	}
	link, ok, err := renderV2RayTunRoutingDeepLink(profile)
	if err != nil {
		t.Fatalf("render routing deep link: %v", err)
	}
	if !ok || !strings.HasPrefix(link, "v2raytun://import_route/") {
		t.Fatalf("unexpected routing deep link: %q", link)
	}
	encoded := strings.TrimPrefix(link, "v2raytun://import_route/")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode routing deep link: %v", err)
	}
	var routing v2RayTunRouting
	if err := json.Unmarshal(decoded, &routing); err != nil {
		t.Fatalf("decode routing JSON: %v", err)
	}
	if len(routing.Rules) != 2 {
		t.Fatalf("expected direct rule plus VPN fallback, got %+v", routing.Rules)
	}
	if routing.Rules[0].Domain[0] != "full:ozon.ru" || routing.Rules[0].Domain[1] != "domain:wildberries.ru" {
		t.Fatalf("marketplace rules missing: %+v", routing.Rules[0])
	}
	if routing.Rules[1].OutboundTag != "proxy" || routing.Rules[1].Port != "0-65535" {
		t.Fatalf("VPN fallback missing: %+v", routing.Rules[1])
	}
}

func TestClientSubscriptionHeadersEmitV2RayTunRouting(t *testing.T) {
	profile := SubscriptionProfile{
		Account: Account{DisplayName: "Demo"},
		RoutingProfile: &RoutingProfile{
			Name: "Smart",
			Rules: []RoutingProfileRule{{Name: "Direct", Action: RoutingActionDirect, DomainSuffixes: []string{"example.org"}}},
		},
	}
	headers, err := clientSubscriptionHeaders(ClientTypeV2RayTun, ClientProtocolVLESS, profile)
	if err != nil {
		t.Fatalf("client headers: %v", err)
	}
	if headers["Routing"] == "" || headers["Profile-Update-Interval"] != "12" {
		t.Fatalf("unexpected V2RayTun headers: %+v", headers)
	}
	if got := subscriptionProfileTitle(profile); got != "base64:RGVtbw==" {
		t.Fatalf("profile title = %q", got)
	}
}

func TestClientSubscriptionHeadersDoNotEmitRoutingForUnsupportedProtocol(t *testing.T) {
	profile := SubscriptionProfile{
		RoutingProfile: &RoutingProfile{
			Name: "Smart",
			Rules: []RoutingProfileRule{{Name: "Direct", Action: RoutingActionDirect, DomainSuffixes: []string{"example.org"}}},
		},
	}
	headers, err := clientSubscriptionHeaders(ClientTypeV2RayTun, ClientProtocolWireGuard, profile)
	if err != nil {
		t.Fatalf("client headers: %v", err)
	}
	if headers["Routing"] != "" {
		t.Fatalf("did not expect routing header for WireGuard: %+v", headers)
	}
}
