package vpnaccounts

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func rg115bTestRoutingProfile() *RoutingProfile {
	return &RoutingProfile{
		ID: "profile-1",
		Name: "Smart routing",
		Rules: []RoutingProfileRule{
			{
				ID: "direct-ru", Name: "Direct RU", Action: RoutingActionDirect,
				Domains: []string{"ozon.ru"}, DomainSuffixes: []string{"wildberries.ru"},
				DomainKeywords: []string{"marketplace"}, GeoSites: []string{"ru"},
				IPCIDRs: []string{"10.0.0.0/8"}, GeoIPs: []string{"ru"},
			},
			{ID: "block-ads", Name: "Block ads", Action: RoutingActionBlock, GeoSites: []string{"category-ads-all"}},
		},
	}
}

func TestRenderV2RayNCustomRoutingRules(t *testing.T) {
	rules, ok := renderV2RayNCustomRoutingRules(rg115bTestRoutingProfile())
	if !ok || len(rules) != 3 {
		t.Fatalf("expected two profile rules plus fallback, got %+v", rules)
	}
	if rules[0].OutboundTag != "direct" || rules[0].Remarks != "Direct RU" {
		t.Fatalf("unexpected direct rule: %+v", rules[0])
	}
	wantDomains := []string{"full:ozon.ru", "domain:wildberries.ru", "keyword:marketplace", "geosite:ru"}
	for i, want := range wantDomains {
		if rules[0].Domain[i] != want {
			t.Fatalf("domain[%d] = %q, want %q", i, rules[0].Domain[i], want)
		}
	}
	if rules[0].IP[0] != "10.0.0.0/8" || rules[0].IP[1] != "geoip:ru" {
		t.Fatalf("unexpected IP rules: %v", rules[0].IP)
	}
	if rules[2].OutboundTag != "proxy" || rules[2].Port != "0-65535" {
		t.Fatalf("expected explicit VPN fallback, got %+v", rules[2])
	}
}

func TestRenderV2BoxRoutingDeepLink(t *testing.T) {
	link, ok, err := renderV2BoxRoutingDeepLink(rg115bTestRoutingProfile())
	if err != nil {
		t.Fatalf("render V2Box deep link: %v", err)
	}
	if !ok || !strings.HasPrefix(link, "v2box://routes?multi=") {
		t.Fatalf("unexpected V2Box link: %q", link)
	}
	encoded := strings.TrimPrefix(link, "v2box://routes?multi=")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode V2Box payload: %v", err)
	}
	var routes []v2BoxRoute
	if err := json.Unmarshal(decoded, &routes); err != nil {
		t.Fatalf("decode V2Box JSON: %v", err)
	}
	if len(routes) < 5 {
		t.Fatalf("expected domain/IP route objects, got %+v", routes)
	}
	if routes[0].Type != "Domain" || routes[0].Tag != "direct" || routes[0].MatchMode != "keyword" {
		t.Fatalf("unexpected first V2Box route: %+v", routes[0])
	}
	foundSuffix := false
	for _, route := range routes {
		for _, item := range route.List {
			if item == `regexp:wildberries\.ru$` {
				foundSuffix = true
			}
		}
	}
	if !foundSuffix {
		t.Fatalf("expected V2Box suffix regexp in routes: %+v", routes)
	}
}

func TestRenderSubscriptionDeliveryPayloadNativeRoutingFormats(t *testing.T) {
	profile := SubscriptionProfile{RoutingProfile: rg115bTestRoutingProfile()}

	v2rayn, err := renderSubscriptionDeliveryPayload(ClientConnectionResponse{}, profile, SubscriptionDeliveryFormatV2RayNRouting)
	if err != nil {
		t.Fatalf("v2rayN routing payload: %v", err)
	}
	if v2rayn.ContentType != "application/json; charset=utf-8" || !strings.Contains(v2rayn.Body, `"outboundTag": "direct"`) {
		t.Fatalf("unexpected v2rayN payload: %+v", v2rayn)
	}

	v2raytun, err := renderSubscriptionDeliveryPayload(ClientConnectionResponse{}, profile, SubscriptionDeliveryFormatV2RayTunRouting)
	if err != nil {
		t.Fatalf("V2RayTun routing payload: %v", err)
	}
	if !strings.HasPrefix(v2raytun.Body, "v2raytun://import_route/") {
		t.Fatalf("unexpected V2RayTun payload: %+v", v2raytun)
	}
	encodedRouting := strings.TrimPrefix(v2raytun.Body, "v2raytun://import_route/")
	decodedRouting, err := base64.StdEncoding.DecodeString(encodedRouting)
	if err != nil {
		t.Fatalf("decode V2RayTun routing payload: %v", err)
	}
	if !strings.Contains(string(decodedRouting), `"outboundTag":"direct"`) || !strings.Contains(string(decodedRouting), `"outboundTag":"proxy"`) {
		t.Fatalf("V2RayTun routing payload lacks direct/proxy semantics: %s", decodedRouting)
	}

	v2box, err := renderSubscriptionDeliveryPayload(ClientConnectionResponse{}, profile, SubscriptionDeliveryFormatV2BoxRouting)
	if err != nil {
		t.Fatalf("V2Box routing payload: %v", err)
	}
	if !strings.HasPrefix(v2box.Body, "v2box://routes?multi=") {
		t.Fatalf("unexpected V2Box payload: %+v", v2box)
	}
}
