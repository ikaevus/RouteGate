package vpnaccounts

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestHAPPRoutingProfileCarriesCurrentDNSBootstrap(t *testing.T) {
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

	var got happRoutingProfile
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal HAPP routing payload: %v", err)
	}
	if got.RemoteDNSType != happRemoteDNSType || got.RemoteDNSDomain != happRemoteDNSDomain || got.RemoteDNSIP != happRemoteDNSIP {
		t.Fatalf("remote DNS = type:%q domain:%q ip:%q", got.RemoteDNSType, got.RemoteDNSDomain, got.RemoteDNSIP)
	}
	if got.DomesticDNSType != happDomesticDNSType || got.DomesticDNSDomain != happDomesticDNSDomain || got.DomesticDNSIP != happDomesticDNSIP {
		t.Fatalf("domestic DNS = type:%q domain:%q ip:%q", got.DomesticDNSType, got.DomesticDNSDomain, got.DomesticDNSIP)
	}
	if got.DNSHosts["cloudflare-dns.com"] != happRemoteDNSIP || got.DNSHosts["dns.google"] != happDomesticDNSIP {
		t.Fatalf("DNS bootstrap hosts = %#v", got.DNSHosts)
	}

	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatalf("unmarshal raw HAPP profile: %v", err)
	}
	if _, exists := raw["RemoteDns"]; exists {
		t.Fatal("legacy RemoteDns field must not be emitted")
	}
	if _, exists := raw["DomesticDns"]; exists {
		t.Fatal("legacy DomesticDns field must not be emitted")
	}
}
