package vpnaccounts

import (
	"encoding/base64"
	"encoding/json"
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
	if routing.DomainStrategy != "AsIs" || routing.DomainMatcher != "hybrid" || len(routing.Rules) != 3 {
		t.Fatalf("unexpected routing: %+v", routing)
	}
	if routing.Rules[0].OutboundTag != "direct" || routing.Rules[1].OutboundTag != "proxy" || routing.Rules[2].OutboundTag != "block" {
		t.Fatalf("unexpected outbound tags: %+v", routing.Rules)
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

func TestClientSubscriptionHeadersEmitV2RayTunRouting(t *testing.T) {
	profile := SubscriptionProfile{
		Account: Account{DisplayName: "Demo"},
		RoutingProfile: &RoutingProfile{
			Name: "Smart",
			Rules: []RoutingProfileRule{{Name: "Direct", Action: RoutingActionDirect, DomainSuffixes: []string{"example.org"}}},
		},
	}
	headers, err := clientSubscriptionHeaders(ClientTypeV2RayTun, profile)
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
