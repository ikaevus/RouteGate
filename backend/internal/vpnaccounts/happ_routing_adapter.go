package vpnaccounts

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

const happRoutingLinkPrefix = "happ://routing/onadd/"

// happRoutingProfile is the provider-managed routing profile format documented
// by HAPP. RouteGate deliberately emits only policy it can derive from its own
// resolved RoutingProfile. Empty GeoIP/GeoSite URLs tell HAPP to use its
// built-in/default datasets, while GlobalProxy=true preserves RouteGate's
// unmatched-traffic -> VPN default.
//
// HAPP's published format groups matchers by action rather than exposing
// RouteGate's arbitrary cross-action rule priorities. Until real-client
// acceptance proves equivalent precedence for overlapping rules, compatibility
// remains partial rather than full Smart Routing.
type happRoutingProfile struct {
	Name           string            `json:"Name"`
	GlobalProxy    string            `json:"GlobalProxy"`
	RemoteDNS      string            `json:"RemoteDns"`
	DomesticDNS    string            `json:"DomesticDns"`
	GeoIPURL       string            `json:"Geoipurl"`
	GeoSiteURL     string            `json:"Geositeurl"`
	DNSHosts       map[string]string `json:"DnsHosts"`
	DirectSites    []string          `json:"DirectSites"`
	DirectIP       []string          `json:"DirectIp"`
	ProxySites     []string          `json:"ProxySites"`
	ProxyIP        []string          `json:"ProxyIp"`
	BlockSites     []string          `json:"BlockSites"`
	BlockIP        []string          `json:"BlockIp"`
	DomainStrategy string            `json:"DomainStrategy"`
	FakeDNS        string            `json:"FakeDNS"`
}

// renderHAPPRoutingLink serializes an already resolved RouteGate RoutingProfile
// into HAPP's provider-managed routing deeplink. It makes no policy decisions:
// DIRECT/VPN/BLOCK are mechanically mapped into HAPP's corresponding matcher
// buckets and the same RouteGate profile remains the single policy source.
func renderHAPPRoutingLink(profile *RoutingProfile) (string, bool, error) {
	if profile == nil || len(profile.Rules) == 0 {
		return "", false, nil
	}

	rendered := happRoutingProfile{
		// A stable name is intentional. HAPP updates an existing subscription-
		// bound profile when it receives the same name; using the mutable
		// RouteGate display name here would leave stale profiles after renames.
		Name:           "RouteGate",
		GlobalProxy:    "true",
		DNSHosts:       map[string]string{},
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
