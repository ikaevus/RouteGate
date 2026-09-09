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
		outboundTag := v2RayTunOutboundTag(rule.Action)
		if outboundTag == "" {
			continue
		}
		domains := v2RayTunDomainConditions(rule)
		ips := v2RayTunIPConditions(rule)
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
