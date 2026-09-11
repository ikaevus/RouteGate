package vpnaccounts

import "strings"

type v2RayNCustomRoutingRule struct {
	Port        string   `json:"port"`
	OutboundTag string   `json:"outboundTag"`
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Enabled     bool     `json:"enabled"`
	Remarks     string   `json:"remarks"`
}

func renderV2RayNCustomRoutingRules(profile *RoutingProfile) ([]v2RayNCustomRoutingRule, bool) {
	if profile == nil || len(profile.Rules) == 0 {
		return nil, false
	}

	rules := make([]v2RayNCustomRoutingRule, 0, len(profile.Rules)+1)
	for _, rule := range profile.Rules {
		outboundTag := routingRuleOutboundTag(rule.Action)
		if outboundTag == "" {
			continue
		}
		domains := routingRuleDomainConditions(rule)
		ips := routingRuleIPConditions(rule)
		if len(domains) == 0 && len(ips) == 0 {
			continue
		}
		remarks := strings.TrimSpace(rule.Name)
		if remarks == "" {
			remarks = "RouteGate rule"
		}
		rules = append(rules, v2RayNCustomRoutingRule{
			Port:        "",
			OutboundTag: outboundTag,
			Domain:      domains,
			IP:          ips,
			Enabled:     true,
			Remarks:     remarks,
		})
	}
	if len(rules) == 0 {
		return nil, false
	}

	// v2rayN custom routing is evaluated in order. RouteGate's client policy
	// defaults unmatched traffic to the VPN, so make that default explicit
	// instead of relying on whichever local routing mode the client had before.
	rules = append(rules, v2RayNCustomRoutingRule{
		Port:        "0-65535",
		OutboundTag: "proxy",
		Enabled:     true,
		Remarks:     "RouteGate fallback: VPN",
	})
	return rules, true
}

// The following mechanically map a resolved RoutingProfile rule to the
// action/domain/IP vocabulary shared by mechanical client routing adapters.
// They contain no routing-policy decisions of their own.

func routingRuleOutboundTag(action string) string {
	switch strings.TrimSpace(action) {
	case RoutingActionDirect:
		return "direct"
	case RoutingActionVPN:
		return "proxy"
	case RoutingActionBlock:
		return "block"
	default:
		return ""
	}
}

func routingRuleDomainConditions(rule RoutingProfileRule) []string {
	conditions := make([]string, 0, len(rule.Domains)+len(rule.DomainSuffixes)+len(rule.DomainKeywords)+len(rule.GeoSites))
	for _, value := range cleanStrings(rule.Domains) {
		conditions = append(conditions, "full:"+strings.TrimPrefix(value, "full:"))
	}
	for _, value := range cleanStrings(rule.DomainSuffixes) {
		conditions = append(conditions, "domain:"+strings.TrimPrefix(value, "domain:"))
	}
	for _, value := range cleanStrings(rule.DomainKeywords) {
		conditions = append(conditions, "keyword:"+strings.TrimPrefix(value, "keyword:"))
	}
	for _, value := range cleanStrings(rule.GeoSites) {
		conditions = append(conditions, "geosite:"+strings.TrimPrefix(value, "geosite:"))
	}
	return conditions
}

func routingRuleIPConditions(rule RoutingProfileRule) []string {
	conditions := append([]string(nil), cleanStrings(rule.IPCIDRs)...)
	for _, value := range cleanStrings(rule.GeoIPs) {
		conditions = append(conditions, "geoip:"+strings.TrimPrefix(value, "geoip:"))
	}
	return conditions
}
