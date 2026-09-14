package vpnaccounts

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestHAPPRoutingProfileLeavesTunnelDNSToClient(t *testing.T) {
	link, ok, err := renderHAPPRoutingLink(&RoutingProfile{Rules: []RoutingProfileRule{{
		Action:  RoutingActionDirect,
		Domains: []string{"ozon.ru"},
	}}})
	if err != nil || !ok {
		t.Fatalf("render HAPP routing link: link=%q ok=%v err=%v", link, ok, err)
	}

	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(link, happRoutingLinkPrefix))
	if err != nil {
		t.Fatalf("decode HAPP routing payload: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("unmarshal raw HAPP profile: %v", err)
	}
	for _, key := range []string{
		"RemoteDns",
		"DomesticDns",
		"RemoteDNSType",
		"RemoteDNSDomain",
		"RemoteDNSIP",
		"DomesticDNSType",
		"DomesticDNSDomain",
		"DomesticDNSIP",
		"DnsHosts",
		"Geoipurl",
		"Geositeurl",
	} {
		if _, exists := raw[key]; exists {
			t.Fatalf("client-owned HAPP field %q must not be emitted", key)
		}
	}

	var got happRoutingProfile
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal HAPP routing payload: %v", err)
	}
	assertStringsEqual(t, "DirectIp", got.DirectIP, happBaselineDirectIP)
}
