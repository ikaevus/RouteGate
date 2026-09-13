package vpnaccounts

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderHAPPRoutingLinkMapsResolvedRouteGatePolicy(t *testing.T) {
	profile := &RoutingProfile{
		ID: "profile-1",
		Name: "RU split",
		Rules: []RoutingProfileRule{
			{
				Action: RoutingActionDirect,
				Domains: []string{"ozon.ru"},
				DomainSuffixes: []string{"wildberries.ru"},
				DomainKeywords: []string{"marketplace"},
				IPCIDRs: []string{"10.0.0.0/8"},
				GeoSites: []string{"ru"},
				GeoIPs: []string{"ru"},
			},
			{
				Action: RoutingActionVPN,
				Domains: []string{"example.com"},
				GeoSites: []string{"youtube"},
				GeoIPs: []string{"telegram"},
			},
			{
				Action: RoutingActionBlock,
				Domains: []string{"ads.example"},
				IPCIDRs: []string{"203.0.113.0/24"},
			},
		},
	}

	link, ok, err := renderHAPPRoutingLink(profile)
	if err != nil {
		t.Fatalf("render HAPP routing link: %v", err)
	}
	if !ok {
		t.Fatal("expected HAPP routing link")
	}
	if !strings.HasPrefix(link, happRoutingLinkPrefix) {
		t.Fatalf("link = %q", link)
	}

	encoded := strings.TrimPrefix(link, happRoutingLinkPrefix)
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode HAPP routing payload: %v", err)
	}
	var got happRoutingProfile
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal HAPP routing payload: %v", err)
	}

	if got.Name != "RouteGate" {
		t.Fatalf("profile name = %q, want stable RouteGate", got.Name)
	}
	if got.GlobalProxy != "true" {
		t.Fatalf("GlobalProxy = %q, want true for unmatched -> VPN", got.GlobalProxy)
	}
	if got.DomainStrategy != "IPIfNonMatch" {
		t.Fatalf("DomainStrategy = %q", got.DomainStrategy)
	}

	assertStringsEqual(t, "DirectSites", got.DirectSites, []string{
		"full:ozon.ru", "domain:wildberries.ru", "keyword:marketplace", "geosite:ru",
	})
	assertStringsEqual(t, "DirectIp", got.DirectIP, []string{"10.0.0.0/8", "geoip:ru"})
	assertStringsEqual(t, "ProxySites", got.ProxySites, []string{"full:example.com", "geosite:youtube"})
	assertStringsEqual(t, "ProxyIp", got.ProxyIP, []string{"geoip:telegram"})
	assertStringsEqual(t, "BlockSites", got.BlockSites, []string{"full:ads.example"})
	assertStringsEqual(t, "BlockIp", got.BlockIP, []string{"203.0.113.0/24"})
}

func TestRenderHAPPRoutingLinkSkipsUnknownAndDeduplicates(t *testing.T) {
	profile := &RoutingProfile{Rules: []RoutingProfileRule{
		{Action: RoutingActionDirect, Domains: []string{"example.com", "example.com"}},
		{Action: "unknown", Domains: []string{"ignored.example"}},
	}}
	link, ok, err := renderHAPPRoutingLink(profile)
	if err != nil || !ok {
		t.Fatalf("render result link=%q ok=%v err=%v", link, ok, err)
	}
	payload, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(link, happRoutingLinkPrefix))
	var got happRoutingProfile
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	assertStringsEqual(t, "DirectSites", got.DirectSites, []string{"full:example.com"})
	if len(got.ProxySites) != 0 || len(got.BlockSites) != 0 {
		t.Fatalf("unexpected rules in other action buckets: %+v", got)
	}
}

func TestRenderHAPPRoutingLinkRequiresUsableRules(t *testing.T) {
	for _, profile := range []*RoutingProfile{
		nil,
		{},
		{Rules: []RoutingProfileRule{{Action: "unknown", Domains: []string{"example.com"}}}},
		{Rules: []RoutingProfileRule{{Action: RoutingActionDirect}}},
	} {
		link, ok, err := renderHAPPRoutingLink(profile)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok || link != "" {
			t.Fatalf("unexpected routing artifact link=%q ok=%v", link, ok)
		}
	}
}

func TestHAPPSubscriptionHeadersAttachRoutingOrthogonally(t *testing.T) {
	profile := SubscriptionProfile{RoutingProfile: &RoutingProfile{Rules: []RoutingProfileRule{
		{Action: RoutingActionDirect, Domains: []string{"ozon.ru"}},
	}}}

	for _, protocol := range []string{ClientProtocolVLESS, ClientProtocolShadowsocks, ClientProtocolHysteria2} {
		headers, err := clientSubscriptionHeaders(ClientTypeHAPP, protocol, profile)
		if err != nil {
			t.Fatalf("%s headers: %v", protocol, err)
		}
		if !strings.HasPrefix(headers["Routing"], happRoutingLinkPrefix) {
			t.Fatalf("%s Routing header = %q", protocol, headers["Routing"])
		}
	}

	for _, protocol := range []string{ClientProtocolWireGuard, ClientProtocolMTProto} {
		headers, err := clientSubscriptionHeaders(ClientTypeHAPP, protocol, profile)
		if err != nil {
			t.Fatalf("%s headers: %v", protocol, err)
		}
		if _, ok := headers["Routing"]; ok {
			t.Fatalf("%s must not receive HAPP routing header with protocol-native fallback", protocol)
		}
	}
}

func TestHAPPSubscriptionHeadersDoNotInventPolicy(t *testing.T) {
	headers, err := clientSubscriptionHeaders(ClientTypeHAPP, ClientProtocolVLESS, SubscriptionProfile{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := headers["Routing"]; ok {
		t.Fatal("HAPP subscription without a RouteGate RoutingProfile must not receive routing policy")
	}
}

func assertStringsEqual(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s len=%d want=%d: got=%v want=%v", name, len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d]=%q want %q; got=%v", name, i, got[i], want[i], got)
		}
	}
}
