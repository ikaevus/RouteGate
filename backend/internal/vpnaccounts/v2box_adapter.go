package vpnaccounts

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

type v2BoxRoute struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Tag       string   `json:"tag"`
	MatchMode string   `json:"matchMode"`
	ListIP    []string `json:"listIP"`
	Remark    string   `json:"remark"`
	IsEnable  bool     `json:"isEnable"`
	List      []string `json:"list"`
}

func renderV2BoxRoutingDeepLink(profile *RoutingProfile) (string, bool, error) {
	routes := renderV2BoxRoutes(profile)
	if len(routes) == 0 {
		return "", false, nil
	}
	encoded, err := json.Marshal(routes)
	if err != nil {
		return "", false, err
	}
	return "v2box://routes?multi=" + base64.StdEncoding.EncodeToString(encoded), true, nil
}

func renderV2BoxRoutes(profile *RoutingProfile) []v2BoxRoute {
	if profile == nil || len(profile.Rules) == 0 {
		return nil
	}

	routes := make([]v2BoxRoute, 0, len(profile.Rules)*3)
	for index, rule := range profile.Rules {
		tag := v2RayTunOutboundTag(rule.Action)
		if tag == "" {
			continue
		}
		baseName := v2BoxRouteName(rule, index)
		remark := strings.TrimSpace(rule.Name)
		if remark == "" {
			remark = "RouteGate rule"
		}

		if values := cleanStrings(rule.DomainKeywords); len(values) > 0 {
			routes = append(routes, newV2BoxDomainRoute(baseName+".keyword", tag, "keyword", remark, values))
		}
		if values := cleanStrings(rule.DomainSuffixes); len(values) > 0 {
			patterns := make([]string, 0, len(values))
			for _, value := range values {
				value = strings.TrimPrefix(value, "domain:")
				patterns = append(patterns, "regexp:"+regexp.QuoteMeta(strings.TrimPrefix(value, "."))+"$")
			}
			routes = append(routes, newV2BoxDomainRoute(baseName+".suffix", tag, "regexp", remark, patterns))
		}
		if values := cleanStrings(rule.Domains); len(values) > 0 {
			domains := make([]string, 0, len(values))
			for _, value := range values {
				domains = append(domains, strings.TrimPrefix(value, "full:"))
			}
			routes = append(routes, newV2BoxDomainRoute(baseName+".domain", tag, "full", remark, domains))
		}
		if values := cleanStrings(rule.GeoSites); len(values) > 0 {
			geosites := make([]string, 0, len(values))
			for _, value := range values {
				geosites = append(geosites, "geosite:"+strings.TrimPrefix(value, "geosite:"))
			}
			routes = append(routes, newV2BoxDomainRoute(baseName+".geosite", tag, "full", remark, geosites))
		}

		ipValues := append([]string(nil), cleanStrings(rule.IPCIDRs)...)
		for _, value := range cleanStrings(rule.GeoIPs) {
			geo := strings.TrimPrefix(value, "geoip:")
			if strings.EqualFold(geo, "private") {
				ipValues = append(ipValues, "geoip:private")
			} else {
				ipValues = append(ipValues, "geoip:"+strings.ToUpper(geo))
			}
		}
		if len(ipValues) > 0 {
			routes = append(routes, v2BoxRoute{
				Name: baseName + ".ip", Type: "IP", Tag: tag, MatchMode: "full",
				ListIP: ipValues, Remark: remark, IsEnable: true, List: []string{},
			})
		}
	}
	return routes
}

func newV2BoxDomainRoute(name, tag, matchMode, remark string, values []string) v2BoxRoute {
	return v2BoxRoute{
		Name: name, Type: "Domain", Tag: tag, MatchMode: matchMode,
		ListIP: []string{}, Remark: remark, IsEnable: true, List: values,
	}
}

func v2BoxRouteName(rule RoutingProfileRule, index int) string {
	id := strings.TrimSpace(rule.ID)
	if id == "" {
		id = strings.TrimSpace(rule.Name)
	}
	if id == "" {
		id = "rule"
	}
	id = strings.NewReplacer(" ", "-", "/", "-", "\\", "-").Replace(id)
	return "routegate." + id + "." + strconv.Itoa(index+1)
}
