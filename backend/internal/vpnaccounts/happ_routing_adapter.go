package vpnaccounts

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

const happRoutingLinkPrefix = "happ://routing/onadd/"

var happBaselineDirectIP = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"224.0.0.0/4",
	"255.255.255.255",
}

// happRoutingProfile intentionally carries only routing policy plus HAPP's
// baseline local-network safety routes. Tunnel DNS is client-local state in
// HAPP and is deliberately omitted so the app can apply its own platform
// defaults instead of RouteGate overriding DNS/bootstrap behaviour on iOS.
type happRoutingProfile struct {
	Name           string   `json:"Name"`
	GlobalProxy    string   `json:"GlobalProxy"`
	DirectSites    []string `json:"DirectSites,omitempty"`
	DirectIP       []string `json:"DirectIp,omitempty"`
	ProxySites     []string `json:"ProxySites,omitempty"`
	ProxyIP        []string `json:"ProxyIp,omitempty"`
	BlockSites     []string `json:"BlockSites,omitempty"`
	BlockIP        []string `json:"BlockIp,omitempty"`
	DomainStrategy string   `json:"DomainStrategy"`
	FakeDNS        string   `json:"FakeDNS"`
}

func renderHAPPRoutingLink(profile *RoutingProfile) (string, bool, error) {
	if profile == nil || len(profile.Rules) == 0 {
		return "", false, nil
	}

	rendered := happRoutingProfile{
		Name:           "RouteGate",
		GlobalProxy:    "true",
		DirectIP:       append([]string(nil), happBaselineDirectIP...),
		DomainStrategy: "IPIfNonMatch",
		FakeDNS:        "false",
	}

	hasRouteGateMatcher := false
	for _, rule := range profile.Rules {
		domains := routingRuleDomainConditions(rule)
		ips := routingRuleIPConditions(rule)
		if len(domains) == 0 && len(ips) == 0 {
			continue
		}

		switch strings.TrimSpace(rule.Action) {
		case RoutingActionDirect:
			rendered.DirectSites = appendUniqueStrings(rendered.DirectSites, domains...)
			rendered.DirectIP = appendUniqueStrings(rendered.DirectIP, ips...)
			hasRouteGateMatcher = true
		case RoutingActionVPN:
			rendered.ProxySites = appendUniqueStrings(rendered.ProxySites, domains...)
			rendered.ProxyIP = appendUniqueStrings(rendered.ProxyIP, ips...)
			hasRouteGateMatcher = true
		case RoutingActionBlock:
			rendered.BlockSites = appendUniqueStrings(rendered.BlockSites, domains...)
			rendered.BlockIP = appendUniqueStrings(rendered.BlockIP, ips...)
			hasRouteGateMatcher = true
		}
	}

	if !hasRouteGateMatcher {
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
