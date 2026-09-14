package vpnaccounts

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

const (
	happRoutingLinkPrefix = "happ://routing/onadd/"
	happRemoteDNSType = "DoH"
	happRemoteDNSDomain = "https://cloudflare-dns.com/dns-query"
	happRemoteDNSIP = "1.1.1.1"
	happDomesticDNSType = "DoH"
	happDomesticDNSDomain = "https://dns.google/dns-query"
	happDomesticDNSIP = "8.8.8.8"
)

type happRoutingProfile struct {
	Name              string            `json:"Name"`
	GlobalProxy       string            `json:"GlobalProxy"`
	RemoteDNSType     string            `json:"RemoteDNSType"`
	RemoteDNSDomain   string            `json:"RemoteDNSDomain"`
	RemoteDNSIP       string            `json:"RemoteDNSIP"`
	DomesticDNSType   string            `json:"DomesticDNSType"`
	DomesticDNSDomain string            `json:"DomesticDNSDomain"`
	DomesticDNSIP     string            `json:"DomesticDNSIP"`
	GeoIPURL          string            `json:"Geoipurl"`
	GeoSiteURL        string            `json:"Geositeurl"`
	DNSHosts          map[string]string `json:"DnsHosts"`
	DirectSites       []string          `json:"DirectSites"`
	DirectIP          []string          `json:"DirectIp"`
	ProxySites        []string          `json:"ProxySites"`
	ProxyIP           []string          `json:"ProxyIp"`
	BlockSites        []string          `json:"BlockSites"`
	BlockIP           []string          `json:"BlockIp"`
	DomainStrategy    string            `json:"DomainStrategy"`
	FakeDNS           string            `json:"FakeDNS"`
}

func renderHAPPRoutingLink(profile *RoutingProfile) (string, bool, error) {
	if profile == nil || len(profile.Rules) == 0 {
		return "", false, nil
	}

	rendered := happRoutingProfile{
		Name:              "RouteGate",
		GlobalProxy:       "true",
		RemoteDNSType:     happRemoteDNSType,
		RemoteDNSDomain:   happRemoteDNSDomain,
		RemoteDNSIP:       happRemoteDNSIP,
		DomesticDNSType:   happDomesticDNSType,
		DomesticDNSDomain: happDomesticDNSDomain,
		DomesticDNSIP:     happDomesticDNSIP,
		DNSHosts: map[string]string{
			"cloudflare-dns.com": happRemoteDNSIP,
			"dns.google":         happDomesticDNSIP,
		},
		DirectSites:    []string{},
		DirectIP:       []string{},
		ProxySites:     []string{},
		ProxyIP:        []string{},
		BlockSites:     []string{},
		BlockIP:        []string{},
		DomainStrategy: "IPIfNonMatch",
		FakeDNS:        "false",
	}

	for _, rule := range profile.Rules {
		domains := routingRuleDomainConditions(rule)
		ips := routingRuleIPConditions(rule)
		switch strings.TrimSpace(rule.Action) {
		case RoutingActionDirect:
			rendered.DirectSites = appendUniqueStrings(rendered.DirectSites, domains...)
			rendered.DirectIP = appendUniqueStrings(rendered.DirectIP, ips...)
		case RoutingActionVPN:
			rendered.ProxySites = appendUniqueStrings(rendered.ProxySites, domains...)
			rendered.ProxyIP = appendUniqueStrings(rendered.ProxyIP, ips...)
		case RoutingActionBlock:
			rendered.BlockSites = appendUniqueStrings(rendered.BlockSites, domains...)
			rendered.BlockIP = appendUniqueStrings(rendered.BlockIP, ips...)
		}
	}

	if len(rendered.DirectSites)+len(rendered.DirectIP)+len(rendered.ProxySites)+len(rendered.ProxyIP)+len(rendered.BlockSites)+len(rendered.BlockIP) == 0 {
		return "", false, nil
	}

	encoded, err := json.Marshal(rendered)
	if err != nil {
		return "", false, err
	}
	return happRoutingLinkPrefix + base64.StdEncoding.EncodeToString(encoded), true, nil
}

func appendUniqueStrings(dst []string, values ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(values))
	for _, value := range dst {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		dst = append(dst, value)
	}
	return dst
}
