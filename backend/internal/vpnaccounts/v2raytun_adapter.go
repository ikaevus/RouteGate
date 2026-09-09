package vpnaccounts

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

type v2RayTunRouting struct {
	DomainStrategy string                  `json:"domainStrategy"`
	DomainMatcher  string                  `json:"domainMatcher"`
	ID             string                  `json:"id,omitempty"`
	Name           string                  `json:"name"`
	Balancers      []any                   `json:"balancers"`
	Rules          []v2RayTunRoutingRule   `json:"rules"`
}

type v2RayTunRoutingRule struct {
	Type        string   `json:"type"`
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"__name__,omitempty"`
	DomainMatch string   `json:"domainMatcher,omitempty"`
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
	OutboundTag string   `json:"outboundTag"`
}

func renderV2RayTunRoutingHeader(profile *RoutingProfile) (string, bool, error) {
	if profile == nil || len(profile.Rules) == 0 {
		return "", false, nil
	}

	routing := v2RayTunRouting{
		DomainStrategy: "AsIs",
		DomainMatcher:  "hybrid",
		ID:             strings.TrimSpace(profile.ID),
		Name:           strings.TrimSpace(profile.Name),
		Balancers:      []any{},
		Rules:          make([]v2RayTunRoutingRule, 0, len(profile.Rules)),
	}
	if routing.Name == "" {
		routing.Name = "RouteGate"
	}

	for _, rule := range profile.Rules {
		outboundTag := v2RayTunOutboundTag(rule.Action)
		if outboundTag == "" {
			continue
		}
		rendered := v2RayTunRoutingRule{
			Type:        "field",
			ID:          strings.TrimSpace(rule.ID),
			Name:        strings.TrimSpace(rule.Name),
			DomainMatch: "hybrid",
			OutboundTag: outboundTag,
			Domain:      v2RayTunDomainConditions(rule),
			IP:          v2RayTunIPConditions(rule),
		}
		if len(rendered.Domain) == 0 {
			rendered.DomainMatch = ""
		}
		if len(rendered.Domain) == 0 && len(rendered.IP) == 0 {
			continue
		}
		routing.Rules = append(routing.Rules, rendered)
	}
	if len(routing.Rules) == 0 {
		return "", false, nil
	}

	encoded, err := json.Marshal(routing)
	if err != nil {
		return "", false, err
	}
	return base64.StdEncoding.EncodeToString(encoded), true, nil
}

func v2RayTunOutboundTag(action string) string {
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

func v2RayTunDomainConditions(rule RoutingProfileRule) []string {
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

func v2RayTunIPConditions(rule RoutingProfileRule) []string {
	conditions := append([]string(nil), cleanStrings(rule.IPCIDRs)...)
	for _, value := range cleanStrings(rule.GeoIPs) {
		conditions = append(conditions, "geoip:"+strings.TrimPrefix(value, "geoip:"))
	}
	return conditions
}
