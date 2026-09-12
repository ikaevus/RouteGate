package vpnaccounts

import (
	"strings"
	"testing"
)

func rg115bTestRoutingProfile() *RoutingProfile {
	return &RoutingProfile{
		ID:   "profile-1",
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

func TestRenderSubscriptionDeliveryPayloadV2RayNRoutingFormat(t *testing.T) {
	profile := SubscriptionProfile{RoutingProfile: rg115bTestRoutingProfile()}

	v2rayn, err := renderSubscriptionDeliveryPayload(ClientConnectionResponse{}, profile, SubscriptionDeliveryFormatV2RayNRouting)
	if err != nil {
		t.Fatalf("v2rayN routing payload: %v", err)
	}
	if v2rayn.ContentType != "application/json; charset=utf-8" || !strings.Contains(v2rayn.Body, `"outboundTag": "direct"`) {
		t.Fatalf("unexpected v2rayN payload: %+v", v2rayn)
	}
}
